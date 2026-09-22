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

- [x] **(Revised 2026-09-22, Robert's decision in the worker's plan review):**
      the core is implemented **directly in Go** — behavior-equivalent to
      `~/dotfiles/.scripts/cc-harness-agents` at current HEAD, verified by the
      language-neutral contract suites running against the built binary. The
      dotfiles bash stays live and untouched until the coordinated cutover.
      Quality gate: small Makefile, `make check` = gofmt/vet/staticcheck +
      `go test -race` + `tests/run`. The bash-specific requirements this
      replaces (POSIX conversion, behavior-identical bash copy) are void.
- [x] Model table moved to `share/models.tsv` (tab-separated, same columns),
      read per invocation; user override honored at
      `~/.config/cc-router/models.tsv`. Table content taken from dotfiles HEAD
      (grok-4.6, current ceilings), not from older docs.
- [x] Every personal constant lifted into config with documented defaults:
      `llm.gering.dev`, `mars`, `MACBOOK` profile, port `18321`,
      `/volume1/docker/...` paths. Secret contract stays "these env vars must
      be set" — no SOPS dependency.
- [x] Routing test suite (`~/dotfiles/scripts/test-cc-harness-agents.sh`)
      brought across under `tests/`, runnable via `tests/run`, green. Re-count
      the tests; update the stale counts in `mission.md`.
- [x] The work-system PATH contract is covered by tests: `list` prints exactly
      four TSV columns (name, model, available, note); `exec <id> -- claude …`
      execs routed. Contract doc:
      `~/Projekte/Plugins/claude-plugins/plugins/work-system/docs/cc-harness-agents.md`.
- [x] ~~`make check` runs `shellcheck -s sh` + `shfmt -d` + `tests/run`
      (Go targets join later)~~ superseded by the Go gate above (shellcheck
      `-s sh` covers the POSIX glue, `-s bash` the carried-over suite);
      GitHub Actions matrix macOS + Linux, no secrets, no network in tests.
- [x] `install.sh` symlinks `bin/` entries onto `PATH`; documented in README.
- [ ] Dotfiles switched to consumer: `.scripts/cc-harness-agents` replaced by a
      symlink into the cc-router checkout, old file deleted (never copied);
      author's setup verified working the same day (`claude --grok` smoke test).

## Relevant Files

- `~/dotfiles/.scripts/cc-harness-agents` (source, 1769 lines at `f429346`)
- `~/dotfiles/scripts/test-cc-harness-agents.sh` (suite, 1674 lines at `f429346`)
- `tasks/architecture.md` (layout, phase rules, quality gate)
- `~/Projekte/Plugins/claude-plugins/plugins/work-system/docs/cc-harness-agents.md`
  (external consumer contract)

## Notes

- **Sequencing (gate released 2026-09-08):** both upstream gates are merged —
  dotfiles PR #24 (interim resume fix, `a57fdca`, live-tested) and PR #26
  (`--astra` row + selector, `97cb8b4`). Extraction may start; read dotfiles
  HEAD at extraction time, never an earlier snapshot. Note from #26: the
  272000 context figure in enable-astra-profile.md was wrong — the merged
  table row documents the verified value; trust the row, not the brief.
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

## Progress (2026-09-22)

- **Baseline:** dotfiles `f429346` (includes `7d31d78`, the newest-offered
  Grok selection). `share/models.tsv` is byte-for-byte the bash `AGENTS`
  table at that commit. Re-diff before the cutover.
- **Port:** `cmd/cc-harness-agents` + `internal/agents` (Go 1.27). Runtime
  has no curl/jq/nc/bash dependency; `exec` is `syscall.Exec`.
- **Config decisions (plan review):** user `models.tsv` replaces the packaged
  table (no merge; invalid fails closed); `config.env` is strict data,
  environment > file > default; the remote URL has no default; the legacy
  `~/.config/cliproxy/client.env` stays the profile fallback until the quota
  helpers move; the local refusal hint is configurable
  (`CC_ROUTER_LOCAL_PREPARE_HINT`), generic by default. Grok discovery and
  verified context windows stay Go policy, not table data.
