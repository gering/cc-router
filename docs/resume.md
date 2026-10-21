# Resuming routed sessions

Runbook for restoring a session's routing when it is resumed. Claude Code
restores a session's model by itself, but not the process environment that
pointed it at CLIProxyAPI — so without this, `claude -c` and
`claude --resume <UUID>` came back on the Anthropic default (or sent a foreign
model id to Anthropic) no matter which model the conversation had been running
on.

## How it works

```
claude -c / --resume <UUID>
  │  zsh wrapper (share/claude.zsh)  or  the executable shim (bin/shim/claude)
  ▼
cc-harness-agents launch             parses the flags, picks the session,
  │  1. the session's sidecar        decides the route — then execs
  │  2. else its transcript (cc-harness-resume, the interim bridge)
  ▼
claude --model <id> --resume <UUID>  with the agent's routing environment
```

`launch` is the one place that parses `claude`'s command line: the wrapper and
the shim both hand it their argv, and it replaces itself with the native
launcher, routed or not. Continue is **rewritten to the resolved UUID**, so
routing and conversation selection cannot pick different conversations.

**The session sidecar** is the primary source. The statusline feeds every render
to `cc-harness-agents record-session`, which writes one small record per session:

```
${XDG_CACHE_HOME:-~/.cache}/cc-router/sessions/<session-id>.json
{"session_id":"…","cwd":"/path/started/in","transcript_path":"…",
 "agent":"sol","route":"remote","model":"gpt-5.6-terra","updated_at":1786435200,
 "rendered_at":1786435200123}
```

`agent` is `CC_ROUTER_AGENT`, which `exec` exports (non-secret, the row name);
`route` is `CLIPROXY_ROUTE`; `model` is the statusline's `model.id`, so a
`/model` switch inside the session is what the record holds. A session started
before the marker existed has a route but no agent, and resolves through the
table's own mapping (`resolve-model`). A native session records only its model.

`rendered_at` orders the writers. The statusline runs `record-session`
detached on every render, so a slow writer of an older render can still be
running when a newer one has written — after a `/model` switch, the old model
would win if the session exits before the next render. The statusline stamps
each render (unix ms, `--rendered-at`) *before* it detaches, and
`record-session` replaces a record only with a render that is not older,
reading and comparing under an exclusive lock on the sessions directory. The
render time is the wall clock rather than a counter from the JSON:
`cost.total_duration_ms` carries over a resume only when the transcript holds
Claude Code's cost state, which it writes on a clean exit — after a crash it
starts again at zero. A stored stamp ahead of the clock (the clock stepped
back) orders nothing. An unstamped writer always writes and keeps the stored
stamp, and a record without `rendered_at` counts as older than any stamped
render, so records written before the stamp existed restore and update as
before.

The record never holds credentials, headers, prompts or transcript text. The
directory is `0700`, files are `0600`, written through a temp file and a rename;
`record-session` reads stdin and the session's own record, never the network,
waits at most a second for the directory lock, and prunes records idle
for 30 days whenever a new session is first recorded; a record that old is
never restored, pruned or not. On read, a record that is
not ours (another owner, group/other bits, a symlink), not valid JSON, carries
an unknown field, or fails the id checks is **ignored with a notice** — the
resume then proceeds as if it had none.

**The transcript bridge** (`cc-harness-resume`, source: `cmd/cc-harness-resume/`)
covers sessions without a record: it reads the last real main-session assistant
model out of Claude Code's own JSONL transcript. Synthetic messages and
sidechains never select a model, and transcript text is never emitted. It is
interim: once every live session has a record it goes.

**The shim** is for callers that never source a shell function — a process
manager, another shell, anything that `exec`s `claude` directly. It is a POSIX
`sh` file that finds the `cc-harness-agents` next to its real location and
`exec`s `launch`, which `exec`s Claude: the process that started the shim *is*
Claude, so nothing lingers and the exit status and signals are Claude's own.
herdr's own restore types `claude --resume <uuid>` into a fresh pane shell, so it
reaches the wrapper either way.

