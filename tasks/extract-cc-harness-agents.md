# Extract cc-harness-agents from the dotfiles

## Goal

Migration step 1 from [`architecture.md`](architecture.md): move
`cc-harness-agents` and its routing test suite out of `~/dotfiles` into this
repository, depersonalised, with the quality gate in place — while the dotfiles
keep working the same day via a symlink.

## Context

`cc-harness-agents` is the core: model table, route probes, environment recipe,
then `exec claude`. Everything else calls it. It is the component with the most
tests, and the tests are language-neutral (fake launchers, argv and env-var
*name* assertions), which later carries the Go port.

**Read the source from `~/dotfiles` HEAD at extraction time — never from notes.**
Development continues there (grok-4.6 already replaced grok-4.5 as the canonical
Grok model; companion self-heal and account retirement landed after the
architecture doc was written).

## Requirements

- [ ] `bin/cc-harness-agents` extracted from `~/dotfiles/.scripts/cc-harness-agents`
      at current HEAD, behavior-identical except for the changes below.
- [ ] **Phase 1 discipline** (`architecture.md`): no refactor, no split into
      `lib/` — depersonalise and ship. Convert the ~4 bashisms to POSIX `sh`
      and switch the shebang to `#!/bin/sh` only if the suite stays green;
      otherwise keep bash and note it in the task file.
- [ ] Model table moved to `share/models.tsv` (tab-separated, same columns),
      read per invocation; user override honored at
      `~/.config/cc-router/models.tsv`. Table content taken from dotfiles HEAD
      (grok-4.6, current ceilings), not from older docs.
- [ ] Every personal constant lifted into config with documented defaults:
      `llm.gering.dev`, `mars`, `MACBOOK` profile, port `18321`,
      `/volume1/docker/...` paths. Secret contract stays "these env vars must
      be set" — no SOPS dependency.
- [ ] Routing test suite (`~/dotfiles/scripts/test-cc-harness-agents.sh`)
      brought across under `tests/`, runnable via `tests/run`, green. Re-count
      the tests; update the stale counts in `mission.md`.
- [ ] The work-system PATH contract is covered by tests: `list` prints exactly
      four TSV columns (name, model, available, note); `exec <id> -- claude …`
      execs routed. Contract doc:
      `~/Projekte/Plugins/claude-plugins/plugins/work-system/docs/cc-harness-agents.md`.
- [ ] `make check` runs `shellcheck -s sh` + `shfmt -d` + `tests/run`
      (Go targets join later); GitHub Actions matrix macOS + Linux, no secrets,
      no network in tests.
- [ ] `install.sh` symlinks `bin/` entries onto `PATH`; documented in README.
- [ ] Dotfiles switched to consumer: `.scripts/cc-harness-agents` replaced by a
      symlink into the cc-router checkout, old file deleted (never copied);
      author's setup verified working the same day (`claude --grok` smoke test).

## Relevant Files

- `~/dotfiles/.scripts/cc-harness-agents` (source, ~1000 lines, read at HEAD)
- `~/dotfiles/scripts/test-cc-harness-agents.sh` (suite, ~733 lines)
- `tasks/architecture.md` (layout, phase rules, quality gate)
- `~/Projekte/Plugins/claude-plugins/plugins/work-system/docs/cc-harness-agents.md`
  (external consumer contract)

## Notes

- **Sequencing (updated 2026-09-06, ROUTER-RESUME-DONE-20260906-A):** dotfiles
  PR #24 (interim resume fix) merged 2026-09-05 (`a57fdca`), live-tested. The
  remaining gate is the `--astra` profile merge in dotfiles — it changes
  `.scripts/cc-harness-agents` too. Start only after that, and re-read HEAD;
  an extraction from an earlier snapshot would overwrite both at cutover.
- Source drift since this task was written: the wrapper now lives at
  `.zsh/functions/claude.zsh` (not `.zshrc`), and `.scripts/cc-harness-resume`
  (interim resume bridge) plus `scripts/test-cc-harness-resume.py` exist as
  additional extraction candidates for the resume task.
- The `claude()` zsh wrapper, statusline/quota, `cliproxy-auth` and the
  companion are **later steps** — out of scope here beyond not breaking them.
- Local-route marker file (`~/.cache/cliproxy-auth/local-ready.json`) is an
  interface shared with `cliproxy-auth`; keep the path readable from config but
  do not move it yet.
- Secrets never in argv/logs/errors; tests assert env-var names only.
- The dotfiles cutover step touches `~/dotfiles` (outside this repo/worktree):
  coordinate with the manager session before executing it, and keep it as the
  final, separately reversible commit in the dotfiles.
