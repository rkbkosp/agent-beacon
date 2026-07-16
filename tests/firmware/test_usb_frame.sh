#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BUILD_DIR=$(mktemp -d)
trap 'rm -rf "$BUILD_DIR"' EXIT

${CC:-cc} -std=c11 -Wall -Wextra -Werror \
  -I"$ROOT_DIR/firmware/components/beacon_transport/include" \
  "$ROOT_DIR/firmware/components/beacon_transport/beacon_usb_frame.c" \
  "$ROOT_DIR/firmware/test/test_usb_frame.c" \
  -o "$BUILD_DIR/test_usb_frame"

"$BUILD_DIR/test_usb_frame"
