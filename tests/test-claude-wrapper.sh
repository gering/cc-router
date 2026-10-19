#!/bin/bash
#
# Black-box tests for the interactive claude wrapper (share/claude.zsh) and
# the launch path behind it: flag parsing, conflicts, the --proxy/--local values
# read from the helper's table, passthrough, the herdr `script` skip, the
# resume-history hint, and the executable shim. The resume restore itself is
# tests/test-cc-harness-resume.py.
# Usage: tests/test-claude-wrapper.sh <pkg-dir>
# Env: ZSH_BIN (default: zsh on PATH) — the zsh binary under test

set -euo pipefail

PKG="${1:?usage: $0 <pkg-dir>}"
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
HELPER="$PKG/bin/cc-harness-agents"
SHIM="$PKG/bin/shim/claude"
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

# Routing is chosen by value: `--proxy <agent|model>` (the remote route),
# `--local <agent|model>`, `--noproxy` for native. `--model` is Claude's own
# and always native. Both spellings of a value, consumed: neither the flag nor
# the agent reaches Claude.
for form in '--proxy sol' '--proxy=sol'; do
  # shellcheck disable=SC2086 # split on purpose: one or two words
  run_zsh $form prompt
  assert_eq 0 "$RC" "$form routes"
  assert_eq 'sol remote' "$(routed)" "$form takes the remote route"
  assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "$form is consumed, the prompt preserved"
done
for form in '--local sol' '--local=sol'; do
  # shellcheck disable=SC2086 # split on purpose: one or two words
  run_zsh $form prompt
  assert_eq 0 "$RC" "$form routes"
  assert_eq 'sol local' "$(routed)" "$form selects the helper's local route"
done

run_zsh --proxy sol -- --local literal
assert_eq 0 "$RC" "zsh wrapper treats --local after bare -- as passthrough"
assert_eq 'sol remote' "$(routed)" "passthrough --local does not select local routing"
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" '--local' "passthrough --local reaches Claude argv"

run_zsh --proxy '' prompt
assert_eq 1 "$RC" "an empty --proxy value is refused, never a native launch"
assert_contains "$ERR" '--proxy needs an agent or model id' "the empty value says what is missing"
for bare in '--proxy' '--proxy --yolo' '--proxy='; do
  # shellcheck disable=SC2086 # split on purpose
  run_zsh $bare
  assert_eq 1 "$RC" "'$bare' without a value is refused"
  assert_contains "$ERR" '--proxy needs an agent or model id' "'$bare' says what is missing"
done
for bare in '--local' '--local -p'; do
  # shellcheck disable=SC2086 # split on purpose
  run_zsh $bare
  assert_eq 1 "$RC" "'$bare' without an agent is refused"
  assert_contains "$ERR" '--local needs an agent or model id' "'$bare' says what is missing"
done
# A word after --local is its value only when it names an agent or a model:
# otherwise --local is the deprecated bare modifier and the word stays a prompt.
run_zsh --local prompt
assert_eq 1 "$RC" "a prompt after a bare --local is not taken for the agent"
assert_contains "$ERR" '--local needs an agent or model id' "the bare --local still needs an agent"
run_zsh --sol --local 'fix the bug'
assert_eq 0 "$RC" "the deprecated --sol --local keeps a following prompt"
assert_eq 'sol local' "$(routed)" "routed locally"
assert_eq 'fix the bug' "$(< "$WRAPPER_CLAUDE_LOG")" "and the prompt reaches Claude"
run_zsh --local gpt-6-luna prompt
assert_eq 'luna local' "$(routed)" "a model id after --local is its value"

run_zsh --local sol --model native
assert_eq 1 "$RC" "zsh wrapper keeps native-model conflict detection"
assert_contains "$ERR" '--local sol conflicts with --model' "zsh wrapper reports native-model conflict"
run_zsh --model native --proxy sol
assert_eq 1 "$RC" "the --model conflict is caught in either order"

run_zsh --proxy sol --proxy grok
assert_eq 1 "$RC" "zsh wrapper keeps foreign-model conflict detection"
assert_contains "$ERR" 'conflicting model flags: --proxy sol and --proxy grok' "zsh wrapper reports foreign-model conflict"
run_zsh --proxy sol --local sol
assert_eq 1 "$RC" "two routes for one agent conflict"
assert_contains "$ERR" '--proxy conflicts with --local' "the route conflict is reported"
run_zsh --proxy sol --proxy=sol prompt
assert_eq 'sol remote' "$(routed)" "repeating the same choice is not a conflict"

