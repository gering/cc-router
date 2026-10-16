#!/bin/bash
#
# Black-box tests for the interactive claude wrapper (share/claude.zsh): flag
# parsing, conflicts, the foreign-model flags read from the helper's table,
# passthrough, the herdr `script` skip and the resume-history hint. The resume
# restore itself is tests/test-cc-harness-resume.py.
# Usage: tests/test-claude-wrapper.sh <pkg-dir>
# Env: ZSH_BIN (default: zsh on PATH) — the zsh binary under test

set -euo pipefail

PKG="${1:?usage: $0 <pkg-dir>}"
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
HELPER="$PKG/bin/cc-harness-agents"
ZSH_BIN="${ZSH_BIN:-$(command -v zsh)}"
CLAUDE_ZSH="$ROOT/share/claude.zsh"
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/test-claude-wrapper.XXXXXX")
trap 'rm -rf -- "$TMP_ROOT"' EXIT

# shellcheck source=tests/assert.sh disable=SC1091
. "$(dirname "${BASH_SOURCE[0]}")/assert.sh"
RC=0
OUT=""
ERR=""
ZSH_FUNCTIONS="$TMP_ROOT/claude-function.zsh"

run_capture() {
  local out_file="$TMP_ROOT/stdout" err_file="$TMP_ROOT/stderr"
  if "$@" > "$out_file" 2> "$err_file"; then
    RC=0
  else
    RC=$?
  fi
  OUT="$(< "$out_file")"
  ERR="$(< "$err_file")"
}

cp "$CLAUDE_ZSH" "$ZSH_FUNCTIONS"
# Keep production pinned to /usr/bin/script, but let this isolated copy use a
# deterministic fake for resume-history behavior. A separate assertion below
# checks that the real source still names the fixed system path.
python3 - "$ZSH_FUNCTIONS" << 'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text()
needle = 'local script_bin="/usr/bin/script"'
assert text.count(needle) == 1
path.write_text(text.replace(needle, 'local script_bin="${TEST_SCRIPT_BIN:-/usr/bin/script}"'))
PY

# Fake wrapper dependencies. The fake helper answers `names` from the REAL
# binary — so the flags under test are the packaged table's, or a user table's
# — and records every routing prefix before it execs its target, matching the
# real helper's process behavior.
CASE_HOME="$TMP_ROOT/home"
FAKE_BIN="$TMP_ROOT/bin"
COMMON_PATH="$FAKE_BIN:/usr/bin:/bin"
mkdir -p "$CASE_HOME" "$FAKE_BIN"
cat > "$FAKE_BIN/cc-harness-agents" << FAKE_HELPER
#!/bin/bash
set -euo pipefail
[ "\$1" != names ] || exec "$HELPER" names
: > "\${WRAPPER_HELPER_LOG:?}"
for arg in "\$@"; do printf '%s\n' "\$arg" >> "\$WRAPPER_HELPER_LOG"; done
[ "\$1" = exec ] || exit 90
shift
[ "\${1:-}" != --local ] || shift
shift
[ "\${1:-}" != -- ] || shift
exec -- "\$@"
FAKE_HELPER
cat > "$FAKE_BIN/claude" << 'FAKE_CLAUDE'
#!/bin/bash
set -euo pipefail
: > "${WRAPPER_CLAUDE_LOG:?}"
for arg in "$@"; do printf '%s\n' "$arg" >> "$WRAPPER_CLAUDE_LOG"; done
printf 'claude --resume "12345678-1234-1234-1234-123456789abc"\n'
FAKE_CLAUDE
cat > "$FAKE_BIN/script" << 'FAKE_SCRIPT'
#!/bin/bash
set -euo pipefail
: > "${FAKE_SCRIPT_RAN:?}"
exit 99
FAKE_SCRIPT
cat > "$FAKE_BIN/test-script" << 'FAKE_TEST_SCRIPT'
#!/bin/bash
set -euo pipefail
[ "$1" = -q ]
log="$2"
shift 2
tmp="${log}.command"
if "$@" > "$tmp" 2>&1; then
  rc=0