The native launcher is never looked up on `PATH` — the shim is `claude` there.
It is `CC_ROUTER_CLAUDE`, default `~/.local/bin/claude` (where Claude Code's
native installer puts its launcher); a path that resolves to the shim or to
cc-router itself is refused instead of recursing.

## Precedence

Highest first:

| Situation | Result |
|---|---|
| `--proxy <agent\|model>` / `--local <agent\|model>` given (or a deprecated `--<agent>`) | the choice wins; no restore runs |
| native `--model` given | it wins; no restore runs |
| `--noproxy` given | native, no restore runs; without `--model` the account default is pinned (`--model default`), so a routed session's stored foreign model never reaches Anthropic |
| deprecated `--fable` / `--opus` / `--sonnet` / `--haiku` given | shorthand for `--model <alias>`, with a notice: it wins, no restore runs |
| a bare `--local` given (deprecated) | the restore runs, but on the local route |
| already inside a routed session (`CC_ROUTER_AGENT` set) | no restore — the inherited route stays |
| the session's record: native | left to Claude, which restores its own model |
| the session's record: routed | that agent, route and exact model, announced on stderr |
| no usable record: the transcript names a routable model | that agent + model, remote route |
| record or transcript names a model nothing routes | `--model default` pinned, notice on stderr |
| routable model the gateway no longer serves | **error** naming the model, the agent and both ways forward; nothing launched |
| routable model, the route fails (403/401/502/TLS, unconfigured) | error naming the layer, nothing launched |
| nothing determinable | **nothing is changed** |

For `-c` only records whose `cwd` is **exactly** the current directory count
(symlinks resolved): a parent directory never matches, and two worktrees of one
repository never cross-select. The newest record wins, skipping one whose
transcript no longer exists; two records last active in the same second are no
answer (notice), and then the transcript decides. A native newest record is no
answer for `-c` either: Claude's own pick may be a newer transcript the cache
never saw (a `claude -p` has no statusline), so the transcript decides.

A recorded **local** route stays local and still has to pass the local gate in
`exec` (the prepare-local window, token, port, credentials) — a record never
bypasses it.

Before a remote restore `launch` asks the route whether it still serves the
model (the check behind `cc-harness-agents serves`). Exit `4` — the route
answered and no longer offers the model — stops the resume (Robert,
2026-10-21; until then it fell back to the Anthropic default):

```
claude: this session ran on gpt-5.6-sol via cc-harness:sol, which the gateway no longer serves — continue on sol's current model with `claude --resume <UUID> --proxy sol`, or on Anthropic with `claude --resume <UUID> --noproxy`
```

Both suggestions are explicit choices, so neither runs the restore again —
which is also why they name the resolved session even after a `-c`: without a
restore, `-c` is Claude's own pick and may be another conversation
(`claude -c --proxy sol` still works when that is the one you mean). Neither needs a
`--model`: Claude Code does not re-apply a resumed session's stored model —
`ANTHROPIC_MODEL` (the agent's current primary under `--proxy`) or the account
default decides, also after an in-session `/model` (observed with Claude Code
2.1.306, interactive and `-p`, against a stub gateway). Every other failure is
the route's and keeps its status and message. An explicit `--proxy <agent>` never
asks: `exec` refuses a model the route lacks with "remote models missing:
<id>". `--local` has no catalog to ask, so its own readiness gate decides.

`--noproxy` exists **for this table**. Without an explicit model choice a bare
`-c` always re-enters whatever foreign agent the session last ran on;
`claude -c --noproxy` is the short way back to Claude, on your own login and
default model (`--model <x>` alongside picks one). Inside a routed session it
also drops the inherited routing recipe, which a plain launch keeps. The native
shorthands (`claude -c --opus` = `--model opus`) still work for the
transition, with a deprecation notice, and never route — not even `--opus`,
which shares its name with the opus row (`--proxy opus` is the gateway).

## A foreign default model

Claude Code saves an in-session `/model` as the default for new sessions
(the `model` key of the user settings). Inside a routed session that saves a
foreign id — `/model gpt-5.6-terra` — which the routed session never notices,
because `ANTHROPIC_MODEL` outranks the setting. The next native start would ask
Anthropic for it and fail (`unrecognized_model`).

So a native start that would use the setting is guarded. If the effective
`model` setting is an id the table routes (the `resolve-model` mapping; a
`claude-*` id never counts), `launch` pins `--model default` and prints one line
naming the file and the key. It never edits the file: remove the key there, or
use `--proxy <id>`.

No guard when something else settles the model or the endpoint: `--model` (or
a deprecated shorthand), a non-empty `ANTHROPIC_MODEL` or `ANTHROPIC_BASE_URL`
in the environment or in the settings' merged `env` (a higher file's value
wins, an empty one included), `--agent`, `--settings`, `--setting-sources`, `--from-pr`,
`--teleport`, a route chosen or restored, or a routed session (unless
`--noproxy` leaves it). A resume restores its session's own model, so a plain
`claude -c` is left alone; `claude -c --noproxy` already pins the default and
adds the notice.

