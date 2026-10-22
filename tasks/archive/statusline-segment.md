> Archived 2026-10-22 · PR #22 (merged @ 10a740c) · task/statusline-segment

# Statusline segment with routing marker and provider quota

Migration step 2 (`tasks/architecture.md`): the statusline's routing part moves
from `~/dotfiles` into the Go core. Robert approved defining it 2026-10-21; the
dotfiles Manager confirmed nothing in flight touches the files.

## Goal

`cc-harness-agents statusline` reads Claude Code's statusline JSON on stdin and
prints the **routing segment** a user embeds in their own statusline. cc-router
ships a segment, never a whole statusline (architecture.md, "The statusline
ships as a segment"). It replaces, in dotfiles:

- the proxy marker in `.claude/statusline.sh` (`CLIPROXY_ROUTE` remote `⇄`,
  local `⌂`, from dotfiles #71);
- the quota fallback for routed sessions, where Claude Code leaves
  `.rate_limits` empty: `.claude/cliproxy-quota-source.sh` plus
  `{codex,grok,kimi}-quota.sh`, specified by `scripts/test-cliproxy-quota.sh`
  (read them at the current dotfiles HEAD before starting — never from notes).

Everything personal stays in dotfiles and is out of scope: model colour
gradient (#68), mail-watch state (`[@ …]`), `[ws …]`, versions, git.
`record-session` is unchanged.

## Shape

- **Two outputs, two real users.** Default: a ready-rendered segment (marker
  plus quota) for the README example. `--json`: the values
  (`route`, `five_hour`/`seven_day` `used_percentage` and `reset_at`) for
  dotfiles' `statusline.sh`, which renders its own bars. Nothing else.
- **Provider comes from the table**, not from model-name globs: the active
  model resolves to a row the way `resolve-model` does, and the row's `cred`
  column (`codex`, `xai`) is the usage-API provider. Native and unknown models →
  marker only, no quota. A provider the usage API does not serve (e.g. the
  `opus` row's `claude`) → no quota, never an error.
- **Render path: cache only, never the network.** Bounded local I/O, hard
  timeout, empty segment on any doubt. A stale cache triggers a detached
  refresh (same pattern as `record-session`), guarded by a lock. Carry over the
  dotfiles timings as documented defaults: TTL 300 s, max age 1800 s, failure
  backoff 60 s, stale lock 120 s, body cap 1 MiB. Cache under
  `${XDG_CACHE_HOME:-~/.cache}/cc-router/quota/`, `0700`/`0600`, temp file +
  rename; per provider and route.
- **Remote refresh:** `GET <usage base>/usage/v1/<provider>` with the two
  per-machine credentials built in memory (reuse the core's header code; never
  argv). The base URL is configuration with a documented default — the dotfiles
  hardcode `https://llm.gering.dev`; that constant must not come across.
- **Local refresh:** the provider's own usage endpoint with the local proxy's
  credentials, as the dotfiles helpers do today (codex `wham/usage`, grok
  billing). Port only what a table row can reach.
- **Validate strictly what you render, ignore the rest** (the exact-key bug
  that once blanked the Codex bars). Map windows by length (≥6 d weekly, ≤6 h
  session), as the dotfiles parser does.

## Hard constraints

- **Never send a keyless or bad-key request to the gateway.** CLIProxy bans an
  IP for 30 min after 5 failed auths. Missing or malformed credentials → no
  request, no quota, a diagnostic naming the variable.
- **Tests stay offline**: `httptest`/stubs only, no live gateway. The gering.dev
  WAF challenges non-EU traffic, so CI could not reach it anyway.
- Secrets never in argv, logs, cache files or error messages; tests assert
  variable names.
- Fail closed and name the failing layer in diagnostics (`403` Access, `401`
  key, `502` origin, TLS) — on stderr of the refresh, never in the segment.

## kimi (Robert, 2026-10-21: may come back)

cc-router's table has no kimi row today, so no routed session reaches
`kimi-quota.sh`. Build so that kimi's return is a table row, not a redesign:

- The remote path is provider-generic (`/usage/v1/<cred>`): a future kimi row
  gets remote quota with no code change. Cover that with a test row.
- The local path needs one parser per provider API. Port codex and grok now;
  leave kimi's local parser out, but keep the per-provider shape so adding it
  is one small entry. Record the kimi assertions of `test-cliproxy-quota.sh`
  as "not ported, pending a kimi row" in `tests/migration-coverage.md`, so
  they are not lost when dotfiles deletes the test.

## Acceptance

- [ ] `statusline` segment and `--json` covered by Go tests; the quota
      assertions of `test-cliproxy-quota.sh` ported as the specification
      (cache/TTL/backoff/lock, strict parsing, no keyless probe, no secret
      leak); `tests/migration-coverage.md` lists them.
- [ ] Render path proven network-free (test fails if it dials).
- [ ] README: segment usage, `--json` fields, config keys and defaults, a
      complete statusline example in `docs/`.
- [ ] `make check` green.
- [ ] After merge (Manager): live check in a routed session, then the dotfiles
      Manager switches `statusline.sh` to the segment and deletes the helpers
      and the test the same day (needs Robert's OK on that merge).
