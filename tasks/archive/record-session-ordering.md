> Archived 2026-10-19 · PR #16 (merged @ e4b0ddf) · task/record-session-ordering

# Record-session ordering

## Goal

Fix issue #15. The dotfiles statusline runs `cc-harness-agents record-session`
detached on every render, so several writers for one session can run at once.
`writeAtomic` (`internal/agents/session.go`) makes each write atomic, but the
last rename wins. A slow writer from an older render (e.g. still
`gpt-6.1-sol`) can replace a newer record (`gpt-5.6-terra` after `/model`).
If the session exits before the next render, resume picks the old model.

A record must only be replaced by a render that is not older than the stored
one. `updated_at` is the writer's clock and does not order renders.

## Approach

1. **Find an ordering value first (research, not guess).** Check the Claude
   Code statusline JSON docs and a real payload for a per-session value that
   only grows. `cost.total_duration_ms` is a candidate. Verify whether it
   resets on resume (`claude -c` / `--resume` keep the session id). Say in the
   PR whether the answer came from the docs or from observation.
2. **Compare and replace under a lock.** Read the stored record, compare, and
   rename while holding a per-session lock (e.g. `flock` on a lock file in the
   sessions dir, `0600`). A lock alone does not order writers; the comparison
   does. If no reliable monotonic value exists, stop and report the options
   instead of picking a workaround.
3. The statusline stays bounded: never block on the lock. If it is held, skip
   the write — the next render rewrites the record anyway.

## Constraints

- No change to the record's consumers (`launch` restore) beyond reading any
  new field. Old records without the new field must still restore.
- Statusline rules: local, bounded, atomic I/O; `0700` dirs, `0600` files.
- DRY, KISS. No new knob.

## Acceptance

- [ ] Test: two concurrent writers whose inputs arrive out of order keep the
      newer record.
- [ ] Test: a resumed session whose ordering value restarts still updates its
      record (or the PR shows why that case cannot happen).
- [ ] Records without the new field restore as before.
- [ ] `make check` green; `tests/migration-coverage.md` updated if a contract
      assertion changes. `Fixes #15`.
