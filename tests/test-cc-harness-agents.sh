#!/bin/bash
#
# Black-box contract suite for cc-harness-agents — the helper-owned cases of
# dotfiles' scripts/test-cc-harness-agents.sh (the bash implementation's
# suite), running against the Go binary. Assertion blocks are carried over
# verbatim where the behaviour is language-neutral; tests/migration-coverage.md
# maps every upstream assertion to its place here, in the Go tests, or to its
# justified retirement.
#
# What changed is the harness, not the assertions: the bash helper shelled out
# to curl/nc, which a PATH fake could intercept. The Go binary speaks HTTPS and
# TCP itself, so a loopback test gateway (tests/gateway) plays the server. The
# upstream FAKE_CURL_*/FAKE_NC_* knobs are kept as the call-site vocabulary and
# translated by run_capture into test-gateway state:
#
#   FAKE_CURL_BODY / FAKE_CURL_STATUS   the test gateway's response for this case
#   FAKE_CURL_RC=7                      a gateway that drops the connection
#   FAKE_CURL_RC=60                     a gateway with an untrusted certificate
#   FAKE_NC_RC=1                        nothing listening on the local port
#   curl_calls                          requests the test gateway observed
#
# Remote-route runs use tests/testdriver — the same code, trusting only the
# test gateway CA (certificate verification stays on). Everything else runs the
# production binary. The Go tests cover the production binary's remote path
# (untrusted system store, transport failures) separately.
#
# Usage: tests/test-cc-harness-agents.sh <pkg-dir>
#   <pkg-dir> holds bin/cc-harness-agents, bin/testdriver, bin/gateway and a
#   share/ next to bin/ (tests/run builds it).

set -euo pipefail

PKG="${1:?usage: $0 <pkg-dir>}"
HELPER="$PKG/bin/cc-harness-agents"
DRIVER="$PKG/bin/testdriver"
TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/test-cc-harness-agents.XXXXXX")
exec 9> >("$PKG/bin/gateway" -root "$TMP_ROOT")
# Preserve the exit status: a suite that dies mid-way must never report success.
trap 'status=$?; exec 9>&-; rm -rf -- "$TMP_ROOT"; exit "$status"' EXIT
for _ in $(seq 100); do
  [ -r "$TMP_ROOT/gateway.env" ] && break
  sleep 0.05
done
# shellcheck source=/dev/null
. "$TMP_ROOT/gateway.env"

PASS=0
FAIL=0
RC=0
OUT=""
ERR=""
CASE_DIR=""
CASE_ID=""
CASE_HOME=""
FAKE_BIN=""
CURL_COUNT=""
CURL_LOG=""
COMMON_PATH=""
TARGET="$TMP_ROOT/capture-env"

pass() {
  PASS=$((PASS + 1))
  printf 'ok %d - %s\n' "$PASS" "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  printf 'not ok - %s\n' "$1" >&2
  return 0
}

assert_eq() {
  local expected="$1" actual="$2" label="$3"
  if [ "$actual" = "$expected" ]; then
    pass "$label"
  else
    printf 'expected: <%s>\nactual:   <%s>\n' "$expected" "$actual" >&2
    fail "$label"
  fi
}

assert_contains() {
  local haystack="$1" needle="$2" label="$3"
  case "$haystack" in
    *"$needle"*) pass "$label" ;;
    *)
      printf 'missing: <%s>\nin: <%s>\n' "$needle" "$haystack" >&2
      fail "$label"
      ;;
  esac
}

assert_not_contains() {
  local haystack="$1" needle="$2" label="$3"
  case "$haystack" in
    *"$needle"*)
      printf 'unexpected: <%s>\nin: <%s>\n' "$needle" "$haystack" >&2
      fail "$label"
      ;;
    *) pass "$label" ;;
  esac
}

assert_secret_safe() {
  local text="$1" label="$2"
  assert_not_contains "$text" "remote-api-secret" "$label (API key)"
  assert_not_contains "$text" "access-id-secret" "$label (Access ID)"
  assert_not_contains "$text" "access-client-secret" "$label (Access secret)"
}

# The case's gateway URL on the trusted test gateway.
remote_url() { printf 'https://127.0.0.1:%s/c/%s' "$GATEWAY_TRUSTED_PORT" "$CASE_ID"; }

gateway_file() { printf '%s/gateway/%s' "$CASE_DIR" "$1"; }

# respond <status|body|location> <value>: an empty value restores the default.
respond() {
  mkdir -p "$CASE_DIR/gateway"
  if [ -n "$2" ]; then printf '%s' "$2" > "$(gateway_file "$1")"; else rm -f "$(gateway_file "$1")"; fi
}

gateway_request() { cat "$(gateway_file request)" 2> /dev/null || true; }

# Runs a command, capturing RC/OUT/ERR. For an `env …` invocation it first
# translates the upstream FAKE_* knobs into test-gateway state, supplies the
# gateway configuration, and picks the binary: remote-route runs need the
# test gateway CA, so they go to the test driver.
run_capture() {
  local out_file="$CASE_DIR/stdout" err_file="$CASE_DIR/stderr"
  local -a cmd=()
  if [ "$1" = env ]; then
    local url port
    url="$(remote_url)"
    port="$GATEWAY_LIVE_PORT"
    cmd=(env)
    shift
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -i) cmd+=("$1") ;;
        FAKE_CURL_COUNT_FILE=* | FAKE_CURL_LOG_FILE=* | FAKE_CURL_RC=0) ;;
        FAKE_CURL_BODY=*) respond body "${1#*=}" ;;
        FAKE_CURL_STATUS=*) respond status "${1#*=}" ;;
        FAKE_CURL_RC=7) url="https://127.0.0.1:$GATEWAY_RESET_PORT/c/$CASE_ID" ;;
        FAKE_CURL_RC=60) url="https://127.0.0.1:$GATEWAY_UNTRUSTED_PORT/c/$CASE_ID" ;;
        FAKE_NC_RC=1) port="$GATEWAY_CLOSED_PORT" ;;
        CC_ROUTER_REMOTE_URL=*) url="${1#*=}" ;;
        *=*) cmd+=("$1") ;;
        *) break ;;
      esac
      shift
    done
    cmd+=("CC_ROUTER_REMOTE_URL=$url" "CC_ROUTER_LOCAL_PORT=$port" "CC_ROUTER_TEST_CA_FILE=$GATEWAY_CA")
    if [ "$1" = "$HELPER" ] && is_remote_run "${@:2}"; then
      set -- "$DRIVER" "${@:2}"
    fi
  fi
  # ${cmd[@]+…}: bash 3.2 reports an empty array as unbound under set -u.
  if ${cmd[@]+"${cmd[@]}"} "$@" > "$out_file" 2> "$err_file"; then
    RC=0
  else
    RC=$?
  fi
  OUT="$(< "$out_file")"
  ERR="$(< "$err_file")"
}

is_remote_run() {
  case "${1:-}" in
    exec) [ "${2:-}" != --local ] ;;
    list)
      local arg
      for arg in "${@:2}"; do [ "$arg" != --local ] || return 1; done
      ;;
    *) return 1 ;;
  esac
}

curl_calls() {
  local count
  count="$(cat "$(gateway_file count)" 2> /dev/null || true)"
  printf '%s' "${count:-0}"
}

write_remote_profile() {
  local profile="${1:-TEST}"
  mkdir -p "$CASE_HOME/.config/cliproxy"
  printf '# selected deployment\nCLIPROXY_PROFILE=%s\n' "$profile" \
    > "$CASE_HOME/.config/cliproxy/client.env"
}

write_local_credentials() {
  mkdir -p "$CASE_HOME/.cli-proxy-api"
  printf 'local-token\n' > "$CASE_HOME/.cli-proxy-api/client.key"
  printf '{"access_token":"x","refresh_token":"r"}\n' > "$CASE_HOME/.cli-proxy-api/xai-test.json"
  printf '{"access_token":"k","refresh_token":"r"}\n' > "$CASE_HOME/.cli-proxy-api/kimi-test.json"
  printf '{"access_token":"c","refresh_token":"r"}\n' > "$CASE_HOME/.cli-proxy-api/codex-test.json"
  # The local route is only sanctioned inside a `cliproxy-auth prepare-local`
  # window, so every local case needs that marker present and fresh.
  write_fallback_marker "$(marker_stamp 3600)"
}

# BSD date spells epoch input -r, GNU date -d @.
marker_stamp() {
  local epoch=$(($(date -u +%s) + $1))
  date -u -r "$epoch" +%Y-%m-%dT%H:%M:%SZ 2> /dev/null || date -u -d "@$epoch" +%Y-%m-%dT%H:%M:%SZ
}