run_zsh --proxy sol --proxy $'evil\e[2J'
assert_eq 1 "$RC" "a conflicting value with control bytes is refused"
assert_not_contains "$ERR" $'\e' "and never echoed raw to the terminal"

run_zsh --proxy nosuch prompt
assert_eq 1 "$RC" "an unknown agent or model is refused"
assert_contains "$ERR" '--proxy nosuch: no such agent' "the refusal names the value"
assert_eq '' "$(routed)" "an unknown value never routes"

# A model id names the agent that serves it and pins exactly that model, as a
# restore does — even an Anthropic id, because the caller chose the proxy.
run_zsh --proxy claude-sonnet-5-5 prompt
assert_eq 0 "$RC" "a model id routes"
assert_eq 'opus remote' "$(routed)" "the model id selects the agent serving it"
assert_eq $'--model\nclaude-sonnet-5-5\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "and the exact model is pinned"

# Every row of the packaged table is a value of --proxy.
while IFS= read -r agent; do
  run_zsh --proxy "$agent" prompt
  assert_eq 0 "$RC" "--proxy $agent is accepted"
  assert_eq "$agent remote" "$(routed)" "--proxy $agent routes through its own row"
done < <(env -i HOME="$CASE_HOME" "$HELPER" names)

# The pre-`--proxy` spelling still routes for the transition, with a notice
# naming today's spelling: `--<agent>`, and a bare `--local` beside one.
run_zsh --sol prompt
assert_eq 0 "$RC" "the deprecated --sol still launches"
assert_eq 'sol remote' "$(routed)" "the deprecated --sol still routes remotely"
assert_contains "$ERR" 'claude: --sol is deprecated — use --proxy sol' "and says what replaces it"
assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "the deprecated flag is consumed too"
run_zsh --local --sol prompt
assert_eq 'sol local' "$(routed)" "the deprecated --local --sol still routes locally"
assert_contains "$ERR" '--local --sol is deprecated — use --local sol' "and the notice names the new form"
run_zsh --sol --local -p prompt
assert_eq 'sol local' "$(routed)" "in either order"
run_zsh --sol --grok
assert_eq 1 "$RC" "two deprecated agent flags still conflict"
assert_contains "$ERR" 'conflicting model flags: --sol and --grok' "naming both as typed"
run_zsh --sol --model native
assert_contains "$ERR" '--sol conflicts with --model' "a deprecated flag still conflicts with --model"
run_zsh --proxy sol prompt
assert_not_contains "$ERR" 'deprecated' "today's spelling carries no notice"

# The values follow the EFFECTIVE table: a user table adds an agent with no
# wrapper edit, and a row it drops no longer routes — its old flag reaches
# Claude untouched, which rejects it, rather than routing anywhere.
mkdir -p "$CASE_HOME/.config/cc-router"
printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
  nova nova-1 nova-1 nova-1 nova-1 nova-1 200000 xai -xai-login xAI \
  > "$CASE_HOME/.config/cc-router/models.tsv"
run_zsh --proxy nova prompt
assert_eq 0 "$RC" "a row added by a user table is an agent"
assert_eq 'nova remote' "$(routed)" "and routes to that row"
run_zsh --grok prompt
assert_eq '' "$(routed)" "a row the table dropped never routes"
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" '--grok' "its old flag reaches Claude as an unknown option"
rm -rf "$CASE_HOME/.config"

# The native shorthands are deprecated but keep their meaning for the
# transition: `--opus` IS `--model opus`, even though a row shares the name —
# a habit must never silently move a session onto the gateway.
for native_case in fable opus sonnet haiku; do
  run_zsh "--$native_case" prompt
  assert_eq 0 "$RC" "the deprecated --$native_case still launches"
  assert_eq $'--model\n'"$native_case"$'\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "--$native_case expands to a native --model"
  assert_eq '' "$(routed)" "--$native_case is native, so it never routes"
  assert_contains "$ERR" "--$native_case is deprecated — use --model $native_case" "--$native_case names its replacement"
done
run_zsh --opus prompt
assert_contains "$ERR" ', or --proxy opus for the gateway' "a shorthand that is also a row names the gateway route"
run_zsh --sonnet prompt
assert_not_contains "$ERR" 'gateway' "a shorthand no row shares has no gateway hint"

