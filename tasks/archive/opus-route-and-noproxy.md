> Archived 2026-10-19 · PR #19 (merged @ d351f9f) · task/opus-route-and-noproxy

# Opus route and --noproxy

## Goal

Issue #17 (Robert, 2026-10-19), as corrected with him the same day:

1. **`--opus`** routes Claude Opus through CLIProxyAPI, which now holds an
   Anthropic (Claude) OAuth login on the remote host. It is a **table row**
   like the other agents (`share/models.tsv`). Flags, `--local` gating and the
   refusal of `--model` alongside an agent flag come from the table and
   `launch` already.
2. **`--noproxy`** — new — starts or resumes **without any proxy**: native
   Anthropic with the user's own login and default model. Its main use is
   `claude -c --noproxy`, which resumes a session that ran through the proxy
   natively instead of restoring its route. It conflicts with every agent flag
   and with `--local`. It combines with `--model`.
3. **`--opus --local` is dropped.** It was asked for to mean "no proxy", which
   is `--noproxy` now. Do not add a local Claude route.

## Constraints

- **Anthropic ids stay native by default.** Today `resolve-model
  claude-opus-5-5` finds no profile, so a plain Opus session resumes natively.
  The new row must not change that. A session restores to `opus` only from a
  session record (`agent=opus`), never by inferring the agent from a
  `claude-*` model id. That applies to the transcript bridge as well. Cover
  this with a test.
- **Subagents stay in the Anthropic family.** No rung, haiku/subagent slot
  included, may fall to a Codex or Grok model.
- **Fill the row from the route's catalog** (all four rungs, fable included),
  with the context ceiling verified, not guessed. Follow
  `tasks/model-onboarding.md` and the table header rules.
- **Check `--noproxy` and `--opus` against Claude Code's own options.** Claude
  Code must not define either name. Say how you checked.
- **Never touch** `~/.local/bin/claude` (the native launcher), the CLIProxyAPI
  `config.yaml`, Caddy, or the panel. Wrapper and shim stay thin callers of
  `launch`.
- DRY, KISS, elegance. No speculative knob.

## PR description must correct the issue

Issue #17 predates stage 2. State plainly what changed against its text:

- the wrappers now live here (`share/claude.zsh`, shim `bin/shim/claude`), not
  in `.zsh/functions/`;
- `~/.local/bin/claude` is the native launcher, not a bash wrapper;
- there is no `--kimi`;
- `--opus --local` is replaced by `--noproxy`, and why;
- Opus resumes through the proxy only via a session record.

## Acceptance

- [ ] `claude --opus -p "…"` over remote answers on the Opus row (smoke, after
      merge and `make build`).
- [ ] `claude -c --noproxy` resumes a routed session natively; `--noproxy`
      with an agent flag or `--local` is refused with a clear message.
- [ ] A native `claude-*` session without a record resumes natively (test).
- [ ] README and help text updated; `make check` green;
      `tests/migration-coverage.md` updated if a contract assertion changes.
- [ ] `list` still prints exactly four TSV columns (work-system contract).
