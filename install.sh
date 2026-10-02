#!/bin/sh
# Link the built commands onto PATH. Nothing is copied: the links point into
# this checkout, whose share/models.tsv the binary reads at runtime, so `git
# pull && make build` updates an installation in place.
#
#   ./install.sh                  links into ~/.local/bin
#   CC_ROUTER_BIN_DIR=DIR ./install.sh
#
# Idempotent. An existing file or a link pointing elsewhere is never
# overwritten. Shell startup files are not touched.
set -eu

ROOT=$(cd "$(dirname "$0")" && pwd -P)
DEST="${CC_ROUTER_BIN_DIR:-$HOME/.local/bin}"
status=0

found=0
for src in "$ROOT"/bin/*; do
  if [ ! -f "$src" ] || [ ! -x "$src" ]; then continue; fi
  case "$src" in *.tmp) continue ;; esac
  found=1
  name=${src##*/}
  link="$DEST/$name"
  mkdir -p "$DEST"
  if [ -L "$link" ] && [ "$(readlink "$link")" = "$src" ]; then
    echo "up to date: $link"
  elif [ -e "$link" ] || [ -L "$link" ]; then
    echo "install.sh: refusing to replace $link (not a link to $src)" >&2
    status=1
  else
    ln -s "$src" "$link"
    echo "linked: $link -> $src"
  fi
done
if [ "$found" -eq 0 ]; then
  echo "install.sh: nothing to install in $ROOT/bin — run: make build" >&2
  exit 1
fi

case ":$PATH:" in
  *":$DEST:"*) ;;
  *) echo "note: $DEST is not on PATH" >&2 ;;
esac
exit "$status"
