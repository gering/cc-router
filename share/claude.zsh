# Interactive `claude` wrapper, sourced from .zshrc.
#
# Definitions only: no output, no PATH mutation, nothing that could disturb the
# Powerlevel10k instant prompt. `scripts/test-cc-harness-agents.sh` and
# `scripts/test-cc-harness-resume.py` both source this file directly, so it must
# stay self-contained.
#
# Wrapper: `--yolo` = --dangerously-skip-permissions; foreign-model flags route
# remotely by default, and `--local` selects the helper's local gateway. Remote
# fails closed on TLS; local trusts whichever process holds the loopback port.
# The helper owns models, probes, and environment recipes; this function remains
# the interactive layer for parsing, conflicts, the herdr `script` skip, and resume.
# Adding an agent = a row in the helper's table + its flag here, spelled EXACTLY
# like the row's `name` (routing passes `${model_flag#--}` straight through).
# `--fable`/`--opus`/`--sonnet`/`--haiku` are the NATIVE counterpart and expand to
# `--model <alias>` — they route nowhere. They exist for resume: the restore only
# stands down for an explicit model choice, so without them `-c` always re-enters
# the transcript's foreign agent and there is no short way back to Claude.
# Exact arg match only (never rewrite prompt text); preserve exit status; trap log cleanup.
claude() {
  local args=()
  local yolo=false
  local model_flag=""
  local local_route=false
  local native_model=false
  local native_flag=""
  local passthrough=false
  local arg
  local resume_selector="" resume_index=0 resume_value_index=0 resume_seen=0
  local value_pending=false optval_pending=false optval_is_resume=false variadic_pending=false
  for arg in "$@"; do
    # After a bare `--` (end of options), pass every token verbatim so a literal
    # `--sol`/`--yolo`/`--grok` argument reaches the CLI instead of being consumed here.
    if [[ "$passthrough" == true ]]; then
      args+=("$arg")
      continue
    fi
    # A REQUIRED option value is data, even if it happens to look like a flag:
    # `--append-system-prompt --continue` passes the literal text `--continue`.
    if [[ "$value_pending" == true ]]; then
      args+=("$arg")
      value_pending=false
      continue
    fi
    # An OPTIONAL or VARIADIC value ends at the next option token, so a
    # `-`-prefixed argument falls through to the parser instead of being eaten.
    if [[ "$optval_pending" == true || "$variadic_pending" == true ]] && [[ "$arg" != -* ]]; then
      args+=("$arg")
      if [[ "$optval_is_resume" == true ]]; then
        resume_selector="$arg"
        resume_value_index=${#args}
        optval_is_resume=false
      fi
      optval_pending=false
      continue
    fi
    optval_pending=false
    variadic_pending=false
    optval_is_resume=false
    case "$arg" in
      -c|--continue)
        resume_selector="continue"
        args+=("$arg")
        resume_index=${#args}
        resume_value_index=0
        (( resume_seen++ ))
        ;;
      -r|--resume)
        resume_selector="picker"
        optval_pending=true
        optval_is_resume=true
        args+=("$arg")
        resume_index=${#args}
        resume_value_index=0
        (( resume_seen++ ))
        ;;
      --resume=*)
        resume_selector="${arg#--resume=}"
        args+=("$arg")
        resume_index=${#args}
        resume_value_index=0
        (( resume_seen++ ))
        ;;
      --model|--model=*)
        if [[ -n "$native_flag" ]]; then
          print -u2 "claude: $native_flag conflicts with --model — pick one"
          return 1
        fi
        native_model=true
        [[ "$arg" == "--model" ]] && value_pending=true
        args+=("$arg")
        ;;
      --fable|--opus|--sonnet|--haiku)
        # Native-model shorthand: `--opus` IS `--model opus`, no more. It earns a
        # flag because the resume restore stands down only for an EXPLICIT model
        # choice — without one, `-c` re-enters whatever foreign agent the
        # transcript last answered with, so getting a resumed session back onto a
        # Claude model meant spelling out `--model opus` every single time.
        # Aliases, not pinned ids: the CLI resolves them to the current release,
        # so this does not go stale on the next model. All four verified against
        # `claude --model <alias>`.
        if [[ -n "$native_flag" && "$native_flag" != "$arg" ]]; then
          print -u2 "claude: conflicting model flags: $native_flag and $arg"
          return 1
        fi
        if [[ -z "$native_flag" && "$native_model" == true ]]; then
          print -u2 "claude: $arg conflicts with --model — pick one"
          return 1
        fi
        if [[ -z "$native_flag" ]]; then
          native_flag="$arg"
          native_model=true
          args+=(--model "${arg#--}")
        fi
        ;;
      # Value-taking CLI flags, split by how many tokens each consumes. Derived
      # from `claude --help` (`<v>` required, `<v...>` variadic, `[v]` optional);
      # refresh it when the CLI grows a flag whose value could be spelled like a
      # resume selector. A flag missing here is not fatal — its value is simply
      # re-parsed as an option, which is how `--resume` used to be forged out of
      # someone else's argument.
      --agent|--agents|--append-system-prompt|--autocompact|--debug-file|--effort|--environment|--fallback-model|--input-format|--json-schema|--max-budget-usd|--name|--output-format|--permission-mode|--permission-prompts|--plugin-dir|--plugin-url|--remote-control-session-name-prefix|--session-id|--setting-sources|--settings|--system-prompt|--system-prompt-snapshot|-n)
        value_pending=true
        args+=("$arg")
        ;;
      --add-dir|--allowedTools|--allowed-tools|--betas|--disallowedTools|--disallowed-tools|--file|--mcp-config|--tools)
        variadic_pending=true
        args+=("$arg")
        ;;
      --cloud|--debug|--from-pr|--prompt-suggestions|--remote-control|--teleport|--worktree|-d|-w)
        optval_pending=true
        args+=("$arg")
        ;;
      --)
        passthrough=true
        args+=("$arg")
        ;;
      --yolo)
        yolo=true
        args+=("--dangerously-skip-permissions")
        ;;
      --dangerously-skip-permissions)
        yolo=true
        args+=("$arg")
        ;;
      --local)
        local_route=true
        ;;
      --astra|--sol|--terra|--luna|--grok)
        # Refuse conflicting model flags instead of last-wins: the model's self-report
        # is unreliable, so a silently wrong backend is not noticeable in-session.
        if [[ -n "$model_flag" && "$model_flag" != "$arg" ]]; then
          print -u2 "claude: conflicting model flags: $model_flag and $arg"
          return 1
        fi
        model_flag="$arg"
        ;;
      *)
        args+=("$arg")
        ;;
    esac
  done

  # Resolve an external command to its PATH location, printing nothing.
  # Always by resolved path, never by bare name: in zsh a bare name also matches
  # a shadowing FUNCTION, and one of these calls is handed the gateway token —
  # checking $commands and then invoking the name would verify one thing and run
  # another. $commands is a hash a long-lived shell can hold stale across a
  # self-update that relocates a launcher, so rehash once before giving up.
  _claude_bin_of() {
    local p="${commands[$1]}"
    if [[ ! -x "$p" ]]; then
      rehash
      p="${commands[$1]}"
    fi
    [[ -x "$p" ]] || return 1
    print -r -- "$p"
  }

  # Restore from the session itself, not a directory-wide "last model" file:
  # several agents may share a cwd. Explicit caller model choices always win.
  # Unknown/missing metadata falls back to normal Claude behavior by user choice.
  # Snapshot the caller's own choice: `model_flag` is overwritten by a successful
  # restore below, and the history hint must not replay a derived value.
  local caller_model_flag="$model_flag"
  local resume_route="" resume_fields=() resume_helper="" resume_status=0
  if [[ -n "$resume_selector" && -z "$model_flag" && "$native_model" == false ]]; then
    # Refuse a second selector rather than rewriting the last one's slot:
    # `claude --resume <uuid> -c` used to leave the earlier selector's value
    # index in place, so the rewrite below emitted `--resume <uuid> --resume` —
    # and that bare trailing `--resume` opens the interactive picker, landing the
    # user on a conversation the routing was resolved from a DIFFERENT one.
    # Inside the gate on purpose: when no restore runs there is nothing to
    # rewrite, so refusing argv the real CLI accepts (last wins) would only be
    # this wrapper inventing a rule of its own.
    if (( resume_seen > 1 )); then
      print -u2 "claude: more than one resume selector (-c / --resume) — pick one"
      unfunction _claude_bin_of
      return 1
    fi
    # A bare `-r`/`--resume` opens the picker: the conversation is not known
    # until after the pick, so the resolver can only answer "indeterminate".
    # Skip the process instead of spawning it for a foregone conclusion.
    if [[ "$resume_selector" == picker ]]; then
      resume_status=1
    elif resume_helper="$(_claude_bin_of cc-harness-resume)"; then
      resume_route="$("$resume_helper" "$resume_selector")" || resume_status=$?
    else
      resume_status=1
    fi
    if (( resume_status == 0 )); then
      resume_fields=("${(@s:	:)resume_route}")
      if (( ${#resume_fields} == 3 )) && [[ "${resume_fields[2]}" != native ]]; then
        model_flag="--${resume_fields[2]}"
        # Same spelling rule as the helper's override_var_name(): uppercase AND
        # '-' to '_'. Plain uppercasing would make a hyphenated agent name an
        # invalid zsh identifier (an error on every resume), and even if it
        # assigned, the helper would read a different variable and silently
        # re-run discovery instead of restoring the recorded model.
        local resume_var="CC_HARNESS_MODEL_${(U)${resume_fields[2]//-/_}}"
        # Function-local so later commands in THIS shell are unaffected. It is
        # deliberately inherited by the resumed session and whatever it spawns:
        # that is what keeps a nested `cc-harness-agents exec` on the same model
        # instead of silently re-running discovery.
        local "$resume_var=${resume_fields[3]}"
        export "$resume_var"
        if (( resume_value_index > 0 )); then
          args[$resume_index]="--resume"
          args[$resume_value_index]="${resume_fields[1]}"
        else
          args[$resume_index]="--resume=${resume_fields[1]}"
        fi
        # Override any model stored by CC itself as well as its environment.
        args=(--model "${resume_fields[3]}" "${args[@]}")
        print -u2 -- "claude: restoring ${resume_fields[3]} via cc-harness:${resume_fields[2]}"
      fi
    elif (( resume_status == 3 )); then
      # The session DID run on a foreign model that nothing routes any more. CC
      # re-applies a session's stored model on resume, so leaving it alone would
      # send a foreign model id to Anthropic; pin the default instead.
      args=(--model default "${args[@]}")
      # The resolver already printed WHY on stderr (no profile / helper
      # unreachable / unusable answer), so this line states the consequence only.
      print -u2 -- "claude: could not restore this session's foreign model; continuing with normal Anthropic defaults"
    fi
    # Every other status means "cannot tell" (no session here, ambiguous id,
    # picker, nothing recorded yet) — change NOTHING. Prepending `--model
    # default` here silently outranked a user's own configured model on every
    # plain `claude --resume`.
  fi

  if [[ "$local_route" == true && -z "$model_flag" ]]; then
    print -u2 "claude: --local requires one foreign-model flag"
    unfunction _claude_bin_of
    return 1
  fi

  # A native --model outranks ANTHROPIC_MODEL, so it would quietly win over the
  # tier flag — the same silent-wrong-tier trap the check above exists for.
  if [[ -n "$model_flag" && "$native_model" == true ]]; then
    print -u2 "claude: $model_flag conflicts with ${native_flag:---model} — pick one"
    unfunction _claude_bin_of
    return 1
  fi

  # The routing recipe (probe + env) belongs to cc-harness-agents; build the
  # `exec` prefix here and let it replace itself with the real binary, so no
  # wrapper process lingers between the shell and `claude` (herdr's agent-state
  # detection and `/close` teardown both key on the pane's root process).
  local cc_exec=() cc_bin=""
  if [[ -n "$model_flag" ]]; then
    if ! cc_bin="$(_claude_bin_of cc-harness-agents)"; then
      print -u2 "claude: cc-harness-agents not on PATH — is ~/.scripts linked to the dotfiles?"
      unfunction _claude_bin_of
      return 1
    fi
    cc_exec=("$cc_bin" exec)
    [[ "$local_route" == true ]] && cc_exec+=(--local)
    cc_exec+=("${model_flag#--}" --)
  fi
  # `command claude` is a builtin form the helper cannot exec, and a bare
  # `claude` inside this function would recurse into it.
  local claude_bin=""
  if ! claude_bin="$(_claude_bin_of claude)"; then
    print -u2 "claude: claude binary not found on PATH"
    unfunction _claude_bin_of
    return 1
  fi
  unfunction _claude_bin_of

  # Inside herdr: skip the `script` wrapper so the real `claude` process is the
  # pane's foreground process and herdr's agent-state detection works. herdr
  # handles session IDs; routing restoration above still runs for its resumes.
  if [[ "${HERDR_ENV:-}" == "1" ]]; then
    "${cc_exec[@]}" "$claude_bin" "${args[@]}"
    return $?
  fi

  # This process inherits routing secrets, so never resolve script(1) through an
  # attacker-controlled PATH entry. This dotfiles target is macOS.
  local script_bin="/usr/bin/script"
  if [[ ! -x "$script_bin" ]]; then
    print -u2 "claude: $script_bin is not executable"
    return 1
  fi

  # mktemp, not a $$-predictable name: with TMPDIR unset this lands in a shared
  # /tmp, where a pre-planted symlink would redirect the transcript and a
  # world-readable file would expose whatever the TUI rendered. mktemp creates
  # it exclusively and 0600. `command` so a shadowing function cannot choose
  # the path — the same reason binaries here are never called by bare name.
  local log rc=0
  if ! log="$(command mktemp "${TMPDIR:-/tmp}/claude-output-XXXXXX")"; then
    print -u2 "claude: could not create the transcript file"
    return 1
  fi
  # EXIT-only cleanup: INT/TERM traps would replace default signal disposition.
  # Expand $log now — EXIT runs after locals are gone, so a late "$log" would be empty.
  # LOCAL_TRAPS keeps the EXIT handler function-scoped.
  setopt LOCAL_OPTIONS LOCAL_TRAPS
  trap "rm -f -- ${(q)log}" EXIT
  "${cc_exec[@]}" "$script_bin" -q "$log" "$claude_bin" "${args[@]}"
  rc=$?
  if [[ -s "$log" ]]; then
    # Scrape the session id only, never a whole command line: anything the TUI
    # rendered ends up in this log, so a `claude --resume "$(...)"` planted in a
    # file or web page would otherwise reach the history verbatim and run on the
    # next up-arrow. Match a UUID and rebuild the line ourselves — a stray format
    # change costs the resume hint, which beats executing scraped text.
    local resume_id
    resume_id=$(tail -c 4096 "$log" | tr -d '\r' | sed $'s/\x1b\\[[0-9;?]*[a-zA-Z]//g' \
      | grep -oE 'claude --resume "[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"' \
      | tail -1 | sed -E 's/^claude --resume "(.*)"$/\1/')
    if [[ "$resume_id" =~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' ]]; then
      local resume_line="claude --resume \"$resume_id\""
      [[ "$yolo" == true ]] && resume_line="$resume_line --yolo"
      [[ "$local_route" == true ]] && resume_line="$resume_line --local"
      # Only a flag the CALLER gave: a restore-derived one would trip the
      # `-z model_flag` gate on replay, skipping the restore and coming back on
      # the agent's primary instead of the tier the session actually ran on.
      [[ -n "$caller_model_flag" ]] && resume_line="$resume_line $caller_model_flag"
      print -s "$resume_line"
    fi
  fi
  return $rc
}
