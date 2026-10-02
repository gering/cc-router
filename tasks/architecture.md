# Architecture: how cc-router is built

Decisions taken for the extraction, with the reasoning worth keeping. This file
answers the "open questions" from [`mission.md`](mission.md); that file remains
the checklist of what still has to move.

## Repository and audience

**Public, MIT.** The repository is already public. That is the point: a public
repository cannot bake in `llm.gering.dev` or `mars`, so it forces the
decoupling the mission asks for. MIT rather than Apache-2.0 — at this size the
patent clause buys nothing.

**Personal machines only.** The README states plainly that this is a client for
your own accounts on your own host, and that giving third parties access
violates provider terms. Said once, in the open, not buried in a task file.

## Language: Go directly, tests as the bridge

**Revised 2026-09-22 by Robert's explicit decision** (made in the extraction
worker's plan review): the core is written in Go **directly** — the earlier
two-step plan (extract bash as-is, port later) is deliberately replaced. The
sections below keep the reasoning that still holds and mark what changed.

The long-term target for the core is **Go**. The reasons are in the current
code, not in taste:

- Validation is the bulk of the logic and bash's weakest ground. The header
  check is ~60 lines (control characters, bare CR, duplicates, auth headers,
  empty lines) plus a 12-line `jq` expression asserting model IDs are printable
  ASCII. That is the security contract, written in the language least able to
  express it.
- The companion is already Go, and the usage types are currently described
  twice — once in Go, once as a `jq` expression in the quota source.
- "Secrets never in argv" becomes structural rather than disciplinary: Go's
  `exec.Command(name, args...)` has no shell interpolation to forget.
- A single binary removes the `jq` dependency, the bash 3.2 vs. 5 split, and
  most of the installation problem.

**The tests are what makes the direct port safe.** The existing suites are
language-neutral — they drive fake launchers and assert argv and environment
variable *names* from the outside. That makes them a black-box specification
that runs against a Go binary exactly as it runs against bash. They come across
**as the specification** and must run green against the Go core (`tests/run`)
alongside Go-native tests; the bash implementation in dotfiles stays live and
untouched until the coordinated cutover.

The earlier two-phase plan ("extract bash as-is, port later") was replaced on
2026-09-22: with the bash implementation still running in dotfiles, an interim
bash extraction would have shipped a second live copy of code the port
discards — the extraction goes straight to the durable shape instead.

The shim stays `sh` permanently — it only resolves a path and `exec`s.

Where shell remains, target **POSIX `sh`**, not bash. The current code is
already effectively POSIX (4 bashisms in 998 lines), and the bash 3.2 that
macOS ships is where a stranger's silent failure would come from.

## Layout

Implemented (migration step 1):

```
cmd/cc-harness-agents/    the binary: CLI entry point and production wiring
internal/agents/          the routing core — table, probes, selection, exec
internal/testpki/         hermetic TLS fixtures (CA, loopback listeners)
share/models.tsv          <- the table as data
tests/                    run  +  the carried-over upstream suites
Makefile                  build / check / fmt over the Go toolchain
install.sh                POSIX, links bin/* onto PATH
.github/workflows/        check.yml — the same gate on macOS and Linux
bin/                      build output, generated, never committed
```

Planned, still in the dotfiles:

```
bin/          cc-router  claude(shim)  cliproxy-auth
statusline/   segment + provider quota helpers
companion/    Go
docs/         server-setup.md  troubleshooting.md
```

**One entry point with subcommands** — the target shape: `cc-router run|models|
status|statusline|auth|doctor`, with the shim, the statusline and the quota
helpers calling *it* and holding no routing logic of their own. Today "the model
table exists once" is discipline; that would make it structure.

Today: one binary, `cc-harness-agents`, with the `list` / `exec` /
`resolve-model` subcommands. `list` and `exec` are work-system's external
contract and do not move when the entry point does — a later `cc-router` gains
the subcommands, it does not rename these.

**`share/models.tsv` is data.** The table is a tab-separated file read per
invocation, not a string baked in at install time, and
`~/.config/cc-router/models.tsv` replaces it completely without forking. (The
dotfiles bash still carries the table inside the script; that is what the
cutover retires.) Tab is the separator, so the table and the TSV output
contract speak one format.

## What one session can and cannot do

A running process cannot have its environment changed from outside. Four
variables are set once by `exec` and frozen for the session:

```
CLAUDE_CODE_MAX_CONTEXT_TOKENS   grok 500k | kimi 262k | codex rows 372k
ANTHROPIC_DEFAULT_{FABLE,OPUS,SONNET,HAIKU}_MODEL    the tier slots, per provider
CLAUDE_CODE_SUBAGENT_MODEL
```