write_fallback_marker() {
  mkdir -p "$CASE_HOME/.cache/cliproxy-auth"
  printf '{"version":1,"prepared_at":"2026-01-01T00:00:00Z","expires_at":"%s",' "$1" \
    > "$CASE_HOME/.cache/cliproxy-auth/local-ready.json"
  printf '"ssh_host":"gateway-host","remote_path":"/srv/cliproxyapi/auths",' \
    >> "$CASE_HOME/.cache/cliproxy-auth/local-ready.json"
  printf '"files":[{"name":"codex-test.json","sha256":"%s"}]}\n' \
    "0000000000000000000000000000000000000000000000000000000000000000" \
    >> "$CASE_HOME/.cache/cliproxy-auth/local-ready.json"
}

setup_case() {
  CASE_DIR=$(mktemp -d "$TMP_ROOT/case.XXXXXX")
  CASE_ID="${CASE_DIR##*/}"
  CASE_HOME="$CASE_DIR/home"
  FAKE_BIN="$CASE_DIR/bin"
  CURL_COUNT="$CASE_DIR/unused-curl-count"
  CURL_LOG="$CASE_DIR/unused-curl-log"
  COMMON_PATH="$FAKE_BIN:/usr/bin:/bin"
  mkdir -p "$CASE_HOME" "$FAKE_BIN" "$CASE_DIR/tmp"
}

cat > "$TARGET" << 'CAPTURE'
#!/bin/bash
set -euo pipefail
printf 'BASE=%s\n' "${ANTHROPIC_BASE_URL-}"
printf 'AUTH=%s\n' "${ANTHROPIC_AUTH_TOKEN-}"
printf 'ROUTE=%s\n' "${CLIPROXY_ROUTE-}"
printf 'MODEL=%s\n' "${ANTHROPIC_MODEL-}"
printf 'SUBAGENT=%s\n' "${CLAUDE_CODE_SUBAGENT_MODEL-}"
printf 'OPUS=%s\n' "${ANTHROPIC_DEFAULT_OPUS_MODEL-}"
printf 'SONNET=%s\n' "${ANTHROPIC_DEFAULT_SONNET_MODEL-}"
printf 'HAIKU=%s\n' "${ANTHROPIC_DEFAULT_HAIKU_MODEL-}"
printf 'CONTEXT=%s\n' "${CLAUDE_CODE_MAX_CONTEXT_TOKENS-}"
printf 'BEDROCK=%s\n' "${CLAUDE_CODE_USE_BEDROCK-}"
printf 'VERTEX=%s\n' "${CLAUDE_CODE_USE_VERTEX-}"
printf 'FOUNDRY=%s\n' "${CLAUDE_CODE_USE_FOUNDRY-}"
printf 'MANTLE=%s\n' "${CLAUDE_CODE_USE_MANTLE-}"
printf 'AWS=%s\n' "${CLAUDE_CODE_USE_ANTHROPIC_AWS-}"
printf 'GATEWAY=%s\n' "${CLAUDE_CODE_USE_GATEWAY-}"
printf 'ANTHROPIC_GOOGLE=%s\n' "${CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD-}"
printf 'NO_PROXY=%s\n' "${NO_PROXY-}"
printf 'no_proxy=%s\n' "${no_proxy-}"
printf 'CUSTOM_BEGIN\n%s\nCUSTOM_END\n' "${ANTHROPIC_CUSTOM_HEADERS-}"
for arg in "$@"; do printf 'ARG=%s\n' "$arg"; done
CAPTURE
chmod +x "$TARGET"

# The one catalog builder, used by the fixtures here AND by the discovery cases
# further down. Defined before its first use.
catalog_of() {
  local out="" id
  for id in "$@"; do
    [ -z "$out" ] || out="$out,"
    out="$out{\"id\":\"$id\"}"
  done
  printf '{"data":[%s]}' "$out"
}

# The full catalog every case starts from, held as an ID LIST rather than a JSON
# literal. The variants below are a FILTER over this list, so a new id is added
# in exactly one place.
ALL_MODEL_IDS=(
  gpt-6-astra gpt-5.3-codex-spark
  grok-4.6 grok-composer-2.5-fast
  kimi-k3 kimi-k3-256k kimi-k2.7-code
  gpt-5.6-sol gpt-5.6-terra gpt-5.6-luna
)
ALL_MODELS="$(catalog_of "${ALL_MODEL_IDS[@]}")"

# How many rows the packaged table is expected to publish. A LITERAL,
# deliberately not derived from share/models.tsv: a count read out of the
# implementation agrees with it by construction and would assert nothing.
EXPECTED_ROWS=6

# ALL_MODELS minus the named id(s). It REFUSES to return a catalog it did not
# actually shrink, so an absence assertion cannot pass for the wrong reason.
catalog_without() {
  local -a keep=()
  local id want found=0 drop
  for id in "${ALL_MODEL_IDS[@]}"; do
    drop=0
    for want in "$@"; do
      if [ "$id" = "$want" ]; then
        drop=1
        found=$((found + 1))
        break
      fi
    done
    [ "$drop" = 1 ] || keep+=("$id")
  done
  if [ "$found" -ne "$#" ]; then
    printf 'catalog_without: not in ALL_MODEL_IDS: %s\n' "$*" >&2
    return 1
  fi
  catalog_of "${keep[@]}"
}

# Remote list uses the profile file, one probe, and classifies every row from it.
setup_case
write_remote_profile TEST
REMOTE_WITHOUT_LUNA="$(catalog_without gpt-5.6-luna)"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  FAKE_CURL_BODY="$REMOTE_WITHOUT_LUNA" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list
assert_eq 0 "$RC" "remote list succeeds"
assert_eq 1 "$(curl_calls)" "remote list probes exactly once"
assert_contains "$OUT" $'cc-harness:sol\tgpt-5.6-sol\tyes\t-' "remote list marks a returned model available"
assert_contains "$OUT" $'cc-harness:luna\tgpt-5.6-luna\tno\tremote models missing: gpt-5.6-luna' "remote list marks a missing model unavailable"
assert_eq "$EXPECTED_ROWS" "$(printf '%s\n' "$OUT" | grep -c '^cc-harness:')" "remote list emits every table row"
assert_eq "$EXPECTED_ROWS" "$(printf '%s\n' "$OUT" | wc -l | tr -d ' ')" "a captured listing carries no header line"
# The header is a terminal-only reading aid: forcing it on must not disturb the
# row contract, and a consumer capturing the output must never receive it.
setup_case
write_remote_profile TEST
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  FAKE_CURL_BODY="$REMOTE_WITHOUT_LUNA" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list --header
assert_eq 0 "$RC" "an explicit header is accepted"
assert_eq 'name model available note' "$(printf '%s\n' "$OUT" | sed -n 1p | tr -s ' ')" "the header names all four columns"
assert_eq "$EXPECTED_ROWS" "$(printf '%s\n' "$OUT" | grep -c '^cc-harness:')" "the header leaves every agent row intact"
# Padding is what makes the table readable: every row's `available` field has to
# start in the same column as the header's.
assert_eq 1 "$(printf '%s\n' "$OUT" | awk '{ print index($0, "yes") }' | sort -u | grep -cv '^0$')" "every available column starts at one shared offset"
assert_contains "$OUT" 'cc-harness:sol    gpt-5.6-sol    yes' "short names are padded to the widest row"

setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" list --local --no-header
assert_eq 0 "$RC" "an explicit no-header is accepted"
assert_eq "$EXPECTED_ROWS" "$(printf '%s\n' "$OUT" | wc -l | tr -d ' ')" "no-header keeps the local listing at one line per agent"

setup_case
write_remote_profile TEST
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  "$HELPER" list --bogus
assert_eq 2 "$RC" "an unknown list flag is a usage error"
assert_contains "$ERR" '--header|--no-header' "usage documents the header flags"

# The remaining branch is the default itself: no flag, no terminal → raw rows.
# Its terminal twin is a single `[ -t 1 ]`, and driving a pty from this suite
# proved flaky (`script` fails in a non-interactive runner and takes the shared
# fake-curl log down with it), so that half is verified by hand instead.
setup_case
write_remote_profile TEST
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  FAKE_CURL_BODY="$REMOTE_WITHOUT_LUNA" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list
assert_eq 0 "$RC" "the default captured listing succeeds"
assert_not_contains "$(printf '%s\n' "$OUT" | sed -n 1p)" 'available' "the default captured listing starts with an agent row"
# The test gateway sees the request as sent. Header names arrive canonicalized by
# the test gateway's HTTP server (names are case-insensitive on the wire).
assert_contains "$(gateway_request)" "GET /c/$CASE_ID/v1/models" "remote probe uses the configured gateway's models URL"
assert_contains "$(gateway_request)" 'Authorization: Bearer remote-api-secret' "remote probe sends bearer authentication"
assert_contains "$(gateway_request)" 'Cf-Access-Client-Id: access-id-secret' "remote probe sends the Access client ID"
assert_contains "$(gateway_request)" 'Cf-Access-Client-Secret: access-client-secret' "remote probe sends the Access client secret"
assert_secret_safe "$ERR" "remote list stderr hides secrets"

