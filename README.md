# cc-router

Run the Claude Code harness against foreign models — Grok, Kimi, the GPT-5.6
tiers — through your own hosted [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI),
with one flag:

```sh
claude --grok     # grok-4.5
claude --kimi     # kimi-k3
claude --sol      # gpt-5.6-sol
claude            # unchanged: Anthropic
```

Your subscriptions stay on a host you control. The laptop holds no OAuth
credentials, only a revocable per-machine key.

> **Status: early.** The mechanism runs in production for its author, but it
> lives in a private dotfiles repository. This repo exists to extract it into
> something a second person can install. See [`tasks/mission.md`](tasks/mission.md)
> for what still has to move and [`tasks/architecture.md`](tasks/architecture.md)
> for how it is being built.

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
the change is in the wrong shape.

**Fail closed, and say which layer failed.** `403` Access, `401` proxy key,
`502` origin, TLS, missing model — each is a distinct message. A generic "proxy
down" sends people to restart a service that never stopped.

**Secrets never reach argv.** Headers go to `curl` on stdin; diagnostics name
variables, never values.

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
- Claude Code, `bash`, `jq`, `curl`
- optional: Cloudflare Access, or another outer authentication layer

## License

[MIT](LICENSE)
