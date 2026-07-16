#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define BEACON_USB_PAYLOAD_MAX (64U * 1024U)
#define BEACON_USB_FRAME_HEADER_SIZE 8U
#define BEACON_USB_FRAME_CRC_SIZE 4U
#define BEACON_USB_FRAME_RAW_MAX \
    (BEACON_USB_FRAME_HEADER_SIZE + BEACON_USB_PAYLOAD_MAX + BEACON_USB_FRAME_CRC_SIZE)
#define BEACON_USB_FRAME_ENCODED_MAX \
    (BEACON_USB_FRAME_RAW_MAX + (BEACON_USB_FRAME_RAW_MAX / 254U) + 1U)
#define BEACON_USB_FRAME_WIRE_MAX (BEACON_USB_FRAME_ENCODED_MAX + 1U)

typedef enum {
    BEACON_USB_FRAME_REJECTED = 0,
    BEACON_USB_FRAME_INCOMPLETE,
    BEACON_USB_FRAME_COMPLETE,
} beacon_usb_frame_result_t;

typedef struct {
    uint8_t *buffer;
    size_t length;
    bool dropping;
} beacon_usb_frame_decoder_t;

size_t beacon_usb_frame_wire_size(size_t payload_length);
bool beacon_usb_frame_encode(const uint8_t *payload, size_t payload_length,
                             uint8_t *output, size_t output_capacity,
                             size_t *output_length);
void beacon_usb_frame_decoder_init(beacon_usb_frame_decoder_t *decoder);
void beacon_usb_frame_decoder_reset(beacon_usb_frame_decoder_t *decoder);
beacon_usb_frame_result_t beacon_usb_frame_decoder_push(
    beacon_usb_frame_decoder_t *decoder, uint8_t byte,
    uint8_t **completed_payload, size_t *completed_length);