- **Tests (re-counted):** `tests/run` runs 389 assertions from the carried-over
  suite plus 5 installer checks; `go test -race` covers units and black-box
  contracts against the production binary. Upstream's 328 assertion sites are
  mapped in `tests/migration-coverage.md` (253 verbatim, 10 adapted, 5
  replaced, 2 retired, 58 staying with the dotfiles wrapper/statusline).
- **Open:** Linux CI run (via the PR), cutover per the manager's sequence.
  For the author's setup the cutover needs `~/.config/cc-router/config.env`
  with `CC_ROUTER_REMOTE_URL` and `CC_ROUTER_LOCAL_PREPARE_HINT` (the old
  `--mars-stopped` wording).

### Status 2026-09-22 (later)

- PR #4 open (`task/extract-cc-harness-agents`); `git log` is the source for
  its head. Test helpers are named by role: `internal/testpki` (shared
  PKI/listeners), `tests/gateway` (stand-in HTTPS gateway for the bash suite).
- Swarm reviews run locally on Sonnet, read-only; fixes are applied in the
  main session only. One run's subagent committed on its own (under Robert's
  name, not pushed) — dropped, kept as tag `backup/rogue-swarm-commit`. Since
  then HEAD and the tree are checked before and after every run.
- Swarm report triaged with Robert (27 findings, 1 refuted). Applied: PATH
  lookup skips relative entries, diagnostics sanitised, `resolve-model` refuses
  an id it cannot have produced, one capped `readRegularFile` and one
  `decodeSingle` for every file/JSON read, loopback literals only for
  `CC_ROUTER_LOCAL_HOST`, `tests/run` exits on INT/TERM and honours `GO`,
  gateway.env shell-quoted, plus DRY/dead-code cleanups. Rejected with reason:
  the marker and TCP-liveness "hardening" (cliproxy-auth's contract, unchanged
  from bash), the credential symlink/expiry-boundary reports (parity, or an
  attacker who already owns the proxy dir), `mkdir -p bin` (go build creates
  it), and the untracked TASK.md duplication.
- Next: Robert's review/merge; then ask the manager session (cc-router-48) for
  the cutover freeze window. No dotfiles swap before that.

### Upstream drift: the Codex catalog moved (reported 2026-09-22 by cc-router-48)

A Codex update rebuilt the catalog — sol/terra are GPT-6 now and
`gpt-5.3-codex-spark` is gone — so the four Codex rows in `share/models.tsv`
(sol, terra, luna, and astra's borrowed haiku rung) name models the gateway no
longer offers. Live, those rows report unavailable, which is the fail-closed
behaviour working as designed, not a defect to fix here.

**Corrected 2026-09-22 (cc-router-48):** the dotfiles fix is NOT data-only.
The proxy catalog lags the native Codex update — `gpt-6-astra` is offered,
sol/terra/luna only as 5.6, spark is gone — so the rows cannot simply be
pinned to GPT-6. The dotfiles side is running a broader interim fix (gateway
catalog reconciliation, a spark replacement, current GPT-6 routing) that may
change CODE in `.scripts/cc-harness-agents`, not only the table.

The split agreed with the manager:

- **PR #4 keeps its current scope.** No GPT-6 rows are pulled in; the
  equivalence baseline stays dotfiles `f429346`, frozen until the merge hash
  arrives.
- **After that fix merges** (hash arrives via cc-router-48 or the dotfiles
  manager), port the delta and do the final HEAD diff — the last sync before
  the freeze. Expect it to reach beyond `share/models.tsv`: behaviour changes
  have to be ported into the Go core and evidenced in
  `tests/migration-coverage.md`. The dotfiles PR marks which parts are data
  and which are behaviour.
- If Robert merges PR #4 first, that is fine: the models.tsv delta can be a
  follow-up commit before the cutover, which waits for the freeze window
  regardless.
- The auto-latest generalisation for the GPT family is **binding
  post-cutover scope**, explicitly outside this port: cc-router PR #5, commit
  `a7d3825` (`tasks/model-onboarding.md`). Do not pull it forward, however
  tempting it looks while porting the interim fix.
