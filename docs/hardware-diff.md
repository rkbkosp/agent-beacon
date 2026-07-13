# Hardware Differences

No electrical or component difference from the documented hardware baseline has
been detected so far.

Confirmed on the connected board:

- ESP32-S3 revision v0.2;
- 16 MB Flash;
- 8 MB octal PSRAM;
- USB-Serial/JTAG transport;
- QMI8658 response at the official I2C address;
- LCD and RGB initialization paths use the official B-board pins.

The missing TF card is an allowed MVP condition, not a hardware difference.
Physical inspection confirmed the official Demo's LCD orientation, color
order, offset, backlight, and RGB cycle. No BSP correction is required before
extracting the official initialization path.

The first custom BSP build did not fill the complete panel. This was a software
geometry error caused by combining a rotated logical write window with the
native X gap, not a hardware difference. Using the official native panel window
resolved it; no hardware baseline change is required.
