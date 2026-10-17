# Interactive `claude` wrapper. Source it from your .zshrc:
#
#   source /path/to/cc-router/share/claude.zsh
#
# Definitions only: no output, no PATH mutation, nothing that could disturb an
# instant prompt. tests/test-claude-wrapper.sh and tests/test-cc-harness-resume.py
# both source this file directly, so it must stay self-contained. It needs
# cc-harness-agents on PATH (install.sh links it).
#
# The terminal layer only: the transcript capture behind the resume-history
# hint, and the herdr `script` skip. Flags (`--<agent>`, `--local`, `--yolo`,
# `--opus` …), conflicts, the resume restore and routing are all
# `cc-harness-agents launch`, so a caller that never sources this file gets
# the same behaviour.

# Resolve an external command to its PATH location, printing nothing.
# Always by resolved path, never by bare name: in zsh a bare name also matches
# a shadowing FUNCTION, so checking $commands and then invoking the name would
# verify one thing and run another. $commands is a hash a long-lived shell can
# hold stale across a self-update that relocates a launcher, so rehash once
# before giving up.
_claude_bin_of() {
  local p="${commands[$1]-}"
  if [[ ! -x "$p" ]]; then
    rehash
    p="${commands[$1]-}"
  fi
  [[ -x "$p" ]] || return 1
  print -r -- "$p"
}

claude() {
  local cc_bin=""
  if ! cc_bin="$(_claude_bin_of cc-harness-agents)"; then
    print -u2 "claude: cc-harness-agents not on PATH — run cc-router's install.sh"
    return 1
  fi

  # Inside herdr: skip the `script` wrapper so the real `claude` process is the
  # pane's foreground process and herdr's agent-state detection works; launch
  # replaces itself with it. herdr handles session IDs, and routing restoration
  # still runs for its resumes.
  # Off macOS too: the capture below is BSD script(1) syntax, which util-linux
  # rejects, so there the launch goes direct and only the history hint is lost.
  if [[ "${HERDR_ENV:-}" == "1" || "$OSTYPE" != darwin* ]]; then
    "$cc_bin" launch -- "$@"
    return $?
  fi

  # Never resolve script(1) through an attacker-controlled PATH entry: the
  # session under it receives the routing secrets. The invocation below is
  # BSD/macOS script(1).
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
  # The replay file receives the caller's own flags from launch, which parsed
  # them: only it knows which `--yolo` was an option and which a prompt.
  local log replay rc=0
  if ! log="$(command mktemp "${TMPDIR:-/tmp}/claude-output-XXXXXX")" \
    || ! replay="$(command mktemp "${TMPDIR:-/tmp}/claude-replay-XXXXXX")"; then
    [[ -n "$log" ]] && command rm -f -- "$log"
    print -u2 "claude: could not create the transcript file"
    return 1
  fi
  # EXIT-only cleanup: INT/TERM traps would replace default signal disposition.
  # Expand the paths now — EXIT runs after locals are gone, so a late "$log"
  # would be empty. LOCAL_TRAPS keeps the EXIT handler function-scoped.
  setopt LOCAL_OPTIONS LOCAL_TRAPS
  trap "rm -f -- ${(q)log} ${(q)replay}" EXIT
  "$script_bin" -q "$log" "$cc_bin" launch --replay-to "$replay" -- "$@"
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
      | tail -1 | sed -E 's/^claude --resume "(.*)"$/\1/') || true
    if [[ "$resume_id" =~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' ]]; then
      local resume_line="claude --resume \"$resume_id\"" flag
      # Same rule for our own file: only option-shaped words reach history.
      for flag in ${(z)"$(<"$replay")"}; do
        [[ "$flag" =~ '^--[A-Za-z0-9][A-Za-z0-9._-]*$' ]] && resume_line="$resume_line $flag"
      done
      print -s "$resume_line"
    fi
  fi
  return $rc
}
