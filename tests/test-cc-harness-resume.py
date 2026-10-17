#!/usr/bin/env python3
"""Offline regression tests: real wrapper, launcher and resolver; fake Claude.

usage: CC_ROUTER_PKG=<pkg-dir> python3 tests/test-cc-harness-resume.py [unittest args]
       tests/run   # builds the package layout and runs every suite

The package layout is bin/cc-harness-agents next to bin/cc-harness-resume, as
`make build` leaves them: launch asks its sibling resolver for a session's
model, and the resolver its sibling binary for the route. Routing is real: the
launcher is tests/testdriver (the production code, trusting only the test
gateway's CA), and the test gateway plays the remote route and the local port.
"""
import atexit
import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tempfile
import time
import unittest

ZSH_BIN = os.environ.get('ZSH_BIN') or shutil.which('zsh')  # same knob as test-claude-wrapper.sh
ROOT = Path(__file__).resolve().parents[1]
WRAPPER = ROOT / 'share/claude.zsh'
PKG = Path(os.environ['CC_ROUTER_PKG'])
RESOLVER = PKG / 'bin/cc-harness-resume'
DRIVER = PKG / 'bin/testdriver'
# The resolver has no .py suffix, so import it by location. load_module() would
# do too, but it is gone in Python 3.12.
_spec = importlib.util.spec_from_file_location(
    'resume', RESOLVER, loader=importlib.machinery.SourceFileLoader('resume', str(RESOLVER)))
resolver = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(resolver)
A = '11111111-1111-4111-8111-111111111111'
B = '22222222-2222-4222-8222-222222222222'
# Every model the packaged table names, plus the user-table and retained ids
# the cases restore: the default catalog serves them all.
SERVED = sorted({m for line in (ROOT / 'share/models.tsv').read_text().splitlines()
                 if line and not line.startswith('#') for m in line.split('\t')[1:6]}
                | {'nova-1', 'gpt-5.6-sol', 'gpt-5.6-luna'})


def start_gateway():
    """The suite's one test gateway, for the life of the process."""
    root = Path(tempfile.mkdtemp(prefix='test-cc-harness-resume.'))
    proc = subprocess.Popen([str(PKG / 'bin/gateway'), '-root', str(root)], stdin=subprocess.PIPE)
    atexit.register(lambda: (proc.stdin.close(), proc.wait(), shutil.rmtree(root, ignore_errors=True)))
    env_file = root / 'gateway.env'
    for _ in range(100):
        if env_file.exists():
            break
        time.sleep(0.05)
    values = dict(line.split('=', 1) for line in env_file.read_text().splitlines())
    return root, {k: shlex.split(v)[0] for k, v in values.items()}


GATEWAY_ROOT, GATEWAY = start_gateway()


