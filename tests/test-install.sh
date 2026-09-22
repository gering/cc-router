#!/bin/sh
# install.sh against a throwaway checkout: links, idempotence, refusal to
# overwrite, and a working command through the installed link.
# Usage: tests/test-install.sh <pkg-dir>
set -eu

PKG="${1:?usage: $0 <pkg-dir>}"
ROOT=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/test-install.XXXXXX")
trap 'rm -rf -- "$TMP"' EXIT INT TERM
fails=0
expect() { # <label> <command…>
  label=$1
  shift
  if "$@"; then echo "ok - $label"; else
    echo "not ok - $label" >&2
    fails=$((fails + 1))
  fi
}
install() { CC_ROUTER_BIN_DIR="$TMP/dest" "$TMP/checkout/install.sh" > "$TMP/out" 2>&1; }
idempotent() { install && grep -q "up to date" "$TMP/out"; }
kept() { ! install && [ "$(cat "$link")" = keep ]; }
explains() { ! install && grep -q "make build" "$TMP/out"; }

mkdir -p "$TMP/checkout/bin" "$TMP/dest"
cp "$ROOT/install.sh" "$TMP/checkout/"
cp "$PKG/bin/cc-harness-agents" "$TMP/checkout/bin/"
ln -s "$ROOT/share" "$TMP/checkout/share"
checkout=$(cd "$TMP/checkout" && pwd -P)
link="$TMP/dest/cc-harness-agents"

install
expect "install links the binary" [ "$(readlink "$link")" = "$checkout/bin/cc-harness-agents" ]
expect "the installed link runs and finds share/" \
  [ "$(HOME="$TMP" "$link" resolve-model gpt-6-astra)" = "$(printf 'astra\tgpt-6-astra')" ]
expect "a second run is idempotent" idempotent

rm "$link"
echo keep > "$link"
expect "an existing file is never replaced" kept

rm -f "$TMP/checkout/bin/cc-harness-agents"
expect "an unbuilt checkout says what to do" explains

[ "$fails" -eq 0 ]
