# Security

## Local Secrets

Wi-Fi credentials, the device-local bridge URI, and the shared bridge token
live in `firmware/config.local.h`. The same generated token is stored in
`macos/configs/token.local` for local CLI and bridge startup. Both files are
ignored by Git and written with mode `0600`; checked-in examples contain only
placeholders.

The bridge example configuration contains no API tokens. QWeather uses an
account-specific API Host, project ID, credential ID, and an Ed25519 private-key
path. The `~/.weather` directory must be mode `0700`; the private key must be
mode `0600` or stricter. It is read directly by the Bridge and is never copied
into the repository, cache, device protocol, logs, or NVS. JWTs are signed in
memory for 15 minutes and are never persisted or printed.

The weather last-good cache is stored below the macOS user cache directory as
`AgentBeacon/qweather-cache.json` with mode `0600`. It contains only endpoint,
location, outing slot/target, fetch/update times, and QWeather/Open-Meteo
upstream weather payloads. Run
`agent-beacon-bridge weather cache clear --config <path>` to delete it.

The Codex token-rate socket and state file contain only aggregate counts and
timestamps. The companion daemon creates both with mode `0600`; the Bridge
rejects a non-regular or group/world-accessible state file. Prompt text, visible
output, reasoning, tool content, credentials, and working directories never
enter this path.

## Device Data Boundary

The MVP device receives short status labels and notification summaries. It does
not store email bodies, OAuth tokens, API keys, full Agent logs, or complete
Codex sessions. RGB, TF, BLE, and USB business transports remain disabled.

## Current Network Scope

M2 uses an authenticated but unencrypted WebSocket on the trusted local LAN.
The `/v2/ws` handshake requires device ID, token, and protocol-version headers;
protected HTTP endpoints require the same token. Do not expose port 8787 to the
public internet. A production remote deployment must use authenticated TLS and
provisioned per-device credentials rather than the M2 shared `ws://` token.

The bridge caps HTTP bodies at 256 KiB and WebSocket messages at 64 KiB. The
firmware reassembles valid fragmented WebSocket text messages up to 64 KiB, and
rejects oversized, invalid-UTF-8, version-mismatched, unknown-enum, or invalid
JSON messages before they reach the UI queue.
