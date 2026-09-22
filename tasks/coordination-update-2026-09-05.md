# Manager handoff: current routing and resume direction

Instruction ID: ROUTER-COORDINATION-20260905-A. Approved by Robert.

## Immediate plan
1. Coordinate the small [Astra profile](enable-astra-profile.md) against current
   dotfiles upstream; do not hold it for the full extraction.
2. Preserve the interim custom-model resume fix currently in dotfiles PR #24.
   Its reviewable payload is available in the local handoff directory communicated
   by the coordinator. Re-read upstream HEAD before extraction/cutover.
3. Plan [data-driven model onboarding](model-onboarding.md) as the durable solution.
4. Update the older resume spec through the normal worktree/PR workflow with the
   corrections below. Keep the Manager checkout on main.

## Corrections to restore-routed-sessions.md
The blanket claim that Herdr bypasses the shell function is incorrect for the
verified 0.8.2 implementation. src/app/agent_resume.rs starts a pane shell and sends
shell_command_from_argv(plan.argv), with Claude's plan being claude --resume UUID.
A fresh interactive zsh therefore loads the current dotfiles wrapper. A real
executable shim remains valuable for callers that directly exec a binary or do
not source that function. Do not base its necessity on an unverified Herdr claim.

The interim reader restores the last real assistant model from existing JSONL,
ignores synthetic/sidechain messages, pins -c to its selected UUID and restores the
routing recipe. Known limitations: historical local/remote route, custom tiers,
context overrides, effort and an unserved /model choice are not reconstructed.
Durable per-session non-secret routing metadata is still the long-term design.

Robert explicitly accepts Anthropic default WITH a concise notice for missing,
unknown or ambiguous resume mapping. Do not fail closed for that mapping case.
This does not authorize insecure TLS, automatic remote/local failover or hiding a
failure of a positively selected proxy route. Reconcile the older spec/test rows
that currently conflict with this distinction.

## Herdr and coordination
Basic Herdr prompt delivery passed with CC and native Codex. Busy prompts could
time out and still be processed later; never infer non-delivery or resend blindly.
The plugin Manager owns guarded transport, discovery and the Manager skill. AMQ,
custom inbox/outbox delivery and broadcast are deferred; don't introduce them as
routing dependencies. The Herdr update remains pending the resume rollout and a
controlled restart test. No proxy or Herdr deployment is authorized by this handoff.

## Requested response
Acknowledge ROUTER-COORDINATION-20260905-A with ownership/order for these tasks and
any concrete overlap with the extraction worker. Work in dedicated worktrees;
review before normal PR/merge gates. No new merge authority from this message.

Sources: https://github.com/herdrdev/herdr/blob/v0.8.2/src/app/agent_resume.rs
and https://github.com/herdrdev/herdr/blob/v0.8.2/src/agent_resume.rs
