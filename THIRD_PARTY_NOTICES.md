# Third-Party Notices

The MIT license in `LICENSE` applies to Agent Beacon's original project code
and documentation. Third-party software remains under its own license.

## Firmware

| Dependency | Version | License |
|---|---:|---|
| ESP-IDF | 5.5.4 | Apache-2.0 |
| Espressif WebSocket Client | 1.7.0 | Apache-2.0 |
| LVGL | 8.3.11 | MIT |

The ST7789T panel initialization values in
`firmware/components/board_ws_147b/board_ws_147b.c` are adapted from the
Espressif ST7789T example distributed with the Waveshare board demo:

```text
SPDX-FileCopyrightText: 2021-2022 Espressif Systems (Shanghai) CO LTD
SPDX-License-Identifier: Apache-2.0
```

A copy of Apache License 2.0 is provided in `LICENSES/Apache-2.0.txt`.

## macOS Bridge

| Dependency | Version | License |
|---|---:|---|
| gorilla/websocket | 1.5.3 | BSD-2-Clause |
| santhosh-tekuri/jsonschema/v5 | 5.3.1 | Apache-2.0 |
| gopkg.in/yaml.v3 | 3.0.1 | MIT and Apache-2.0; see the upstream per-file terms |

Package managers download these dependencies; they are not vendored in this
repository. Their source distributions include the complete license and notice
files. Binary redistributors are responsible for retaining all notices required
by those licenses.

## Milan Fonts

Milan font files are not part of Agent Beacon, are not distributed by this
repository, and are not covered by its MIT license. The build accepts only
user-supplied local copies. Supplying a font does not grant any right to use,
embed, modify, or redistribute it; those rights must come from the font's
copyright holder or another authorized licensor.
