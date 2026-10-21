> Archived 2026-10-21 · PR #21 (merged @ 0290779) · task/resume-retired-model-error

# Fail a resume whose recorded model is retired

## Problem

`claude -c` / `claude --resume <id>` on a session that ran on a foreign model
the gateway no longer serves (`serves` → not served) currently prints a notice
and continues on the Anthropic default (`restore` in
`internal/agents/launch.go`, `resumeUnroutable` → `accountDefault`). The
conversation silently changes provider and model.

## Goal (Robert, 2026-10-21)

That case **fails** with a non-zero exit and one clear message that names the
retired model, the agent, and the two ways forward:

- `claude -c --proxy <agent>` (or `--resume <id> --proxy <agent>`) — continue
  on the agent's current model;
- `claude -c --noproxy` — continue on Anthropic.

Both suggested commands must then start without error. Verify that, including
that Claude Code does not re-apply the session's stored (retired) model over
the one launch passes — if it does, `launch` must pass `--model` explicitly on
those paths.

## Scope

- Only the "not served" verdict changes. Every other route failure already
  errors naming its layer; `resumeUnknown` still changes nothing.
- The local route has no catalog; unchanged.
- An explicit `--proxy`/`--local`/`--noproxy`/`--model` on the resume already
  bypasses restore — keep it that way; that is the escape hatch.
- The transcript bridge path (no record) gets the same behaviour when it
  resolves a retired model.

## Acceptance

- [ ] Retired recorded model → exit non-zero, message as above, nothing
      launched; stub-launcher test in the wrapper and shim suites.
- [ ] `-c --proxy <agent>` and `-c --noproxy` on that session start cleanly on
      the agent's current model / Anthropic (unit tests plus a live smoke in
      the PR description).
- [ ] `docs/resume.md` and README updated; `make check` green;
      `tests/migration-coverage.md` updated if a contract assertion changes.
