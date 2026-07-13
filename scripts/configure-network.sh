#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
output=${AGENT_BEACON_CONFIG_OUTPUT:-$ROOT_DIR/firmware/config.local.h}
token_output=${AGENT_BEACON_TOKEN_OUTPUT:-$ROOT_DIR/macos/configs/token.local}
ssid=""
server=""
device_id="agent-beacon"
token=""

usage() {
  cat <<'EOF'
Usage: scripts/configure-network.sh --ssid NAME --server HOST [--device-id ID] [--token TOKEN]

Read the saved AirPort password for NAME from the macOS Keychain and create the
ignored firmware/config.local.h file without printing the password.
EOF
}

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ssid)
      [[ $# -ge 2 ]] || die "--ssid requires a value"
      ssid=$2
      shift 2
      ;;
    --token)
      [[ $# -ge 2 ]] || die "--token requires a value"
      token=$2
      shift 2
      ;;
    --server)
      [[ $# -ge 2 ]] || die "--server requires a value"
      server=$2
      shift 2
      ;;
    --device-id)
      [[ $# -ge 2 ]] || die "--device-id requires a value"
      device_id=$2
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

[[ -n "$ssid" ]] || die "--ssid is required"
[[ -n "$server" ]] || die "--server is required"
[[ "$server" =~ ^[A-Za-z0-9.:-]+$ ]] || die "--server must be a host name or IP address"
[[ "$device_id" =~ ^[A-Za-z0-9._-]+$ ]] || die "--device-id contains unsupported characters"
case "$ssid" in
  *$'\n'*|*$'\r'*) die "SSID must not contain a newline" ;;
esac

password=$(security find-generic-password -D "AirPort network password" -a "$ssid" -w 2>/dev/null) ||
  die "no saved AirPort password found for the requested SSID"
case "$password" in
  *$'\n'*|*$'\r'*) die "Wi-Fi password must not contain a newline" ;;
esac
if [[ -z "$token" && -f "$token_output" ]]; then
  token=$(<"$token_output")
fi
if [[ -z "$token" ]]; then
  token=$(openssl rand -hex 32) || die "could not generate bridge token"
fi
case "$token" in
  *$'\n'*|*$'\r'*) die "Bridge token must not contain a newline" ;;
esac
[[ ${#token} -ge 32 && ${#token} -le 128 ]] || die "Bridge token must contain 32..128 characters"

escape_c_string() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

escaped_ssid=$(escape_c_string "$ssid")
escaped_password=$(escape_c_string "$password")
escaped_server=$(escape_c_string "$server")
escaped_device_id=$(escape_c_string "$device_id")
escaped_token=$(escape_c_string "$token")

mkdir -p "$(dirname "$output")" "$(dirname "$token_output")"
umask 077
temporary="$output.tmp.$$"
token_temporary="$token_output.tmp.$$"
trap 'rm -f "$temporary" "$token_temporary"' EXIT
{
  printf '#pragma once\n\n'
  printf '#define BEACON_WIFI_SSID "%s"\n' "$escaped_ssid"
  printf '#define BEACON_WIFI_PASSWORD "%s"\n' "$escaped_password"
  printf '#define BEACON_WEBSOCKET_URI "ws://%s:8787/v2/ws"\n' "$escaped_server"
  printf '#define BEACON_DEVICE_ID "%s"\n' "$escaped_device_id"
  printf '#define BEACON_BRIDGE_TOKEN "%s"\n' "$escaped_token"
} >"$temporary"
chmod 600 "$temporary"
printf '%s\n' "$token" >"$token_temporary"
chmod 600 "$token_temporary"
mv -f "$temporary" "$output"
mv -f "$token_temporary" "$token_output"
trap - EXIT

printf 'Created local network config for device %s at %s; bridge token stored at %s\n' \
  "$device_id" "$output" "$token_output"