else
  rc=$?
fi
/bin/cat "$tmp"
/bin/cp "$tmp" "$log"
rm -f -- "$tmp"
exit "$rc"
FAKE_TEST_SCRIPT
chmod +x "$FAKE_BIN/cc-harness-agents" "$FAKE_BIN/claude" \
  "$FAKE_BIN/script" "$FAKE_BIN/test-script"
WRAPPER_HELPER_LOG="$TMP_ROOT/wrapper-helper"
WRAPPER_CLAUDE_LOG="$TMP_ROOT/wrapper-claude"
FAKE_SCRIPT_RAN="$TMP_ROOT/fake-script-ran"

run_zsh() {
  : > "$WRAPPER_HELPER_LOG"
  : > "$WRAPPER_CLAUDE_LOG"
  # Expanded by the child zsh, not this Bash test harness.
  # shellcheck disable=SC2016
  run_capture env -i HOME="$CASE_HOME" PATH="$COMMON_PATH" HERDR_ENV=1 \
    WRAPPER_HELPER_LOG="$WRAPPER_HELPER_LOG" WRAPPER_CLAUDE_LOG="$WRAPPER_CLAUDE_LOG" \
    FAKE_SCRIPT_RAN="$FAKE_SCRIPT_RAN" \
    "$ZSH_BIN" -f -c \
    'source "$1"; shift; claude "$@"' zsh "$ZSH_FUNCTIONS" "$@"
}

helper_prefix() { sed -n "1,$1p" "$WRAPPER_HELPER_LOG"; }

run_zsh --sol prompt
assert_eq 0 "$RC" "zsh wrapper routes foreign models remotely by default"
assert_eq $'exec\nsol\n--' "$(helper_prefix 3)" "zsh remote prefix omits --local"
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" 'prompt' "zsh remote route preserves prompt argv"

run_zsh --local --sol prompt
assert_eq 0 "$RC" "zsh wrapper accepts --local with a foreign model"
assert_eq $'exec\n--local\nsol\n--' "$(helper_prefix 4)" "zsh local prefix delegates route selection to helper"

run_zsh --sol -- --local literal
assert_eq 0 "$RC" "zsh wrapper treats --local after bare -- as passthrough"
assert_eq $'exec\nsol\n--' "$(helper_prefix 3)" "passthrough --local does not select local routing"
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" '--local' "passthrough --local reaches Claude argv"

run_zsh --local prompt
assert_eq 1 "$RC" "zsh wrapper rejects --local without a foreign model"
assert_contains "$ERR" '--local requires one foreign-model flag' "zsh wrapper explains bare --local rejection"

run_zsh --local --sol --model native
assert_eq 1 "$RC" "zsh wrapper keeps native-model conflict detection"
assert_contains "$ERR" 'conflicts with --model' "zsh wrapper reports native-model conflict"

run_zsh --sol --grok
assert_eq 1 "$RC" "zsh wrapper keeps foreign-model conflict detection"
assert_contains "$ERR" 'conflicting model flags' "zsh wrapper reports foreign-model conflict"

# Every row of the packaged table is a flag, spelled exactly like the row name
# and consumed by the wrapper: a selector that leaked through would reach
# Claude as an unknown option instead of routing.
while IFS= read -r agent; do
  run_zsh "--$agent" prompt
  assert_eq 0 "$RC" "zsh wrapper accepts --$agent"
  assert_eq $'exec\n'"$agent"$'\n--' "$(helper_prefix 3)" "--$agent hands the row name straight to the helper"
  assert_not_contains "$(< "$WRAPPER_CLAUDE_LOG")" "--$agent" "--$agent is consumed, never leaked into Claude argv"
done < <(env -i HOME="$CASE_HOME" "$HELPER" names)

