# Mission: extract cc-router from the dotfiles

## Goal

Turn the working, private CLIProxyAPI client mechanism into a public repository
a second person can install — without inheriting Robert's dotfiles, secrets, or
host names.

## Where it comes from

Everything already runs in production, hosted on Mars and reached as
`llm.gering.dev`. The source of truth today is the private `gering/dotfiles`
repository:

| Source | Role |
|---|---|
| `.scripts/cc-harness-agents` | model table, route probes, environment recipe |
| `.scripts/cliproxy-auth` | server-side OAuth logins, controlled local fallback |
| `.claude/cliproxy-quota-source.sh` + `{codex,grok,kimi}-quota.sh` | statusline quota |
| `services/cliproxy-companion/` | usage and admin facades (Go) |
| `.zshrc` `claude()` | the interactive flag layer |
| `docs/cliproxyapi-remote.md` | operator runbook |
| `.claude/knowledge/services/cliproxy-remote.md` | the pitfalls worth keeping |

Test suites that must come along, because they encode the security contract:
206 routing tests, 309 auth-CLI tests and 180 quota tests, plus the companion's
Go suite with `-race`. Re-count rather than trust these numbers — they were read
off `gering/dotfiles` at `f0effff`, and the routing suite grows whenever the listing
contract does.

## What has to change on the way out

- [ ] **Remove every personal constant.** `llm.gering.dev`, `mars`,
      `/volume1/docker/...`, the `MACBOOK` profile, port `18321`. All of it
      becomes configuration with documented defaults.
- [ ] **Name the config surface.** One file (`~/.config/cc-router/config.env`?)
      instead of today's mix of `client.env`, SOPS-provided variables and
      environment overrides. Decide what a fresh install needs to fill in.
- [ ] **Decouple secret storage.** The dotfiles use SOPS/age. A stranger will
      not. Define the contract as "these variables must be in the environment"
      and ship one worked example, rather than a dependency.
- [ ] **Keep the model table editable.** It is the thing users will change
      first. It must be data, not code, and it must survive updates.
- [ ] **Ship the companion.** Source plus a build; decide whether a prebuilt
      binary or an image is published, and how a user pins it.
- [x] **Installation.** Decided: `git clone` plus `install.sh` symlinks; a
      Homebrew tap once the core is a Go binary; a Claude Code plugin for the
      Claude-Code-side pieces only. See [`architecture.md`](architecture.md).
- [ ] **Server-side setup guide.** CLIProxyAPI, the reverse-proxy route, the
      block on `/v0/management*` and `/admin*`, and the outer auth layer —
      written for someone whose host is not a Synology.
- [x] **Decide the license.** MIT.
- [x] **Decide the audience.** Personal machines only. The README says so, and
      says plainly that sharing a subscription violates provider terms.

## Deliberately out of scope

- Hosting CLIProxyAPI for anyone. This is a client plus two narrow facades.
- Sharing subscriptions with third parties. Provider terms prohibit it; the
  documentation should say so plainly and point to per-user API keys instead.

## Non-negotiables

Carried over from the review rounds that produced the current implementation.
Each of these fixed a real, reproducible defect:

1. No automatic remote→local fallback. Parallel refresh of the same OAuth
   credentials ends in `invalid_grant`.
2. No plaintext-HTTP fallback and never `curl -k`. TLS is the only thing that
   authenticates the gateway; the local route is honest about being weaker.
3. Secrets never in argv, never in logs, never in error messages.
4. The generic management API is never published, and the admin facade stays
   reachable only through a tunnel with its own key.
5. Usage responses carry no tokens, filenames, or account e-mail addresses.
6. Validate strictly what you consume, but ignore provider fields you do not
   render — an exact key set blanked the Codex bars once already.
7. The statusline never waits on the network.

## Open questions

Layout, language, distribution and the plugin question are settled in
[`architecture.md`](architecture.md). What remains:

1. How does a user onboard a second machine — documented steps, or a command?
2. Does the companion ship as a prebuilt binary, an image, or source only —
   and how does a user pin it?
