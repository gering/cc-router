# Remove the kimi row

## Goal

Robert's Kimi subscription has ended (confirmed 2026-10-02). Remove the `kimi`
agent so `list` and the work-system picker stop offering a route that cannot
work. This is independent of the other tasks and can go first.

## Requirements

- [ ] Delete the `kimi` row from `share/models.tsv`.
- [ ] Update tests and fixtures that assume six agents.
- [ ] `tests/migration-coverage.md`: record which kimi cases are retired, and why.
- [ ] README and docs: drop Kimi from the examples, or mark it as an example of
      a provider row.
- [ ] `make check` green; live `list` shows five agents.

## Coordination

- The dotfiles wrapper `.zsh/functions/claude.zsh` still accepts `--kimi`
  (line 149). Without the row, `--kimi` turns from "unavailable" into an
  unknown-agent error. Ask the dotfiles Manager to drop `--kimi` from the
  wrapper in step, and land both changes together.
- Old kimi sessions on resume: `resolve-model` no longer maps them, so the
  bridge's existing fallback applies. Do not add special handling.
- Resubscribing later means adding the row back (or using a user override in
  `~/.config/cc-router/models.tsv`).
