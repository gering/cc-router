# cc-router

Run the Claude Code harness against foreign models — Grok, the GPT
tiers, or Claude itself through the gateway's own login — through your own
hosted [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI), with one
flag:

```sh
claude --proxy grok        # the newest canonical Grok the gateway offers
claude --proxy sol         # gpt-6-sol
claude --proxy opus        # Claude Opus through the gateway's Claude login
claude --local sol         # the same agent on the machine-local gateway
claude                     # unchanged: Anthropic
claude -c --noproxy        # resume a routed session natively
```

Your subscriptions stay on a host you control. The laptop holds no OAuth
credentials, only a revocable per-machine key.

> **Status: early.** The routing core, `cc-harness-agents` (Go), the
> interactive `claude` wrapper, the executable shim and session resume are
> here. The statusline, its quota helpers, the login tooling (`cliproxy-auth`)
> and the companion still live in the author's dotfiles and move next — see
> [`tasks/mission.md`](tasks/mission.md) and [`tasks/architecture.md`](tasks/architecture.md).

## Install

```sh
git clone https://github.com/gering/cc-router && cd cc-router
make build        # needs Go (version in go.mod; mise.toml pins it for mise users)
./install.sh      # links bin/* into ~/.local/bin (CC_ROUTER_BIN_DIR=… to change)
```

For the `claude --proxy` flags, source the wrapper from your `~/.zshrc`:

```sh
source /path/to/cc-router/share/claude.zsh
```

For callers that never source it — a process manager, another shell,
anything that runs `claude` directly — put the shim's directory **ahead of** the
native Claude Code launcher on `PATH`:

```sh
export PATH="/path/to/cc-router/bin/shim:$PATH"
```

