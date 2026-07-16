#!/usr/bin/env bash

BRIDGE_SERVICE_WAS_LOADED=false
BRIDGE_SERVICE_DOMAIN=""
BRIDGE_SERVICE_PLIST=""

bridge_service_pause_for_serial() {
  local port=$1
  if [[ ${AGENT_BEACON_MANAGE_BRIDGE_SERVICE:-1} != 1 || $(uname -s) != Darwin || ! -c $port ]]; then
    return 0
  fi
  BRIDGE_SERVICE_DOMAIN="gui/$(id -u)"
  local service="$BRIDGE_SERVICE_DOMAIN/com.stepatero.agentbeacon"
  BRIDGE_SERVICE_PLIST="$HOME/Library/LaunchAgents/com.stepatero.agentbeacon.plist"
  if ! launchctl print "$service" >/dev/null 2>&1; then
    return 0
  fi
  printf 'Pausing Agent Beacon Bridge to release %s\n' "$port"
  launchctl bootout "$service"
  BRIDGE_SERVICE_WAS_LOADED=true
  for _ in {1..50}; do
    if ! launchctl print "$service" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done
  printf 'error: Agent Beacon Bridge did not release its LaunchAgent\n' >&2
  bridge_service_resume || true
  return 1
}

bridge_service_resume() {
  if [[ $BRIDGE_SERVICE_WAS_LOADED != true ]]; then
    return 0
  fi
  if [[ ! -f $BRIDGE_SERVICE_PLIST ]]; then
    printf 'warning: Bridge plist disappeared; service was not restarted\n' >&2
    return 1
  fi
  printf 'Restarting Agent Beacon Bridge\n'
  launchctl bootstrap "$BRIDGE_SERVICE_DOMAIN" "$BRIDGE_SERVICE_PLIST"
  launchctl enable "$BRIDGE_SERVICE_DOMAIN/com.stepatero.agentbeacon" >/dev/null 2>&1 || true
  launchctl kickstart -k "$BRIDGE_SERVICE_DOMAIN/com.stepatero.agentbeacon" >/dev/null
  BRIDGE_SERVICE_WAS_LOADED=false
}
