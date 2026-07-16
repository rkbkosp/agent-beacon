# Test Report

## 2026-07-14 M0 Bring-up

### Automated Tooling

Command:

```bash
./tests/scripts/test_m0_scripts.sh
```

Result: 11/11 passed.

Covered behavior:

- single USB modem auto-detection;
- ambiguous device rejection with explicit `--port` guidance;
- exact 16 MB backup validation;
- independently hashed external backup copy;
- factory restore dry-run default;
- explicit `--yes` requirement and invalid-size rejection;
- pinned toolchain reporting;
- EIM environment resolution without inheriting shell functions;
- firmware flash refusal when no valid 16 MB factory backup exists;
- guarded flash invocation when a valid backup exists;
- serial monitor port forwarding.

Live read-only validation also passed:

```bash
make doctor
make detect-device PORT=/dev/cu.usbmodemXXXX
```

### Official Demo Build

Command: `idf.py set-target esp32s3 && idf.py build`

Result: passed with ESP-IDF v5.5.4.

Application size: `0x182730` bytes in a `0x300000` partition (50% free).

The official Demo emits duplicate-symbol Kconfig informational warnings. No
compiler, linker, or image-generation errors occurred.

### Device Flash and Runtime

Flash result: bootloader, partition table, and app writes all passed esptool
hash verification.

Runtime result:

- Flash and PSRAM checks passed;
- QMI8658 responded;
- LCD/LVGL initialization returned success;
- Wi-Fi scan completed with 16 networks;
- BLE scan started;
- no panic or reset observed for 20 seconds after initialization.

Expected exception: TF mount returned `ESP_ERR_TIMEOUT` because no card is
inserted. TF is outside the MVP requirement.

### Manual Visual Check

Result: passed on the connected physical board.

The official `Onboard Parameter` page had correct orientation, natural colors,
no visible offset or clipping, and stable backlight. The board RGB also cycled
normally.

### Custom BSP And Four-Color Calibration

Host tests:

```text
calibration tests passed
board geometry tests passed
```

ESP-IDF v5.5.4 build: passed. The application is `246,496` bytes and leaves
94% of its 4 MiB partition free. Flashing passed esptool hash verification for
the bootloader, partition table, and application.

The initial rotated write window produced correct colors without full-panel
coverage. A geometry regression test was added, and the BSP was corrected to
use the official native `172x320` write window with `X=34`, `Y=0`. After the
fix, physical inspection confirmed complete full-panel red, yellow, blue, and
green coverage. Multiple cycles completed without panic or reset.

M0 result: passed.

## 2026-07-14 M1 Local UI

### Automated Tests

The host suite covers the dynamic 15/6/8-second carousel timing, two-page
rotation while Codex is inactive, interrupted-page restoration, elapsed time
across state boundaries, BOOT debounce and
short/double/2-second/5-second gestures, the Codex/Agents/Weather models, theme
palettes, and native-panel geometry.

Result: passed.

### Build And Flash

Pinned dependencies for this historical M2 build were ESP-IDF `5.5.4` and LVGL
`8.3.11`. That image, which included the UI, v2 networking, and notification
queue, was `0x129ce0` bytes and left 71% of the 4 MiB application partition
free. The current Milan-enabled image is recorded separately below.

Bootloader, partition table, and app writes passed esptool hash verification.
Runtime reported a logical `320x172` LVGL display using 90-degree software
rotation over the native `172x320` panel. The 8 MB PSRAM test passed.

### Current Visual Gate

The prior color, orientation, full-panel coverage, and BOOT electrical checks
remain valid. The latest three-page layouts and notification surfaces are live
on the connected board but still require explicit human confirmation against
`docs/ui.md`. Framebuffer dump and golden-image tooling are not implemented yet.

M1 result: logic/build/flash passed; latest visual acceptance pending.

## 2026-07-14 M2 Mock Real-Time Loop

### Protocol And Firmware Logic

`make test` passed. Host coverage includes:

