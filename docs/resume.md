# Custom-model resume (`cc-harness-resume`)

Runbook for restoring a foreign model when a Claude Code session is resumed.
Without it, `claude -c` and `claude --resume <UUID>` came back on the Anthropic
default no matter which model the conversation had actually been running on.

## How it works

```
claude -c                  cc-harness-resume            cc-harness-agents
  (zsh wrapper)      →     reads the transcript    →    resolve-model <id>
                           of THAT session              (pure table lookup)
        ↓                          ↓                            ↓
  rewrites argv          UUID / agent / model          agent + model id
        ↓
  cc-harness-agents exec <agent> -- claude --model <id> --resume <UUID>
```

`cc-harness-resume` (source: `cmd/cc-harness-resume/`) reads the last real main-session assistant model out
of Claude Code's own JSONL transcript for the named session. Synthetic messages
and sidechains never select a model; no prompts, credentials or new sidecars are
stored, and transcript text is never emitted.

The wrapper then re-enters that agent's `cc-harness-agents` profile and pins both
`CC_HARNESS_MODEL_<AGENT>` and CC's own `--model`. **Continue is rewritten to the
resolved UUID**, so routing and conversation selection cannot pick different
conversations.

## Precedence

| Situation | Result |
|---|---|
| `--sol` / `--grok` / any tier flag given | the flag wins; no restore runs |
| native `--model` given | it wins; no restore runs |
| `--fable` / `--opus` / `--sonnet` / `--haiku` given | shorthand for `--model <alias>`: it wins, no restore runs |
| session last answered with a `claude-*` model | normal Anthropic resume path |
| session last answered with a routable foreign model | that agent + model restored, announced on stderr |
| foreign model recorded, nothing routes it | `--model default` pinned, notice on stderr |
| routable foreign model the gateway no longer serves | `--model default` pinned, notice on stderr |
| routable foreign model, the route fails (403/401/502/TLS, unconfigured) | error naming the layer, nothing launched |
| nothing determinable | **nothing is changed** |

`--local` still selects the local gateway; remote stays the default.

Before a remote restore the wrapper asks `cc-harness-agents serves <model>`.
Exit `4` — the route answered and no longer offers the model — is the only
status that falls back to the Anthropic default (Robert, 2026-10-16); every
other failure is the route's and keeps its status and message. An explicit
`--sol`/`--grok` never asks: it runs `exec`, which refuses a model the route
lacks with "remote models missing: <id>". `--local` has no catalog to ask, so
its own readiness gate in `exec` decides.

The four native shorthands exist **for this table**. Without an explicit model
choice a bare `-c` always re-enters whatever foreign agent the transcript last
answered with, so getting a resumed session back onto a Claude model used to mean
spelling out `--model opus` every time. `claude -c --opus` is the short form and
nothing else — it never routes.

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

`install.sh` links `cc-harness-resume` next to `cc-harness-agents`, and your
`.zshrc` sources `share/claude.zsh` (see the README), so fresh interactive
shells pick this up. For a shell that was already open, source the file again.

Herdr injects the saved resume command into a *fresh pane shell*, so the sourced
function participates in its restore — which is why the bridge lives in the zsh
wrapper and not in an executable shim. Calling the Claude binary by absolute path
or via `command claude` deliberately bypasses all of this.

## Limits

- The transcript gives the model that last **answered**, not an unserved `/model`
  choice made just before exiting.
- Historical route (`--local` vs remote) and custom per-tier settings are not
  recoverable from a transcript. Remote is assumed; pass `--local` explicitly.
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
  rung) resolves to no profile, so the wrapper prints that it could not restore
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

This is an interim bridge. [`tasks/restore-routed-sessions.md`](../tasks/restore-routed-sessions.md)
replaces it with durable session routing metadata and an executable shim, and
keeps these regression cases.

## Testing

`make check` runs `tests/test-claude-wrapper.sh` and
`tests/test-cc-harness-resume.py` together with the routing suite. Either
wrapper suite alone can go green while the other's contract is broken: both
source the same wrapper and build their own fakes.

Sources: [CC sessions](https://code.claude.com/docs/en/sessions),
[model configuration](https://code.claude.com/docs/en/model-config),
[Herdr restore implementation](https://github.com/herdrdev/herdr/blob/v0.8.2/src/app/agent_resume.rs).
