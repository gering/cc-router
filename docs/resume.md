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
 "agent":"sol","route":"remote","model":"gpt-5.6-terra","updated_at":1786435200}
```

`agent` is `CC_ROUTER_AGENT`, which `exec` exports (non-secret, the row name);
`route` is `CLIPROXY_ROUTE`; `model` is the statusline's `model.id`, so a
`/model` switch inside the session is what the record holds. A session started
before the marker existed has a route but no agent, and resolves through the
table's own mapping (`resolve-model`). A native session records only its model.

The record never holds credentials, headers, prompts or transcript text. The
directory is `0700`, files are `0600`, written through a temp file and a rename;
`record-session` reads stdin only, never the network, and prunes records idle
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
| `--sol` / `--grok` / any agent flag given | the flag wins; no restore runs |
| native `--model` given | it wins; no restore runs |
| `--fable` / `--opus` / `--sonnet` / `--haiku` given | shorthand for `--model <alias>`: it wins, no restore runs |
| `--local` given | the restore runs, but on the local route |
| already inside a routed session (`CC_ROUTER_AGENT` set) | no restore — the inherited route stays |
| the session's record: native | left to Claude, which restores its own model |
| the session's record: routed | that agent, route and exact model, announced on stderr |
| no usable record: the transcript names a routable model | that agent + model, remote route |
| record or transcript names a model nothing routes | `--model default` pinned, notice on stderr |
| routable model the gateway no longer serves | `--model default` pinned, notice on stderr |
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
answered and no longer offers the model — is the only verdict that falls back
to the Anthropic default (Robert, 2026-10-16); every other failure is the
route's and keeps its status and message. An explicit `--sol`/`--grok` never
asks: `exec` refuses a model the route lacks with "remote models missing:
<id>". `--local` has no catalog to ask, so its own readiness gate decides.

The four native shorthands exist **for this table**. Without an explicit model
choice a bare `-c` always re-enters whatever foreign agent the session last ran
on, so getting a resumed session back onto a Claude model used to mean
spelling out `--model opus` every time. `claude -c --opus` is the short form
and nothing else — it never routes.

## Disabling

- One launch: pass an explicit model flag (`--opus`, `--model …`, `--sol`).
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

## Activation

`install.sh` links `cc-harness-agents` and `cc-harness-resume`, and your
`.zshrc` sources `share/claude.zsh` (see the README), so fresh interactive
shells pick this up. For a shell that was already open, source the file again.
For callers that never source it, put `bin/shim` of the checkout ahead of the
native launcher on `PATH`. Records start with the statusline hook:

```sh
# in your statusline script, which receives Claude Code's JSON on stdin
input=$(cat)
printf '%s' "$input" | cc-harness-agents record-session 2>/dev/null &
```

Calling the native binary by absolute path deliberately bypasses all of this.

## Limits

- A record holds the model the statusline last showed, so a `/model` switch
  counts even before the model answered. The transcript bridge gives the model
  that last **answered** instead.
- Custom per-tier settings and effort are recorded nowhere; a resume uses
  today's table. The route is recorded; for a session without a record remote
  is assumed — pass `--local` explicitly.
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
  successor. Pass the tier flag (`--luna`, …) to continue on a routed model.
  An id a row DOES still carry but the route no longer serves ends the same
  way: `serves` reports it, and the session resumes on the Anthropic default
  with a notice (decided 2026-10-16).
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

The bridge is interim: session records replace it as the primary source
([`tasks/restore-routed-sessions.md`](../tasks/restore-routed-sessions.md)), and
it keeps these regression cases until it goes.

## Testing

`make check` runs `tests/test-claude-wrapper.sh` (flags, the history hint, the
shim) and `tests/test-cc-harness-resume.py` (the restore: records, transcripts,
fallbacks) together with the routing suite. Both drive the real wrapper and
the real `launch` against the test gateway; only Claude is fake.

Sources: [CC sessions](https://code.claude.com/docs/en/sessions),
[model configuration](https://code.claude.com/docs/en/model-config),
[Herdr restore implementation](https://github.com/herdrdev/herdr/blob/v0.8.2/src/app/agent_resume.rs).
