# cc-router

Run the Claude Code harness against foreign models — Grok, Kimi, the GPT-5.6
tiers — through your own hosted [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI),
with one flag:

```sh
claude --grok     # the newest canonical Grok the gateway offers
claude --kimi     # kimi-k3
claude --sol      # gpt-5.6-sol
claude            # unchanged: Anthropic
```

Your subscriptions stay on a host you control. The laptop holds no OAuth
credentials, only a revocable per-machine key.

> **Status: early.** The routing core, `cc-harness-agents`, is here (Go). The
> interactive `claude --grok` flags, the statusline and the login tooling still
> live in the author's dotfiles and move next — see [`tasks/mission.md`](tasks/mission.md)
> and [`tasks/architecture.md`](tasks/architecture.md). Until then, launch
> routed sessions with `cc-harness-agents exec` directly.

## Install

```sh
git clone https://github.com/gering/cc-router && cd cc-router
make build        # needs Go (version in go.mod; mise.toml pins it for mise users)
./install.sh      # links bin/* into ~/.local/bin (CC_ROUTER_BIN_DIR=… to change)
```

The links point into the checkout: `git pull && make build` updates in place,
and `share/models.tsv` is read on every invocation. Uninstall by removing the
links. The binary has no runtime dependencies.

## Usage

```sh
cc-harness-agents list [--local] [--header|--no-header]
cc-harness-agents exec [--local] <agent> [--] <command…>
cc-harness-agents resolve-model <model-id>
```

`exec sol -- claude` probes the route, sets the routing environment and
**replaces itself** with `claude` (same PID — no wrapper stays in the process
tree). The target receives the route's key: `exec` is secret-bearing by design,
so the boundary is who may run it. `list` prints one row per agent — raw TSV
(`name model available note`, exactly four columns) to a pipe, a padded table
to a terminal. `resolve-model` maps a recorded model id back to its agent with
no network access. Exit codes: `1` unavailable, `2` usage or unknown agent,
`3` not configured, `126` the target is not executable, `127` no such target;
after a successful `exec` the status is the target's.

These are external contracts: work-system discovers the helper on `PATH` and
parses `list`; the resume bridge calls `resolve-model`.

## Configuration

Non-secret settings resolve per key: **environment > `~/.config/cc-router/config.env`
> default.** A variable that is set wins even when empty — and an empty value
is then rejected, so unset it rather than blank it. The file holds `KEY=VALUE`
lines and `#` comments; it is parsed as data, never sourced, and unknown or
duplicate keys are errors.

| Key | Default | Meaning |
|---|---|---|
| `CC_ROUTER_REMOTE_URL` | — (required for the remote route) | `https://` gateway base URL |
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
used. Grok discovery and the verified context windows are policy in the code,
not table data. `CC_HARNESS_MODEL_<AGENT>=<id>` pins one agent to a model.

## Why

Claude Code speaks the Anthropic API. CLIProxyAPI translates that to OpenAI,
xAI and Kimi, so one harness can drive any of them — but only if something sets
the right base URL, auth token, model, tier defaults and context ceiling for
each provider. Doing that by hand per session is how you end up in a silently
wrong backend.

`cc-router` is that something, plus the parts you need once the proxy is not on
localhost anymore: authentication that can be revoked per machine, OAuth logins
that run where the tokens live, and quota that reaches the statusline without
blocking it.

## Architecture

```
claude --sol
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
| `claude()` wrapper | Interactive layer only: flag parsing, conflict checks, resume hint. |
| `cliproxy-auth` | Tunnels to the private admin API; runs provider OAuth logins on the host that keeps the tokens. |
| quota helpers | Feed the statusline from the read-only usage API, always without blocking it. |
| companion | The two narrow facades in front of the management API. |

## Design rules

These are the decisions worth keeping when the code moves here.

**Remote is the default; local is explicit and gated.** The host refreshes the
OAuth tokens. A second proxy on the same credentials races those refreshes into
`invalid_grant`, so there is no automatic fallback — `--local` says it out loud,
and it only works inside a prepared window.

**A new model is a table row, never a code path.** Provider, tiers and the real
context ceiling live in one line of a table. If a sixth model needs a branch,
the change is in the wrong shape. What is policy, not data: which newer
releases discovery may pick, and their verified windows — a pinned or
discovered id outside that list gets a conservative ceiling and says so.

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
freely (`/model gpt-5.6-terra` in a `--sol` session), because the context
ceiling and the tier defaults are shared. Across providers they are not, so
switching provider means starting a session — the resume path exists to make
that cost one command.

## Requirements

- CLIProxyAPI on a host you control
- an HTTPS ingress with a valid certificate — TLS is what authenticates the
  gateway, and there is deliberately no plaintext fallback
- Claude Code; Go to build `cc-harness-agents` (no runtime dependencies)
- Cloudflare Access in front of the gateway: the remote route sends a service
  token on every request and refuses to run without the two
  `CLIPROXY_CF_ACCESS_<PROFILE>_*` variables

## Development

`make check` is the one gate, locally and in CI (macOS + Linux): gofmt, `go
vet`, staticcheck, shellcheck/shfmt, `go test -race`, `tests/run` — the
contract suite carried over from the bash implementation, running against the
built binary through a local HTTPS test gateway — and finally `make build`, so
the gate never passes on something that does not build. `make fmt` formats.
[`tests/migration-coverage.md`](tests/migration-coverage.md) maps every
upstream assertion to its place here.

## License

[MIT](LICENSE)
