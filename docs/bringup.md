# Hardware Bring-up

- Date: 2026-07-14
- Board: Waveshare ESP32-S3-LCD-1.47B
- Port: `/dev/cu.usbmodemXXXX` (local suffix redacted)

## Host Environment

```text
macOS 26.5.2 (Build 25F84)
arm64
Python 3.14.6
git 2.55.0
Go 1.26.5
ESP-IDF v5.5.4
esptool.py v4.12.dev3
EIM 0.17.1
```

ESP-IDF source commit:
`735507283d5b2f9fb363a1901172dbd9e847945d` (`v5.5.4`).

Activate the toolchain with:

```bash
source ~/.espressif/tools/activate_idf_v5.5.4.sh
```

For non-interactive checks, `make doctor` reads the EIM activation file's `-e`
environment output instead of relying on its interactive shell functions.

## Device Identification

Read-only `esptool read_mac` and `flash_id` results:

```text
Chip: ESP32-S3 QFN56, revision v0.2
Features: Wi-Fi, BLE, embedded 8 MB octal PSRAM (AP, 3.3 V)
Crystal: 40 MHz
USB mode: USB-Serial/JTAG
MAC: redacted from the public report
Flash: 16 MB, Quad I/O capability, 3.3 V
Flash manufacturer/device: 0x20/0x4018
```

## Factory Flash Backup

The complete `0x00000000..0x00ffffff` range was read at 460800 baud before
any Flash write.

```text
Repository copy: backups/factory-20260714-133516.bin
External copy: ~/Documents/AgentBeaconBackups/factory-20260714-133516.bin
Size: 16,777,216 bytes
SHA-256: 4bdfb696a9e96ea53164164f8f697a50f0d667c6e61a24d03325753fbb4c2340
```

Both copies were hashed independently and matched. `backups/*.bin` is ignored
by Git. `scripts/restore-factory.sh` validates the exact size, optionally
checks an expected digest, and is dry-run unless `--yes` is supplied.

## Official Demo

Source:
`https://files.waveshare.com/wiki/ESP32-S3-LCD-1.47B/ESP32-S3-LCD-1.47B-Demo.zip`

```text
Archive size: about 60 MB
Archive SHA-256: 9e375aeb82e4ad56212cbbfbf6a8dc7ddb1183469d9904b2d09c7ba070699e08
Project: ESP-IDF/ESP32-S3-LCD-1.47B-Test
LVGL: 8.3.11
espressif/led_strip: 2.5.5
ESP-IDF used for regression: 5.5.4
Built application size: 0x182730 bytes
Built application SHA-256: 74f1d36dab9af2808ab85aba761d4a782040ce4e628a6bdac7cde63ebe48baac
```

The unmodified project built successfully. Its duplicate Kconfig symbols emit
informational warnings under ESP-IDF v5.5.4 but do not fail configuration or
compilation. The built bootloader, partition table, and application were
flashed at `0x0`, `0x8000`, and `0x10000`; esptool verified every write.

## Official BSP Parameters

```text
LCD controller: ST7789T, SPI3, mode 0, 12 MHz
LCD resolution: 172 x 320
LCD pins: SCLK 40, MOSI 45, CS 42, DC 41, RST 39, backlight 46
LCD format: RGB565 (COLMOD 0x55), official driver configured BGR
LCD visible offset: X=34, Y=0
LCD default transform: no XY swap, mirror X=true, mirror Y=false
Backlight: LEDC low-speed timer, channel 0, 4 kHz, 13-bit duty
RGB: GPIO38, one addressable LED, RMT at 10 MHz, RGB order
BOOT: GPIO0, active low with pull-up
IMU: QMI8658 at 0x6b, I2C0, SCL 47, SDA 48, 400 kHz
TF: CLK 14, CMD 15, D0 16, D1 18, D2 17, D3 21
Battery ADC: ADC1 channel 0
```

The ST7789 initialization sequence uses sleep-out, RGB565, porch/gate/VCOM,
power/gamma settings, inversion on (`0x21`), display on (`0x29`), and RAM write
(`0x2c`). Agent Beacon's BSP must preserve this sequence until color and panel
timing are revalidated on hardware.

## Runtime Regression

Serial evidence after flashing the official Demo:

- Bootloader and application report ESP-IDF v5.5.4.
- 16 MB Flash detected at 80 MHz DIO.
- 8 MB octal PSRAM detected at 80 MHz; memory test passed.
- QMI8658 responded with device ID `0x7c`.
- LCD panel and LVGL initialization completed without error.
- BLE scanning started; Wi-Fi scanning found 16 access points.
- No reboot or panic occurred during the observation window.
- TF initialization timed out because no card is inserted; TF is not required
  for the MVP and the Demo continued running.

Physical visual confirmation completed on 2026-07-14:

- the `Onboard Parameter` page had the expected orientation;
- colors appeared correct, with no visible RGB/BGR swap;
- content had no visible offset or clipping error;
- the backlight was stable;
- the board RGB completed its color cycle normally.

This closes the official Demo visual regression. The current project scope does
not use RGB as an MVP alert dependency. The custom BSP calibration described below
subsequently closed the final M0 gate.

## Repeatable Safety Tooling

The following repository commands were exercised after the live regression:

```text
make doctor                                      passed
make detect-device PORT=/dev/cu.usbmodemXXXX    passed
./tests/scripts/test_m0_scripts.sh               11/11 passed
```

`scripts/flash-firmware.sh` refuses to invoke `idf.py flash` unless it finds a
factory image whose size is exactly 16,777,216 bytes. It prints the selected
backup digest and target port before flashing. `scripts/monitor.sh` uses the
same pinned IDF tool resolution without depending on inherited shell functions.

## Custom BSP Calibration

The custom firmware pins ESP-IDF `5.5.4` in `firmware/dependencies.lock` and
uses only the LCD, backlight, and BOOT input for M0. RGB, TF, and IMU are not
initialized. The BSP preserves the official 12 MHz SPI3 bus, GPIO assignments,
ST7789 command table, BGR/RGB565 configuration, and native panel window.

```text
Application size: 246,496 bytes (0x3c2e0)
Application partition: 4 MiB (94% free)
Application SHA-256: 283f49031013b4f12ab01f307d80523bc58cc4f31cb927d7ed5ef79120f6a980
```

The first landscape implementation incorrectly combined a rotated `320x172`
write window with the native 34-pixel X gap. The resulting CASET range exceeded
the controller window, so colors were correct but did not cover the full LCD.
The BSP now keeps the hardware write window at the official native `172x320`,
`X=34`, `Y=0`; logical landscape rotation is left to the later LVGL driver.

After rebuilding and reflashing, the physical board showed complete full-panel
red, yellow, blue, and green pages in sequence. Serial logs completed multiple
cycles with 8 MB PSRAM self-test success and no panic or reset.

## M0 Checklist

- [x] Factory firmware backed up and checksummed in two locations.
- [x] Real serial port, chip revision, Flash size, and PSRAM confirmed.
- [x] Official Demo builds under the pinned ESP-IDF version.
- [x] Official Demo flashed with esptool write verification.
- [x] IMU, LCD initialization, Wi-Fi scan, and stable boot confirmed by logs.
- [x] LCD direction, color order, offset, and backlight confirmed visually.
- [x] RGB animation confirmed visually.
- [x] Custom BSP displays full-panel red, yellow, blue, and green calibration pages.
