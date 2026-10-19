# Route --opus through the CLIProxy Claude OAuth login

## Origin
Robert, 2026-10-19 21:16 · issue [#17](https://github.com/gering/cc-router/issues/17).
Since 2026-10-19 the CLIProxy on Mars (panel cliproxy.gering.dev) carries an
Anthropic OAuth login (Claude). The `claude` wrapper only knows `--sol`, `--terra`,
`--luna`, `--grok` and `--kimi`; plain `claude` runs natively at Anthropic, not via
CLIProxy, so the new login is unused until a selector routes to it.

## Requirements
- [ ] New exclusive flag `--opus`, analogous to the other model flags: routes via
      cc-harness-agents and the remote CLIProxy (`CC_ROUTER_REMOTE_URL`, e.g.
      https://llm.gering.dev) to an Opus model served by the Claude OAuth route.
- [ ] Onboard it as a profile row per [model onboarding](model-onboarding.md)
      (`share/models.tsv`, one resolver for list/exec): model id, alias, and the
      fable/opus/sonnet/haiku/subagent slots from the served Claude catalog, with
      verified context windows (unknown stays visible, never inherited).
- [ ] Subagents must not silently fall back to sol or luna (Dev-Retro topic
      subagent pinning); every rung resolves on the Claude route or fails visibly.
- [ ] `--opus --local` routes to the local CLIProxy on Robert's Mac, like `--local`
      for the other flags. On the Grok Bot box there is no local proxy: abort
      cleanly with a clear error, no silent fallback to remote or native.
- [ ] Keep zsh (`share/claude.zsh`) and bash/shim (`cmd/claude-shim/claude`)
      wrappers in sync; reject conflicts with `--model` and other model flags as today.
- [ ] Update README and help text.
- [ ] Tests (`tests/test-claude-wrapper.sh`, list/exec, selector parsing, catalog
      absence) plus a bounded live smoke: short prompt with `claude --opus -p` via
      remote; record the effective model. Never print auth headers or tokens.

## Out of scope
Do not change the CLIProxy `config.yaml` or Caddy. Save nothing in the panel.

## Next
Separate PR, normal review and existing merge gates. After merge Oskar pulls it on
the box and runs the `--opus` smoke.
