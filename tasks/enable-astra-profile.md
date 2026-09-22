# Enable Astra through the existing CC routing path

## Priority and ownership
User-approved 2026-09-05. Deliver the small daily-use profile promptly; do not wait
for the full router extraction. The cc-router Manager coordinates ownership with
the live dotfiles implementation so only one worker changes each source. Use a
separate implementation worktree. Do not bundle it into the ready resume PR.

## Evidence and problem
The current proxy was tested on 2026-09-05: its catalog exposes `gpt-6-astra`;
Anthropic-format `/v1/messages` returned a text reply, a forced tool_use, a valid
tool-result continuation, and streaming tool_use. Recheck at implementation time.
The immediate missing pieces are the client model profile and `claude --astra`
selector. A proxy upgrade was not required for these smoke tests.

The observed proxy model-definition context cap was 272000. Treat that as an
initial conservative observation to verify for the actual account/route, not a
universal model limit. Do not substitute the public API window for a subscription
route's effective limit. Do not copy the older GPT tier cap without evidence.

## Acceptance
- [ ] Register `astra` -> `gpt-6-astra` using the existing routing table and add
      the matching wrapper selector where that table currently lives.
- [ ] Reuse the Codex provider authentication recipe; no new secret or auth path.
- [ ] Choose documented, available tier/subagent defaults and verify context cap.
- [ ] Keep explicit-model conflict checks and normal Anthropic launch behavior.
- [ ] Test list/exec, selector parsing, catalog absence, and resume of Astra once
      the interim resume fix is integrated; unknown-model fallback stays explicit.
- [ ] Run a bounded live text/tool/stream smoke with no project tools and record
      the effective selected model. Never print auth headers or token values.
- [ ] Coordinate with extraction before cutover; neither move nor overwrite newer
      dotfiles behavior from an earlier snapshot.
- [ ] Separate PR, normal review and existing merge gates. No Herdr restart or
      proxy deployment merely to ship the local selector.

## Next
See [data-driven onboarding](model-onboarding.md). Ship this small profile first;
that architecture is not a dependency for giving the user --astra now.
