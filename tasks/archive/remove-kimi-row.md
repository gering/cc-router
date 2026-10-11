> Archived 2026-10-11 · PR #8 (merged @ b99f8c3) · task/remove-kimi-row

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

## Decisions (Robert, 2026-10-02)

- **Historical kimi resumes fall back to the Anthropic default with a short
  notice.** That is the bridge's existing behavior for unmapped models.
  `resolve-model` simply stops mapping kimi ids; add no retained entry and no
  special case.
- **This task changes the dotfiles side too, via a dotfiles PR** that lands in
  step with the cc-router PR. The dotfiles Manager reviews it; Robert merges
  (or approves the merge).

## Coordination

- dotfiles changes in that PR:
  - `.zsh/functions/claude.zsh:149`: drop `--kimi`.
  - `scripts/test-cc-harness-resume.py:87-90` and `:212-213`: these expect a
    kimi mapping/override. Turn them into "kimi id → Anthropic fallback with
    notice". Around line 147, kimi appears only as an older transcript message,
    so check whether it still holds.
- Order: merge the cc-router PR, run `git pull && make build` in the main
  checkout, then merge the dotfiles PR right after, so `--kimi` and the row
  disappear together.
- Resubscribing later means adding the row back (or using a user override in
  `~/.config/cc-router/models.tsv`).
- Window between the core build and the dotfiles merge: wrappers in shells that
  are already open may still accept `--kimi` and get an unknown-agent error.
  That is acceptable for this short window and needs no special case.
- Stop/rollback: if the dotfiles PR cannot be merged right after the core
  build, stop and revert the cc-router merge (`git revert` plus rebuild in the
  main checkout). Never leave the row removed for long while the wrapper still
  offers `--kimi`. dotfiles master, with its foreign uncommitted changes, stays
  untouched; dotfiles work happens only in a separate worktree.
