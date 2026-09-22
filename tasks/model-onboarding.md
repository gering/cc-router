# Make new-model onboarding a validated data change

## Goal
New models should become selectable through one reviewed profile update, without
synchronized hand edits across CLI flags, the picker, resume mapping and status.
User-approved direction 2026-09-05; build on architecture.md, not a second registry.

## Directive 2026-09-26: full GPT-6 support with auto-latest

Robert's instruction after the Codex catalog update broke `--sol` (the exported
haiku rung `gpt-5.3-codex-spark` vanished; fail-closed made the whole row
unavailable — correct behavior, stale data):

- **Tier semantics for the GPT-6 family:** Fable level = Astra, Opus level =
  Sol, Sonnet level = Terra, Haiku level = Luna. The table has no fable column —
  the primary-model slot carries the fable-level model; the opus/sonnet/haiku
  rungs get gpt-6-sol / gpt-6-terra / gpt-6-luna once the catalog serves them.
- **Auto-latest for astra/sol/terra/luna exactly like Grok** (dotfiles PR #43
  pattern: newest offered canonical id per family, bounded candidate list,
  note names what was auto-selected). Recurring manual pins are the failure
  mode this task exists to end.
- **Verify ceilings per model, never copy across generations** — gpt-6-astra
  reports 900000 (not the 272000 from the old brief, not the 372000 of the
  5.6 tier). Unknown windows stay visible, per the requirements below.
- **No borrowed rungs across generations.** The astra row borrowing
  gpt-5.6-luna as its haiku rung is the documented hazard (retiring 5.6 kills
  the row); the GPT-6 rung set must come from the same family once available.

Interim state (revised same day): the proxy catalog lags the native Codex
update — it serves `gpt-6-astra` but still only the 5.6 ids for sol/terra/luna,
and spark is gone, so a pure data pin to GPT-6 is not currently possible. The
dotfiles side therefore runs a broader interim fix (gateway catalog
reconciliation, spark-rung replacement, current GPT-6 routing) — explicitly
without building a parallel bash auto-latest architecture.

**Binding commitment to the dotfiles side (2026-09-26):** the auto-latest
generalization is owned here, in the Go core, as its own follow-up step after
the cutover. Its acceptance criteria, agreed across both sides:

- Selection draws only from the **actually served route catalog** — never from
  announced/marketing ids; catalog presence is checked per route, per probe.
- **Four-tier semantics per family** (fable/opus/sonnet/haiku rungs filled from
  the same generation; no cross-generation borrowed rungs).
- **Safe context windows**: verified per selected model or visibly unknown —
  auto-selection must never inherit a window across models or generations.
- **Resume stability**: exact-model resume and explicit pins always beat
  auto-latest; a resumed session is never silently upgraded.

## Current friction
The routing helper embeds a profile table while the zsh wrapper separately lists
model flags. Dynamic discovery is currently special-cased for Grok; new GPT models
still require client edits even when the proxy already serves them. Context limits
and tier defaults require verification independent of catalog presence. Provider
response names may differ from advertised aliases (e.g. Grok's -build suffix).

## Requirements
- [ ] Use share/models.tsv or the selected equivalent as the single profile source
      for selection, listing, validated aliases/reverse lookup and wrappers/shims.
- [ ] Keep stable named profiles distinct from catalog discovery. Catalog presence
      proves availability, not context limits, tool compatibility or tier fitness.
- [ ] Provide a bounded onboarding check: catalog diff -> explicit candidate ->
      text/tool/tool-result/stream compatibility -> verified context/tier metadata
      -> atomic profile activation with an easy rollback.
- [ ] Unknown limits must be visible. Do not auto-select an unverified larger
      window or silently upgrade a resumed session away from its saved model.
- [ ] Allow explicit model pins and user profile overrides that survive updates.
- [ ] Reread profile data per invocation; derive supported flags rather than
      growing a case branch for every model. Preserve public list/exec contracts.
- [ ] Distinguish Access/auth/proxy/catalog/model/adapter failures in diagnostics;
      print no credentials. Cache only where its staleness is explicit.
- [ ] Keep remote/local credential-refresh isolation and the local readiness gate.
- [ ] Recheck upstream issues/PRs before patching any proxy adapter defect. Upgrade
      only when its relevant behavior is required and verified, not on every new
      model announcement. Record tested proxy/client/profile versions.
- [ ] Cover onboarding Astra as the first end-to-end example and unknown/missing
      catalog models as negative cases. No silent fallback between proxy routes.

## Dependencies
The extraction may establish the data layout; the immediate
[--astra profile](enable-astra-profile.md) should not wait for this whole feature.
The resume task owns durable model/profile/route state. Coordinate shared files.
