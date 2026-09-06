# Restore routed Claude sessions through an executable shim

## Goal

Make `claude --resume <session-id>` and `claude -c` restore both the last active
model and the complete routing environment, including when a session is restored
directly by herdr.

The solution must work for native Claude models as well as models routed through
CLIProxyAPI, without requiring users to remember the original `--grok`, `--kimi`,
`--sol`, `--terra`, `--luna`, or `--local` flags.

## Problem

The current implementation uses an interactive zsh function. That function owns
flag parsing and delegates foreign-model routing to `cc-harness-agents`, but it is
not involved when another process launches the executable directly.

**Correction (2026-09-05, ROUTER-COORDINATION-20260905-A):** an earlier revision
of this spec claimed herdr's automatic restore bypasses that shell function. That
is wrong for the verified herdr implementation (v0.8.2 sources,
`src/app/agent_resume.rs`; 0.8.2 deployed everywhere since 2026-09-06): herdr
starts a pane **shell** and sends
`shell_command_from_argv(plan.argv)` — `claude --resume <uuid>` — into it, so a
fresh interactive zsh loads the current wrapper. The shim remains valuable for
callers that `exec` the binary directly or do not source that function
(non-interactive contexts, other shells, process managers), but its necessity
must not be argued from the herdr claim.

For a caller that does launch the executable directly:

- the zsh function is bypassed;
- Claude Code can restore the model recorded in the session transcript;
- Claude Code cannot restore the process environment that selected CLIProxyAPI;
- the proxy URL, credentials, headers, provider selectors, tier mappings and
  context ceiling are therefore missing;
- applying the current routing profile naively sets `ANTHROPIC_MODEL` again and
  can overwrite a later in-session `/model` switch with the profile's original
  primary model.

Some herdr panes also contain `launch_argv`, but that describes the original
launch rather than the last active model, is not consistently present, and is
not used by herdr's automatic Claude restore path.

**Interim bridge:** dotfiles PR #24 (`.scripts/cc-harness-resume` + wrapper
integration, merged 2026-09-05 and live-tested) restores the last real assistant model from the session's JSONL,
ignores synthetic/sidechain messages, pins `-c` to its selected UUID and
reapplies the routing recipe. Known limitations there: historical local/remote
route, custom tiers, context overrides, effort and an unserved `/model` choice
are not reconstructed. This spec's durable sidecar + shim design is what
eventually replaces that bridge; the bridge is transcript-parsing by necessity,
not the long-term mechanism.

## Design

### Executable shim

Ship a real `claude` executable shim and install it before the native Claude Code
launcher on `PATH`.

The shim must:

- call the native launcher through a stable, explicitly configured path;
- avoid name-based recursion;
- replace itself with the helper or native launcher via `exec`, so herdr still
  sees the real Claude process as the pane's foreground process;
- preserve normal Claude behavior when no cc-router flag or restorable routing
  state is present;
- support non-interactive callers and process managers, not only zsh.

The existing shell function may remain as a thin interactive layer for terminal
capture and the resume-history hint, but model selection, restore detection and
routing must live below it in the executable path.

### Session routing sidecar

Maintain a non-secret sidecar for each observed Claude session, for example:

```text
~/.cache/cc-router/sessions/<session-id>.json
```

Suggested shape:

```json
{
  "session_id": "45ac6aba-bef2-4d27-b0bf-2e85c9fba620",
  "cwd": "/Users/example/project",
  "transcript_path": "/Users/example/.claude/projects/.../session.jsonl",
  "profile": "grok",
  "route": "remote",
  "model": "grok-4.5",
  "updated_at": 1786435200
}
```

Populate it from the Claude Code statusline input, which exposes the session ID,
workspace, transcript path, `model.id`, and `model.display_name`. Export a
non-secret profile marker such as `CC_ROUTER_PROFILE=sol` from the routing helper
so the statusline does not need to infer the active profile during normal use.

Requirements:

- never store credentials, headers, prompts or transcript content;
- create the cache directory with mode `0700` and files with mode `0600`;
- write atomically using a temporary file plus rename;
- do no network work from the statusline;
- validate session IDs, model IDs, routes and profile names before consuming
  them as arguments;
- treat the transcript path as metadata only; do not parse JSONL as the primary
  restore mechanism;
- provide bounded cleanup for stale sidecars.

### Model-to-profile resolution

Keep the model table and all reverse lookup logic in `cc-harness-agents` or its
extracted cc-router equivalent. Do not duplicate provider/model tables in the
shim, statusline or shell configuration.

Resolution order for existing sessions that predate `CC_ROUTER_PROFILE`:

1. use a valid cached profile marker;
2. prefer an exact match against a profile's primary model;
3. otherwise accept a unique matching tier/provider recipe;
4. use a documented canonical profile only when all matching recipes are
   equivalent;
5. when matches are ambiguous and materially different, or the model is unknown,
   launch the **Anthropic default with a concise notice** naming what could not
   be mapped. Never guess foreign routing — but do not fail closed either:
   Robert explicitly accepts the Anthropic fallback for the resume-mapping case
   (2026-09-05). This does not authorize insecure TLS, automatic remote/local
   failover, or hiding the failure of a *positively selected* proxy route —
   an explicit `--grok`/`--sol`/`--local` that cannot be honored is still an
   error, not a fallback.

### Explicit `--resume`

For `claude --resume <session-id>`:

- explicit user routing/model arguments always win;
- native sessions are passed to Claude Code without an injected model, allowing
  Claude's own session-model restoration to operate;
- routed sessions restore the cached profile and route, then invoke the native
  launcher with the cached exact model as an explicit `--model <model-id>`;
- the routing helper still supplies the proxy URL, authentication, headers,
  provider cleanup, tier defaults, subagent model and context ceiling;
