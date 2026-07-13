#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

source_dir="$TEST_DIR/dist"
output_dir="$TEST_DIR/fonts"
mkdir -p "$source_dir"

for name in MiLanPro-Medium-400.ttf MiLanPro-SemiBold-540.ttf; do
  printf '\000\001\000\000test-font' >"$source_dir/$name"
done

"$ROOT_DIR/scripts/install-local-fonts.sh" \
  --source-dir "$source_dir" \
  --output-dir "$output_dir" >/dev/null

for name in MiLanPro-Medium-400.ttf MiLanPro-SemiBold-540.ttf; do
  cmp "$source_dir/$name" "$output_dir/$name"
done

printf 'not-a-font' >"$source_dir/MiLanPro-Medium-400.ttf"
if "$ROOT_DIR/scripts/install-local-fonts.sh" \
  --source-dir "$source_dir" \
  --output-dir "$output_dir" >/dev/null 2>&1; then
  printf 'expected invalid TTF to be rejected\n' >&2
  exit 1
fi

printf 'Font install script tests passed\n'
