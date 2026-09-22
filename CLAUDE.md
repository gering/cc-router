# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository state

The routing core `cc-harness-agents` is implemented here in Go
(`cmd/cc-harness-agents`, `internal/agents`, table in `share/models.tsv`). The
wrapper, statusline, `cliproxy-auth` and companion still live in the private
`gering/dotfiles` repository and move later.

- **`make check` is the gate** (gofmt/vet/staticcheck, shellcheck/shfmt,
  `go test -race`, `tests/run`, then `make build`); `make fmt` formats. The contract suite
  in `tests/` came across from dotfiles — keep its assertions language-neutral
  and update `tests/migration-coverage.md` when it changes.
- `tasks/architecture.md` holds the decisions: layout, language, distribution,
  quality gate, migration order. Read it before proposing structural changes.
  `tasks/mission.md` is the extraction checklist,
  `tasks/restore-routed-sessions.md` the first feature spec.
- Test counts for the not-yet-extracted suites in `tasks/mission.md` were read
  off dotfiles at `f0effff`. Re-count; never cite them as current.

## External coordination

- **`~/dotfiles` is live upstream.** Router development continues there while
  extraction runs. Before every migration step, read the current dotfiles HEAD
  (`git -C ~/dotfiles log -- .scripts/cc-harness-agents .scripts/cliproxy-auth
  .claude/cliproxy-quota-source.sh services/cliproxy-companion`) — never work
  from notes or an earlier snapshot. Model names drift too (grok-4.6 already
  replaced 4.5 there).
- **work-system consumes `cc-harness-agents` via PATH.** The plugin-side
  contract is `plugins/work-system/docs/cc-harness-agents.md` in
  `~/Projekte/Plugins/claude-plugins`: `list` prints exactly four TSV columns
  (name, model, available, note); `exec <id> -- claude …` launches routed.
  Relocating the binary is explicitly supported, but the listing and exec
  interfaces are external contracts — cover them in tests, change them only in
  step with work-system.

## What cc-router is

A client that makes the Claude Code harness talk to foreign models (Grok, Kimi,
GPT-5.6 tiers) through a self-hosted [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI),
selected by flag: `claude --grok`, `--kimi`, `--sol`, plain `claude` stays
Anthropic. The laptop holds no OAuth credentials — only a revocable per-machine
key.

## Architecture

Five pieces, deliberately layered so routing logic exists exactly once:

- **`cc-harness-agents`** — the core. Owns the model table, the route probes and
  the environment recipe (base URL, auth, headers, tier defaults, subagent
  model, context ceiling), then execs `claude`. Everything else calls it.
- **`claude()` wrapper / executable shim** — interactive layer only: flag
  parsing, conflict checks, resume hints. Must never duplicate the model table.
  The planned shim (see the restore task) sits ahead of the native launcher on
  `PATH` so non-shell callers like herdr also get routing.
- **`cliproxy-auth`** — tunnels to the private admin API; runs provider OAuth
  logins on the host that keeps the tokens.
- **quota helpers** — feed the statusline from the read-only usage API.
- **companion** (Go) — two narrow facades in front of CLIProxyAPI's management
  API: `:8320` read-only usage, `:8321` admin over loopback/SSH only.
  CLIProxyAPI itself is `:8317`. The generic management API is never published.

Every public request carries two independent per-machine credentials:
Cloudflare Access service token (rejects `403`) and proxy key (rejects `401`).

## Non-negotiable constraints

Each fixed a real, reproducible defect. Full list in `tasks/mission.md`; the
ones that bite during implementation:

- **No automatic remote→local fallback.** Two proxies refreshing the same OAuth
  credentials produce `invalid_grant`. `--local` is explicit and gated.
- **No plaintext HTTP, never `curl -k`.** TLS is what authenticates the gateway.
- **Secrets never in argv, logs or error messages.** Headers are built in
  memory and set on the request, never passed to a subprocess; diagnostics name
  variables, never values. Tests assert env var *names*.
- **A new model is a table row, never a code path.** Provider, tiers and real
  context ceiling in one data line. A branch per model is the wrong shape.
- **Fail closed and name the failing layer.** `403` Access, `401` proxy key,
  `502` origin, TLS, missing model — distinct messages, not "proxy down".
- **The statusline never waits on the network**, and does only local, bounded,
  atomic I/O (`0700` dirs, `0600` files, temp-file + rename).
- **Validate strictly what you consume; ignore provider fields you don't
  render.** An exact key set once blanked the Codex quota bars.
- **Two output shapes:** a TTY gets a padded table, a pipe gets the raw TSV
  contract, so consumers' fields never shift.

## Extraction rules

- **The core is written in Go directly** (Robert's decision, 2026-09-22 — it
  replaced the earlier "extract bash first, port later" plan). The dotfiles
  bash stays live and untouched until the coordinated cutover; the
  language-neutral test suites come across as the specification and must run
  green against the Go binary. Where shell remains (shim, install), target
  POSIX `sh`, not bash: macOS has bash 3.2.
- Strip every personal constant on the way out — `llm.gering.dev`, `mars`,
  `/volume1/docker/...`, the `MACBOOK` profile, port `18321`. They become
  configuration with documented defaults.
- Secret storage is a contract ("these variables must be in the environment"),
  not a SOPS/age dependency.
- Out of scope: hosting CLIProxyAPI for anyone, and sharing subscriptions with
  third parties (provider terms forbid it — say so plainly, point to per-user
  API keys).