# The flags follow the EFFECTIVE table: a user table adds a row's flag with no
# wrapper edit, and a row it drops is no longer a flag — the option reaches
# Claude untouched, which rejects it, rather than routing anywhere.
mkdir -p "$CASE_HOME/.config/cc-router"
printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
  nova nova-1 nova-1 nova-1 nova-1 nova-1 200000 xai -xai-login xAI \
  > "$CASE_HOME/.config/cc-router/models.tsv"
run_zsh --nova prompt
assert_eq 0 "$RC" "a row added by a user table is a flag"
assert_eq $'exec\nnova\n--' "$(helper_prefix 3)" "and routes to that row"
run_zsh --grok prompt
assert_eq '' "$(< "$WRAPPER_HELPER_LOG")" "a row the table dropped never routes"
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" '--grok' "its flag reaches Claude as an unknown option"
rm -rf "$CASE_HOME/.config"

# Unknown options and prompt text pass through untouched.
run_zsh --verbose --print 'tell me about --sol'
assert_eq 0 "$RC" "unknown options still launch"
assert_eq '' "$(< "$WRAPPER_HELPER_LOG")" "an unknown option never routes"
assert_eq $'--verbose\n--print\ntell me about --sol' "$(< "$WRAPPER_CLAUDE_LOG")" "unknown options and prompt text reach Claude verbatim"

# A bare launch must not reach the routing helper at all — not even for names.
run_zsh prompt
assert_eq 0 "$RC" "a plain launch still succeeds"
assert_eq '' "$(< "$WRAPPER_HELPER_LOG")" "a plain launch never reaches the routing helper"

# Without the helper there is no table: a foreign flag is an unknown option to
# Claude, never a silent launch on the default model.
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture env -i HOME="$CASE_HOME" PATH="$TMP_ROOT/claude-only:/usr/bin:/bin" HERDR_ENV=1 \
  WRAPPER_CLAUDE_LOG="$WRAPPER_CLAUDE_LOG" \
  "$ZSH_BIN" -f -c 'mkdir -p "$2" && ln -sf "$3" "$2/claude"; source "$1"; claude --sol prompt' \
  zsh "$ZSH_FUNCTIONS" "$TMP_ROOT/claude-only" "$FAKE_BIN/claude"
assert_eq 0 "$RC" "without the helper the launch still reaches Claude"
assert_eq $'--sol\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "and the foreign flag is passed on for Claude to reject"

# Native-model shorthand. The point of these is the RESUME path: the restore only
# stands down for an explicit model choice, so `-c --opus` has to reach Claude as
# a real `--model opus` and must never touch the routing helper.
for native_case in fable opus sonnet haiku; do
  run_zsh "--$native_case" prompt
  assert_eq 0 "$RC" "zsh wrapper accepts --$native_case"
  # The fake logs one argv entry per line, so the expansion is a two-line pair.
  assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" "--model
$native_case" "--$native_case expands to a native --model"
  assert_not_contains "$(< "$WRAPPER_CLAUDE_LOG")" "--$native_case" "the shorthand itself never reaches Claude argv"
  assert_eq '' "$(< "$WRAPPER_HELPER_LOG")" "--$native_case is native, so it never routes"
done

run_zsh --opus --sol
assert_eq 1 "$RC" "a native shorthand conflicts with a foreign flag"
# The message must name the flag the USER typed. Reporting "--model" here would
# point at a flag that never appeared on the command line.
assert_contains "$ERR" 'conflicts with --opus' "the conflict names the shorthand, not --model"
assert_not_contains "$ERR" 'conflicts with --model' "a shorthand conflict never blames a bare --model"

run_zsh --sol --model native
assert_eq 1 "$RC" "a bare --model still conflicts with a foreign flag"
assert_contains "$ERR" 'conflicts with --model' "a bare --model is still named as itself"

run_zsh --opus --model sonnet
assert_eq 1 "$RC" "a native shorthand conflicts with an explicit --model"
assert_contains "$ERR" 'conflicts with --model' "the shorthand/--model conflict is reported"

run_zsh --model sonnet --opus
assert_eq 1 "$RC" "the conflict is caught in either order"

