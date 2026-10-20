> Archived 2026-10-20 · PR #20 (merged @ e250619) · task/foreign-default-model-guard

# Foreign default-model guard

## Problem

`/model X` inside a routed session (`claude --proxy sol`, then `/model
gpt-5.6-terra`) makes Claude Code save X as the global default in
`~/.claude/settings.json` ("saved as your default for new sessions"). That is
Claude Code's behaviour, not ours. Routed sessions don't notice, because
`ANTHROPIC_MODEL` outranks the setting. A later native start (plain `claude`
or `claude --noproxy`) reads the foreign id and asks Anthropic for a model it
does not serve. Seen live on 2026-10-19 during the Sol→Terra resume smoke.

## Goal (Robert chose option A, 2026-10-19)

A native start without an explicit model (plain `claude` or `--noproxy`, no
`--model`, no `ANTHROPIC_MODEL`) whose effective default model is a **foreign
id known to the table** starts on Anthropic's default instead. It prints one
concise notice on stderr that names the settings file and key and says how to
clear it. The guard **never edits the settings file**: it belongs to the user
and is symlinked from dotfiles.

## Shape

- The check sits in `launch` on the native path, next to the existing
  native-model handling (`internal/agents/launch.go`). It reads the table only
  when a setting holds a model, so a plain `claude` with no model setting still
  never loads it.
- "Foreign" means the id resolves to a table agent through the same mapping
  `resolve-model` uses: no list of Anthropic ids, no new knob. `claude-*` ids
  never count as foreign, the `opus` row included (PR #19: Anthropic ids never
  infer a route).
- Explicit choices win and are left alone: `--model`, a set `ANTHROPIC_MODEL`,
  `--proxy`/`--local`, a routed restore, and a session inside a routed one
  (`CC_ROUTER_AGENT`).
- **Research before choosing the override.** Find how to start Claude Code on
  its Anthropic default without touching settings (e.g. `--model default`, or
  an alias). Also find which settings files count (user, project, local, and
  `CLAUDE_CONFIG_DIR`) from Claude Code's documented precedence. Say in the PR
  whether each answer came from the docs or from observation.

## Constraints

- Validate only the `model` key; ignore every other field. An unreadable or
  invalid settings file means no guard and a normal start, never a failure.
- Secrets never in argv, logs or messages. The notice names the file and key,
  never other content.
- DRY, KISS, elegance. Wrapper and shim stay thin callers.

## Acceptance

- [ ] Native start with a table-known foreign default → Anthropic default, one
      notice, settings file byte-identical afterwards. The same holds for
      `claude -c --noproxy`.
- [ ] Anthropic (`claude-*`) or unknown default → untouched, no notice; the
      table is not loaded when no model is set.
- [ ] `--model`, `ANTHROPIC_MODEL`, `--proxy`/`--local`, a routed restore and
      `CC_ROUTER_AGENT` → no guard.
- [ ] Wrapper and shim suites cover the path; README/`docs/resume.md` mention
      it; `make check` green; `tests/migration-coverage.md` updated if a
      contract assertion changes.
