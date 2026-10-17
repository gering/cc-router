#!/bin/bash
#
# Black-box tests for the interactive claude wrapper (share/claude.zsh) and
# the launch path behind it: flag parsing, conflicts, the foreign-model flags
# read from the helper's table, passthrough, the herdr `script` skip and the
# resume-history hint. The resume restore itself is
# tests/test-cc-harness-resume.py.
# Usage: tests/test-claude-wrapper.sh <pkg-dir>
# Env: ZSH_BIN (default: zsh on PATH) — the zsh binary under test

set -euo pipefail

PKG="${1:?usage: $0 <pkg-dir>}"
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
HELPER="$PKG/bin/cc-harness-agents"
ZSH_BIN="${ZSH_BIN:-$(command -v zsh)}"
CLAUDE_ZSH="$ROOT/share/claude.zsh"
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/test-claude-wrapper.XXXXXX")

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

# Routing is real: cc-harness-agents is tests/testdriver (the production code,
# trusting only the test gateway's CA), and the test gateway plays the remote
# route and the local port. Claude is fake and reports its argv and which
# agent and route it was handed.
exec 9> >("$PKG/bin/gateway" -root "$TMP_ROOT")
# Preserve the exit status: a suite that dies mid-way must never report success.
# shellcheck disable=SC2154 # assigned inside the trap itself
trap 'status=$?; exec 9>&-; rm -rf -- "$TMP_ROOT"; exit "$status"' EXIT
for _ in $(seq 100); do
  [ -r "$TMP_ROOT/gateway.env" ] && break
  sleep 0.05
done
# shellcheck source=/dev/null
. "$TMP_ROOT/gateway.env"
CASE_HOME="$TMP_ROOT/home"
FAKE_BIN="$TMP_ROOT/bin"
COMMON_PATH="$FAKE_BIN:/usr/bin:/bin"
mkdir -p "$CASE_HOME/.cli-proxy-api" "$CASE_HOME/.cache/cliproxy-auth" "$FAKE_BIN" "$TMP_ROOT/wrapper/gateway"
ln -s "$PKG/bin/testdriver" "$FAKE_BIN/cc-harness-agents"
# The catalog serves every model of the packaged table and the user-table row.
catalog=$({ cut -f2-6 "$ROOT/share/models.tsv" | grep -v '^#' | tr '\t' '\n' && echo nova-1; } |
  sort -u | sed 's/.*/{"id":"&"}/' | paste -sd, -)
printf '{"data":[%s]}' "$catalog" > "$TMP_ROOT/wrapper/gateway/body"
# The local route: a token, credentials and a fresh prepare-local window.
printf 'local-token\n' > "$CASE_HOME/.cli-proxy-api/client.key"
printf '{"access_token":"x","refresh_token":"r"}\n' > "$CASE_HOME/.cli-proxy-api/xai-test.json"
printf '{"access_token":"c","refresh_token":"r"}\n' > "$CASE_HOME/.cli-proxy-api/codex-test.json"
expires=$(($(date -u +%s) + 3600))
expires=$(date -u -r "$expires" +%Y-%m-%dT%H:%M:%SZ 2> /dev/null || date -u -d "@$expires" +%Y-%m-%dT%H:%M:%SZ)
printf '{"version":1,"prepared_at":"2026-01-01T00:00:00Z","expires_at":"%s","ssh_host":"h","remote_path":"/p","files":[{"name":"codex-test.json","sha256":"%064d"}]}\n' \
  "$expires" 0 > "$CASE_HOME/.cache/cliproxy-auth/local-ready.json"
cat > "$FAKE_BIN/claude" << 'FAKE_CLAUDE'
#!/bin/bash
set -euo pipefail
: > "${WRAPPER_CLAUDE_LOG:?}"
for arg in "$@"; do printf '%s\n' "$arg" >> "$WRAPPER_CLAUDE_LOG"; done
[ -z "${CC_ROUTER_AGENT-}" ] || printf '%s %s\n' "$CC_ROUTER_AGENT" "$CLIPROXY_ROUTE" > "$WRAPPER_ROUTE_LOG"
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
: > "${TEST_SCRIPT_RAN:?}"
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
chmod +x "$FAKE_BIN/claude" "$FAKE_BIN/script" "$FAKE_BIN/test-script"
WRAPPER_ROUTE_LOG="$TMP_ROOT/wrapper-route"
WRAPPER_CLAUDE_LOG="$TMP_ROOT/wrapper-claude"
FAKE_SCRIPT_RAN="$TMP_ROOT/fake-script-ran"
TEST_SCRIPT_RAN="$TMP_ROOT/test-script-ran"
ROUTE_ENV=(
  HOME="$CASE_HOME" PATH="$COMMON_PATH" CC_ROUTER_CLAUDE="$FAKE_BIN/claude"
  CC_ROUTER_REMOTE_URL="https://127.0.0.1:$GATEWAY_TRUSTED_PORT/c/wrapper"
  CC_ROUTER_LOCAL_PORT="$GATEWAY_LIVE_PORT" CC_ROUTER_TEST_CA_FILE="$GATEWAY_CA"
  CLIPROXY_PROFILE=TEST CLIPROXY_API_KEY_TEST=remote-api-secret
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret
  WRAPPER_ROUTE_LOG="$WRAPPER_ROUTE_LOG" WRAPPER_CLAUDE_LOG="$WRAPPER_CLAUDE_LOG"
  FAKE_SCRIPT_RAN="$FAKE_SCRIPT_RAN" TEST_SCRIPT_RAN="$TEST_SCRIPT_RAN"
)

