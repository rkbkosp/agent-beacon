# Architecture

## Runtime Data Flow

```text
Codex app-server   Token-rate state   0-0 / Keychain   Herdr   QWeather   HTTP producers
        |                  |                  |           |         |             |
        +------------------+------------------+-----+-----+---------+-------------+
                                                   v
                     Go protocol validation -> TTL/dedupe/revision store -> Device Hub
                                                                                |
                                                               USB CDC primary | Wi-Fi fallback
                                                                                v
ESP transport mux -> raw JSON queue -> protocol task -> notification queue
                                                     |
                                                     v
                                              UI task / LVGL
                                                     |
                                                     v
ESP protocol ACK -> active transport queue -> Go ACK store
```

The UI loop in `app_main` is the only code that mutates LVGL objects. USB and
WebSocket receivers only validate/reassemble frames and copy complete JSON into
a FreeRTOS queue. The protocol task owns cJSON parsing and emits fixed-size C
notification values to the UI queue.

## Firmware Tasks

- `main`: UI, carousel, BOOT input, notification scheduler, and LVGL timer work.
- `transport_usb_rx`: COBS/CRC validation, USB heartbeat timeout, and primary selection.
- `transport_net_rx`: forwards WebSocket messages only while USB is inactive.
- `network_task`: Wi-Fi/WebSocket lifecycle and reconnect backoff; WebSocket pauses during USB sessions.
- `transport_tx`: sends protocol output through the currently selected transport.
- `protocol_task`: hello/snapshot exchange, revision tracking, JSON parsing, ACK encoding.

Notification scheduling is a pure C component. It preserves the interrupted
page and remaining carousel time, sorts by urgency, priority, creation time,
and ID, and applies the 1000 ms interrupt guard from `docs/notify.md`. The pending
queue has 12 regular slots plus 2 urgent reserve slots, with 64 recent IDs for
dedupe and bounded replay.

## Bridge

The Go bridge uses one bounded write queue and writer goroutine per device,
independent of whether the session is USB CDC or WebSocket. In-process providers publish
business payloads through a bounded update channel; external producers post a
complete v2 notification envelope to the authenticated `/v2/notifications`
endpoint. Both paths converge before TTL, ID, and dedupe-key admission. The state
store assigns continuous revisions under the same lock, so rejected events do
not create revision gaps. Broadcasts perform nonblocking writes to each bounded
device queue; a slow device is disconnected instead of blocking other clients.

The Codex section coordinator executes the normalized adapter once per
`CODEX_HOME`, reads only the seven-day rate-limit window and reset-card summary,
and combines both homes with the independently refreshed 0-0 balance and the
patched Codex daemon's global visible-output rate. The rate state is polled at
200 ms, validated against the daemon v1 metric contract and 0600 file mode, and
published only when the displayed rate/activity/freshness changes. A missing or
stopped daemon becomes stale after two seconds. The relay secret is read from
macOS Keychain for each request and is never included in device payloads or logs.

The Herdr provider connects directly to the configured NDJSON Unix socket. It
bootstraps with `session.snapshot`, subscribes to pane/agent/workspace/tab
events, resyncs immediately after an event, and performs a full resync every 60
seconds. It also derives `agents.codex_active` from the real session identity
(`agent_session` first, top-level `agent` as fallback) and `working` status; a
disconnect forces the field false. Agent rows use Herdr's public five-state
model and priority order; the firmware independently applies the same ordering
before retaining four rows.

Bridge state is currently in memory. The bridge token is loaded from a 0600
token file or environment variable and is required for every v2 HTTP request
except `/healthz`, every WebSocket upgrade, and every USB device hello. The
production LaunchAgent uses absolute paths, a deterministic PATH, `RunAtLoad`,
and `KeepAlive`; provider secrets stay in Keychain. SQLite persistence and
multi-device history remain later work.

The macOS USB runner scans the configured device glob, sets the CDC tty to raw
mode, and performs the same hello/snapshot/device-message flow as WebSocket.
Firmware selects USB after the first valid COBS/CRC frame, pauses WebSocket, and
returns to Wi-Fi after disconnect or a 12-second heartbeat timeout.