class Fixture(unittest.TestCase):
    """A project dir, Claude's config dir, the package's launcher and a case
    on the test gateway."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name).resolve()
        self.cwd = self.base / 'project space'
        self.cwd.mkdir()
        self.config = self.base / 'config'
        self.project = self.config / 'projects' / resolver.project_key(self.cwd)
        self.project.mkdir(parents=True)
        self.bin = self.base / 'bin'
        self.bin.mkdir()
        (self.bin / 'cc-harness-resume').symlink_to(RESOLVER)
        (self.bin / 'cc-harness-agents').symlink_to(DRIVER)
        # Claude is fake: it reports its argv and what routing it was handed.
        self.exe('claude', '''#!/usr/bin/env python3
import os,json,sys
print(json.dumps({'args':sys.argv[1:], 'agent':os.getenv('CC_ROUTER_AGENT'), 'route':os.getenv('CLIPROXY_ROUTE'), 'model':os.getenv('ANTHROPIC_MODEL'), 'pid':os.getpid(), 'pins':{k:v for k,v in os.environ.items() if k.startswith('CC_HARNESS_MODEL_')}}))
''')
        self.case = re.sub(r'[^A-Za-z0-9._]', '_', self.id())
        self.gateway = GATEWAY_ROOT / self.case / 'gateway'
        self.gateway.mkdir(parents=True)
        self.serve(SERVED)
        # The local route: a token, codex credentials and a fresh
        # prepare-local window, with the gateway's accepting port as the proxy.
        proxy = self.base / '.cli-proxy-api'
        proxy.mkdir()
        (proxy / 'client.key').write_text('local-token\n')
        (proxy / 'codex-test.json').write_text('{"access_token":"c","refresh_token":"r"}\n')
        marker = self.base / '.cache/cliproxy-auth/local-ready.json'
        marker.parent.mkdir(parents=True)
        marker.write_text(json.dumps({
            'version': 1, 'prepared_at': '2026-01-01T00:00:00Z',
            'expires_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(time.time() + 3600)),
            'ssh_host': 'gateway-host', 'remote_path': '/srv/cliproxyapi/auths',
            'files': [{'name': 'codex-test.json', 'sha256': '0' * 64}]}) + '\n')

    def serve(self, models=None, status=None):
        """What the remote route answers this case: a catalog, or a status."""
        if models is not None:
            (self.gateway / 'body').write_text(json.dumps({'data': [{'id': m} for m in models]}))
        if status is not None:
            (self.gateway / 'status').write_text(str(status))

    def probes(self):
        """Catalog requests the remote route has seen this case."""
        count = self.gateway / 'count'
        return int(count.read_text()) if count.exists() else 0

    def exe(self, name, body):
        p = self.bin / name
        p.write_text(body)
        p.chmod(0o755)

    def session(self, uuid=A, models=('gpt-6-sol',), extra=()):
        p = self.project / f'{uuid}.jsonl'
        records = [{'type':'assistant', 'sessionId':uuid, 'cwd':str(self.cwd), 'message':{'model':m}} for m in models]
        p.write_text(''.join(json.dumps(r)+'\n' for r in [*records, *extra]))
        return p

    def env(self, **extra):
        env = dict(os.environ, PATH=f'{self.bin}:'+os.environ['PATH'], CLAUDE_CONFIG_DIR=str(self.config),
                   HERDR_ENV='1', HOME=str(self.base), PWD=str(self.cwd), CC_ROUTER_CLAUDE=str(self.bin / 'claude'),
                   CC_ROUTER_REMOTE_URL=f"https://127.0.0.1:{GATEWAY['GATEWAY_TRUSTED_PORT']}/c/{self.case}",
                   CC_ROUTER_LOCAL_PORT=GATEWAY['GATEWAY_LIVE_PORT'], CC_ROUTER_TEST_CA_FILE=GATEWAY['GATEWAY_CA'],
                   CLIPROXY_PROFILE='TEST', CLIPROXY_API_KEY_TEST='remote-api-secret',
                   CLIPROXY_CF_ACCESS_TEST_CLIENT_ID='access-id-secret',
                   CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET='access-client-secret')
        for key in list(env):
            if key.startswith(('CC_HARNESS_MODEL_', 'CLIPROXY_ROUTE', 'CC_ROUTER_AGENT', 'XDG_CACHE_HOME')): del env[key]
        env.update(extra)
        return {k: v for k, v in env.items() if v is not None}

    def raw_wrapper(self, *args, **extra):
        return subprocess.run([ZSH_BIN,'-f','-c','source "$1"; shift; claude "$@"','zsh',str(WRAPPER),*args],
                              cwd=self.cwd, env=self.env(**extra), capture_output=True, text=True)

    def run_resolver(self, selector, cwd=None):
        cwd = cwd or self.cwd
        return subprocess.run([str(self.bin/'cc-harness-resume'), selector], cwd=cwd, capture_output=True,
                              text=True, env=dict(os.environ, CLAUDE_CONFIG_DIR=str(self.config), PWD=str(cwd),
                                                  HOME=str(self.base)))

    def run_wrapper(self, *args, **extra):
        result = self.raw_wrapper(*args, **extra)
        self.assertEqual(result.returncode,0,result.stderr)
        return json.loads(result.stdout), result.stderr



class ResumeTests(Fixture):
    """The transcript bridge decides."""

    def test_uuid_restores_model_and_route(self):
        self.session()
        data, _ = self.run_wrapper('--resume', A, 'hello world')
        self.assertEqual(data['agent'],'sol')
        self.assertEqual(data['pins'],{'CC_HARNESS_MODEL_SOL':'gpt-6-sol'})
        self.assertEqual(data['args'],['--model','gpt-6-sol','--resume',A,'hello world'])
        # Once by `serves` before the restore, once by the routed exec.
        self.assertEqual(self.probes(), 2, 'a remote restore asks the route first')

    def test_dotted_agent_name_restores_through_a_shell_identifier(self):
        # A user-table name may carry '.', which no shell identifier can: the
        # wrapper must spell the override variable exactly as the helper reads it.
        table = self.base / '.config/cc-router/models.tsv'
        table.parent.mkdir(parents=True)
        table.write_text('nova.v2\tnova-1\tnova-1\tnova-1\tnova-1\tnova-1\t200000\tnova\t-nova-login\tNova\n')
        self.session(models=('nova-1',))
        data, _ = self.run_wrapper('--resume', A)
        self.assertEqual(data['agent'],'nova.v2')
        self.assertEqual(data['pins'],{'CC_HARNESS_MODEL_NOVA_V2':'nova-1'})

    def test_continue_selects_latest_session_and_pins_uuid(self):
        old = self.session()
        latest = self.session(B, ('grok-4.6',))
        os.utime(old,(1,1));os.utime(latest,(2,2))
        data, _ = self.run_wrapper('-c')
        self.assertEqual(data['agent'],'grok')
        self.assertIn('--resume='+B,data['args'])
        self.assertNotIn('-c',data['args'])

    def test_dropped_kimi_agent_falls_back_to_anthropic(self):
        # The kimi row is gone (subscription ended). A historical kimi session
        # still resumes — on the Anthropic default, with the same notice as any
        # unmapped model, never silently on some other foreign row.
        # resolve-model goes to the real binary next to the resolver, not the
        # setUp fake.
        self.session(B, ('kimi-k3',))
        data, warning = self.run_wrapper('-c')
        self.assertIsNone(data['agent'])
        self.assertIn('no routing profile for model kimi-k3',warning)
        self.assertIn('normal Anthropic',warning)
        self.assertEqual(data['args'][:2], ['--model','default'])

    def test_astra_restores_model_and_route(self):
        # astra is the newest row, so resume has to reach it end to end: the
        # transcript's model maps back to the agent AND is pinned, or the session
        # silently continues on the default Claude model.
        self.session(models=('gpt-6-astra',))
        data, _ = self.run_wrapper('--resume', A, 'hello world')
        self.assertEqual(data['agent'],'astra')
        self.assertEqual(data['pins'],{'CC_HARNESS_MODEL_ASTRA':'gpt-6-astra'})
        self.assertEqual(data['args'],['--model','gpt-6-astra','--resume',A,'hello world'])

    def test_shared_codex_rungs_resolve_to_their_own_rows(self):
        # All four codex rows carry the same Astra/Sol/Terra/Luna ladder, and
        # every rung is also some row's primary. A transcript ending on a rung
        # belongs to the row whose primary it is — an exact primary match
        # outranks a tier match — which is safe because every codex row shares
        # the ladder and the ceiling, so no restore changes the window.
        for model, agent in (('gpt-6-luna','luna'),('gpt-5.6-terra','terra'),('gpt-6-astra','astra')):
            self.session(models=('gpt-6-sol', model))
            self.assertEqual(resolver.resolve(A,self.cwd,self.config)[1:],(agent,model))

    def test_superseded_codex_ids_resume_exactly(self):
        # gpt-5.6-sol/luna left the ladder when gpt-6 took their rungs, but the
        # route still serves them. A session recorded on one resumes on exactly
        # that id in its former row: never refused, never moved to the successor.
        for model, agent in (('gpt-5.6-sol','sol'),('gpt-5.6-luna','luna')):
            self.session(models=(model,))
            data, _ = self.run_wrapper('--resume', A, 'hello world')
            self.assertEqual(data['agent'],agent)
            self.assertEqual(data['pins'],{'CC_HARNESS_MODEL_'+agent.upper():model})
            self.assertEqual(data['args'][:2],['--model',model])

    def test_retired_spark_rung_is_not_silently_remapped(self):
        # gpt-5.3-codex-spark was the codex haiku rung until the route dropped
        # it. A session whose last reply came from it must NOT resume as luna
        # (the rung's successor) under the pretence of an exact restore: the
        # wrapper states it could not restore and pins the Anthropic default.
        self.session(models=('gpt-6-sol','gpt-5.3-codex-spark'))
        data, warning = self.run_wrapper('--resume='+A)
        self.assertIsNone(data['agent'])
        self.assertIn('no routing profile for model gpt-5.3-codex-spark',warning)
        self.assertIn('normal Anthropic',warning)
        self.assertEqual(data['args'][:2], ['--model','default'])

    def test_explicit_tier_flag_on_continue_skips_the_restore(self):
        # `claude -c --sol` — the reported failure — is an explicit choice: no
        # restore runs, nothing is pinned, the helper gets `sol` as asked.
        self.session(models=('gpt-6-astra',))
        data, _ = self.run_wrapper('-c','--sol')
        self.assertEqual(data['agent'],'sol')
        self.assertEqual(data['pins'],{})
        self.assertEqual(data['args'],['-c'])

    def test_last_real_model_ignores_synthetic_and_sidechain(self):
        self.session(models=('gpt-6-astra','gpt-6-sol','<synthetic>'),extra=({'type':'assistant','isSidechain':True,'message':{'model':'grok-4.6'}},))
        self.assertEqual(resolver.resolve(A,self.cwd,self.config)[1: ],('sol','gpt-6-sol'))

    def test_native_latest_does_not_reuse_old_custom_model(self):
        self.session(models=('gpt-6-sol','claude-opus-5'))
        data, _ = self.run_wrapper('-r',A)
        self.assertIsNone(data['agent'])
        self.assertEqual(data['args'],['-r',A])

    # --- a recorded model the gateway no longer serves (Robert, 2026-10-16) ---
    # The table still maps it, but the route dropped it: the session resumes on
    # the Anthropic default with a notice, like an unmapped model. Only that
    # verdict (serves exit 4) falls back; a failing route stays an error.

    def test_unserved_recorded_model_falls_back_to_anthropic(self):
        self.session(models=('gpt-5.6-sol',))
        self.serve([m for m in SERVED if m != 'gpt-5.6-sol'])
        data, warning = self.run_wrapper('--resume', A)
        self.assertIsNone(data['agent'])
        self.assertEqual(data['pins'], {})
        self.assertEqual(data['args'], ['--model', 'default', '--resume', A])
        self.assertIn('no longer serves gpt-5.6-sol', warning)
        self.assertIn('normal Anthropic', warning)

    def test_fallback_keeps_continue_on_the_resolved_session(self):
        # The decision was made for the resolved transcript, so the fallback
        # pins it too: a bare -c would let Claude pick another one.
        self.session(models=('gpt-5.6-sol',))
        self.serve([m for m in SERVED if m != 'gpt-5.6-sol'])
        data, _ = self.run_wrapper('-c')
        self.assertEqual(data['args'], ['--model', 'default', '--resume=' + A])

    def test_route_failure_on_resume_fails_closed(self):
        self.session()
        self.serve(status=403)
        result = self.raw_wrapper('-c')
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, '')
        self.assertIn('Cloudflare Access rejected', result.stderr)
        self.assertNotIn('normal Anthropic', result.stderr)

    def test_unconfigured_route_on_resume_keeps_its_status(self):
        self.session()
        result = self.raw_wrapper('--resume', A, CC_ROUTER_REMOTE_URL=None)
        self.assertEqual((result.returncode, result.stdout), (3, ''))

    def test_explicit_flag_never_falls_back(self):
        # No restore runs, so nothing asks the route first: exec gets the
        # explicit agent and fails there, naming the cause, if it cannot serve.
        self.session(models=('gpt-5.6-sol',))
        self.serve([m for m in SERVED if m != 'gpt-5.6-sol'])
        data, _ = self.run_wrapper('--resume', A, '--sol')
        self.assertEqual(data['agent'], 'sol')
        self.assertEqual(self.probes(), 1, 'only the routed exec probes')

    def test_local_resume_does_not_ask_the_remote_route(self):
        self.session()
        data, _ = self.run_wrapper('--local', '-c')
        self.assertEqual((data['agent'], data['route']), ('sol', 'local'))
        self.assertEqual(self.probes(), 0)

    def test_native_resume_does_not_ask_the_route(self):
        self.session(models=('claude-opus-5',))
        self.run_wrapper('--resume', A)
        self.assertEqual(self.probes(), 0)

    def test_unroutable_foreign_model_pins_the_default(self):
        # A foreign model nothing serves: CC would re-apply the stored id against
        # Anthropic, so the wrapper has to pin the default back.
        self.session(models=('gpt-9.9-unknown',))
        data, warning = self.run_wrapper('--resume='+A)
        self.assertIsNone(data['agent'])
        self.assertIn('normal Anthropic',warning)
        self.assertEqual(data['args'][:2], ['--model','default'])

    def test_indeterminate_leaves_the_model_alone(self):
        # Nothing recorded, and the bare picker: the wrapper cannot tell whether a
        # foreign model was ever involved, so it must not outrank the user's own
        # configured model with `--model default`.
        self.session(models=())
        for argv in [('--resume='+A,), ('--resume',), ('-c',)]:
            data, _ = self.run_wrapper(*argv)
            self.assertIsNone(data['agent'])
            self.assertNotIn('--model', data['args'])

    def test_second_resume_selector_is_refused(self):
        self.session()
        result = self.raw_wrapper('--resume', A, '-c')
        self.assertEqual(result.returncode, 1)
        self.assertIn('more than one resume selector', result.stderr)

    def test_selector_conflict_is_only_refused_when_a_restore_would_run(self):
        # With an explicit --model no restore happens, so there is no argv slot
        # to rewrite and the CLI's own last-wins rule should apply untouched.
        self.session()
        result = self.raw_wrapper('--model', 'opus', '-c', '--resume', A)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('more than one resume selector', result.stderr)

    def test_variadic_and_required_values_are_not_resume(self):
        self.session()
        # A required value spelled like a flag is data; a variadic list ends at
        # the next option token, so the `--resume=` behind it still resolves.
        data, _ = self.run_wrapper('--fallback-model','--continue','--tools','Bash','Edit','--resume='+A)
        self.assertEqual(data['agent'],'sol')
        self.assertEqual(data['args'][:2],['--model','gpt-6-sol'])
        self.assertIn('Edit', data['args'])

    def test_unknown_id_of_a_single_agent_family_still_routes(self):
        # grok is the only row claiming the `grok-` prefix, so a newer id routes.
        self.session(models=('grok-9.9-experimental',))
        self.assertEqual(resolver.resolve(A,self.cwd,self.config)[1:],('grok','grok-9.9-experimental'))

    def test_ambiguous_family_is_not_guessed(self):
        # astra, sol, terra and luna all claim `gpt-`, so an unknown gpt id stays
        # unroutable instead of being handed whichever tier matched first.
        self.session(models=('gpt-9.9-unknown',))
        with self.assertRaises(resolver.Unroutable):
            resolver.resolve(A,self.cwd,self.config)

    def test_explicit_model_wins(self):
        self.session()
        data, _ = self.run_wrapper('--resume',A,'--grok')
        self.assertEqual(data['agent'],'grok')
        self.assertEqual(data['pins'],{})
        data, _ = self.run_wrapper('--resume',A,'--model','opus')
        self.assertIsNone(data['agent'])
        # The native shorthands are the reason the restore is escapable at all
        # without typing `--model <alias>`: each must stand the restore down and
        # reach the CLI as a real --model.
        for flag, alias in (('--opus','opus'), ('--fable','fable'),
                            ('--sonnet','sonnet'), ('--haiku','haiku')):
            data, _ = self.run_wrapper('--resume',A,flag)
            self.assertIsNone(data['agent'], flag)
            self.assertEqual(data['pins'], {}, flag)
            self.assertIn('--model', data['args'])
            self.assertEqual(data['args'][data['args'].index('--model')+1], alias)
            self.assertNotIn(flag, data['args'])

    def test_local_can_be_selected_on_resume(self):
        self.session()
        data, _ = self.run_wrapper('--local','-c')
        self.assertEqual(data['route'],'local')

    def test_grok_backend_alias(self):
        self.session(models=('grok-4.6-build',))
        self.assertEqual(resolver.resolve(A,self.cwd,self.config)[1:],('grok','grok-4.6'))

    def test_prompt_values_and_end_of_options_are_not_resume(self):
        data, _ = self.run_wrapper('--append-system-prompt','--continue','--','--resume',A)
        self.assertIsNone(data['agent'])
        self.assertEqual(data['args'],['--append-system-prompt','--continue','--','--resume',A])

    def test_duplicate_uuid_falls_back(self):
        original = self.session()
        other = self.config/'projects'/'other'
        other.mkdir();(other/original.name).write_text(original.read_text())
        data, warning = self.run_wrapper('--resume',A)
        self.assertIsNone(data['agent'])
        self.assertIn('ambiguous across 2 projects',warning)
        self.assertIn('explicit model flag',warning)

    def test_missing_transcript_is_silent(self):
        # No transcript anywhere: Claude reports "No conversation found" itself,
        # so a resolver line blaming a model flag would only mislead.
        data, warning = self.run_wrapper('--resume', B)
        self.assertEqual(data['args'], ['--resume', B])
        self.assertEqual(warning, '')
        result = self.run_resolver(B)
        self.assertEqual((result.returncode, result.stdout, result.stderr), (1, '', ''))

    def test_nothing_to_continue_and_never_answered_are_silent(self):
        # Empty project folder, then a session with no assistant reply yet: both
        # are ordinary resumes Claude handles on its own.
        data, warning = self.run_wrapper('-c')
        self.assertEqual((data['args'], warning), (['-c'], ''))
        self.session(models=('<synthetic>',))
        for argv in [('-c',), ('--resume', A)]:
            data, warning = self.run_wrapper(*argv)
            self.assertEqual((data['args'], warning), (list(argv), ''))

    def test_continue_without_any_answer_is_silent(self):
        # Transcripts that never recorded a cwd (no user/assistant row yet) are
        # "never answered", not "recorded somewhere else".
        (self.project/f'{A}.jsonl').write_text(json.dumps({'type':'summary','sessionId':A})+'\n')
        result = self.run_resolver('continue')
        self.assertEqual((result.returncode, result.stderr), (1, ''))

    def test_continue_explains_when_key_is_uncertain(self):
        # A symlinked shell cwd may be keyed by Claude under its logical path,
        # so an empty folder under the resolved key proves nothing.
        link = self.base/'link'
        link.symlink_to(self.cwd)
        result = self.run_resolver('continue', cwd=link)
        self.assertEqual(result.returncode, 1)
        self.assertIn('this exact directory', result.stderr)

    def test_continue_without_model_explains_when_newer_transcript_differs(self):
        # The cwd-matched session has no model, so nothing is pinned and Claude
        # would continue the newer transcript recorded elsewhere, unrestored.
        mine = self.session(models=('<synthetic>',))
        other = self.session(B)
        other.write_text(other.read_text().replace(str(self.cwd), str(self.base)))
        os.utime(mine,(1,1));os.utime(other,(2,2))
        result = self.run_resolver('continue')
        self.assertEqual(result.returncode, 1)
        self.assertIn('records another directory', result.stderr)

    def test_blank_model_reads_as_never_answered(self):
        self.session(models=('',))
        result = self.run_resolver(A)
        self.assertEqual((result.returncode, result.stderr), (1, ''))

    def test_leading_cwdless_row_does_not_hide_a_foreign_model(self):
        p = self.project/f'{A}.jsonl'
        p.write_text(json.dumps({'type':'user','sessionId':A})+'\n'+self.session().read_text())
        data, _ = self.run_wrapper('-c')
        self.assertEqual(data['agent'], 'sol')

    def test_newer_cwdless_transcript_keeps_continue_silent(self):
        mine = self.session(models=('<synthetic>',))
        newer = self.project/f'{B}.jsonl'
        newer.write_text(json.dumps({'type':'summary','sessionId':B})+'\n')
        os.utime(mine,(1,1));os.utime(newer,(2,2))
        result = self.run_resolver('continue')
        self.assertEqual((result.returncode, result.stderr), (1, ''))

    def test_uppercase_selector_still_restores(self):
        self.session()
        if not (self.project/f'{A.upper()}.jsonl').exists():
            self.skipTest('case-sensitive filesystem')
        self.assertEqual(resolver.resolve(A.upper(), self.cwd, self.config)[1], 'sol')

    def test_continue_with_foreign_cwd_transcripts_explains(self):
        # Transcripts exist but none records this cwd: Claude may still continue
        # one of them unrestored, so the hint is real here.
        p = self.session()
        p.write_text(p.read_text().replace(str(self.cwd), str(self.base)))
        _, warning = self.run_wrapper('-c')
        self.assertIn('this exact directory', warning)

    def test_cross_directory_uuid_and_truncated_tail(self):
        p = self.session()
        with p.open('a') as f: f.write('{"interrupted":')
        self.assertEqual(resolver.resolve(A,self.base,self.config)[1],'sol')


class HelperFailureTests(unittest.TestCase):
    """The resolver against a BROKEN cc-harness-agents.

    Copied, not symlinked: the resolver finds the helper as its own sibling via
    `Path(__file__).resolve()`, so only a real copy in a scratch dir lets a fake
    helper stand in for the real one.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name).resolve()
        self.cwd = self.base / 'work'
        self.cwd.mkdir()
        self.config = self.base / 'config'
        self.project = self.config / 'projects' / resolver.project_key(self.cwd)
        self.project.mkdir(parents=True)
        (self.project / f'{A}.jsonl').write_text(json.dumps(
            {'type': 'assistant', 'sessionId': A, 'cwd': str(self.cwd),
             'message': {'model': 'gpt-6-sol'}}) + '\n')
        self.bin = self.base / 'bin'
        self.bin.mkdir()
        self.resolver = self.bin / 'cc-harness-resume'
        self.resolver.write_text(RESOLVER.read_text())
        self.resolver.chmod(0o755)

    def helper(self, body=None, mode=0o755):
        h = self.bin / 'cc-harness-agents'
        if body is not None:
            h.write_text(body)
            h.chmod(mode)
        return h

    def run_resolver(self):
        return subprocess.run([str(self.resolver), A], cwd=self.cwd, capture_output=True, text=True,
                              env=dict(os.environ, CLAUDE_CONFIG_DIR=str(self.config), HOME=str(self.base)))

    def test_missing_helper_is_unroutable_not_indeterminate(self):
        # A foreign model IS on record. Reporting "cannot tell" (exit 1) would
        # leave CC to re-apply that stored id against Anthropic, so this has to
        # stay exit 3 — but the reason must reach stderr.
        result = self.run_resolver()
        self.assertEqual(result.returncode, 3)
        self.assertIn('cannot reach cc-harness-agents', result.stderr)

    def test_non_executable_helper_names_the_cause(self):
        self.helper('#!/bin/bash\nexit 0\n', mode=0o644)
        result = self.run_resolver()
        self.assertEqual(result.returncode, 3)
        self.assertIn('cannot reach cc-harness-agents', result.stderr)

    def test_unusable_helper_output_is_not_treated_as_a_route(self):
        self.helper('#!/bin/bash\necho "garbage without a tab"\n')
        result = self.run_resolver()
        self.assertEqual(result.returncode, 3)
        self.assertIn('unusable routing profile', result.stderr)

    def test_helper_refusal_is_relayed_verbatim(self):
        self.helper('#!/bin/bash\necho "cc-harness-agents: no routing profile for model $2" >&2\nexit 1\n')
        result = self.run_resolver()
        self.assertEqual(result.returncode, 3)
        self.assertIn('no routing profile', result.stderr)

    def test_a_transcript_model_is_never_a_path(self):
        # The id is exported into the session and reaches the gateway inside a
        # URL path, so traversal is never restored — and is reported, unlike a
        # session that simply never recorded a model.
        (self.project / f'{A}.jsonl').write_text(json.dumps(
            {'type': 'assistant', 'sessionId': A, 'cwd': str(self.cwd),
             'message': {'model': 'grok-4.6/../../v1/anything'}}) + '\n')
        self.helper('#!/bin/bash\nprintf "grok\\t%s\\n" "$2"\n')
        result = self.run_resolver()
        self.assertEqual(result.returncode, 1)
        self.assertIn('no recorded model', result.stderr)


if __name__ == '__main__':
    unittest.main()
