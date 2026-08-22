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

## Language: bash now, Go later, tests as the bridge

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

**But not as a rewrite.** The existing suites are already language-neutral —
they drive fake launchers and assert argv and environment variable *names* from
the outside. That makes them a black-box specification that runs against a Go
binary exactly as it runs against bash, which turns a later port from a risk
into a refactor.

Therefore, three phases:

1. **Extract bash as-is.** Depersonalise, move the table to `share/`, add
   `make check` and CI. The repository becomes installable; daily work never
   stops.
2. **Harden the tests.** They are the contract, not the implementation.
3. **Port component by component to Go**, each against the same suite. Order by
   validation density: `cliproxy-auth` and the probe/header path first.

> **Do not invest in bash that phase 3 discards.** No cosmetic refactor of
> `cliproxy-auth`, no split into a dozen `lib/` files. Phase 1 depersonalises
> and ships; that is all.

The shim stays `sh` permanently — it only resolves a path and `exec`s.

Where shell remains, target **POSIX `sh`**, not bash. The current code is
already effectively POSIX (4 bashisms in 998 lines), and the bash 3.2 that
macOS ships is where a stranger's silent failure would come from.

## Layout

```
bin/          cc-router  claude(shim)  cliproxy-auth
lib/          table.sh probe.sh env.sh diag.sh route.sh
share/        models.tsv          <- the table as data
statusline/   segment + provider quota helpers
companion/    Go
tests/        run  +  test-*.sh
docs/         server-setup.md  troubleshooting.md
install.sh
```

**One entry point with subcommands:** `cc-router run|models|status|statusline|
auth|doctor`. The shim, the statusline and the quota helpers call *it* and hold
no routing logic of their own. Today "the model table exists once" is
discipline; this makes it structure.

**`share/models.tsv` is data.** Today the table is a string inside the script.
As a file it survives `git pull`, a user can override it from
`~/.config/cc-router/models.tsv` without forking, and it is read per invocation
rather than baked in at install time. Switch the `|` separator to tab so the
table and the TSV output contract speak one format.

## What one session can and cannot do

A running process cannot have its environment changed from outside. Four
variables are set once by `exec` and frozen for the session:

```
CLAUDE_CODE_MAX_CONTEXT_TOKENS   grok 500k | kimi 262k | gpt 372k
ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU}_MODEL    the tier slots, per provider
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
- In-session model switching is allowed **within a provider**. Sol/Terra/Luna
  share one ceiling and one set of tier defaults; that is why the table carries
  three Codex rows with identical values.
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
make check   # shellcheck -s sh + shfmt -d + tests/run + go vet + go test -race
```

One target, same in CI. GitHub Actions matrix over macOS and Linux — the only
reason CI exists here is that the author's machine has bash 3.2 and a stranger's
does not. Tests never touch the network: fake launchers, assertions on variable
*names*, so the suite runs in CI without any secret.

Keep the existing hand-rolled harness rather than adopting bats: it works, it
has no dependencies, and a stranger runs it with `sh tests/run`.

## Distribution

**Primary: checkout plus symlinks.** `git clone && ./install.sh`, update via
`git pull`. No infrastructure, identical on macOS and Linux, and — the real
argument — it is the same mechanism used for development, so there is no second
install path that goes untested.

**Later: a Homebrew tap.** Nearly free once the core is a Go binary
(GoReleaser builds, the formula points at the release). Maintaining a formula
for shell scripts first would gain nothing the symlink does not.

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
