# A statusline with the cc-router segment

cc-router does not ship a statusline. It ships one segment — the route marker
and the routed provider's quota — and the session record that lets `claude -c`
resume a routed session on its agent. Both come from the JSON Claude Code
pipes into your statusline command. The segment itself is described in the
README, [The statusline segment](../README.md#the-statusline-segment).

## The script

`~/.claude/statusline.sh`, POSIX `sh` plus `jq`:

```sh
#!/bin/sh
input=$(cat)

# The session record for a routed resume. Detached: the statusline never
# waits for it.
{ printf '%s' "$input" | cc-harness-agents record-session; } </dev/null >/dev/null 2>&1 &

model=$(printf '%s' "$input" | jq -r '.model.display_name // .model.id // empty')
dir=$(printf '%s' "$input" | jq -r '.workspace.current_dir // .cwd // empty')

# Route marker and provider quota; empty for a native session. It reads a
# local cache only and gives up after 500 ms, so it is safe on every render.
router=$(printf '%s' "$input" | cc-harness-agents statusline 2>/dev/null)

# Native sessions: Claude Code's own quota (routed ones leave it empty).
quota=
[ -n "$router" ] || quota=$(printf '%s' "$input" | jq -r '
  [(.rate_limits.five_hour.used_percentage // empty | "5h \(round)%"),
   (.rate_limits.seven_day.used_percentage // empty | "7d \(round)%")]
  | join(" ")')

line="[$model${router:+ $router}${quota:+ $quota}] ${dir##*/}"
printf '%s\n' "$line"
```

`~/.claude/settings.json`:

```json
{
  "statusLine": { "type": "command", "command": "~/.claude/statusline.sh" }
}
```

A routed remote session renders `[gpt-6-sol ⇄ 5h 42% 7d 80%] cc-router`, a
local one `[grok-4.6 ⌂ 7d 12%] cc-router`, a native one
`[Opus 5.5 5h 10% 7d 31%] cc-router`.

## Drawing your own bars

`--json` hands over the values instead of the text, so a statusline can render
bars, colours or time-to-reset itself. Every field is optional; a native
session is `{}`:

```sh
values=$(printf '%s' "$input" | cc-harness-agents statusline --json 2>/dev/null)
route=$(printf '%s' "$values" | jq -r '.route // empty')
session=$(printf '%s' "$values" | jq -r '.five_hour.used_percentage // empty')
weekly=$(printf '%s' "$values" | jq -r '.seven_day.used_percentage // empty')
reset=$(printf '%s' "$values" | jq -r '.seven_day.reset_at // empty')   # epoch seconds
```

Prefer `.rate_limits` when Claude Code filled it, and fall back to these values
when it did not — that is the case for every routed session.

## When the quota is missing

The marker without a quota means no fresh enough cache yet, or the last
refresh failed. Run the refresh by hand in the routed session's environment to
see why — its diagnostics name the failing layer (`401` proxy key, `403`
Access, `502` origin, TLS, a missing variable) and never a secret:

```sh
cc-harness-agents statusline --refresh remote codex
```

The provider is the table row's `cred` column (`codex`, `xai`, …). After a
failure the statusline waits before it tries again (60 s; 30 minutes after a
rejected credential), so delete `${XDG_CACHE_HOME:-~/.cache}/cc-router/quota/<provider>-<route>.failed`
once the cause is fixed if you do not want to wait.
