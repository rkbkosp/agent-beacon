# Architecture

## Runtime Data Flow

```text
Codex app-server   Token-rate state   0-0 API / Keychain   Herdr socket   QWeather
        |                  |                  |                  |          |
        +------------------+------------------+--------+---------+----------+
                                          v
Go protocol validation -> in-memory dedupe/revision store -> WebSocket Hub
                                                            |
                                                            v
ESP network task -> raw JSON queue -> protocol task -> notification queue
                                                            |
                                                            v
                                                     UI task / LVGL
                                                            |
                                                            v
ESP protocol ACK -> network transmit queue -> Go ACK store
```

The UI loop in `app_main` is the only code that mutates LVGL objects. Wi-Fi and
WebSocket callbacks only update event bits or copy complete text frames into a
FreeRTOS queue. The protocol task owns cJSON parsing and emits fixed-size C
notification values to the UI queue.

## Firmware Tasks

- `main`: UI, carousel, BOOT input, notification scheduler, and LVGL timer work.
- `network_task`: Wi-Fi and WebSocket lifecycle, reconnect backoff, RX/TX queues.
- `protocol_task`: hello/snapshot exchange, revision tracking, JSON parsing, ACK encoding.

Notification scheduling is a pure C component. It preserves the interrupted
page and remaining carousel time, sorts by urgency, priority, creation time,
and ID, and applies the 1000 ms interrupt guard from `docs/notify.md`. The pending
queue has 12 regular slots plus 2 urgent reserve slots, with 64 recent IDs for
dedupe and bounded replay.

## Bridge

The Go bridge uses one write goroutine per device. Producers perform nonblocking
writes to a bounded device queue; a slow device is disconnected instead of
blocking other clients. The state store assigns continuous revisions under the
same lock used for ID and dedupe-key admission, so rejected events do not create
revision gaps.

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
token file or environment variable and is required for every v2 HTTP and
WebSocket operation except `/healthz`. The production LaunchAgent uses absolute
paths, a deterministic PATH, `RunAtLoad`, and `KeepAlive`; provider secrets stay
in Keychain. SQLite persistence and multi-device history remain later work.
