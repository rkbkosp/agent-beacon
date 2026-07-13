#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/.." && pwd)

source_dir=${MILAN_FONT_DIST:-$HOME/Downloads/milan-font-builder/dist}
output_dir="$ROOT_DIR/firmware/components/beacon_fonts/font_assets"

usage() {
  cat <<'EOF'
Usage: scripts/install-local-fonts.sh [options]

Options:
  --source-dir DIR  Directory containing the generated Milan TTF files
  --output-dir DIR  Local firmware font directory
EOF
}

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-dir)
      [[ $# -ge 2 ]] || die "--source-dir requires a value"
      source_dir=$2
      shift 2
      ;;
    --output-dir)
      [[ $# -ge 2 ]] || die "--output-dir requires a value"
      output_dir=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

font_names=(MiLanPro-Medium-400.ttf MiLanPro-SemiBold-540.ttf)
for name in "${font_names[@]}"; do
  path="$source_dir/$name"
  [[ -s "$path" ]] || die "missing font: $path"
  signature=$(od -An -t x1 -N4 "$path" | tr -d ' \n')
  [[ "$signature" == "00010000" || "$signature" == "4f54544f" ]] || \
    die "invalid TTF/OTF signature: $path"
done

mkdir -p "$output_dir"
for name in "${font_names[@]}"; do
  install -m 0644 "$source_dir/$name" "$output_dir/$name"
  printf 'Installed %s SHA256=%s\n' "$name" \
    "$(shasum -a 256 "$output_dir/$name" | awk '{print $1}')"
done

printf 'Local font assets: %s\n' "$output_dir"