- missing, stale or invalid cache data must not result in guessed routing — the
  session launches on the Anthropic default with a concise notice instead.

### `-c` / `--continue`

For `claude -c` and `claude --continue`:

1. find cache entries whose `cwd` exactly matches the current directory;
2. select the uniquely newest valid entry;
3. rewrite the invocation internally to `--resume <session-id>` so the chosen
   session and routing profile cannot diverge;
4. apply the same native/routed restore behavior as explicit `--resume`;
5. if no unique entry exists, pass the original continue invocation through
   unchanged and emit a concise diagnostic rather than guessing.

Do not select a session globally or by prefix-related directories. Worktrees that
share a repository must remain distinct.

### Precedence

Use this precedence, highest first:

1. explicit foreign-model selector;
2. explicit `--model`;
3. explicit route selector such as `--local` combined with a foreign profile;
4. valid session sidecar;
5. Claude Code's native resume behavior;
6. unchanged fail-safe passthrough.

Conflicting explicit arguments must remain an error rather than becoming
last-one-wins behavior.

## Security and reliability constraints

- Secrets never appear in argv, diagnostics, cache files or shell history.
- Preserve the existing fail-closed TLS behavior.
- Preserve the rule that remote and local proxies do not automatically fail over
  to each other while they can share refresh credentials.
- A cached local route must still pass the local-route readiness gate.
- Cache values are data, never shell fragments; invoke all commands as argv.
- An attacker-controlled `PATH` entry must not replace the verified native
  launcher after resolution.
- Unknown profiles and models must produce actionable diagnostics without
  printing credential values.
- The shim must preserve the native launcher's exit status and signal behavior.

## Acceptance criteria

- [ ] A native Claude session resumed by UUID keeps its last active native model.
- [ ] A routed session resumed by UUID restores its proxy route and exact last
      active model.
- [ ] A routed session switched with `/model` resumes on the switched model, not
      the profile's original primary model.
- [ ] A restore invoked by directly executing `claude --resume <uuid>` (no shell
      function sourced — the shim path) succeeds. herdr itself reaches the
      wrapper through a fresh pane shell on the verified 0.8.2 implementation;
      a herdr restore must succeed on that path too, but it does not prove the
      shim.
- [ ] `claude -c` restores the same session, profile, route and model for an exact
      working directory.
- [ ] Two worktrees of the same repository cannot cross-select each other's
      routing state.
- [ ] Explicit model/profile arguments override cached state.
- [ ] Missing or ambiguous sidecars do not trigger guessed foreign routing —
      they launch the Anthropic default with a concise notice.
- [ ] A cached `--local` route cannot bypass the existing local readiness gate.
- [ ] Native `claude`, `claude --resume`, and `claude -c` behavior remains
      unchanged when no routed state exists.
- [ ] The statusline performs only local, bounded, atomic I/O.
- [ ] The final installed process tree contains no lingering shim process.
- [ ] Installation puts the shim ahead of the native launcher without replacing
      or modifying the native Claude Code installation.
- [ ] Documentation explains precedence, cache location, troubleshooting and
      how to disable automatic restoration.

## Test matrix

Cover at least:

| Invocation | Cached state | Expected result |
|---|---|---|
| `claude` | none | Native launch unchanged |
| `claude --resume ID` | native Opus | Native resume, no injected foreign route |
| `claude --resume ID` | Grok remote | Grok route plus exact cached model |
| `claude --resume ID` | Kimi local | Local gate plus exact cached model |
| `claude --resume ID` | Sol profile, Terra model | Codex route with Terra as active model |
| `claude --resume ID --grok` | cached Sol | Explicit Grok wins |
| `claude --resume ID --model opus` | cached Grok | Explicit native model wins without stale routing |
| `claude -c` | one exact-CWD entry | Rewritten to matching explicit resume |
| `claude -c` | ambiguous exact-CWD entries | Safe passthrough with diagnostic |
| `claude -c` | only parent-directory entry | No match |
| direct executable launch, no shell function sourced | routed sidecar | Routing restored via the shim |
| herdr pane restore (fresh shell, 0.8.2 path) | routed sidecar | Wrapper loads; same restore result |
| malformed or wrong-mode sidecar | any | Ignored safely; Anthropic default + notice |
| unknown model/profile in resume mapping | any | Anthropic default + concise notice, no credential disclosure |
| unknown model/profile as explicit selector | any | Actionable failure, no credential disclosure |

Tests must use fake launchers and helpers and assert argv and environment variable
names without recording secret values.

## Migration from the dotfiles implementation

Source behavior currently lives in the private dotfiles repository:

- `.scripts/cc-harness-agents`
- `.scripts/cc-harness-resume` (interim resume bridge, dotfiles PR #24)
- `.zsh/functions/claude.zsh` `claude()` (moved out of `.zshrc`)
- `.claude/statusline.sh`
- `scripts/test-cc-harness-agents.sh`
- `scripts/test-cc-harness-resume.py` (offline resume regression tests)

Extract the generic implementation here first. Keep the dotfiles integration as a
small installer/configuration consumer rather than maintaining a second routing
implementation.

For sessions already running when the feature is installed, allow the statusline
to bootstrap a sidecar from `model.id`. Prefer the active profile marker when it
exists; otherwise use the helper's validated reverse mapping. Do not scan or parse
all Claude transcripts during installation.

## Verification

After automated tests pass, verify manually with:

1. a native Claude session that switches models before exit;
2. one Grok or Kimi session restored by explicit UUID;
3. one Codex session that switches between Sol and Terra;
4. `-c` from a repository root and from a worktree;
5. a complete herdr server restart followed by automatic pane restoration.

Confirm the model shown by Claude Code and make one harmless request after each
restore to prove that both model selection and provider routing are active.
