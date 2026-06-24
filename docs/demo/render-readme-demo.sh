#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CAST_PATH="${DBF_DEMO_CAST_PATH:-$ROOT_DIR/docs/demo/debianform-quickstart.cast}"
SVG_PATH="${DBF_DEMO_SVG_PATH:-$ROOT_DIR/docs/demo/debianform-quickstart.svg}"
WIDTH="${DBF_DEMO_COLS:-90}"
HEIGHT="${DBF_DEMO_ROWS:-28}"

die() {
  printf 'render-readme-demo: %s\n' "$*" >&2
  exit 1
}

[[ -f "$CAST_PATH" ]] || die "cast file not found: $CAST_PATH"
command -v npx >/dev/null 2>&1 || die "npx is required to render SVG"

mkdir -p "$(dirname "$SVG_PATH")"

npx --yes svg-term-cli@2.1.1 \
  --in "$CAST_PATH" \
  --out "$SVG_PATH" \
  --width "$WIDTH" \
  --height "$HEIGHT" \
  --window \
  --padding 14

printf 'wrote %s\n' "$SVG_PATH"
