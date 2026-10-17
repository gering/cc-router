# shellcheck shell=bash
# TAP-style assertions shared by the bash suites. Sourced, never executed:
# every suite counts with the same PASS/FAIL and reports through `finish`.

PASS=0
FAIL=0

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

# The suite's last line: the plan, then the verdict as the exit status.
finish() {
  printf '1..%d\n' "$PASS"
  if [ "$FAIL" -ne 0 ]; then
    printf '%d tests failed\n' "$FAIL" >&2
    exit 1
  fi
  printf '%d tests passed\n' "$PASS"
}
