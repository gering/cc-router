> Archived 2026-10-19 · PR #14 (merged @ 6b2222e) · task/list-note-and-doc-pointers

# List note and doc pointers

## Goal

Two small corrections after stages 2a/2b.

1. **Issue #12.** `cc-harness-agents list` shows `auto-selected gpt-6.1-sol …`
   on the terra, luna and astra rows, although their model column is their own
   primary. What moved is the shared ladder's opus rung. The note must describe
   what moved for **that row**: no note when the row's primary did not move,
   and a moved rung named as a rung (e.g. `opus rung auto-selected
   gpt-6.1-sol`) if it is worth showing at all — pick the simpler form that is
   still unambiguous. The note is generated in `internal/agents/selection.go`
   (discovery from #9). `Fixes #12`.
2. **Stale pointers.** `tasks/restore-routed-sessions.md` was archived to
   `tasks/archive/restore-routed-sessions.md` when PR #13 closed. `CLAUDE.md`
   ("the first feature spec") and any other live reference (README,
   `tasks/architecture.md`, `docs/`) still point at the old path or describe
   the wrapper/shim/statusline as "still in dotfiles". Update them to the
   current state: wrapper, resume bridge, `launch`, `record-session` and the
   shim live here; statusline, quota helpers, `cliproxy-auth` and the
   companion still live in dotfiles.

## Constraints

- The `list` TSV contract (exactly 4 columns) is unchanged; only note text.
- Keep `CLAUDE.md` terse — it is loaded into every session.
- DRY, KISS, elegance.

## Acceptance

- [ ] Test with a fake catalog where only `gpt-6.1-sol` is new: the sol row
      notes its auto-selection; terra/luna/astra carry no misleading
      `auto-selected gpt-6.1-sol`.
- [ ] No live doc points at `tasks/restore-routed-sessions.md`.
- [ ] `make check` green; `tests/migration-coverage.md` updated if a
      contract assertion changes.