run_zsh --opus --proxy sol
assert_eq 1 "$RC" "a native shorthand conflicts with a route"
assert_contains "$ERR" '--proxy sol conflicts with --opus' "the conflict names the shorthand, not --model"
run_zsh --opus --model sonnet
assert_eq 1 "$RC" "a native shorthand conflicts with an explicit --model"
assert_contains "$ERR" '--opus conflicts with --model' "the shorthand/--model conflict is reported"
run_zsh --model sonnet --opus
assert_eq 1 "$RC" "the conflict is caught in either order"
run_zsh --opus --fable
assert_eq 1 "$RC" "two different native shorthands conflict"
assert_contains "$ERR" 'conflicting model flags: --opus and --fable' "the native/native conflict is reported"
run_zsh --opus --opus prompt
assert_eq 1 "$(grep -c -- '--model' "$WRAPPER_CLAUDE_LOG")" "a repeated shorthand is only expanded once"
run_zsh --opus -- --opus literal
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" '--opus' "passthrough --opus reaches Claude argv"

# --noproxy: native, whatever the session ran on. It conflicts with every
# route and combines with --model.
run_zsh --noproxy prompt
assert_eq 0 "$RC" "--noproxy launches"
assert_eq '' "$(routed)" "--noproxy never routes"
assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "a fresh --noproxy launch is plain Claude"
run_zsh --noproxy --model sonnet prompt
assert_eq $'--model\nsonnet\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "--noproxy combines with --model"
for routed_by in '--proxy sol' '--local sol' '--sol'; do
  # shellcheck disable=SC2086 # split on purpose
  run_zsh --noproxy $routed_by prompt
  assert_eq 1 "$RC" "--noproxy refuses $routed_by"
  assert_contains "$ERR" "--noproxy conflicts with $routed_by" "naming $routed_by"
done
run_zsh --local --noproxy prompt
assert_contains "$ERR" '--noproxy conflicts with --local' "a bare --local is refused too"

# Resuming natively: no restore, and the session's stored model — foreign if
# it ran routed — is replaced by the account default, unless --model says.
mkdir -p "$CASE_HOME/.cache/cc-router/sessions"
chmod 700 "$CASE_HOME/.cache/cc-router" "$CASE_HOME/.cache/cc-router/sessions"
printf '{"session_id":"12345678-1234-1234-1234-123456789abc","cwd":"/","agent":"sol","route":"remote","model":"gpt-6-sol","updated_at":%s}' \
  "$(date +%s)" > "$CASE_HOME/.cache/cc-router/sessions/12345678-1234-1234-1234-123456789abc.json"
chmod 600 "$CASE_HOME/.cache/cc-router/sessions/12345678-1234-1234-1234-123456789abc.json"
run_zsh --resume 12345678-1234-1234-1234-123456789abc
assert_eq 'sol remote' "$(routed)" "without --noproxy the recorded route is restored"
run_zsh --resume 12345678-1234-1234-1234-123456789abc --noproxy
assert_eq 0 "$RC" "--noproxy resumes a routed session"
assert_eq '' "$(routed)" "natively"
assert_eq $'--model\ndefault\n--resume\n12345678-1234-1234-1234-123456789abc' "$(< "$WRAPPER_CLAUDE_LOG")" "on the account default, never the stored foreign model"
run_zsh -c --noproxy --model opus
assert_eq $'-c\n--model\nopus' "$(< "$WRAPPER_CLAUDE_LOG")" "an explicit --model is the native model instead"
rm -rf "$CASE_HOME/.cache/cc-router"

# `/model` inside a routed session saves the foreign id as Claude's default
# for new sessions. A native start that would use it starts on the account
# default instead, with one notice, and never touches the file.
SETTINGS="$CASE_HOME/.claude/settings.json"
PROJECT="$TMP_ROOT/project"
mkdir -p "$CASE_HOME/.claude" "$PROJECT/.claude"
printf '{"model":"gpt-5.6-terra","theme":"dark"}' > "$SETTINGS"
cp "$SETTINGS" "$TMP_ROOT/settings.orig"
cd "$PROJECT"
# The notice names the cleaned path; TMPDIR may end in a slash.
guard_notice="\"model\" in ${SETTINGS//\/\///} is gpt-5.6-terra, which only the gateway serves"
run_zsh prompt
assert_eq 0 "$RC" "a foreign default still launches"
assert_eq '' "$(routed)" "natively"
assert_eq $'--model\ndefault\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "on the account default"
assert_contains "$ERR" "$guard_notice" "naming the file and the key"
assert_eq 1 "$(grep -c 'only the gateway serves' <<< "$ERR")" "once"
run_capture env -i "${ROUTE_ENV[@]}" "$SHIM" prompt
assert_eq $'--model\ndefault\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "the shim guards the same start"
assert_contains "$ERR" "$guard_notice" "with the same notice"
run_zsh -c --noproxy
assert_eq $'--model\ndefault\n-c' "$(< "$WRAPPER_CLAUDE_LOG")" "-c --noproxy pins the default once"
assert_eq 1 "$(grep -c 'only the gateway serves' <<< "$ERR")" "and names the setting once"
run_zsh -c
assert_eq '-c' "$(< "$WRAPPER_CLAUDE_LOG")" "a plain resume keeps the session's own model"
assert_not_contains "$ERR" 'only the gateway serves' "without a notice"
for explicit in '--model sonnet' '--settings {}' '--setting-sources=project' '--agent reviewer'; do
  # shellcheck disable=SC2086 # split on purpose: one or two words
  run_zsh $explicit prompt
  assert_not_contains "$(< "$WRAPPER_CLAUDE_LOG")" default "$explicit is the caller's choice"
  assert_not_contains "$ERR" 'only the gateway serves' "$explicit: no notice"
