> Archived 2026-10-17 · PR #11 (merged @ a662d23) · task/migrate-claude-wrapper

# Migrate the claude wrapper

## Goal

Move the interactive `claude()` wrapper and the interim resume bridge from the
dotfiles into cc-router, derive the foreign-model flags from the agent table,
and implement Robert's resume decision. Stage 2a of the extraction; the shim and
session sidecar follow in [restore-routed-sessions](restore-routed-sessions.md).

## Source (read dotfiles HEAD first, never a snapshot)

- `~/dotfiles/.zsh/functions/claude.zsh` — `claude()` (346 lines at `e560b4d`)
- `~/dotfiles/.scripts/cc-harness-resume` — transcript → `resolve-model` bridge
- `~/dotfiles/scripts/test-cc-harness-resume.py` and the wrapper assertions in
  `~/dotfiles/scripts/test-cc-harness-agents.sh` — the specification

## Changes

1. **Flags from the table.** Today `--astra|--sol|--terra|--luna|--grok` is a
   hard-coded case arm, and the header says "adding an agent = a row + its flag
   here". Derive the set from the effective table (packaged or user override)
   instead, through an **offline** helper call — `list` probes the network and
   must not run on every `claude` start. Add the smallest subcommand that does
   it (e.g. names only), cover it in tests, and keep `list`/`exec` output
   unchanged (work-system contract). Prompt text and unknown `--x` arguments
   still pass through untouched; native `--fable/--opus/--sonnet/--haiku` stay.
2. **Resume decision (Robert, 2026-10-16).** When a resumed session's exact
   recorded model is mapped to an agent but the gateway no longer serves it,
   the session starts on the **Anthropic default with a concise notice**
   (`--model default`, as the unmappable case already does) instead of failing.
   Only that case falls back: an explicit `--grok`/`--sol`/`--local` that cannot
   be honored, and route failures (403/401/502/TLS), stay errors that name the
   layer. The helper must report "model not served" distinguishably from route
   failures; never infer it from message text. Record the decision in
   `restore-routed-sessions.md` (resolution order, test matrix).
3. **Move, then link.** The files live in cc-router (wrapper under `share/`,
   the bridge with the other executables); `install.sh` covers them. The
   dotfiles then source/link the cc-router copies and delete their own — one
   implementation, never two. The dotfiles change goes through a dotfiles PR,
   coordinated with the dotfiles Manager; nothing there is merged by this task.
4. **Strip personal constants** (`~/.scripts` hints, dotfiles paths) into
   neutral wording or configuration.

## Constraints

- The wrapper stays zsh: it is a sourced interactive function, not the shim.
  Keep it thin; routing logic stays in the Go helper.
- The herdr `script` skip, the resume-history hint, `--yolo`, conflict checks
  and the single-selector rule carry over unchanged.
- Carry the tests across as the specification and run them in `make check`;
  update `tests/migration-coverage.md`.
- DRY, KISS, elegance (see CLAUDE.md).

## Acceptance

- [ ] A new table row gets its `--<name>` flag with no wrapper edit (test with
      a user override table).
- [ ] A removed row's flag is no longer accepted.
- [ ] Resume on a recorded model the gateway no longer serves: Anthropic
      default plus notice. Same model via explicit flag: error naming the cause.
- [ ] Route failures on resume still fail closed, layer named.
- [ ] `make check` green; `list` output byte-identical to before.
- [ ] dotfiles PR open that replaces the local copies with links; live smoke
      after Robert merges both: `claude --grok`, `claude --sol`, a resume.