Reaching every model is *not* the problem — CLIProxyAPI serves all providers
behind one base URL, and the probe already holds the full list. The problem is
that switching provider mid-session leaves the ceiling and the tier slots
pointing at the old one: the session believes it has more context than exists
(no timely auto-compact, then a hard failure mid-task), and every subagent and
haiku-tier call still goes to the previous provider. That is precisely the
silently-wrong-backend class the design rules exist to prevent.

Consequently:

- `cc-router models` lists everything — provider, tier, ceiling, availability.
- In-session model switching is allowed **between rows that share a ceiling
  and tier defaults** — the four Codex rows (Astra/Sol/Terra/Luna) carry one
  ladder at one 372k ceiling and differ only in the primary. That is why
  Astra is exported at 372k although it serves 900k: every rung is one
  `/model` away, and the ceiling is one number per session.
- Switching provider means restarting, and
  [`restore-routed-sessions.md`](restore-routed-sessions.md) is what makes a
  restart cost one command instead of a session.
- A `--mixed` mode may be offered later: ceiling clamped to the minimum across
  providers (262k), tier slots on a neutral model, free switching. Honest about
  costing Grok half its window. Never the default.

## Live updates for running sessions

Frozen per session: the route, the token, the model, the ceiling. There is no
hot-reload for that in any language.

Re-read from disk on every use, because Claude Code spawns them as
subprocesses: the statusline (per render), hook/command/skill scripts, the
quota helpers, and anything typed in a terminal. Note the distinction — the
*contents* of the file a configuration points at are read fresh each time; the
wiring in `settings.json` is not necessarily.

Three levers follow:

1. **Install by symlink, never by copy** — as the dotfiles already do
   (`~/.scripts -> dotfiles/.scripts`). Saving a file is the deploy step.
2. **Push what can be live into the live layer.** A model table read per
   invocation appears immediately in every statusline, quota call and new
   session.
3. **Make the restart cheap instead of avoiding it.** With `claude -c` restoring
   route, profile and exact model, a restart costs a command and no context.

## Improvements over the dotfiles implementation

- **Probe cache with a short TTL.** Every launch currently pays a network probe
  before `exec`. A ~60 s cache removes that from the common path. Fail-closed is
  preserved by caching **successes only, never failures**: an outage is seen at
  once, a working state is not re-proven every time.
- **Surface ceiling drift.** The statusline knows `model.id`; if it disagrees
  with the active profile's ceiling, say so. Cheap once the session sidecar
  exists, and it covers the cross-provider switch described above.
- **`cc-router config`** prints every value with its origin (file / env /
  default); secrets only as set/missing.
- **The statusline ships as a segment, not a statusline.** `cc-router
  statusline` reads the Claude Code JSON on stdin and emits one segment the user
  embeds in their own line; `docs/` carries a complete example. It reads cache
  only, never the network, with a hard timeout and an empty fallback.

## Quality gate

```
make check   # gofmt/vet/staticcheck, shellcheck (-s sh for the POSIX glue,
             # -s bash for the carried-over suite) + shfmt -d, go test -race,
             # tests/run (contract suites against the built binary), then
             # make build
```

One target, same in CI (small Makefile, native Go tooling — per Robert's
2026-09-22 decision). GitHub Actions matrix over macOS and Linux. Tests never
touch the network: fake launchers, assertions on variable *names*, so the suite
runs in CI without any secret.

Keep the existing hand-rolled harness rather than adopting bats: it works, it
has no dependencies, and a stranger runs it with `sh tests/run`.

## Distribution

**Primary: checkout plus symlinks.** `git clone && make build && ./install.sh`,
update via `git pull && make build`. The core is a compiled binary, so building
is part of the install path; `install.sh` links what `make build` produced.
Building needs a Go toolchain — `mise.toml` pins the version the gate runs — and
running the binary needs nothing else. No infrastructure, identical on macOS and
Linux, and — the real argument — it is the same mechanism used for development,
so there is no second install path that goes untested.

**Next: a Homebrew tap.** Nearly free now that the core is a Go binary
(GoReleaser builds, the formula points at the release), and it removes the
toolchain requirement for people who only want to use it.

**Separately: a Claude Code plugin** for the Claude-Code-side pieces only —
statusline segment, `/cc-router:status`, `/cc-router:models`, a diagnostic
skill. It cannot be the install path: the `claude` shim must be on `PATH`
before Claude Code starts.

## Migration from the dotfiles

cc-router becomes the source of truth; the dotfiles become a consumer holding
only `client.env` (host names, SOPS secrets) and a symlink into the checkout.

Order — each step must leave the author working the same day:

1. `cc-harness-agents` — the core; the most tests; everything else calls it.
2. statusline + quota — depend on 1, independently testable.
3. `cliproxy-auth` — largest file, least entangled.
4. companion — rename the module, decide the release path.

Per step: move the file, lift personal values into config, replace the dotfiles
path with a symlink. The symlink makes each step live immediately and reversible
immediately. Once a file has moved it is **deleted** in the dotfiles, never
copied — there is never a second routing implementation.