- strict protocol v2 envelopes, enum validation, UTF-8, and 64 KiB limits;
- typed snapshot and state-patch application with revision-gap recovery;
- fragmented WebSocket text-frame reassembly;
- 1/2/4/8/15/30 second reconnect policy with bounded jitter;
- 12 regular queue slots, 2 urgent reserve slots, and 64 recent IDs;
- urgency/priority ordering, 1000 ms interrupt guard, immediate protocol-error
  override, supersede, TTL, bounded replay, and queue eviction;
- all ten ACK states: `received`, `queued`, `shown`, `completed`, `interrupted`,
  `superseded`, `expired`, `dropped`, `invalid`, and `duplicate`;
- all 13 required Mock fixtures and the absence of forbidden task, inbox,
  message, evaluation, benchmark, and Codex five-hour-window fields.

JSON Schema validation passes for v2 snapshot, notification, ACK, and envelope
examples, including negative validation cases.

### macOS Bridge

The real arm64 bridge passed public `/healthz`, token-protected v2 endpoints,
and the `/v2/ws` header checks. Integration tests verify server hello, device
hello, snapshot ordering, state-patch/notification broadcasts, flat ACK
ingestion, dedupe, continuous revisions, and snapshot requests.

### Firmware Build

Pinned dependencies: ESP-IDF `5.5.4`, LVGL `8.3.11`, and Espressif
`esp_websocket_client 1.7.0`. The network-enabled image builds with
`-Werror=frame-larger-than=512`; `app_main` is 96 bytes by disassembly. This
gate was added after the first physical build exposed a 1552-byte main-task
frame and stack overflow during Wi-Fi association.

### Physical Network Loop

Passed on the connected Waveshare ESP32-S3-LCD-1.47B:

- joined the configured 2.4 GHz network and received a DHCP lease;
- connected to the authenticated bridge at its configured LAN address;
- completed server hello, device hello, and revision 0 snapshot;
- applied typed Codex, Agents, and Weather state patches;
- displayed `herdr-blocked`, `herdr-done`, `codex-critical`, and
  `weather-leave-umbrella` fixture notifications;
- bridge observed `received -> shown -> completed` for blocked and weather;
- bridge observed `received -> shown -> interrupted` when urgent Codex quota
  interrupted an active normal Agent notification, followed by Codex
  `completed`;
- repeated fixture facts updated state without replaying duplicate notifications;
- BOOT double press replayed the latest unexpired weather notification from the
  device's recent history;
- after the bridge stopped, the device detected TCP close and retried with
  backoff; after restart it reconnected and received a new revision 0 snapshot;
- no panic, reset, or stack overflow occurred on the corrected image.

M2 result: protocol, Mock bridge, notification scheduler, physical networking,
ACK loop, and reconnect passed. Latest on-screen layout confirmation remains the
separate M1 visual gate above.

## 2026-07-14 Herdr Agents Live List

The bridge now consumes the local Herdr 0.7.3 NDJSON socket rather than scanning
processes or terminal output itself. Automated coverage verifies initial
`session.snapshot`, `events.subscribe`, event-driven resync, 60-second full
resync, socket reconnect, workspace/tab display names, preservation of Herdr
session metadata, and priority ordering.

The real socket at `~/.config/herdr/herdr.sock` returned connected state and the
same workspace-oriented labels as the Herdr sidebar. During physical testing,
live status changes produced consecutive Agents state patches on the device;
the active workspace row moved ahead of the idle rows. The corrected firmware applies
`blocked > done > working > idle > unknown`, then revision, focus, and name,
retains four rows, and reports remaining rows as `+N`.

The post-provider handshake regression verifies that a device receives the
initial snapshot before any live provider broadcast. A bridge restart on the
physical board produced `hello -> snapshot revision=1`, with no preceding
state patch.

Herdr has no public `failed` status. Errors requiring action remain Herdr
`blocked`; Agent Beacon does not introduce `agent.failed` or infer state from
terminal text.