done
run_capture env -i "${ROUTE_ENV[@]}" ANTHROPIC_MODEL=claude-sonnet-5-5 "$SHIM" prompt
assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "a set ANTHROPIC_MODEL outranks the setting: no guard"
run_capture env -i "${ROUTE_ENV[@]}" ANTHROPIC_MODEL= "$SHIM" prompt
assert_eq $'--model\ndefault\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "an empty ANTHROPIC_MODEL is unset, as Claude reads it"
run_zsh --proxy sol prompt
assert_eq 'sol remote' "$(routed)" "--proxy routes as before"
assert_not_contains "$ERR" 'only the gateway serves' "--proxy: no notice"
run_capture env -i "${ROUTE_ENV[@]}" CC_ROUTER_AGENT=sol CLIPROXY_ROUTE=remote "$SHIM" prompt
assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "inside a routed session: no guard"
run_capture env -i "${ROUTE_ENV[@]}" CC_ROUTER_AGENT=sol CLIPROXY_ROUTE=remote ANTHROPIC_MODEL=gpt-6-sol "$SHIM" --noproxy prompt
assert_eq $'--model\ndefault\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "--noproxy leaves the route, so the guard applies"
printf '{"model":"claude-sonnet-5-5"}' > "$PROJECT/.claude/settings.local.json"
run_zsh prompt
assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "the project's own Anthropic model outranks the user's"
for model in claude-opus-5-5 mystery-1; do
  printf '{"model":"%s"}' "$model" > "$PROJECT/.claude/settings.local.json"
  run_zsh prompt
  assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "$model is no foreign default"
  assert_eq '' "$ERR" "$model: no notice"
done
printf '{"model":' > "$PROJECT/.claude/settings.local.json"
run_zsh prompt
assert_eq 0 "$RC" "an invalid settings file still launches"
assert_eq $'--model\ndefault\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "skipped like Claude skips it, so the user's foreign default is guarded"
printf '{"env":{"ANTHROPIC_MODEL":"claude-sonnet-5-5"}}' > "$PROJECT/.claude/settings.local.json"
run_zsh prompt
assert_eq prompt "$(< "$WRAPPER_CLAUDE_LOG")" "an ANTHROPIC_MODEL in a settings env outranks the model key: no guard"
if cmp -s "$SETTINGS" "$TMP_ROOT/settings.orig"; then
  pass "the settings file is never written"
else
  fail "the settings file is never written"
fi
cd "$ROOT"
rm -rf "$CASE_HOME/.claude" "$PROJECT"

# Inside a routed session the recipe is inherited, so a plain claude stays
# routed; --noproxy leaves it behind.
cat > "$FAKE_BIN/claude-env" << 'FAKE_ENV'
#!/bin/sh
env | grep -oE '^(ANTHROPIC_[A-Z_]*|CLAUDE_CODE_[A-Z_]*|CLIPROXY_ROUTE|CC_ROUTER_AGENT)=' | sort > "$WRAPPER_CLAUDE_LOG"
FAKE_ENV
chmod +x "$FAKE_BIN/claude-env"
INHERITED=(CC_ROUTER_CLAUDE="$FAKE_BIN/claude-env" CC_ROUTER_AGENT=sol CLIPROXY_ROUTE=remote
  ANTHROPIC_BASE_URL=https://gateway.invalid ANTHROPIC_AUTH_TOKEN=routed-secret ANTHROPIC_MODEL=gpt-6-sol
  CLAUDE_CODE_MAX_CONTEXT_TOKENS=372000)