# An explicit profile override wins without reading or trusting client.env.
setup_case
mkdir -p "$CASE_HOME/.config/cliproxy"
printf 'this would be rejected if parsed\n' > "$CASE_HOME/.config/cliproxy/client.env"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_CURL_BODY="$ALL_MODELS" \
  CLIPROXY_PROFILE=OVERRIDE CLIPROXY_API_KEY_OVERRIDE=override-key \
  CLIPROXY_CF_ACCESS_OVERRIDE_CLIENT_ID=override-id \
  CLIPROXY_CF_ACCESS_OVERRIDE_CLIENT_SECRET=override-secret \
  "$HELPER" exec sol -- "$TARGET"
assert_eq 0 "$RC" "explicit profile override succeeds"
assert_contains "$OUT" 'AUTH=override-key' "explicit profile resolves its API key indirectly"
assert_contains "$OUT" 'CF-Access-Client-Id: override-id' "explicit profile resolves its Access ID indirectly"
assert_not_contains "$ERR" 'override-secret' "explicit profile secret is absent from stderr"

# Remote exec merges headers safely and leaves both proxy bypass variables alone.
setup_case
write_remote_profile TEST
INHERITED_HEADERS=$'X-Trace: one\r\ncf-access-client-id: stale-id\r\nCF-Access-Client-Id: stale-id-2\r\nCF-Access-Client-Secret: stale-secret\r\ncf-access-client-secret: stale-secret-2\r\nX-Other: two'
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_CURL_BODY="$ALL_MODELS" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  ANTHROPIC_CUSTOM_HEADERS="$INHERITED_HEADERS" \
  NO_PROXY='corp.example' no_proxy='lower.example' \
  CLAUDE_CODE_USE_BEDROCK=1 CLAUDE_CODE_USE_VERTEX=1 \
  CLAUDE_CODE_USE_GATEWAY=1 CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD=1 \
  "$HELPER" exec sol -- "$TARGET" alpha beta
assert_eq 0 "$RC" "remote exec succeeds"
assert_eq 1 "$(curl_calls)" "remote exec probes exactly once"
assert_contains "$OUT" "BASE=$(remote_url)" "remote exec exports the configured HTTPS base URL"
assert_contains "$OUT" 'AUTH=remote-api-secret' "remote exec exports the selected API key"
assert_contains "$OUT" 'ROUTE=remote' "remote exec identifies the route"
assert_contains "$OUT" 'NO_PROXY=corp.example' "remote exec preserves NO_PROXY"
assert_contains "$OUT" 'no_proxy=lower.example' "remote exec preserves no_proxy"
assert_contains "$OUT" $'CUSTOM_BEGIN\nX-Trace: one\nX-Other: two\nCF-Access-Client-Id: access-id-secret\nCF-Access-Client-Secret: access-client-secret\nCUSTOM_END' "remote header merge preserves order and canonicalizes Access headers"
assert_not_contains "$OUT" 'stale-id' "remote header merge removes inherited Access ID"
assert_not_contains "$OUT" 'stale-secret' "remote header merge removes inherited Access secret"
assert_contains "$OUT" 'BEDROCK=' "remote exec clears provider selectors"
assert_contains "$OUT" 'GATEWAY=' "remote exec clears the gateway selector"
assert_contains "$OUT" 'ANTHROPIC_GOOGLE=' "remote exec clears the Anthropic Google selector"
assert_contains "$OUT" 'MODEL=gpt-5.6-sol' "remote exec exports the primary model"
assert_contains "$OUT" 'ARG=alpha' "remote exec preserves target argv"
assert_secret_safe "$ERR" "remote exec stderr hides secrets"

# Header rejection covers malformed lines, case-insensitive duplicates, auth,
# and control characters without disclosing any inherited value.
for header_case in malformed duplicate authorization proxy_authorization x_api_key bare_cr control; do
  setup_case
  write_remote_profile TEST
  case "$header_case" in
    malformed) bad_headers='No-Colon' ;;
    duplicate) bad_headers=$'X-Test: one\nx-test: two' ;;
    authorization) bad_headers='Authorization: inherited-secret' ;;
    proxy_authorization) bad_headers='Proxy-Authorization: inherited-secret' ;;
    x_api_key) bad_headers='X-Api-Key: inherited-secret' ;;
    bare_cr) bad_headers=$'X-Test: one\rX-Other: two' ;;
    control) bad_headers=$'X-Test: one\tbad' ;;
  esac
  run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
    FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_CURL_BODY="$ALL_MODELS" \
    CLIPROXY_API_KEY_TEST=remote-api-secret \
    CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
    CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
    ANTHROPIC_CUSTOM_HEADERS="$bad_headers" \
    "$HELPER" exec sol -- "$TARGET"
  assert_eq 1 "$RC" "remote headers reject $header_case input"
  assert_secret_safe "$ERR" "remote header $header_case error hides secrets"
done

# The list CLI exposes the same explicit local route.
setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" list --local
assert_eq 0 "$RC" "local list succeeds"
assert_eq 0 "$(curl_calls)" "local list uses only the reachability probe"
assert_contains "$OUT" $'cc-harness:sol\tgpt-5.6-sol\tyes\t-' "local list keeps provider availability checks"

# Local mode retains the prior token, OAuth, gateway, and loopback bypass recipe.
setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  NO_PROXY='corp.example' no_proxy='lower.example' \
  ANTHROPIC_CUSTOM_HEADERS=$'CF-Access-Client-Id: remote-id\nX-Local: kept\ncf-access-client-id: remote-id-2\nCF-Access-Client-Secret: remote-secret\ncf-access-client-secret: remote-secret-2' \
  CLAUDE_CODE_USE_GATEWAY=1 CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD=1 \
  "$HELPER" exec --local sol -- "$TARGET"
assert_eq 0 "$RC" "local exec succeeds"
assert_eq 0 "$(curl_calls)" "local exec uses only the reachability probe"
assert_contains "$OUT" "BASE=http://127.0.0.1:$GATEWAY_LIVE_PORT" "local exec exports the loopback gateway"
assert_contains "$OUT" 'AUTH=local-token' "local exec exports the local token"
assert_contains "$OUT" 'ROUTE=local' "local exec identifies the route"
assert_contains "$OUT" 'NO_PROXY=127.0.0.1,localhost,corp.example,lower.example' "loopback bypass is added only locally"
assert_contains "$OUT" 'no_proxy=127.0.0.1,localhost,corp.example,lower.example' "local lowercase bypass matches the merged list"
assert_contains "$OUT" $'CUSTOM_BEGIN\nX-Local: kept\nCUSTOM_END' "local exec preserves non-Access custom headers"
assert_not_contains "$OUT" 'remote-id' "local exec removes every inherited Access ID"
assert_not_contains "$OUT" 'remote-secret' "local exec removes every inherited Access secret"
assert_contains "$OUT" 'GATEWAY=' "local exec clears the gateway selector"
assert_contains "$OUT" 'ANTHROPIC_GOOGLE=' "local exec clears the Anthropic Google selector"

setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_NC_RC=1 FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" exec --local sol -- "$TARGET"
assert_eq 1 "$RC" "local exec rejects an unreachable loopback gateway"
assert_contains "$ERR" "nothing listening on 127.0.0.1:$GATEWAY_CLOSED_PORT" "local reachability error names the local gateway"
assert_eq 0 "$(curl_calls)" "local reachability failure never invokes curl"

# Local header normalization rejects the same unsafe non-CF input as remote.
for header_case in malformed duplicate authorization control; do
  setup_case
  write_local_credentials
  case "$header_case" in
    malformed) bad_headers='No-Colon' ;;
    duplicate) bad_headers=$'X-Test: one\nx-test: two' ;;
    authorization) bad_headers='Authorization: inherited-secret' ;;
    control) bad_headers=$'X-Test: one\tbad' ;;
  esac
  run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
    FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
    ANTHROPIC_CUSTOM_HEADERS="$bad_headers" \
    "$HELPER" exec --local sol -- "$TARGET"
  assert_eq 1 "$RC" "local headers reject $header_case input"
done