## 2026-07-14 M5 QWeather Provider

### Automated Coverage

The new `internal/providers/qweather` package has 29 focused tests covering:

- PKCS#8 Ed25519 loading, minimal JWT fields, public-key verification, token
  reuse, two-minute early refresh, invalidation, and concurrent callers;
- complete weather YAML parsing, `~` expansion, account-specific Host
  validation, coordinate precision, duration defaults, and isolated disablement
  when weather configuration is invalid;
- HTTPS/Bearer requests for now, 24-hour, and 72-hour APIs; one 401 re-sign
  retry; 403/429/5xx classification; `Retry-After`; 1 MiB response bounds; API
  response code checks; and invalid string-number handling;
- atomic `0600` last-good cache persistence without credential fields;
- explicit `Asia/Shanghai` target selection before lunch, between lunch and
  leave, after leave, Friday-to-Monday, and non-working days;
- exact/nearest hourly record selection, 24h/72h horizon switching, all wet
  icon ranges, Chinese text fallback, stale/insufficient unknown decisions,
  transition notification dedupe, T-30 single reminder, refresh coalescing,
  and bounded retry backoff;
- credential-safe doctor and Weather CLI behavior.

Fresh verification commands:

```text
go vet ./...                              PASS
go test -race ./... -count=1             PASS
go build ./cmd/agent-beacon-bridge       PASS
make test                                PASS
```

The repository-wide suite includes scripts, firmware host tests, protocol JSON,
all Go packages, the existing Herdr provider, and the real provider publication
path into Bridge state patches and WebSocket broadcasts.

### Local Credential Readiness

The existing `~/.weather/ed25519-private.pem` is mode `0600`, the public key is
mode `0644`, and the containing `~/.weather` directory was tightened from
`0755` to the required `0700`. No key content was read or copied during this
check.

A complete ignored `macos/configs/config.local.yaml` was created with mode
`0600`. Credential-safe live acceptance then passed all doctor checks:

```text
configuration / key permissions / PKCS#8 Ed25519       PASS
JWT header and payload / system time                    PASS
account Host DNS / TLS                                  PASS
weather now / weather 24h / future target selection    PASS
attribution / optional public-key pair                  PASS
```

At 16:46 CST, a real `weather refresh` returned a fresh 16:40 observation for
杭州 and a fresh 19:00 forecast. The next outing was correctly selected as
`leave`; its umbrella decision was `not_required` with high confidence based on
fresh dry hourly records. Because the first live run occurred after 12:00, the
already-past lunch slot was correctly reported as unavailable rather than
invented; a regression test now verifies that this cannot fail live diagnostics,
while earlier last-good forecast records remain mergeable from the secure cache.

M5 result: implementation, automated acceptance, and account-specific live API
acceptance passed. Physical LCD data verification remains pending.

## 2026-07-14 Simplified Chinese Milan UI

### Local Font Assets

The two supplied font files were installed into the Git-ignored local firmware
asset directory and embedded into the device image without adding either TTF to
source control:

```text
MiLanPro-Medium-400.ttf
  SHA-256 a92fe77572853530f5a4112d0768925902a25207d403b4285d17879412264bc5
MiLanPro-SemiBold-540.ttf
  SHA-256 0d49d94153d5afbdc7680729534305ed286aa605139fe259817464b28a7dabbc
```

The supplied files contain no embedded license metadata. Their local use does
not change the requirement that distributed fonts have a confirmed
redistribution license.

### Automated Verification

- font installation tests validate both required filenames and reject invalid
  TTF signatures;
- both Medium and SemiBold cover all 124 Chinese characters and symbols used by
  the firmware UI, protocol defaults, and Mock-visible copy;
- `make test`, `go test -race ./...`, and `go vet ./...` pass;
- the UI model and protocol JSON tests assert the Simplified Chinese labels;
- no authored UI code references the former Montserrat fonts.