run_capture env -i "${ROUTE_ENV[@]}" "${INHERITED[@]}" "$SHIM" prompt
assert_contains "$(< "$WRAPPER_CLAUDE_LOG")" 'ANTHROPIC_BASE_URL=' "a plain launch inside a routed session keeps the route"
run_capture env -i "${ROUTE_ENV[@]}" "${INHERITED[@]}" "$SHIM" --noproxy prompt
assert_eq 0 "$RC" "--noproxy launches inside a routed session"
assert_eq '' "$(< "$WRAPPER_CLAUDE_LOG")" "and hands Claude none of the routing recipe"

run_zsh --yolo prompt
assert_eq $'--dangerously-skip-permissions\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "--yolo expands to --dangerously-skip-permissions"

# The script wrapper path must preserve the route and its agent in the
# reconstructed resume-history command, as one `--local=<agent>` word. The isolated function copy uses a
# deterministic script(1) fake; the production source path is asserted separately.
rm -f -- "$FAKE_SCRIPT_RAN" "$TEST_SCRIPT_RAN"
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture env -i "${ROUTE_ENV[@]}" TEST_SCRIPT_BIN="$FAKE_BIN/test-script" \
  "$ZSH_BIN" -f -c 'OSTYPE=darwin25; source "$1"; claude --yolo --local sol; fc -l -1' zsh "$ZSH_FUNCTIONS"
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
assert_contains "$OUT" 'claude --resume "12345678-1234-1234-1234-123456789abc" --yolo --local=sol' "resume history preserves yolo, local route, and agent in one word"

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

# The executable shim: the same launch path without any shell function, as
# herdr's direct restores, process managers and other shells reach it. It
# execs the PRODUCTION helper next to it, which does not trust the test
# gateway's CA, so routing is shown on the local route.
: > "$WRAPPER_ROUTE_LOG"
run_capture env -i "${ROUTE_ENV[@]}" PATH="$PKG/bin/shim:$COMMON_PATH" /bin/sh -c 'claude --local sol --yolo prompt'
assert_eq 0 "$RC" "the shim launches without a shell function"
assert_eq 'sol local' "$(routed)" "the shim routes a --local choice"
assert_eq $'--dangerously-skip-permissions\nprompt' "$(< "$WRAPPER_CLAUDE_LOG")" "the shim parses like the wrapper"

# Every step execs: the process that ran the shim IS Claude, so nothing
# lingers between a pane and its agent. Claude's own status comes back.
cat > "$FAKE_BIN/claude-pid" << 'FAKE_PID'
#!/bin/sh
echo "$$" > "$WRAPPER_CLAUDE_LOG"
exit 7
FAKE_PID
chmod +x "$FAKE_BIN/claude-pid"
# Expanded by the child sh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture env -i "${ROUTE_ENV[@]}" CC_ROUTER_CLAUDE="$FAKE_BIN/claude-pid" \
  /bin/sh -c 'echo "$$"; exec "$1" --resume 12345678-1234-1234-1234-123456789abc' sh "$SHIM"
assert_eq 7 "$RC" "the shim preserves Claude's exit status"
assert_eq "$OUT" "$(< "$WRAPPER_CLAUDE_LOG")" "no shim or helper process lingers: Claude keeps the caller's pid"

# Found through a link, the shim still finds its own helper.
ln -s "$SHIM" "$TMP_ROOT/linked-claude"
run_capture env -i "${ROUTE_ENV[@]}" "$TMP_ROOT/linked-claude" prompt
assert_eq 0 "$RC" "a linked shim resolves its real location"

# The native launcher is configured, never looked up by name: pointing it at
# the shim itself is refused instead of recursing.
run_capture env -i "${ROUTE_ENV[@]}" CC_ROUTER_CLAUDE="$TMP_ROOT/linked-claude" "$SHIM" prompt
assert_eq 3 "$RC" "a launcher that is the shim itself is refused"
assert_contains "$ERR" 'not Claude Code' "and the refusal says why"
run_capture env -i "${ROUTE_ENV[@]}" CC_ROUTER_CLAUDE="$TMP_ROOT/absent" "$SHIM" prompt
assert_eq 127 "$RC" "a missing native launcher is not found"
assert_contains "$ERR" 'set CC_ROUTER_CLAUDE' "and the message names the setting"

# The file must define its function in a bare shell, with no startup output
# that could disturb an instant prompt.
# Expanded by the child zsh, not this Bash test harness.
# shellcheck disable=SC2016
run_capture "$ZSH_BIN" -f -c 'source "$1"; whence -w claude' zsh "$CLAUDE_ZSH"
assert_eq 0 "$RC" "the wrapper loads in a bare zsh"
assert_eq 'claude: function' "$OUT" "claude.zsh defines claude"
assert_eq "" "$ERR" "sourcing the wrapper stays silent"

finish