# The local route is gated on the cliproxy-auth fallback marker, and that gate
# runs BEFORE the token, gateway and OAuth probes.
setup_case
write_local_credentials
rm -f "$CASE_HOME/.cache/cliproxy-auth/local-ready.json"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" list --local
assert_eq 0 "$RC" "local list still exits 0 without a fallback marker"
assert_eq "$EXPECTED_ROWS" "$(printf '%s\n' "$OUT" | grep -c '^cc-harness:')" "a missing marker still lists every row"
assert_contains "$OUT" $'cc-harness:sol\tgpt-5.6-sol\tno\tthe local route is not prepared' "a missing marker marks every row unavailable"
assert_contains "$(printf '%s\n' "$OUT" | grep '^cc-harness:grok')" 'grok-4.6 is the last-known model, not current discovery (this route has no catalog)' "a blocked local listing still labels the last-known model"
assert_eq 4 "$(printf '%s\n' "$OUT" | awk -F'\t' '$1 == "cc-harness:grok" { print NF }')" "and stays at four columns"
assert_contains "$OUT" 'cliproxy-auth prepare-local' "the missing marker note names the fix"

setup_case
write_local_credentials
rm -f "$CASE_HOME/.cache/cliproxy-auth/local-ready.json"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" exec --local sol -- "$TARGET"
assert_eq 3 "$RC" "local exec refuses without a fallback marker"
assert_contains "$ERR" 'cliproxy-auth prepare-local' "the exec refusal names the fix"

setup_case
write_local_credentials
write_fallback_marker "$(marker_stamp -3600)"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" exec --local sol -- "$TARGET"
assert_eq 3 "$RC" "local exec refuses an expired fallback marker"
assert_contains "$ERR" 'the local fallback expired' "the expired marker is named as expired"
assert_contains "$ERR" 'cliproxy-auth prepare-local' "the expired marker names the fix"

setup_case
write_local_credentials
write_fallback_marker "$(marker_stamp -3600)"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" list --local
assert_contains "$OUT" 'the local fallback expired' "local list reports an expired marker per row"

setup_case
write_local_credentials
mkdir -p "$CASE_HOME/.cache/cliproxy-auth"
printf 'not json\n' > "$CASE_HOME/.cache/cliproxy-auth/local-ready.json"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" exec --local sol -- "$TARGET"
assert_eq 3 "$RC" "local exec refuses a corrupt fallback marker"
assert_contains "$ERR" 'marker is unusable' "a corrupt marker is reported as unusable"

# Marker first: with neither a marker nor a token nor a live gateway, the marker
# is what the user hears about.
setup_case
mkdir -p "$CASE_HOME/.cli-proxy-api"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_NC_RC=1 FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" exec --local sol -- "$TARGET"
assert_eq 3 "$RC" "the marker gate precedes the token and gateway probes"
assert_contains "$ERR" 'the local route is not prepared' "the marker reason wins over the token reason"
assert_not_contains "$ERR" 'no usable token' "the token probe never ran"
assert_not_contains "$ERR" 'nothing listening' "the gateway probe never ran"

# A valid marker does not weaken any of the existing local checks.
setup_case
write_local_credentials
rm -f "$CASE_HOME/.cli-proxy-api/client.key"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" exec --local sol -- "$TARGET"
assert_eq 3 "$RC" "a prepared fallback still requires the gateway token"
assert_contains "$ERR" 'no usable token' "the token probe runs once the marker is valid"

# HTTP, redirect, transport, JSON, and model errors are classified distinctly.
# The last field is the number of requests the gateway observes: a transport
# failure happens before any request is delivered — a TLS failure above all
# must never hand the secrets to the untrusted peer.
for spec in \
  '401|0|{}|proxy API key rejected|1' \
  '403|0|{}|Cloudflare Access rejected|1' \
  '502|0|{}|proxy origin unavailable|1' \
  '302|0|{}|returned a redirect|1' \
  '200|7|{}|network request failed|0' \
  '200|60|{}|TLS validation failed|0' \
  '200|0|not-json|invalid JSON|1'; do
  setup_case
  write_remote_profile TEST
  IFS='|' read -r status curl_rc body expected requests <<< "$spec"
  run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
    FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
    FAKE_CURL_STATUS="$status" FAKE_CURL_RC="$curl_rc" FAKE_CURL_BODY="$body" \
    CLIPROXY_API_KEY_TEST=remote-api-secret \
    CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
    CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
    "$HELPER" exec sol -- "$TARGET"
  assert_eq 1 "$RC" "remote probe failure exits 1 ($expected)"
  assert_contains "$ERR" "$expected" "remote probe classifies $expected"
  assert_eq "$requests" "$(curl_calls)" "failed remote exec still probes exactly once ($expected)"
  assert_secret_safe "$ERR" "remote $expected error hides secrets"
done

setup_case
write_remote_profile TEST
MISSING_SOL="$(catalog_without gpt-5.6-sol)"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_CURL_BODY="$MISSING_SOL" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" exec sol -- "$TARGET"
assert_eq 1 "$RC" "remote exec rejects a missing requested model"
assert_contains "$ERR" 'remote models missing: gpt-5.6-sol' "missing-model error names only the model"
assert_secret_safe "$ERR" "missing-model stderr hides secrets"

setup_case
write_remote_profile TEST
MISSING_HAIKU="$(catalog_without gpt-5.3-codex-spark)"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_CURL_BODY="$MISSING_HAIKU" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list
assert_eq 0 "$RC" "remote list completes when a tier model is missing"
assert_contains "$OUT" $'cc-harness:sol\tgpt-5.6-sol\tno\tremote models missing: gpt-5.3-codex-spark' "remote list validates exported tier models"
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\tyes\t-' "missing Codex tier does not affect unrelated rows"
assert_secret_safe "$ERR" "missing-tier stderr hides secrets"

# --- astra --------------------------------------------------------------------
#
# astra is the newest codex-family row and the first that does NOT share the
# sol/terra/luna tier set, so it needs its own list/exec/absence coverage: a
# fixture that satisfies those three says nothing about this row.

setup_case
write_remote_profile TEST
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_CURL_BODY="$ALL_MODELS" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list
assert_eq 0 "$RC" "remote list succeeds with a full catalog"
assert_contains "$OUT" $'cc-harness:astra\tgpt-6-astra\tyes\t-' "a complete catalog makes astra available"

# The whole ladder has to reach the environment: the row's own primary, its own
# haiku rung, and the measured ceiling — a wrong ceiling is invisible in-session
# until auto-compact fires at the wrong point.
setup_case
write_remote_profile TEST
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_CURL_BODY="$ALL_MODELS" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" exec astra -- "$TARGET"
assert_eq 0 "$RC" "remote exec runs the astra row"
assert_contains "$OUT" 'MODEL=gpt-6-astra' "astra exec exports its own primary"
assert_contains "$OUT" 'OPUS=gpt-6-astra' "astra opus rung is the primary itself"
assert_contains "$OUT" 'SONNET=gpt-6-astra' "astra sonnet rung mirrors the primary, so it holds the announced window"
assert_contains "$OUT" 'HAIKU=gpt-5.6-luna' "astra haiku rung is the verified 5.6 model, not the unverified spark id"
assert_contains "$OUT" 'CONTEXT=900000' "astra exports the measured context ceiling"
assert_secret_safe "$ERR" "astra exec stderr hides secrets"

# Catalog absence, both columns. A vanished id is exactly how the codex rows went
# unavailable once (gpt-5.4-mini), so astra's own ids get the same check.
setup_case
write_remote_profile TEST
MISSING_ASTRA="$(catalog_without gpt-6-astra)"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_CURL_BODY="$MISSING_ASTRA" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list
assert_eq 0 "$RC" "remote list completes when astra is absent from the catalog"
assert_contains "$OUT" $'cc-harness:astra\tgpt-6-astra\tno\tremote models missing: gpt-6-astra' "an absent astra primary marks only its own row unavailable"
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\tyes\t-' "an absent astra leaves unrelated rows alone"

setup_case
write_remote_profile TEST
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_CURL_BODY="$REMOTE_WITHOUT_LUNA" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list
assert_eq 0 "$RC" "remote list completes when the astra haiku rung is absent"
# gpt-5.6-luna is astra's haiku rung AND the luna row's primary, so losing it has
# to take out both rows, each naming the id from its own perspective.
assert_contains "$OUT" $'cc-harness:astra\tgpt-6-astra\tno\tremote models missing: gpt-5.6-luna' "astra validates its own exported tier"
assert_contains "$OUT" $'cc-harness:luna\tgpt-5.6-luna\tno\tremote models missing: gpt-5.6-luna' "the shared id takes the luna row down with it"
assert_contains "$OUT" $'cc-harness:sol\tgpt-5.6-sol\tyes\t-' "the astra tier gap does not touch the sol row"

# Invalid profile syntax and invalid secret bytes are rejected before execution.
setup_case
mkdir -p "$CASE_HOME/.config/cliproxy"
printf 'CLIPROXY_PROFILE=bad-name\n' > "$CASE_HOME/.config/cliproxy/client.env"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" list
assert_eq 3 "$RC" "invalid profile syntax is a capability error"
assert_contains "$ERR" 'must match [A-Z0-9_]+' "invalid profile syntax has a safe diagnostic"

