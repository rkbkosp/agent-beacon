#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BRIDGE_URL=${BRIDGE_URL:-http://127.0.0.1:8787}
BRIDGE_BIN=${BRIDGE_BIN:-$ROOT_DIR/macos/bin/agent-beacon-bridge}
TOKEN_FILE=${AGENT_BEACON_TOKEN_FILE:-$ROOT_DIR/macos/configs/token.local}
TOKEN=${AGENT_BEACON_TOKEN:-}

if [[ -z "$TOKEN" && -s "$TOKEN_FILE" ]]; then
  TOKEN=$(<"$TOKEN_FILE")
fi
if [[ -z "$TOKEN" ]]; then
  printf 'error: bridge token is missing; run scripts/configure-network.sh first\n' >&2
  exit 1
fi

if [[ ! -x "$BRIDGE_BIN" ]]; then
  printf 'Building agent-beacon-bridge...\n'
  (cd "$ROOT_DIR/macos" && go build -o bin/agent-beacon-bridge ./cmd/agent-beacon-bridge)
fi

if ! curl --fail --silent --show-error \
  -H "X-Agent-Beacon-Token: $TOKEN" "$BRIDGE_URL/readyz" >/dev/null; then
  printf 'error: bridge is not ready at %s; run make bridge-run first\n' "$BRIDGE_URL" >&2
  exit 1
fi

run_fixture() {
  "$BRIDGE_BIN" emit --server "$BRIDGE_URL" --token "$TOKEN" --fixture "$1" >/dev/null
  printf 'Emitted fixture: %s\n' "$1"
  sleep 0.4
}

run_fixture codex-normal
run_fixture codex-critical
run_fixture herdr-all-statuses
run_fixture herdr-blocked
run_fixture herdr-done
run_fixture weather-leave-umbrella
run_fixture weather-stale

printf 'Emitted the protocol v2 Mock sequence to %s\n' "$BRIDGE_URL"