run_zsh --opus --fable
assert_eq 1 "$RC" "two different native shorthands conflict"
assert_contains "$ERR" 'conflicting model flags' "the native/native conflict is reported"

run_zsh --opus --opus prompt
assert_eq 0 "$RC" "repeating the same native shorthand is not a conflict"
assert_eq 1 "$(grep -c -- '--model' "$WRAPPER_CLAUDE_LOG")" "a repeated shorthand is only expanded once"

run_zsh --opus -- --opus literal
assert_eq 0 "$RC" "a literal --opus after bare -- is passthrough"
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" '--opus' "passthrough --opus reaches Claude argv"

run_zsh --yolo prompt
assert_eq $'--dangerously-skip-permissions\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "--yolo expands to --dangerously-skip-permissions"

# The script wrapper path must preserve both --local and the foreign model in the
# reconstructed resume-history command. The isolated function copy uses a
# deterministic script(1) fake; the production source path is asserted separately.
rm -f -- "$FAKE_SCRIPT_RAN"
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture env -i HOME="$CASE_HOME" PATH="$COMMON_PATH" \
  WRAPPER_HELPER_LOG="$WRAPPER_HELPER_LOG" WRAPPER_CLAUDE_LOG="$WRAPPER_CLAUDE_LOG" \
  FAKE_SCRIPT_RAN="$FAKE_SCRIPT_RAN" TEST_SCRIPT_BIN="$FAKE_BIN/test-script" \
  "$ZSH_BIN" -f -c 'OSTYPE=darwin25; source "$1"; claude --yolo --local --sol; fc -l -1' zsh "$ZSH_FUNCTIONS"
assert_eq 0 "$RC" "zsh wrapper script path succeeds"
if grep -Fq 'local script_bin="/usr/bin/script"' "$CLAUDE_ZSH"; then
  pass "zsh wrapper pins the production system script path"
else
  fail "zsh wrapper pins the production system script path"
fi
assert_contains "$(< "$WRAPPER_HELPER_LOG")" "$FAKE_BIN/test-script" "isolated resume test uses its deterministic script fake"
if [ ! -e "$FAKE_SCRIPT_RAN" ]; then
  pass "zsh wrapper never executes PATH script"
else
  fail "zsh wrapper never executes PATH script"
fi
assert_contains "$OUT" 'claude --resume "12345678-1234-1234-1234-123456789abc" --yolo --local --sol' "resume history preserves yolo, local route, and model"

# Off macOS the BSD script(1) invocation would fail, so the launch goes direct.
rm -f -- "$FAKE_SCRIPT_RAN" "$WRAPPER_CLAUDE_LOG"
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture env -i HOME="$CASE_HOME" PATH="$COMMON_PATH" \
  WRAPPER_HELPER_LOG="$WRAPPER_HELPER_LOG" WRAPPER_CLAUDE_LOG="$WRAPPER_CLAUDE_LOG" \
  FAKE_SCRIPT_RAN="$FAKE_SCRIPT_RAN" TEST_SCRIPT_BIN="$FAKE_BIN/script" \
  "$ZSH_BIN" -f -c 'OSTYPE=linux-gnu; source "$1"; claude prompt' zsh "$ZSH_FUNCTIONS"
assert_eq 0 "$RC" "a launch off macOS succeeds"
assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "and reaches Claude directly"
if [ ! -e "$FAKE_SCRIPT_RAN" ]; then
  pass "off macOS script(1) is never invoked"
else
  fail "off macOS script(1) is never invoked"
fi

# The file must define its function in a bare shell, with no startup output
# that could disturb an instant prompt.
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture "$ZSH_BIN" -f -c 'source "$1"; whence -w claude' zsh "$CLAUDE_ZSH"
assert_eq 0 "$RC" "the wrapper loads in a bare zsh"
assert_eq 'claude: function' "$OUT" "claude.zsh defines claude"
assert_eq "" "$ERR" "sourcing the wrapper stays silent"

finish