setup_case
write_remote_profile TEST
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  CLIPROXY_API_KEY_TEST=$'remote-api-secret\nsecond-line' \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list
assert_eq 3 "$RC" "control characters in remote secrets are rejected"
assert_contains "$ERR" 'contains a control character' "control-character error is generic"
assert_secret_safe "$ERR" "secret-validation stderr hides secrets"

setup_case
write_remote_profile TEST
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  CLIPROXY_API_KEY_TEST='' \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list
assert_eq 3 "$RC" "empty remote credentials are rejected"
assert_contains "$ERR" 'is empty' "empty-credential error is generic"
assert_secret_safe "$ERR" "empty-credential stderr hides other secrets"

# --- canonical model discovery ------------------------------------------------

# Every grok id the live xAI catalog exposed when this was written (probed
# 2026-08-14 against the author's gateway, unchanged on the 2026-09-21
# re-probe; grok-4.7 joined it later that day and is added per case), verbatim. The point
# of carrying the whole set is that the discovery pattern is only meaningful
# against the real mix of canonical releases and variants; extra ids for a case
# are appended.
LIVE_GROK_IDS=(
  grok-3-mini grok-3-mini-fast
  grok-4.20-0309-non-reasoning grok-4.20-0309-reasoning grok-4.20-multi-agent-0309
  grok-4.3 grok-4.5 grok-4.6
  grok-build-0.1 grok-composer-2.5-fast
  grok-imagine-image grok-imagine-image-quality
  grok-imagine-video grok-imagine-video-1.5-preview
)

xai_catalog() {
  catalog_of "${LIVE_GROK_IDS[@]}" "$@"
}

# The per-case remote environment as an array — `env -i` needs the words kept
# apart, and an unquoted command substitution would leave that to word splitting.
REMOTE_ENV=()
set_remote_env() {
  REMOTE_ENV=(
    "HOME=$CASE_HOME" "TMPDIR=$CASE_DIR/tmp" "PATH=$COMMON_PATH"
    "FAKE_CURL_COUNT_FILE=$CURL_COUNT" "FAKE_CURL_LOG_FILE=$CURL_LOG"
    "CLIPROXY_API_KEY_TEST=remote-api-secret"
    "CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret"
    "CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret"
  )
}

# The whole live catalog resolves to the pinned model: not one of the 4.20
# variants wins despite 20 > 6, and no note is raised because nothing was
# withheld — this is the case that proves the pattern excludes dated, reasoning,
# non-reasoning, multi-agent, build, composer, image, video and preview ids.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog)" \
  "$HELPER" list --no-header
assert_eq 0 "$RC" "discovery over the live catalog succeeds"
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\tyes\t-' "the live catalog selects grok-4.6, its newest candidate, with no note"
assert_not_contains "$OUT" 'grok-4.20' "no 4.20 variant is ever selected"
assert_not_contains "$OUT" 'reasoning' "reasoning and non-reasoning variants stay out of the selection"
assert_not_contains "$OUT" 'multi-agent' "multi-agent variants stay out of the selection"
assert_not_contains "$OUT" 'grok-3' "a superseded major version is never selected"

# The ladder against today's live catalog: newest on the opus tier, the one
# below it on the sonnet tier, composer untouched on haiku. This is what Claude
# Code's /model picker shows as Custom Opus / Custom Sonnet / Custom Haiku.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog)" \
  "$HELPER" exec grok -- "$TARGET"
assert_eq 0 "$RC" "exec over the live catalog succeeds"
assert_contains "$OUT" 'MODEL=grok-4.6' "the live ladder puts the newest model on the primary"
assert_contains "$OUT" 'OPUS=grok-4.6' "the live ladder puts the newest model on the opus tier"
assert_contains "$OUT" 'SONNET=grok-4.5' "the live ladder puts the predecessor on the sonnet tier"
assert_contains "$OUT" 'HAIKU=grok-composer-2.5-fast' "the haiku tier stays on composer"

# With only one canonical model in reach there is no rung below, so the sonnet
# tier mirrors the primary rather than falling off the table's stale value.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_of grok-4.6 grok-composer-2.5-fast)" \
  "$HELPER" exec grok -- "$TARGET"
assert_eq 0 "$RC" "exec with a single canonical model succeeds"
assert_contains "$OUT" 'MODEL=grok-4.6' "a lone canonical model is the primary"
assert_contains "$OUT" 'SONNET=grok-4.6' "the sonnet tier mirrors the primary when nothing older exists"

# An override collapses the whole ladder onto one model — that is the point of
# pinning: one reproducible id, not a pair that still drifts.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK=grok-4.5 \
  FAKE_CURL_BODY="$(xai_catalog)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'MODEL=grok-4.5' "an override sets the primary"
assert_contains "$OUT" 'OPUS=grok-4.5' "an override sets the opus tier"
assert_contains "$OUT" 'SONNET=grok-4.5' "an override collapses the sonnet tier onto the same model"
assert_contains "$OUT" 'HAIKU=grok-composer-2.5-fast' "an override still leaves the haiku tier alone"

# A FUTURE release is selected with no edit to the helper — the whole point. Its
# window is known nowhere, so it is ASSUMED to be its predecessor's — and the note
# says exactly that, naming the release the number was borrowed from.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog grok-4.8)" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.8\tyes\tauto-selected grok-4.8 (candidates: grok-4.8 grok-4.6 grok-4.5 grok-4.3; last-known: grok-4.6); context window 500000 ASSUMED from predecessor grok-4.7, not verified' "a future 4.x is selected unedited, with candidates and the assumed window named"
LIST_MODEL="$(printf '%s\n' "$OUT" | awk -F'\t' '$1 == "cc-harness:grok" { print $2 }')"
assert_eq 4 "$(printf '%s\n' "$OUT" | awk -F'\t' '$1 == "cc-harness:grok" { print NF }')" "a selection note keeps the row at four columns"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog grok-4.8)" \
  "$HELPER" exec grok -- "$TARGET" --flag 'two words'
assert_eq 0 "$RC" "exec with a future release succeeds"
assert_eq "MODEL=$LIST_MODEL" "$(printf '%s\n' "$OUT" | grep '^MODEL=')" "exec exports exactly the model list reported for the same catalog"
assert_contains "$OUT" 'SUBAGENT=grok-4.8' "the subagent model follows the selection"
assert_contains "$OUT" 'OPUS=grok-4.8' "the opus tier follows the selection"
assert_contains "$OUT" 'SONNET=grok-4.6' "the sonnet tier takes the rung below the new primary"
assert_contains "$OUT" 'HAIKU=grok-composer-2.5-fast' "a tier pointed elsewhere never follows the selection"
assert_contains "$OUT" 'CONTEXT=500000' "a future release is given its predecessor's window"
assert_contains "$OUT" $'ARG=--flag\nARG=two words' "the selection note never leaks into the target argv"
assert_contains "$ERR" 'auto-selected grok-4.8' "exec names the automatic selection on stderr"
assert_contains "$ERR" 'ASSUMED from predecessor grok-4.7, not verified' "exec names the assumption on stderr, every run"
assert_secret_safe "$ERR" "the selection note carries no secret"

# A future MAJOR inside the allow-list, and numeric ordering across all three
# shapes that a string or decimal sort gets wrong: 5.0 > 4.20 > 4.9.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog grok-4.9 grok-4.20 grok-5.0)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'MODEL=grok-5.0' "5.0 outranks every 4.x"
assert_contains "$OUT" 'SONNET=grok-4.20' "4.20 outranks 4.9 by component, not by decimal value"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog grok-4.9 grok-4.20)" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.20\tyes\t' "4.20 is the newest of 4.6, 4.9 and 4.20"
assert_contains "$OUT" 'candidates: grok-4.20 grok-4.9 grok-4.6 grok-4.5 grok-4.3;' "candidates are listed newest first"

# Everything that LOOKS newer but is not a canonical major.minor id of an allowed
# major. None of it may be selected or even named as a candidate.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(xai_catalog grok-5 grok-6.0 grok-6 grok-3.9 grok-4.6.1 grok-4.9-build-fast \
    grok-4.9-fast grok-4.9-0101 grok-4.9-reasoning grok-4.9-preview grok-composer-4.9 \
    grok-4.9-image 'grok-4.9 ' ' grok-4.9' 'Grok-4.9' 'grok-4.' 'grok-.9' 'xgrok-4.9' \
    'grok-4.9;id' 'grok-4.99999999999999999999')" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\tyes\t-' "bare majors, other majors, patch ids, variants and malformed ids are never selected"
assert_not_contains "$OUT" 'candidates' "and none of them is reported as a candidate"

# A long run of digits is not a version: it must lose to a real release rather
# than win by being compared as "not older" — and it must not abort the helper.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_of grok-4.99999999999999999999 grok-4.6 grok-4.5 grok-composer-2.5-fast)" \
  "$HELPER" list --no-header