The shim finds the native launcher at `CC_ROUTER_CLAUDE` (default
`~/.local/bin/claude`, the native installer's), never on `PATH`. Nothing of
the Claude Code installation is replaced or modified.

The links point into the checkout: `git pull && make build` updates in place,
and `share/models.tsv` is read on every invocation. Uninstall by removing the
links, the `PATH` entry and the `source` line. The binary has no runtime
dependencies; the resume bridge (`cc-harness-resume`) needs `python3`, the
wrapper `zsh`.

## Usage

```sh
cc-harness-agents list [--local] [--header|--no-header]
cc-harness-agents exec [--local] <agent> [--] <command…>
cc-harness-agents names
cc-harness-agents resolve-model <model-id>
cc-harness-agents serves <model-id>
cc-harness-agents launch [--replay-to <file>] -- <claude args…>
cc-harness-agents record-session [--rendered-at <unix-ms>] < statusline.json
cc-harness-agents statusline [--json] < statusline.json
cc-harness-agents statusline --refresh <remote|local> <provider>
```

`exec sol -- claude` probes the route, sets the routing environment and
**replaces itself** with `claude` (same PID — no wrapper stays in the process
tree). The target receives the route's key: `exec` is secret-bearing by design,
so the boundary is who may run it. `list` prints one row per agent — raw TSV
(`name model available note`, exactly four columns) to a pipe, a padded table
to a terminal. `names` prints the agent names, one per line, without network
access — the values `claude --proxy`/`--local` accept. `resolve-model` maps a recorded model
id back to its agent with no network access. `serves` asks the remote route
whether it still offers a model id. `launch` is what `claude` runs (the wrapper
and the shim both call it): it parses the flags below, restores a resumed
session's routing and execs the native launcher. `record-session` reads Claude
Code's statusline JSON on stdin and writes the session's record. `statusline`
prints the routing segment for your own statusline — see
[The statusline segment](#the-statusline-segment).

Exit codes: `1` unavailable, `2` usage or unknown agent, `3` not configured,
`4` (`serves` only) the route answered without the model, `126` the target is
not executable, `127` no such target; after a successful `exec` or `launch`
the status is the target's. `launch` answers a command line it refuses
(conflicting model flags, `--proxy`/`--local` without an agent, two resume
selectors)
with `1` and a `claude:` message, as the wrapper always did.

These are external contracts: work-system discovers the helper on `PATH` and
parses `list`; the wrapper and the shim call `launch`, the statusline
`record-session` and `statusline`, the resume bridge `resolve-model`.

**The `claude` flags.** `claude --proxy <agent>` routes through `exec` on the
remote route, `claude --local <agent>` on the local one, for every row of the
effective table (`cc-harness-agents names`). The value may also be a model id:
it routes on the agent that serves it, pinned to exactly that model
(`--proxy claude-sonnet-5-5`, `--proxy gpt-6.1-sol`). `--model` is Claude's own
flag and always native; `--noproxy` launches or resumes natively with your own
login, combines with `--model`, and on a resume without one replaces the
session's stored model with the account default. `--yolo` is
`--dangerously-skip-permissions`. Conflicting choices are an error, never last
one wins. The earlier spellings — `--<agent>` and a bare `--local` beside one —
still route during the transition and print a deprecation notice naming the
new form. The `--fable/--opus/--sonnet/--haiku` shorthands are deprecated too
and keep their native meaning (`--opus` is `--model opus`, never the opus row —
that is `--proxy opus`).
`claude -c` and `claude --resume <id>` restore the agent, route and
exact model the session last ran on — from the session's record when the
statusline wrote one, else from its transcript (`cc-harness-resume`). When that
model maps to no agent, the session resumes on the Anthropic default with a
notice. When the gateway no longer serves it, the resume fails and names the
way back for that session: `claude --resume <id> --proxy <agent>` for the
agent's current model, or `claude --resume <id> --noproxy` for Anthropic. A route that fails (`403`, `401`, `502`,
TLS) stays an error, and an explicit model flag always wins over the
restore. An Anthropic (`claude-*`) model id resumes natively unless the
session's record names its agent: the opus row never claims a native session
from its transcript alone. Precedence, the record's location, troubleshooting and how to turn it
off: [`docs/resume.md`](docs/resume.md).
A `/model` switch inside a routed session is saved by Claude Code as the
default for new sessions. A native start that would use it — plain `claude` or
`--noproxy` with nothing else settling the model or the endpoint — starts on
the Anthropic default instead when that saved id is one the table routes, and
says which settings file holds it; the file itself is never touched. What
counts as settled, and which settings files are read:
[`docs/resume.md`](docs/resume.md#a-foreign-default-model).

### The statusline segment

cc-router ships a **segment**, not a statusline: one short piece you embed in
your own. `cc-harness-agents statusline` reads Claude Code's statusline JSON on
stdin and prints, for a routed session, the route marker — `⇄` remote, `⌂`
local — followed by the provider's quota as *used* percentages:

```
⇄ 5h 42% 7d 80%
```

Claude Code fills `.rate_limits` only from Anthropic's own responses, so a
routed session would otherwise show no quota at all. A native session prints
nothing; a model no table row serves, or a provider without quota, prints the
marker alone. `--json` prints the values instead, for a statusline that draws
its own bars — every field optional, a native session is `{}`:

```json
{"route":"remote","five_hour":{"used_percentage":42,"reset_at":1786017600},"seven_day":{"used_percentage":80}}
```

`reset_at` is epoch seconds, absent when the source names no reset. A complete
example statusline: [`docs/statusline.md`](docs/statusline.md).

**It never waits on the network.** The render reads a local cache only, gives
up after 500 ms with an empty segment, and exits `0` whatever happens. A stale
cache starts a detached refresh (`statusline --refresh <route> <provider>`,
also runnable by hand to see why a quota is missing — its diagnostics go to its
stderr, never into the segment). The provider is the table row's `cred`
column, found from the session's model the way `resolve-model` finds it.

- **remote**: `GET <CC_ROUTER_USAGE_URL>/usage/v1/<cred>` with the same two
  per-machine credentials as the gateway. Any row's provider is asked, so a
  new row needs no code. Missing or malformed credentials send nothing.
- **local**: the provider's own usage endpoint with the newest usable
  credential from `CC_ROUTER_PROXY_DIR` (`<cred>-*.json`), then the provider
  CLI's own login — `codex` (`~/.codex/auth.json`) and `xai`
  (`~/.grok/auth.json`) today. Other providers get no local quota.

| Timing | Value |
|---|---|
| a cache younger than this is not refreshed | 300 s |
| usage data older than this is not shown | 1800 s (and at most 300 s in the future) |
| after a failed refresh, no retry for | 60 s |
| after `401`/`403`/`404`, no retry for | 1800 s — CLIProxyAPI bans an address after five failed authentications in 30 minutes |
| response body cap; connect / total timeout | 1 MiB; 3 s / 8 s |

The cache lives in `${XDG_CACHE_HOME:-~/.cache}/cc-router/quota/`
(`<cred>-<route>.json`, `.failed`, `.lock`; directory `0700`, files `0600`,
written by temp file and rename). One refresh runs at a time per provider and
route; its lock is an `flock` the kernel releases when the holder exits, so a
crashed refresh never leaves a stale lock.

## Configuration

Non-secret settings resolve per key: **environment > `~/.config/cc-router/config.env`
> default.** A variable that is set wins even when empty — and an empty value
is then rejected, so unset it rather than blank it. The file holds `KEY=VALUE`
lines and `#` comments; it is parsed as data, never sourced, and unknown or
duplicate keys are errors.

| Key | Default | Meaning |
|---|---|---|
| `CC_ROUTER_REMOTE_URL` | — (required for the remote route) | `https://` gateway base URL |
| `CC_ROUTER_USAGE_URL` | `CC_ROUTER_REMOTE_URL` | `https://` base of the read-only usage API (`/usage/v1/<provider>`) |
| `CLIPROXY_PROFILE` | from `~/.config/cliproxy/client.env` | selects the secret variables below |
| `CC_ROUTER_LOCAL_HOST` | `127.0.0.1` | loopback literals only (`127.0.0.1`, `::1`) |
| `CC_ROUTER_LOCAL_PORT` | `8317` | local CLIProxyAPI |
| `CC_ROUTER_PROXY_DIR` | `~/.cli-proxy-api` | local token and provider credentials |
| `CC_ROUTER_LOCAL_MARKER` | `~/.cache/cliproxy-auth/local-ready.json` | the local-fallback window marker |
| `CC_ROUTER_LOCAL_PREPARE_HINT` | `run: cliproxy-auth prepare-local` | what the local refusal tells you to run |

Secrets are never configuration — they must be in the environment:
`CLIPROXY_API_KEY_<PROFILE>`, `CLIPROXY_CF_ACCESS_<PROFILE>_CLIENT_ID`,
`CLIPROXY_CF_ACCESS_<PROFILE>_CLIENT_SECRET`. How they get there (a password
manager, SOPS, a keychain) is yours to choose.

**Models.** `share/models.tsv` is the agent table: one tab-separated row per
agent (name, model, fable/opus/sonnet/haiku tiers, the session's context
ceiling, credential prefix, login flag, provider). `~/.config/cc-router/models.tsv`, when present,
**replaces** it entirely — copy the packaged file and edit it; new packaged
rows then need merging by hand. An invalid table is refused, never partially
used. Catalog discovery (Grok releases, Codex minors per role) and the
verified context windows are policy in the code, not table data.
`CC_HARNESS_MODEL_<AGENT>=<id>` pins one agent's primary to a model; the other
roles keep discovering.

## Why

Claude Code speaks the Anthropic API. CLIProxyAPI translates that to OpenAI,
xAI and others, so one harness can drive any of them — but only if something sets
the right base URL, auth token, model, tier defaults and context ceiling for
each provider. Doing that by hand per session is how you end up in a silently
wrong backend.

`cc-router` is that something, plus the parts you need once the proxy is not on
localhost anymore: authentication that can be revoked per machine, OAuth logins
that run where the tokens live, and quota that reaches the statusline without
blocking it.

## Architecture

```
claude --proxy sol
     │
     ▼
cc-harness-agents ──── probes the route, sets the environment, execs claude
     │
     │  Cloudflare Access (service token)  +  per-machine proxy key
     ▼
https://llm.example.dev
     │
     ▼
┌─────────────────────────────────────────────────┐
│ your host                                        │
│                                                  │
│  CLIProxyAPI  :8317   the LLM API                │
│  companion    :8320   GET /usage/v1  (read-only) │
│  companion    :8321   /admin/v1      (SSH only)  │
└─────────────────────────────────────────────────┘
```

Every public request carries two independent credentials, and both are per
machine — so one laptop can be cut off without touching the others:

| Layer | Rejects with |
|---|---|
| Cloudflare Access service token | `403` |
| Proxy key (`api-keys`) | `401` |

The generic CLIProxyAPI management API is never published. A narrow companion
reaches it over the container loopback and exposes only fixed operations:
normalized quota, and start/poll/cancel for a provider login.

## Components

| Piece | Does |
|---|---|
| `cc-harness-agents` | Owns the model table, the route probes and the environment recipe. Everything else calls it. |
| `claude()` wrapper | Interactive layer only: flag parsing, conflict checks, resume restore and hint. Its flags come from the table. |
| `cliproxy-auth` | Tunnels to the private admin API; runs provider OAuth logins on the host that keeps the tokens. |
| statusline segment | `cc-harness-agents statusline`: route marker and provider quota from a local cache, refreshed in the background from the read-only usage API. |
| companion | The two narrow facades in front of the management API. |

## Design rules

These are the decisions worth keeping when the code moves here.

**Remote is the default; local is explicit and gated.** The host refreshes the
OAuth tokens. A second proxy on the same credentials races those refreshes into
`invalid_grant`, so there is no automatic fallback — `--local <agent>` says it out loud,
and it only works inside a prepared window.

**A new model is a table row, never a code path.** Provider, tiers and the real
context ceiling live in one line of a table. If a sixth model needs a branch,
the change is in the wrong shape. What is policy, not data: which newer
releases discovery may pick, their verified windows, and which superseded ids
stay resumable on their former row — a pinned or discovered id outside the
verified list gets its verified major's window (Codex minors) or a conservative
ceiling, and says so.

**Fail closed, and say which layer failed.** `403` Access, `401` proxy key,
`502` origin, TLS, missing model — each is a distinct message. A generic "proxy
down" sends people to restart a service that never stopped.

**Secrets never reach argv.** Headers are built in memory, no subprocess sees
them; diagnostics name variables, never values.

**Two output shapes.** A terminal gets a padded table; a pipe gets the raw TSV
contract, so a consumer's fields never shift.

## Scope and provider terms

This is a client for **your own** provider accounts on **your own** host. It
does not host anything for anyone, and it is not a way to share a subscription:
every provider's terms tie a subscription to one person, so giving a friend a
proxy key violates them. If you want a second person on your gateway, give them
their own per-user API key with the provider — that is what the key layer is
for.

One session speaks to one provider. Within a provider you can switch models
freely (`/model gpt-5.6-terra` in a `--proxy sol` session), because the context
ceiling and the tier defaults are shared. Across providers they are not, so
switching provider means starting a session — the resume path exists to make
that cost one command.

## Requirements

- CLIProxyAPI on a host you control
- an HTTPS ingress with a valid certificate — TLS is what authenticates the
  gateway, and there is deliberately no plaintext fallback
- Claude Code; Go to build `cc-harness-agents` (no runtime dependencies);
  `zsh` for the wrapper and `python3` for the resume bridge
- Cloudflare Access in front of the gateway: the remote route sends a service
  token on every request and refuses to run without the two
  `CLIPROXY_CF_ACCESS_<PROFILE>_*` variables

## Development

`make check` is the one gate, locally and in CI (macOS + Linux): gofmt, `go
vet`, staticcheck, shellcheck/shfmt, `go test -race`, `tests/run` — the
contract suite carried over from the bash implementation, running against the
built binary through a local HTTPS test gateway, plus the wrapper and resume
suites (zsh, python3) — and finally `make build`, so
the gate never passes on something that does not build. `make fmt` formats.
[`tests/migration-coverage.md`](tests/migration-coverage.md) maps every
upstream assertion to its place here.

## License

[MIT](LICENSE)
