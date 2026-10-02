# Move Sol to gpt-6.1-sol

## Goal

Robert's request (2026-10-02): after the cutover, route the Sol role to
`gpt-6.1-sol`. This is the first model change made in cc-router rather than in
the dotfiles.

## Preconditions

- The cutover freeze is lifted. This task is not part of the freeze.
- CLIProxyAPI on mars runs v8.0.10 (dotfiles task `cliproxyapi-v8-upgrade`).

## Step 1 — probe the route

- `gpt-6.1-sol` appears in the **route** catalog, not just the native Codex
  list.
- A small inference, tool-call and streaming check passes.
- The context window is measured on the route. Large-context floor probes need
  Robert's explicit budget, and a passing floor is not a maximum.

## Step 2 — change cc-router

- [ ] `share/models.tsv`: the sol primary and the opus rung become `gpt-6.1-sol`.
      The ladder stays shared across the four Codex rows.
- [ ] `verifiedContext` gets the measured window. Unknown means conservative,
      with a visible note. The session ceiling stays the smallest reachable rung.
- [ ] `retainedModels` gets `gpt-6-sol|sol`, so existing sessions stay
      resumable on their primary.
- [ ] Tests: `resolve-model` currently refuses `gpt-6.1-sol` on purpose. Update
      that case and the ledger in `tests/migration-coverage.md`.
- [ ] `make check` is green, and `cc-harness-agents list` shows sol on
      `gpt-6.1-sol` live after `git pull && make build`.

## Notes

- This is exactly the manual bump that [model-onboarding](model-onboarding.md)'s
  auto-latest is meant to retire. Keep it a data change plus policy entries; add
  no new code path.
- dotfiles no longer owns the table. Changes go here only.