assert_eq 0 "$RC" "an overlong version component does not abort the listing"
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\tyes\t-' "an overlong version component is not a candidate"

# Leading zeros are decimal, not octal: 4.08 is the eighth release.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog grok-4.08)" \
  "$HELPER" list --no-header
assert_eq 0 "$RC" "a zero-padded minor does not abort the listing"
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.08\tyes\t' "a zero-padded minor compares as a decimal number"

# Catalog builder for entries that carry context metadata: id=window pairs.
catalog_with_context() {
  local out="" pair
  for pair in "$@"; do
    [ -z "$out" ] || out="$out,"
    out="$out{\"id\":\"${pair%%=*}\",\"context_length\":${pair#*=}}"
  done
  printf '{"data":[%s]}' "$out"
}

# THE PREDECESSOR ASSUMPTION, bounded. It chains from a VERIFIED row only: 4.9
# next to an equally unknown 4.8 borrows from 4.7, never from 4.8.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog grok-4.8 grok-4.9)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'MODEL=grok-4.9' "two unknown releases: the newest is selected"
assert_contains "$ERR" 'ASSUMED from predecessor grok-4.7, not verified' "an assumption never chains from another assumption"
assert_contains "$OUT" 'SONNET=grok-4.8' "a rung with the same assumed window holds the ceiling"

# An EXPLICIT limit always wins over the assumption — above all a smaller one.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_with_context grok-4.8=128000 grok-4.6=500000 grok-composer-2.5-fast=200000)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=128000' "a smaller catalog limit is respected, not overridden by the assumption"
assert_not_contains "$ERR" 'ASSUMED' "an explicit limit is not reported as an assumption"
assert_contains "$OUT" 'SONNET=grok-4.6' "a larger rung still holds the smaller ceiling"

# It DOES cross a major: a new generation gets at least its predecessor's window
# (windows grow, they do not shrink) — still named as an assumption.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog grok-4.7 grok-5.0)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'MODEL=grok-5.0' "the first release of a new major is selected"
assert_contains "$OUT" 'CONTEXT=500000' "and gets at least the window of the newest verified 4.x"
assert_contains "$ERR" 'context window 500000 ASSUMED from predecessor grok-4.7, not verified' "named as an assumption across the major boundary too"
assert_contains "$OUT" 'SONNET=grok-4.7' "the verified 4.x rung holds the assumed ceiling"

# Nothing is assumed for a release OLDER than everything verified, for a
# variant, or for an agent that does not discover.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK=grok-4.1 \
  FAKE_CURL_BODY="$(xai_catalog grok-4.1)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=200000' "a release below every verified one has no predecessor to assume from"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK=grok-4.8-fast \
  FAKE_CURL_BODY="$(xai_catalog grok-4.8-fast)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=200000' "a variant never inherits a canonical release's window"

# grok-4.7 itself needs no assumption: its window is provider-documented.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(xai_catalog grok-4.7)" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.7\tyes\tauto-selected grok-4.7 (candidates: grok-4.7 grok-4.6 grok-4.5 grok-4.3; last-known: grok-4.6)' "grok-4.7 is selected from the catalog"
assert_not_contains "$(printf '%s\n' "$OUT" | grep '^cc-harness:grok')" 'ASSUMED' "with a documented window, not an assumed one"

# METADATA PRESENT. The catalog's own context_length is believed for a candidate,
# but only up to the largest window this route has measured for the family.

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_with_context grok-4.8=400000 grok-4.6=500000 grok-composer-2.5-fast=200000)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'MODEL=grok-4.8' "a release with catalog metadata is selected"
assert_contains "$OUT" 'CONTEXT=400000' "catalog metadata supplies the ceiling"
assert_contains "$OUT" 'SONNET=grok-4.6' "a rung that holds the announced ceiling is kept"
assert_not_contains "$ERR" 'context window unknown' "a known window is not called unknown"
assert_contains "$ERR" 'context window 400000 from catalog metadata, not measured' "an advertised ceiling is named as advertised"

# jq keeps a number's literal form, and `[ 3E+5 -le N ]` is an error rather than
# a comparison — which once read as "above the cap" and inflated the window.
for float_ctx in '300000.0' '3e5' '300000.00' '3.0E5'; do
  setup_case
  write_remote_profile TEST
  set_remote_env
  run_capture env -i "${REMOTE_ENV[@]}" \
    FAKE_CURL_BODY="{\"data\":[{\"id\":\"grok-4.8\",\"context_length\":$float_ctx},{\"id\":\"grok-composer-2.5-fast\"}]}" \
    "$HELPER" exec grok -- "$TARGET"
  assert_eq 0 "$RC" "context_length $float_ctx does not fail the probe"
  assert_contains "$OUT" 'CONTEXT=300000' "an integral $float_ctx is read as 300000, never inflated to the cap"
  assert_not_contains "$ERR" 'integer expression' "context_length $float_ctx never reaches a shell comparison raw"
done

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_with_context grok-4.8=2000000 grok-4.6=500000 grok-composer-2.5-fast=200000)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=500000' "an advertised window is clamped to the largest measured one"

# A measured value outranks an advertised one: the upstream registry lists
# grok-4.3 at 1000000, this route measured 500000 — and the clamp is not what
# decides it, so use a number BELOW the cap to tell the two sources apart.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_with_context grok-4.6=300000 grok-4.5=300000 grok-composer-2.5-fast=200000)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=500000' "a measured window outranks catalog metadata"

# The session ceiling is the PRIMARY's and is never re-exported on /model, so a
# rung that cannot hold it must not be offered: the tier mirrors the primary.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_with_context grok-4.9=500000 grok-4.8=300000 grok-composer-2.5-fast=200000)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=500000' "the primary's window is announced"
assert_contains "$OUT" 'SONNET=grok-4.9' "a rung with a smaller window is skipped rather than overflowed"

# Hostile or sloppy metadata leaves the window UNKNOWN — never a parsed guess,
# and never a failed probe: the catalog itself is still valid. grok-4.1 on
# purpose: it is older than every verified release, so there is no predecessor to
# assume from and "unknown" is observable as the conservative ceiling.
for bad_ctx in '"500000"' '-1' '0' '999' '1024' '31999' '500000.5' '99999999999' 'null' 'true' '[500000]' '{"n":500000}'; do
  setup_case
  write_remote_profile TEST
  set_remote_env
  run_capture env -i "${REMOTE_ENV[@]}" \
    FAKE_CURL_BODY="{\"data\":[{\"id\":\"grok-4.1\",\"context_length\":$bad_ctx},{\"id\":\"grok-composer-2.5-fast\"}]}" \
    "$HELPER" exec grok -- "$TARGET"
  assert_eq 0 "$RC" "context_length $bad_ctx does not fail the probe"
  assert_contains "$OUT" 'CONTEXT=200000' "context_length $bad_ctx leaves the window unknown"
done

# Metadata is matched per id, exactly: an id carrying the table separator cannot
# donate its window to the candidate it imitates, and a variant's advertised
# window is never believed at all.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY='{"data":[{"id":"grok-4.1|450000","context_length":450000},{"id":"grok-4.1"},{"id":"grok-composer-2.5-fast"}]}' \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'MODEL=grok-4.1' "a separator-bearing id does not displace the real candidate"
assert_contains "$OUT" 'CONTEXT=200000' "a separator-bearing id cannot forge another id's window"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK=grok-4.20-0309-reasoning \
  FAKE_CURL_BODY="$(catalog_with_context grok-4.20-0309-reasoning=400000 grok-composer-2.5-fast=200000)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=200000' "a variant's advertised window is never believed"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_KIMI=kimi-k2.7-code \
  FAKE_CURL_BODY="$(catalog_with_context kimi-k2.7-code=1000000 kimi-k3=1000000 kimi-k3-256k=1000000)" \
  "$HELPER" exec kimi -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=200000' "an agent without discovery never reads catalog metadata"

# LATEST MEANS CURRENTLY OFFERED. A valid catalog that no longer offers the
# last-known model is an answer: the newest candidate it DOES offer is selected,
# downwards if need be, and the move is named.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_of grok-4.3 grok-4.5 grok-composer-2.5-fast)" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.5\tyes\tauto-selected grok-4.5 (candidates: grok-4.5 grok-4.3; last-known: grok-4.6)' "a valid catalog below the last-known model selects its newest candidate"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_of grok-4.3 grok-4.5 grok-composer-2.5-fast)" \
  "$HELPER" exec grok -- "$TARGET"
assert_eq 0 "$RC" "exec follows a catalog below the last-known model"
assert_contains "$OUT" 'MODEL=grok-4.5' "exec exports what the catalog offers, not the stale last-known model"
assert_contains "$OUT" 'SONNET=grok-4.3' "the rung below follows downwards too"
assert_contains "$OUT" 'CONTEXT=500000' "a measured older release keeps its measured window"

