#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BUILD_DIR=$(mktemp -d)
trap 'rm -rf "$BUILD_DIR"' EXIT

cc \
  -std=c11 \
  -Wall \
  -Wextra \
  -Werror \
  -I"$ROOT_DIR/firmware/components/beacon_protocol/include" \
  -I"$ROOT_DIR/firmware/components/beacon_notifications/include" \
  -I"$ROOT_DIR/firmware/components/beacon_state/include" \
  "$ROOT_DIR/firmware/test/test_protocol.c" \
  "$ROOT_DIR/firmware/components/beacon_protocol/beacon_protocol_types.c" \
  -o "$BUILD_DIR/test_protocol"

"$BUILD_DIR/test_protocol"
printf 'Protocol tests passed\n'
