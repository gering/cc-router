# Codex minor discovery

## Goal

Robert's decision (2026-10-02): new Codex minors are picked up automatically
instead of by hand-editing the table. This replaces the planned manual move of
Sol to `gpt-6.1-sol`.

## Policy

- **Verified majors only.** Within a major whose window is verified (today: 6),
  each role selects the newest offered `gpt-<major>.<n>-<sol|terra|luna|astra>`
  from the **route** catalog. The pattern is exact: no variants, no other
  suffixes.
- **A minor inherits its major's verified window** (372000 for major 6).
  This is Robert's explicit policy: a minor is not expected to shrink the
  window. It is the documented per-family exception to "windows are never
  inherited" in [model-onboarding](model-onboarding.md), and it does not cross
  majors.
- **Major 7 and later are not auto-selected** until their window has been
  measured and added to the verified list.
- **Rungs move independently.** Each rung of the shared Codex ladder resolves
  on its own, in all four Codex rows. If only `gpt-6.1-sol` is offered, only the
  sol primary and the opus rung move.
- **Superseded ids stay resumable through the pattern**, not through a
  hand-maintained retained list: any served `gpt-6.<n>-<role>` resolves to its
  role.
- `list` notes the choice like Grok does: `auto-selected …`.

## Shape

Follow the existing Grok discovery (4.x/5.x selected, major 6+ excluded). Use
one discovery mechanism parameterised by family, not a second copy. Use one
resolver for `list` and `exec`.

## Acceptance

- [ ] With a fake catalog: 6.0 only, 6.1-sol only, mixed minors per role, a
      7.x id present (ignored), variant ids present (ignored).
- [ ] Resume: an old `gpt-6-sol` session resolves to sol after 6.1 appears; an
      explicit pin outranks discovery.
- [ ] Ceiling unchanged at 372000. The session ceiling is still the smallest
      reachable rung.
- [ ] `make check` green; `tests/migration-coverage.md` updated.
- [ ] Live: after `git pull && make build`, `cc-harness-agents list` shows the
      newest served minors with the auto-selected note.

## Notes

- The retained entries `gpt-5.6-sol|sol` and `gpt-5.6-luna|luna` cover
  major 5.6. Keep them unless the pattern covers that major as well.
- The resume bridge in dotfiles calls `resolve-model`. Its interface does not
  change.