# A VALID catalog with no candidate substitutes nothing — no variant, no other
# major — and the last-known model is labelled as exactly that.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_of grok-composer-2.5-fast grok-imagine-image grok-3-mini grok-6.0)" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\tno\tremote models missing: grok-4.6, grok-4.5; catalog offers no canonical grok model — grok-4.6 is the last-known model, not current discovery' "no eligible candidate: unavailable, nothing substituted, fallback labelled"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" \
  FAKE_CURL_BODY="$(catalog_of grok-composer-2.5-fast grok-6.0)" \
  "$HELPER" exec grok -- "$TARGET"
assert_eq 1 "$RC" "exec refuses when the catalog offers no candidate"
assert_not_contains "$OUT" 'MODEL=' "nothing is exec'd on a last-known model the route does not offer"
assert_contains "$ERR" 'not current discovery' "the refusal names the fallback for what it is"

# STALE / UNAVAILABLE catalog: the model column still has to show something, and
# what it shows is labelled — for the discovering agent only.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_STATUS=503 \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\tno\tremote proxy origin unavailable (HTTP 503); grok-4.6 is the last-known model, not current discovery (catalog unavailable)' "an unavailable catalog labels the last-known model"
assert_contains "$OUT" $'cc-harness:sol\tgpt-5.6-sol\tno\tremote proxy origin unavailable (HTTP 503)' "a row without discovery carries no fallback label"
assert_not_contains "$(printf '%s\n' "$OUT" | grep '^cc-harness:sol')" 'last-known' "the label is not sprayed over rows that never discover"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY='{"data":"nope"}' \
  "$HELPER" list --no-header
assert_contains "$OUT" 'remote model probe returned invalid JSON; grok-4.6 is the last-known model, not current discovery (catalog unavailable)' "an unparsable catalog is stale, not empty"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_STATUS=503 \
  "$HELPER" exec grok -- "$TARGET"
assert_eq 1 "$RC" "exec never runs on a stale catalog"
assert_not_contains "$OUT" 'MODEL=' "no model is exported without a catalog on the remote route"

# A MISSING TIER still fails closed after discovery: selecting the primary must
# not publish a row whose other rungs the route cannot serve.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" FAKE_CURL_BODY="$(catalog_of grok-4.8 grok-4.6)" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.8\tno\tremote models missing: grok-composer-2.5-fast; auto-selected grok-4.8' "a missing haiku tier keeps the row unavailable after discovery"

# An explicit pin is checked against the catalog like any other model, and
# discovery does not quietly replace it.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK=grok-4.5 \
  FAKE_CURL_BODY="$(catalog_of grok-4.8 grok-4.6 grok-composer-2.5-fast)" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.5\tno\tremote models missing: grok-4.5; pinned to grok-4.5 via CC_HARNESS_MODEL_GROK' "a pin the catalog lacks is reported missing, never swapped for the latest"

# RESUME. The wrapper restores a recorded model through the override variable;
# a newer release in the catalog must not move a resumed session.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK=grok-4.6 \
  FAKE_CURL_BODY="$(xai_catalog grok-4.8)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'MODEL=grok-4.6' "a session recorded on grok-4.6 resumes on grok-4.6 while 4.7 is offered"
assert_contains "$OUT" 'SONNET=grok-4.5' "and keeps the ladder it was started with"
assert_contains "$OUT" 'CONTEXT=500000' "and its measured window"

setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK=grok-4.8 \
  FAKE_CURL_BODY="$(xai_catalog grok-4.8 grok-4.9)" \
  "$HELPER" exec grok -- "$TARGET"
assert_contains "$OUT" 'MODEL=grok-4.8' "a session recorded on an auto-selected release resumes on that exact release"
# Documented limit, asserted so it cannot drift silently: restoring a model that
# is not the table's own primary goes through the override path, and an override
# names ONE reproducible model — the ladder-tracking tiers collapse onto it.
assert_contains "$OUT" 'SONNET=grok-4.8' "a restored non-table release collapses the sonnet rung, as every override does"
assert_contains "$OUT" 'HAIKU=grok-composer-2.5-fast' "and leaves the haiku rung alone"
assert_contains "$OUT" 'CONTEXT=500000' "with the same assumed ceiling a fresh start gets"
assert_contains "$ERR" 'ASSUMED from predecessor grok-4.7' "and the assumption is named on a resume too"

# The resume helper's mapping needs no edit for a future release either.
run_capture env -i HOME="$TMP_ROOT" PATH="/usr/bin:/bin" "$HELPER" resolve-model grok-4.8
assert_eq $'grok\tgrok-4.8' "$OUT" "resolve-model routes a future release to grok"
run_capture env -i HOME="$TMP_ROOT" PATH="/usr/bin:/bin" "$HELPER" resolve-model grok-5.0-build
assert_eq $'grok\tgrok-5.0' "$OUT" "resolve-model normalises a future build id"

# LOCAL/REMOTE ISOLATION. The local route never fetches a catalog and never
# discovers; a native `grok` on PATH is never asked what it offers.
setup_case
write_local_credentials
cat > "$FAKE_BIN/grok" << 'FAKE_GROK'
#!/bin/bash
printf 'called\n' >> "$FAKE_GROK_LOG"
printf 'grok-4.9\n'
FAKE_GROK
chmod +x "$FAKE_BIN/grok"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" FAKE_GROK_LOG="$CASE_DIR/grok-log" \
  FAKE_CURL_BODY="$(xai_catalog grok-4.9)" \
  "$HELPER" list --local --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\tyes\tgrok-4.6 is the last-known model, not current discovery (this route has no catalog)' "the local route lists the last-known model and says so"
assert_eq 0 "$(curl_calls)" "the local route fetches no catalog"
assert_eq "" "$(cat "$CASE_DIR/grok-log" 2> /dev/null || true)" "the native grok CLI is never consulted locally"

setup_case
write_remote_profile TEST
set_remote_env
cat > "$FAKE_BIN/grok" << 'FAKE_GROK'
#!/bin/bash
printf 'called\n' >> "$FAKE_GROK_LOG"
printf 'grok-4.9\n'
FAKE_GROK
chmod +x "$FAKE_BIN/grok"
run_capture env -i "${REMOTE_ENV[@]}" FAKE_GROK_LOG="$CASE_DIR/grok-log" \
  FAKE_CURL_BODY="$(xai_catalog)" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\tyes\t-' "a native CLI that lists a newer model does not move the remote route"
assert_eq "" "$(cat "$CASE_DIR/grok-log" 2> /dev/null || true)" "the native grok CLI is never consulted remotely"
assert_eq 1 "$(curl_calls)" "discovery still costs exactly one request"

# An explicit override outranks discovery and is stated in the note.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK=grok-4.5 \
  FAKE_CURL_BODY="$(xai_catalog)" \
  "$HELPER" list --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.5\tyes\tpinned to grok-4.5 via CC_HARNESS_MODEL_GROK' "an override wins over the catalog and says so"

# An override to a model with no verified window keeps the session safe by
# falling back to the ceiling Claude Code assumes for an unknown slug.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK=grok-4.20-0309-reasoning \
  FAKE_CURL_BODY="$(xai_catalog)" \
  "$HELPER" exec grok -- "$TARGET"
assert_eq 0 "$RC" "an override to a variant model still execs"
assert_contains "$OUT" 'MODEL=grok-4.20-0309-reasoning' "an override reaches the environment verbatim"
assert_contains "$OUT" 'CONTEXT=200000' "an unverified override gets the conservative ceiling"
assert_contains "$ERR" 'context window unknown, conservative ceiling 200000' "the unknown ceiling is stated on stderr"

# A malformed override is dropped rather than exported as a broken model id.
setup_case
write_remote_profile TEST
set_remote_env
run_capture env -i "${REMOTE_ENV[@]}" CC_HARNESS_MODEL_GROK='grok 4.6' \
  FAKE_CURL_BODY="$(xai_catalog)" \
  "$HELPER" list --no-header
assert_contains "$OUT" 'CC_HARNESS_MODEL_GROK is malformed' "a malformed override is reported"
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\t' "a malformed override leaves the pin in place"

# The local route has no catalog: an override still applies, discovery cannot.
setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  CC_HARNESS_MODEL_GROK=grok-4.5 \
  "$HELPER" exec --local grok -- "$TARGET"
assert_eq 0 "$RC" "local exec with an override succeeds"
assert_contains "$OUT" 'MODEL=grok-4.5' "the local route honours an explicit override"
assert_contains "$OUT" 'CONTEXT=500000' "a verified override keeps its real ceiling on the local route"

setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  "$HELPER" list --local --no-header
assert_contains "$OUT" $'cc-harness:grok\tgrok-4.6\t' "the local route lists the deterministic pinned model"

# A pinned tier gets ITS OWN verified ceiling, not the row's. Before, only the
# primary was recognised, so `--kimi` resumed on its sonnet tier auto-compacted
# at 200000 instead of 262144 and claimed the window was unverified.
setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  CC_HARNESS_MODEL_KIMI=kimi-k3-256k \
  "$HELPER" exec --local kimi -- "$TARGET"
assert_eq 0 "$RC" "a tier override execs"
assert_contains "$OUT" 'MODEL=kimi-k3-256k' "the tier override reaches the environment"
assert_contains "$OUT" 'CONTEXT=262144' "a verified tier keeps its own real ceiling"
assert_not_contains "$ERR" 'context window unknown' "a model the table declares is never called unverified"

# Pinning the primary is the silent default; any other tier is a real change and
# says so, so a restored session never looks like an untouched one.
setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  CC_HARNESS_MODEL_KIMI=kimi-k3 \
  "$HELPER" exec --local kimi -- "$TARGET"
assert_not_contains "$ERR" 'pinned to kimi-k3' "pinning the primary stays quiet"

# …and it must stay a NO-OP, not just a quiet one. The resume path exports
# CC_HARNESS_MODEL_<AGENT>=<the row's own primary>, which used to collapse every
# tier tracking the previous rung onto that primary: a resumed astra ran its
# haiku rung on gpt-6-astra where a fresh launch runs gpt-5.6-luna, silently and
# at the primary's price. Assert the WHOLE ladder, not only the note.
setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  CC_HARNESS_MODEL_ASTRA=gpt-6-astra \
  "$HELPER" exec --local astra -- "$TARGET"
assert_eq 0 "$RC" "pinning the astra primary execs"
assert_contains "$OUT" 'MODEL=gpt-6-astra' "the resumed primary is the pinned one"
assert_contains "$OUT" 'SONNET=gpt-6-astra' "pinning the primary leaves the sonnet rung on the table value"
assert_contains "$OUT" 'HAIKU=gpt-5.6-luna' "pinning the primary leaves the haiku rung alone"
assert_contains "$OUT" 'CONTEXT=900000' "pinning the primary keeps the row ceiling"

# The collapse itself is still correct for a DIFFERENT model: an override names
# one reproducible model, so the rung that tracks the ladder follows it there.
setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  CC_HARNESS_MODEL_ASTRA=gpt-5.6-sol \
  "$HELPER" exec --local astra -- "$TARGET"
assert_contains "$OUT" 'MODEL=gpt-5.6-sol' "an override to another model still wins"
assert_contains "$OUT" 'SONNET=gpt-5.6-sol' "the tracking rung collapses onto a real override"
assert_contains "$OUT" 'HAIKU=gpt-5.6-luna' "a rung the override does not name is never rewritten"

# Sharing a row is NOT sharing a window. grok-composer-2.5-fast sits in the
# 500000 grok row but only holds 200k, so handing it the row ceiling would push
# auto-compact 300k past the real limit — a hard upstream overflow mid-session,
# the exact failure VERIFIED_CONTEXT exists to prevent.
setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  CC_HARNESS_MODEL_GROK=grok-composer-2.5-fast \
  "$HELPER" exec --local grok -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=200000' "a smaller tier never inherits the row ceiling"

# A tier whose real window is undocumented stays conservative AND says so.
setup_case
write_local_credentials
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_COUNT_FILE="$CURL_COUNT" FAKE_CURL_LOG_FILE="$CURL_LOG" \
  CC_HARNESS_MODEL_SOL=gpt-5.3-codex-spark \
  "$HELPER" exec --local sol -- "$TARGET"
assert_contains "$OUT" 'CONTEXT=200000' "an undocumented tier gets the conservative ceiling"
assert_contains "$ERR" 'context window unknown' "and the unknown ceiling is stated"

# --- resolve-model: the machine-readable contract the resume helper reads -----
# It replaced regex-scraping $AGENTS out of this file's source, so these cases
# are what keeps that table's shape from silently becoming an external contract.
setup_case
run_capture "$HELPER" resolve-model gpt-5.6-terra
assert_eq 0 "$RC" "resolve-model resolves a primary"
assert_contains "$OUT" $'terra\tgpt-5.6-terra' "a shared-tier agent is named by its primary"

setup_case
run_capture "$HELPER" resolve-model kimi-k3-256k
assert_contains "$OUT" $'kimi\tkimi-k3-256k' "resolve-model resolves a tier model"

# astra owns its primary AND its sonnet rung (both gpt-6-astra); only the haiku
# rung, gpt-5.6-luna, is borrowed — and it is luna's primary. An exact primary
# match outranks a tier match, so borrowing must not start stealing that id, and
# that same rule decides which row a session ending on the rung resumes into.
setup_case
run_capture "$HELPER" resolve-model gpt-6-astra
assert_eq 0 "$RC" "resolve-model accepts the astra primary"
assert_eq $'astra\tgpt-6-astra' "$OUT" "resolve-model maps the astra primary to its row"
run_capture "$HELPER" resolve-model gpt-5.6-luna
assert_eq $'luna\tgpt-5.6-luna' "$OUT" "the borrowed haiku rung still resolves to luna"

# gpt-5.3-codex-spark is the haiku rung of sol, terra AND luna at once. A tier id
# several rows share must still resolve — to one of them, deterministically —
# rather than being refused the way an ambiguous FAMILY prefix is.
setup_case
run_capture "$HELPER" resolve-model gpt-5.3-codex-spark
assert_eq 0 "$RC" "a tier shared by three rows still resolves"
assert_contains "$OUT" 'gpt-5.3-codex-spark' "the shared tier keeps its own id in the answer"

setup_case
run_capture "$HELPER" resolve-model grok-4.6-build
assert_contains "$OUT" $'grok\tgrok-4.6' "an xAI build id normalises to the proxy alias"

setup_case
run_capture "$HELPER" resolve-model grok-9.9-experimental
assert_contains "$OUT" $'grok\tgrok-9.9-experimental' "an unknown id routes when one agent claims the prefix"

setup_case
run_capture "$HELPER" resolve-model gpt-9.9-unknown
assert_eq 1 "$RC" "an unknown gpt id is refused rather than guessed"
assert_contains "$ERR" 'no routing profile' "the refusal says why"

setup_case
run_capture "$HELPER" resolve-model claude-opus-5
assert_eq 1 "$RC" "a native model has no agent profile"

# --- replacements for curl-argv assertions ------------------------------------
# Upstream asserted curl's flags (--max-redirs 0, --max-filesize, --proto
# =https, --header @-). The Go binary has no curl; these cases assert the same
# guarantees as observable behaviour instead.

# A redirect is reported, never followed: the target is not requested.
setup_case
write_remote_profile TEST
respond location "https://127.0.0.1:$GATEWAY_TRUSTED_PORT/c/$CASE_ID.followed/v1/models"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  FAKE_CURL_STATUS=302 \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" exec sol -- "$TARGET"
assert_eq 1 "$RC" "a redirect fails the probe"
assert_eq 1 "$(curl_calls)" "a redirect costs exactly one request"
assert_eq "" "$(cat "$TMP_ROOT/$CASE_ID.followed/gateway/count" 2> /dev/null || true)" "the redirect target is never requested"

# The response body is capped at 1 MiB.
setup_case
write_remote_profile TEST
respond body "{\"data\":[],\"pad\":\"$(head -c 1048576 /dev/zero | tr '\0' x)\"}"
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" exec sol -- "$TARGET"
assert_eq 1 "$RC" "an oversized catalog fails the probe"
assert_contains "$ERR" 'exceeds 1048576 bytes' "the body cap is named"

# HTTPS only: a plaintext gateway URL is refused before any request.
setup_case
write_remote_profile TEST
run_capture env -i HOME="$CASE_HOME" TMPDIR="$CASE_DIR/tmp" PATH="$COMMON_PATH" \
  CC_ROUTER_REMOTE_URL="http://127.0.0.1:$GATEWAY_TRUSTED_PORT/c/$CASE_ID" \
  CLIPROXY_API_KEY_TEST=remote-api-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=access-id-secret \
  CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-client-secret \
  "$HELPER" list
assert_eq 3 "$RC" "a plaintext gateway URL is a configuration error"
assert_contains "$ERR" 'must use https://' "the plaintext refusal says why"
assert_eq 0 "$(curl_calls)" "a plaintext gateway is never contacted"
assert_secret_safe "$ERR" "the URL refusal hides secrets"

printf '1..%d\n' "$PASS"
if [ "$FAIL" -ne 0 ]; then
  printf '%d tests failed\n' "$FAIL" >&2
  exit 1
fi
printf '%d tests passed\n' "$PASS"