The first two physical candidates used Espressif FreeType `2.13.3`. The LVGL
SBit cache candidate failed in the TrueType interpreter's `realloc` path, and a
direct no-hinting memory-face candidate failed while FreeType released the
previous rendered bitmap. Both failures were reproducible at the first label
and produced decoded backtraces before the candidates were replaced.

The final firmware uses LVGL `8.3.11` TinyTTF memory fonts backed by the same
embedded TTF bytes, with a 4 KiB glyph cache for each of five sizes. The LVGL
memory pool is 64 KiB. Pinned runtime dependencies are ESP-IDF `5.5.4`, LVGL
`8.3.11`, and `esp_websocket_client 1.7.0`; FreeType is no longer in the
manifest or dependency lock. The final application is `0x4a9690` bytes in an
8 MiB application partition, leaving `0x356970` bytes (42%) free.

### Physical Runtime

The final bootloader, partition table, and application passed esptool write
hash verification on the connected USB modem. Runtime reported both exact font
asset sizes, completed the first TinyTTF label render, started the three-page
UI, joined Wi-Fi, and obtained a DHCP lease. The device ran for more than
one complete carousel cycle without a panic or software reset.

After the Mac bridge started, the device completed the v2 handshake and applied
the real Herdr snapshot at revision 1. A `herdr-blocked` fixture then produced
an Agents patch at revision 2 and displayed the 7-second Chinese notification;
the device remained stable after the overlay returned. The final binary, which
also translates invalid relay credentials and both weather-slot labels, was
then flashed and applied the real Herdr snapshot at revision 4. Final visual
inspection of font shape, spacing, and clipping remains a human LCD acceptance
step.

## 2026-07-14 Weather UI Row Layout

The Weather page now renders Current, Lunch, and Leave as three equal-width
columns in one row. Lunch and Leave omit their exact times, and the next-outing
recommendation uses the semantic slot label instead of `12:00` or `19:00`.
The required-umbrella state grew from a 20 px / 14 px treatment to a 54 px red
area with 24 px type. The not-required state uses the screen background with no
colored fill; the stale/unknown state retains its yellow safety treatment.

Fresh verification:

```text
idf.py build    PASS
make test       PASS
```

The updated binary is `0x4a9760` bytes and leaves 42% of the application
partition free. It was flashed to the connected USB modem; bootloader,
application, and partition-table writes all passed hash verification. The
device then initialized its 320x172 LVGL display, loaded both Milan fonts, and
rejoined Wi-Fi without a panic or reset. Physical LCD visual
verification of the new layout remains a human acceptance step. The configured
Bridge endpoint reset its WebSocket connection during this check, so live
provider data was not part of the post-flash verification.

## 2026-07-14 M3 Real Providers and LaunchAgent

### Provider Wiring Audit

The production bridge no longer starts from a Mock business snapshot. Its
Codex section now coordinates two independent real `CODEX_HOME` adapter calls
with the independently refreshed 0-0 balance, preventing either upstream from
overwriting the other fields in the atomic protocol section. The adapter uses
the locally installed Codex app-server `account/rateLimits/read` method and
selects only the seven-day window; its normalized schema contains no five-hour
window fields.

The audit also found that Herdr snapshot/event data was real but its live
`blocked` and `done` transitions were not entering the notification path. The
provider now emits the specified transition notifications without replaying
the initial snapshot. QWeather was already fully connected and remains
unchanged. Production configuration sets `providers.mock.enabled: false`, and
the fixture endpoint returns `404` in that mode.

Focused and repository-wide verification:

```text
Codex adapter, both CODEX_HOME values                  PASS
0-0 HTTPS request with Keychain secret                PASS
Herdr snapshot/event socket                           PASS
QWeather now/hourly provider                          PASS
production fixture endpoint disabled (HTTP 404)       PASS
go test ./...                                         PASS
go vet ./...                                          PASS
go test -race ./... -count=1                          PASS
make test                                             PASS
```

