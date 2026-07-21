# UI Specification

## Display Geometry

- Physical panel: `172x320`, native controller window at `X=34`, `Y=0`.
- Logical UI: `320x172`, LVGL software rotation 90 degrees.
- Draw buffers: two internal DMA buffers, each 20 native rows.
- Page intervals: Codex quota 8 seconds, active Token rate 15 seconds, Agents 6
  seconds, Weather 8 seconds.

## Carousel

The carousel always contains one Codex slot, Agents, and Weather. The Codex slot
shows the original full quota panel for 8 seconds while `agents.codex_active` is
false, and substitutes the Token-rate dashboard for 15 seconds while it is true.
A false-to-true transition immediately selects Token rate and starts a full
15-second interval; repeated active snapshots do not extend it. A true-to-false
transition immediately replaces a visible Token-rate dashboard with the quota
panel and starts its full 8-second interval, without interrupting an already
visible Agents or Weather page. Diagnostics is a non-carousel surface and
notifications are full-screen overlays, so either one defers the visible swap
until that explicit surface exits. BOOT short press advances among the current
Codex-slot variant, Agents, and Weather and resets the destination interval;
double press replays the latest unexpired notification; triple press immediately
pins the Codex Token-rate page and pauses the carousel; the next short press resumes the
carousel from Token rate when active, or from the quota panel when inactive. A
2-second hold toggles diagnostics and a 5-second hold is reserved for
provisioning. A notification saves both the page
and remaining interval, which are restored after the notification queue drains.

There are no task, inbox, message, evaluation, benchmark, or Codex five-hour
window pages or models in the current scope.

State patches always update the in-memory model. A patch redraws the visible
carousel page without a fade only when it changes that page's domain or the
shared system status shown in every header. Other page domains remain a silent
background update and render from the latest model on the next page switch.
Notifications continue to suppress carousel redraws while visible.

Every page header names the active device link as `USB 在线` or `WiFi 在线`.
Stale provider state retains the transport name as `USB 部分可用` or
`WiFi 部分可用`; a transport switch clears ready state until the new session's
snapshot arrives, then redraws the visible header.

The inactive Codex slot uses the original full quota panel with both Home rows,
weekly reset and reset-card metadata, plus the 0-0 balance. The active Codex slot
uses a split dashboard: an LVGL 240-degree meter occupies the left 160 px and
displays the daemon's global estimated visible-output rate in `tok/s`; the right
142 px contains two compact weekly-quota fuel bars, reset-card metadata, and the
0-0 balance. The numeric rate remains exact when the 0..240 needle is pinned at
its upper limit. Missing or stale rate data renders `--` instead of a misleading
zero. When a valid nonzero rate returns to zero while the Token-rate page is
visible, the needle eases back to zero over 1.5 seconds; the numeric rate updates
immediately, and unavailable data never replays an old rate.

## Notification Themes

| Theme | Background | Foreground | Default |
|---|---:|---:|---:|
| Blue | `#3b82f6` | `#ffffff` | event-defined |
| Yellow | `#f5c842` | `#101318` | event-defined |
| Red | `#e5484d` | `#ffffff` | event-defined |
| Green | `#30a46c` | `#ffffff` | event-defined |

Title and detail are centered and truncated by LVGL's dot mode. Color is not the
only signal: every notification includes a textual title and detail. Display
duration comes from the validated v2 notification (`1500..12000` ms).

The firmware UI uses locally installed MiLanPro Medium at 14/18 px and
MiLanPro SemiBold at 14/18/24 px through LVGL 8.3 TinyTTF memory fonts, with a
4 KiB glyph cache per size. Fixed UI copy and Mock-visible copy are Simplified
Chinese; upstream workspace, tab, and Agent names remain unchanged. The two
local TTF files are ignored by Git and must be installed with
`scripts/install-local-fonts.sh` after the user confirms that the font license
permits device embedding.

Automated coverage checks confirm that both installed weights contain every
Chinese character and symbol currently referenced by the firmware UI and Mock
provider. Dynamic-text missing-glyph fallback and a framebuffer dump still
remain before complete `docs/ui.md` visual acceptance is closed.