run_zsh() {
  : > "$WRAPPER_ROUTE_LOG"
  : > "$WRAPPER_CLAUDE_LOG"
  # Expanded by the child zsh, not this Bash test harness.
  # shellcheck disable=SC2016
  run_capture env -i "${ROUTE_ENV[@]}" HERDR_ENV=1 \
    "$ZSH_BIN" -f -c \
    'source "$1"; shift; claude "$@"' zsh "$ZSH_FUNCTIONS" "$@"
}

routed() { cat "$WRAPPER_ROUTE_LOG"; }

run_zsh --sol prompt
assert_eq 0 "$RC" "zsh wrapper routes foreign models remotely by default"
assert_eq 'sol remote' "$(routed)" "zsh remote route is the default"
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" 'prompt' "zsh remote route preserves prompt argv"

run_zsh --local --sol prompt
assert_eq 0 "$RC" "zsh wrapper accepts --local with a foreign model"
assert_eq 'sol local' "$(routed)" "--local selects the helper's local route"

run_zsh --sol -- --local literal
assert_eq 0 "$RC" "zsh wrapper treats --local after bare -- as passthrough"
assert_eq 'sol remote' "$(routed)" "passthrough --local does not select local routing"
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
  assert_eq "$agent remote" "$(routed)" "--$agent routes through its own row"
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
assert_eq 'nova remote' "$(routed)" "and routes to that row"
run_zsh --grok prompt
assert_eq '' "$(routed)" "a row the table dropped never routes"
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" '--grok' "its flag reaches Claude as an unknown option"
rm -rf "$CASE_HOME/.config"

# Unknown options and prompt text pass through untouched.
run_zsh --verbose --print 'tell me about --sol'
assert_eq 0 "$RC" "unknown options still launch"
assert_eq '' "$(routed)" "an unknown option never routes"
assert_eq $'--verbose\n--print\ntell me about --sol' "$(< "$WRAPPER_CLAUDE_LOG")" "unknown options and prompt text reach Claude verbatim"

run_zsh prompt
assert_eq 0 "$RC" "a plain launch still succeeds"
assert_eq '' "$(routed)" "a plain launch never routes"
assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "and reaches Claude unchanged"

# Without the helper nothing can parse the flags: refuse, naming the fix,
# rather than launching on the default model.
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture env -i HOME="$CASE_HOME" PATH=/usr/bin:/bin HERDR_ENV=1 \
  "$ZSH_BIN" -f -c 'source "$1"; claude --sol prompt' zsh "$ZSH_FUNCTIONS"
assert_eq 1 "$RC" "without the helper the wrapper refuses"
assert_contains "$ERR" 'cc-harness-agents not on PATH' "and names what is missing"

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
  assert_eq '' "$(routed)" "--$native_case is native, so it never routes"
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
rm -f -- "$FAKE_SCRIPT_RAN" "$TEST_SCRIPT_RAN"
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture env -i "${ROUTE_ENV[@]}" TEST_SCRIPT_BIN="$FAKE_BIN/test-script" \
  "$ZSH_BIN" -f -c 'OSTYPE=darwin25; source "$1"; claude --yolo --local --sol; fc -l -1' zsh "$ZSH_FUNCTIONS"
assert_eq 0 "$RC" "zsh wrapper script path succeeds"
if grep -Fq 'local script_bin="/usr/bin/script"' "$CLAUDE_ZSH"; then
  pass "zsh wrapper pins the production system script path"
else
  fail "zsh wrapper pins the production system script path"
fi
if [ -e "$TEST_SCRIPT_RAN" ]; then
  pass "isolated resume test uses its deterministic script fake"
else
  fail "isolated resume test uses its deterministic script fake"
fi
assert_eq 'sol local' "$(routed)" "the launch under script(1) is still routed"
if [ ! -e "$FAKE_SCRIPT_RAN" ]; then
  pass "zsh wrapper never executes PATH script"
else
  fail "zsh wrapper never executes PATH script"
fi
assert_contains "$OUT" 'claude --resume "12345678-1234-1234-1234-123456789abc" --yolo --local --sol' "resume history preserves yolo, local route, and model"

# A route the restore brought back is not the caller's: the hint replays the
# caller's flags only, so a local record is not pinned local forever.
mkdir -p "$CASE_HOME/.cache/cc-router/sessions"
chmod 700 "$CASE_HOME/.cache/cc-router" "$CASE_HOME/.cache/cc-router/sessions"
printf '{"session_id":"12345678-1234-1234-1234-123456789abc","cwd":"/","agent":"sol","route":"local","model":"gpt-6-sol","updated_at":%s}' \
  "$(date +%s)" > "$CASE_HOME/.cache/cc-router/sessions/12345678-1234-1234-1234-123456789abc.json"
chmod 600 "$CASE_HOME/.cache/cc-router/sessions/12345678-1234-1234-1234-123456789abc.json"
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture env -i "${ROUTE_ENV[@]}" TEST_SCRIPT_BIN="$FAKE_BIN/test-script" \
  "$ZSH_BIN" -f -c 'OSTYPE=darwin25; source "$1"; claude --resume 12345678-1234-1234-1234-123456789abc; fc -l -1' zsh "$ZSH_FUNCTIONS"
assert_eq 'sol local' "$(routed)" "a recorded local route is restored"
assert_contains "$OUT" 'claude --resume "12345678-1234-1234-1234-123456789abc"' "and the hint is written"
assert_not_contains "$OUT" '--local' "but never replays the restored route as the caller's"
rm -rf "$CASE_HOME/.cache/cc-router"

# Off macOS the BSD script(1) invocation would fail, so the launch goes direct.
rm -f -- "$FAKE_SCRIPT_RAN" "$WRAPPER_CLAUDE_LOG"
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture env -i "${ROUTE_ENV[@]}" TEST_SCRIPT_BIN="$FAKE_BIN/script" \
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