At live acceptance time, both Codex homes were `fresh` and exposed real weekly
remaining/reset-card fields, the 0-0 response returned a valid balance, Herdr
was connected with four current agents, and QWeather returned a
fresh Hangzhou current observation and fresh next-outing forecast. The derived
system freshness was `fresh`.

### LaunchAgent Installation and Restart

`agent-beacon-bridge install-service` now installs a stable binary, private
configuration, and the existing device token below
`~/Library/Application Support/AgentBeacon`, writes the standard
`com.stepatero.agentbeacon` plist, bootstraps it in `gui/$UID`, and waits for an
authenticated `/readyz`. The plist supplies deterministic `HOME`/`PATH`, uses
absolute paths, and enables both `RunAtLoad` and `KeepAlive`. The 0-0 credential
is stored in the login Keychain and is absent from YAML, plist, and logs.

Live launchd acceptance:

```text
plist lint                                             PASS
LaunchAgent state                                     running
RunAtLoad / KeepAlive                                 enabled
config and token modes                                0600
bridge listener                                       TCP *:8787
authenticated readyz                                  HTTP 200
two consecutive idempotent reinstalls                 PASS
reinstall with ZERO_API_KEY unset (Keychain only)     PASS
forced SIGTERM KeepAlive restart                      PID changed
post-restart real providers                           all fresh/connected
stderr                                                empty
```

This completes M3 provider wiring and the launchd/autostart slice of M6. The
M6 SoftAP and 24-hour soak requirements remain separate acceptance work.

## 2026-07-15 Global Token-Rate Dashboard

The Codex carousel page now renders a 240-degree LVGL speedometer for the global
patched-Codex `visible_output_tokens_per_second` EMA. Two compact weekly-quota
fuel rows and the 0-0 balance remain on the right. The protocol distinguishes a
valid idle `0.0` from unavailable/stale data, which renders as `--`.

Automated and build verification:

```text
make test                                             PASS
go test ./...                                         PASS
protocol/schema example validation                   PASS
token-rate 0600/contract/freshness tests              PASS
idf.py build                                          PASS
agent_beacon.bin                                      0x4abd90 bytes
application partition free                           42%
```

`bridge-service-install` installed both the Bridge and the copied patched
`codex-token-rate-daemon` as `RunAtLoad`/`KeepAlive` LaunchAgents. Live state:

```text
com.stepatero.agentbeacon.tokenrate                   running
codex-token-rate.sock                                 mode 0600
codex-token-rate.json                                 mode 0600
idle Bridge snapshot                                 fresh 0.0 tok/s
synthetic local delta Bridge snapshot                 fresh 14.0 tok/s, 1 session, 1 stream
post-window Bridge snapshot                           fresh 0.0 tok/s
```

The rebuilt firmware passed esptool hash verification on
`/dev/cu.usbmodem21201`, initialized LVGL and both Milan fonts, joined Wi-Fi,
connected WebSocket, and applied the real snapshot at revision 18. A second
synthetic rate burst produced live Codex/System patches through revision 29
without a panic or reset. Physical LCD inspection of exact needle geometry,
spacing, and clipping remains a human visual acceptance step.

## 2026-07-16 Completion-Output Token-Rate Contract Alignment

The Bridge now consumes the current patched daemon contract,
`completion_output_tokens_per_second`, and strictly validates the daemon-only
`tool_active_streams` count without adding it to the device protocol. The patched Codex launcher
uses Agent Beacon's published launchd socket when its inherited socket is unset or has been
removed, preventing a stale standalone path from creating a second aggregate.

Verification:

```text
make test                                             PASS
go vet ./...                                          PASS
go test -race ./...                                   PASS
just fmt                                              PASS
installed daemon SHA-256                              matches ../codex release
LaunchAgent socket/state                              mode 0600
real codex-patched exec                               daemon and Bridge near 60 tok/s
real active session/stream counts                     1 / 1
post-window Bridge snapshot                           fresh 0.0 tok/s
remaining token-rate listeners                        one AgentBeacon socket
stale TMPDIR launcher environment                     falls back to AgentBeacon socket
```

