# UI Specification

## Display Geometry

- Physical panel: `172x320`, native controller window at `X=34`, `Y=0`.
- Logical UI: `320x172`, LVGL software rotation 90 degrees.
- Draw buffers: two internal DMA buffers, each 20 native rows.
- Page intervals: Codex 8 seconds, Agents 6 seconds, Weather 8 seconds.

## Carousel

The carousel contains exactly Codex, Agents, and Weather. Diagnostics is a
non-carousel surface and notifications are full-screen overlays. BOOT short
press advances immediately and resets the destination page interval; double
press replays the latest unexpired notification; 2-second hold toggles
diagnostics; 5-second hold is reserved for provisioning. A notification saves
both the page and remaining interval, which are restored after the notification
queue drains.

There are no task, inbox, message, evaluation, benchmark, or Codex five-hour
window pages or models in the current scope.

State patches always update the in-memory model. A patch redraws the visible
carousel page without a fade only when it changes that page's domain or the
shared system status shown in every header. Other page domains remain a silent
background update and render from the latest model on the next page switch.
Notifications continue to suppress carousel redraws while visible.

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