The effective setting is the first file that sets `model`, highest first:

1. `.claude/settings.local.json` in the root of the **main checkout** — in a git
   repository Claude reads it from there only, also from a subdirectory or a
   linked worktree, whose own copy it ignores; outside one, from the current
   directory;
2. `.claude/settings.json` in the current directory (not the repository root);
3. `settings.json` in `CLAUDE_CONFIG_DIR` (default `~/.claude`).

The table is read only when one of them sets a model. A file that cannot be read
or parsed is skipped, as Claude skips it; so is anything but a regular file of
at most 1 MiB, which a checkout could plant to block the launch. A symlinked
current directory is resolved first. Keys are matched exactly (`Model` is
not `model`). Managed settings, which outrank all of these, are not read.

Sources: the precedence, `CLAUDE_CONFIG_DIR` and `/model` writing the user
settings come from the [settings](https://code.claude.com/docs/en/settings) and
[model configuration](https://code.claude.com/docs/en/model-config) docs. The
rest was observed with Claude Code 2.1.304, where the docs are silent:
`--model default` outranks a foreign `model` setting and runs the account
default, also ahead of subcommands; where each project file is read from; that
an unparsable file is skipped; that `env.ANTHROPIC_MODEL` outranks `model`;
that an empty `ANTHROPIC_MODEL` counts as unset; and that the key is
case-sensitive.

## Disabling

- One launch: pass an explicit choice (`--noproxy`, `--model …`, `--proxy sol`).
- Every launch: `CC_ROUTER_RESTORE=0` turns the restore off; flags still work.
- No records: leave `record-session` out of the statusline; the transcript
  bridge still restores what it can.
- No shim: take its directory off `PATH`; the wrapper keeps working.

## Troubleshooting

- **"ignoring session record — …"** names the file and the reason. Delete the
  file, or fix the directory's mode (`chmod 700`).
- **A resume lands on the wrong model.** `cat` the session's record: `model` is
  what the statusline last showed. No record means the transcript decided.
- **"no Claude Code launcher at …"** — set `CC_ROUTER_CLAUDE` to the native
  launcher's absolute path.
- **"… is cc-router itself, not Claude Code"** — `CC_ROUTER_CLAUDE` points at
  the shim; point it at the native launcher.
- **`-c` continues a different session than Claude would.** It continues the
  newest *recorded* session of this exact directory on purpose; pass
  `--resume <UUID>` to choose.

## The resolver's exit status is an instruction

`cc-harness-resume continue|<UUID>` prints `UUID<TAB>agent<TAB>model` and exits:

- **0** — resolved. Route through the named agent, or `native`: leave it alone.
- **3** — a foreign model *is* recorded and could not be restored: nothing
  serves it, **or the helper could not be asked at all** (missing, not
  executable, timed out, unusable answer). CC re-applies a session's stored model
  on resume, so the caller pins `--model default` to stop a foreign id being sent
  to Anthropic. "Could not ask" is deliberately *not* exit 1 — a foreign model is
  on record, so doing nothing is the one unsafe option. Which of the two it was
  is on stderr; the exit status does not distinguish them because the caller's
  action is the same.
- **1** — indeterminate: nothing says a foreign model was ever involved — no
  session here, an ambiguous id, the interactive picker, or nothing recorded yet.
  **The caller must change nothing.** An earlier version prepended `--model
  default` for this case too, which silently outranked a user's own configured
  model on every plain `claude --resume`.
  Exit 1 is **silent** when Claude handles the case itself: no transcript for
  the UUID (Claude prints "No conversation found"), nothing to continue in this
  directory, or a session that never got an answer. It prints a line only when
  an explicit model flag would change the outcome: an id duplicated across
  projects, `-c` where Claude may continue a transcript the resolver could not
  restore (one recorded in another cwd, or a symlinked/very long cwd whose
  folder key is uncertain), a `--resume <search term>`, or a recorded model
  that failed the id check. A hint ahead of Claude's own "No conversation
  found" once made a never-saved session look like a model-restore bug.

It never aborts the launch. A malformed transcript row, a torn last line, an
unexpected answer — all of it comes back as at most one line on stderr and a
status, never a traceback.

## Model matching

`cc-harness-agents resolve-model` owns the mapping; the resolver only asks. The
order is: exact primary match, then exact tier match, then a superseded id the
table retains (`retainedModels`, e.g. gpt-5.6-sol → sol), then a release the
row's family could discover (gpt-6.1-sol → sol, grok-4.8 → grok), then a family
fallback **derived from the table** — any other id routes to the row whose
primary shares its vendor prefix, but only when exactly one agent claims that
prefix. `grok-` resolves that way; `gpt-` is claimed by astra, sol, terra *and*
luna, so a gpt id outside the discovery pattern (gpt-7-sol, a variant) stays
unroutable rather than being handed an arbitrary tier. xAI
answers name build ids (`grok-4.6-build`), which normalise to the proxy alias.

**Anthropic ids are never inferred.** `resolve-model` refuses every `claude-*`
id (exit `1`, "resumes natively"), the opus row's own primary included, and the
bridge answers `native` for one before it asks. A session restores onto the
opus agent only from a record naming `agent=opus`; a record whose agent the
table no longer has resumes a `claude-*` id natively, left to Claude. The pure
table lookup still maps those ids — that is what `claude --proxy
claude-sonnet-5-5` routes by — but resume inference never does.

## Activation

`install.sh` links `cc-harness-agents` and `cc-harness-resume`, and your
`.zshrc` sources `share/claude.zsh` (see the README), so fresh interactive
shells pick this up. For a shell that was already open, source the file again.
For callers that never source it, put `bin/shim` of the checkout ahead of the
native launcher on `PATH`. Records start with the statusline hook:

```sh
# in your statusline script, which receives Claude Code's JSON on stdin
input=$(cat)
# Stamp before detaching: the stamp orders this render's writer against later ones.
rendered_at=$(perl -MTime::HiRes=time -e 'printf "%d", time * 1000')
printf '%s' "$input" | cc-harness-agents record-session --rendered-at "$rendered_at" 2>/dev/null &
```

The stamp needs a millisecond clock; macOS `bash` 3.2 has none, hence `perl`
(any source of unix milliseconds will do). An empty stamp — no clock — is
taken as none: the record is then written unordered, as before. Upgrade the
binary before the statusline — an older `record-session` refuses the flag,
and an older `launch` ignores a record that carries `rendered_at`.

Calling the native binary by absolute path deliberately bypasses all of this.

## Limits

- A record holds the model the statusline last showed, so a `/model` switch
  counts even before the model answered. The transcript bridge gives the model
  that last **answered** instead.
- Custom per-tier settings and effort are recorded nowhere; a resume uses
  today's table. The route is recorded; for a session without a record remote
  is assumed — pass `--local <agent>` explicitly.
- A restore reuses the `CC_HARNESS_MODEL_<AGENT>` override path, and an override
  is by definition a request for one reproducible model. When the restored model
  is a **different** one than the row's primary, it collapses the tiers that
  *track the ladder* — every rung the table points at the primary or at the
  rung below it — onto the restored model. That includes "Custom Fable" wherever
  it equals the primary (grok, astra) and, in the luna row, the haiku
  rung. Rungs pointed elsewhere stay what the table says. The ceiling never
  rises above the row's: rungs left in place must still fit it. Exception: a
  **retained** id (below) is a resume of its row's former primary, so only the
  rungs equal to the primary follow it and the sonnet rung is not collapsed.
- A **retired model id is not restored**. A transcript that last answered on
  an id no row carries any more (gpt-5.3-codex-spark, the former codex haiku
  rung) resolves to no profile, so `launch` prints that it could not restore
  and pins the Anthropic default — it does not quietly resume on the rung's
  successor. Pass `--proxy <agent>` (`--proxy luna`, …) to continue on a routed model.
  An id the table still maps but the route no longer serves is refused
  instead: `serves` reports it, and the resume fails naming
  `--proxy <agent>` and `--noproxy` (decided 2026-10-21; it fell back to the
  Anthropic default before).
- A **superseded** id the route still serves (gpt-5.6-sol/luna after gpt-6 took
  their rungs) is restored exactly as the **primary**: it resolves to its former
  row under its own id, so the session continues on gpt-5.6, not on the
  successor. The rest of the ladder is today's table, not the historical one — a
  resumed old Sol session runs `OPUS=gpt-5.6-sol SONNET=gpt-5.6-terra
  HAIKU=gpt-6-luna`. The transcript records only the answering model, so the old
  ladder cannot be recovered; every current rung holds the shared ceiling.
- All four codex rows share one Astra/Sol/Terra/Luna ladder and one ceiling, so
  a codex restore lands on that ladder; only the primary (and, for a retained
  id, the rungs equal to it) differs.
- An **auto-selected Grok release is such a different model**: the table row
  still names the last-known release, so a session started as 4.7/4.6 resumes as
  4.7/4.7 — its sonnet rung sits on the primary, unlike a fresh launch. If a
  resumed sub-agent runs on a different model than a fresh one, check this first.
- Restoring the row's **own primary** — the common case for every agent that
  does not auto-select — changes nothing at all: every tier
  keeps its table value and no note is printed. It used to collapse here too,
  which silently ran a resumed session's sonnet rung on the primary; for a
  table primary that is no longer the reason a resumed sub-agent differs.
- The context ceiling is per MODEL, never per row: `verifiedContext` in
  `internal/agents/selection.go` holds the windows that are actually measured,
  and a model whose window is known nowhere is assumed to share its predecessor's, or failing that falls back to a
  conservative 200k — **and says which on stderr**. That includes a
  resumed auto-selected Grok release nobody has measured yet. The ceiling is not
  recorded with the session: it is looked up again at resume from the same
  sources, so it only differs from the start if a measurement or catalog
  metadata appeared in between.
  Sharing a row does not mean sharing a window — `grok-composer-2.5-fast` sits in
  the 500k grok row but only holds 200k, and handing it the row's number would
  push auto-compact 300k past the real limit into a hard upstream overflow.
- `-c` looks at top-level UUID transcripts in the encoded current directory,
  newest first, and checks each one's recorded cwd. Explicit UUID lookup works
  across projects and refuses ambiguous duplicates. A change to CC's persistence
  format needs an adjustment here.
- The resolver parses the selected transcript in full to learn its last model —
  about 10 ms for a typical file, 0.4 s for a 96 MB outlier. Reverse-scanning was
  judged not worth the complexity at that cost.
- Named resume searches and the interactive picker cannot choose routing ahead of
  time, because the conversation is not known until after the pick.

The bridge is interim: session records are now the primary source
([`tasks/archive/restore-routed-sessions.md`](../tasks/archive/restore-routed-sessions.md)),
and it keeps these regression cases until it goes.

## Testing

`make check` runs `tests/test-claude-wrapper.sh` (flags, the history hint, the
shim) and `tests/test-cc-harness-resume.py` (the restore: records, transcripts,
fallbacks) together with the routing suite. Both drive the real wrapper and
the real `launch` against the test gateway; only Claude is fake.

Sources: [CC sessions](https://code.claude.com/docs/en/sessions),
[model configuration](https://code.claude.com/docs/en/model-config),
[Herdr restore implementation](https://github.com/herdrdev/herdr/blob/v0.8.2/src/app/agent_resume.rs).