The standalone TMPDIR daemon was terminated after validation. Patched Codex processes that were
already running before the migration still need a restart because their process environment cannot
be changed retroactively; newly launched processes use the shared AgentBeacon socket.

## 2026-07-15 Herdr-Gated Token Dashboard Carousel

The Herdr provider now publishes `agents.codex_active=true` only when at least
one session identified as Codex is `working`. Session metadata is preferred,
the top-level Herdr agent label is the compatibility fallback, and a provider
disconnect forces the field false. Firmware uses the transition as a carousel
control signal: activation immediately selects the speed dashboard for 15
seconds, while deactivation removes it without interrupting an already visible
Agents or Weather page.

Automated verification covers any-one-active aggregation, non-Codex working
sessions, inactive Codex states, disconnected state, idempotent active patches,
the 15-second boundary, filtered automatic/manual navigation, deactivation on
and away from the Codex page, notification restoration, and diagnostics.

```text
make test                                             PASS
go vet ./...                                          PASS
go test -race ./...                                   PASS
idf.py build                                          PASS
agent_beacon.bin                                      0x4abf10 bytes
application partition free                           42%
```

The installed production Bridge reported a real Herdr snapshot with
`codex_active=true` and a `working` Codex session named `agent-bacon`. The
rebuilt image passed the factory-backup guard and esptool hash verification on
`/dev/cu.usbmodem21201`; after boot it connected to the production WebSocket and
accepted the full snapshot at revision 4, then patches through revision 6,
without a panic or reset.

## 2026-07-16 Type-C USB Primary Transport

The ESP32-S3 USB-Serial/JTAG CDC now carries framed protocol v2 traffic as the
primary device transport. Wi-Fi remains associated but its WebSocket is paused
after the first valid USB frame; disconnect or 12 seconds without a valid host
heartbeat restores the existing WebSocket path and performs a fresh
hello/snapshot exchange.

Automated and build verification:

```text
COBS/CRC frame round-trip, corruption, resync, limits    PASS
macOS fragmented tty reads, writes, timeout EOF          PASS
USB hello/token/snapshot/broadcast/ACK integration       PASS
invalid USB device token                                 rejected
configuration defaults and explicit disablement          PASS
repository make test                                     PASS
Go race detector                                         PASS
ESP-IDF v5.5.4 build                                     PASS
agent_beacon.bin                                         0x4ae440 bytes
application partition free                              41%
```

The image passed the factory-backup guard and esptool hash verification on the
connected physical board. The installed LaunchAgent held the USB modem steadily
for more than 15 seconds, `/v2/devices` reported one ready `usb:` connection,
and the firmware had no active device WebSocket. A real HTTP notification then
traversed the shared device Hub over USB and produced `received`, `shown`, and
`completed` ACKs.

Failover was validated by stopping the USB-capable LaunchAgent and running the
Bridge with `serve --disable-usb`. After the 12-second device timeout,
`/v2/devices` reported the same board ready over `wifi` and the LAN WebSocket was
established. Restarting the production service returned the board to USB.
Finally, `make detect-device` confirmed that maintenance scripts automatically
release the CDC port for esptool and restore the Bridge afterward.

## 2026-07-16 Transport-Aware Header Status

Every carousel and diagnostics header now reports the active business link as
`USB 在线` or `WiFi 在线`. Provider degradation keeps the transport visible as
`USB 部分可用` or `WiFi 部分可用`; a link switch clears snapshot readiness until
the new transport completes synchronization.

Host model tests cover both transports, stale state, disconnect, and an invalid
transport kind. The four labels measure 59, 61, 87, and 89 pixels respectively
with the production 14 px Milan font, fitting the narrowest 90 px header slot.
`make test` and the ESP-IDF build passed; the `0x4ae520` image leaves 41% of the
application partition free. Flashing passed all esptool hash checks, and the
restarted Bridge reported the physical device ready over USB.
