# Make new-model onboarding a validated data change

## Goal
New models should become selectable through one reviewed profile update, without
synchronized hand edits across CLI flags, the picker, resume mapping and status.
User-approved direction 2026-09-05; build on architecture.md, not a second registry.

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
