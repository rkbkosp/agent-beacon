#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

mkdir -p "$TEST_DIR/bin"
cat >"$TEST_DIR/bin/security" <<'EOF'
#!/usr/bin/env bash
printf 'p"a\\ss'
EOF
chmod +x "$TEST_DIR/bin/security"
cat >"$TEST_DIR/bin/openssl" <<'EOF'
#!/usr/bin/env bash
printf '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n'
EOF
chmod +x "$TEST_DIR/bin/openssl"

output="$TEST_DIR/config.local.h"
token_output="$TEST_DIR/token.local"

if PATH="$TEST_DIR/bin:$PATH" AGENT_BEACON_CONFIG_OUTPUT="$output" \
  AGENT_BEACON_TOKEN_OUTPUT="$token_output" \
  "$ROOT_DIR/scripts/configure-network.sh" --ssid 'Lab "2G"' >/dev/null 2>&1; then
  printf 'expected missing --server to be rejected\n' >&2
  exit 1
fi

PATH="$TEST_DIR/bin:$PATH" AGENT_BEACON_CONFIG_OUTPUT="$output" \
  AGENT_BEACON_TOKEN_OUTPUT="$token_output" \
  "$ROOT_DIR/scripts/configure-network.sh" \
  --ssid 'Lab "2G"' \
  --server 192.0.2.10 \
  --device-id test-device >/dev/null

grep -F '#define BEACON_WIFI_SSID "Lab \"2G\""' "$output" >/dev/null
grep -F '#define BEACON_WIFI_PASSWORD "p\"a\\ss"' "$output" >/dev/null
grep -F '#define BEACON_WEBSOCKET_URI "ws://192.0.2.10:8787/v2/ws"' "$output" >/dev/null
grep -F '#define BEACON_DEVICE_ID "test-device"' "$output" >/dev/null
grep -F '#define BEACON_BRIDGE_TOKEN "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"' "$output" >/dev/null
grep -Fx '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef' "$token_output" >/dev/null

permissions=$(stat -f '%Lp' "$output")
[[ "$permissions" == "600" ]] || {
  printf 'expected mode 600, got %s\n' "$permissions" >&2
  exit 1
}
token_permissions=$(stat -f '%Lp' "$token_output")
[[ "$token_permissions" == "600" ]] || {
  printf 'expected token mode 600, got %s\n' "$token_permissions" >&2
  exit 1
}

printf 'Network config script tests passed\n'
