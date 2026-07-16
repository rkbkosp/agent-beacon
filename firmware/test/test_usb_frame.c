#include "beacon_usb_frame.h"

#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void test_round_trip_with_zero_bytes(void)
{
    const uint8_t payload[] = {'{', '"', 'x', '"', ':', 0U, '1', '}'};
    const size_t capacity = beacon_usb_frame_wire_size(sizeof(payload));
    uint8_t *wire = malloc(capacity);
    assert(wire != NULL);
    size_t wire_length = 0U;
    assert(beacon_usb_frame_encode(payload, sizeof(payload), wire, capacity,
                                   &wire_length));
    assert(wire_length <= capacity);
    assert(wire[wire_length - 1U] == 0U);

    beacon_usb_frame_decoder_t decoder;
    beacon_usb_frame_decoder_init(&decoder);
    uint8_t *completed = NULL;
    size_t completed_length = 0U;
    for (size_t index = 0; index < wire_length; ++index) {
        const beacon_usb_frame_result_t result = beacon_usb_frame_decoder_push(
            &decoder, wire[index], &completed, &completed_length);
        assert(result == (index + 1U == wire_length ? BEACON_USB_FRAME_COMPLETE
                                                    : BEACON_USB_FRAME_INCOMPLETE));
    }
    assert(completed_length == sizeof(payload));
    assert(memcmp(completed, payload, sizeof(payload)) == 0);
    free(completed);
    free(wire);
    beacon_usb_frame_decoder_reset(&decoder);
}

static void test_crc_rejection_and_resynchronization(void)
{
    const uint8_t payload[] = "first";
    const size_t capacity = beacon_usb_frame_wire_size(sizeof(payload) - 1U);
    uint8_t *wire = malloc(capacity);
    assert(wire != NULL);
    size_t wire_length = 0U;
    assert(beacon_usb_frame_encode(payload, sizeof(payload) - 1U, wire, capacity,
                                   &wire_length));
    wire[wire_length / 2U] ^= 0x40U;

    beacon_usb_frame_decoder_t decoder;
    beacon_usb_frame_decoder_init(&decoder);
    uint8_t *completed = NULL;
    size_t completed_length = 0U;
    beacon_usb_frame_result_t result = BEACON_USB_FRAME_INCOMPLETE;
    for (size_t index = 0; index < wire_length; ++index) {
        result = beacon_usb_frame_decoder_push(&decoder, wire[index], &completed,
                                               &completed_length);
    }
    assert(result == BEACON_USB_FRAME_REJECTED);
    assert(completed == NULL);

    const uint8_t next_payload[] = "next";
    const size_t next_capacity = beacon_usb_frame_wire_size(sizeof(next_payload) - 1U);
    uint8_t *next_wire = malloc(next_capacity);
    size_t next_length = 0U;
    assert(next_wire != NULL);
    assert(beacon_usb_frame_encode(next_payload, sizeof(next_payload) - 1U,
                                   next_wire, next_capacity, &next_length));
    for (size_t index = 0; index < next_length; ++index) {
        result = beacon_usb_frame_decoder_push(&decoder, next_wire[index], &completed,
                                               &completed_length);
    }
    assert(result == BEACON_USB_FRAME_COMPLETE);
    assert(completed_length == sizeof(next_payload) - 1U);
    assert(memcmp(completed, next_payload, completed_length) == 0);
    free(completed);
    free(next_wire);
    free(wire);
    beacon_usb_frame_decoder_reset(&decoder);
}

static void test_size_limits_and_oversized_stream(void)
{
    assert(beacon_usb_frame_wire_size(0U) == 0U);
    assert(beacon_usb_frame_wire_size(BEACON_USB_PAYLOAD_MAX + 1U) == 0U);
    uint8_t byte = 1U;
    size_t output_length = 0U;
    assert(!beacon_usb_frame_encode(&byte, 0U, &byte, 1U, &output_length));

    beacon_usb_frame_decoder_t decoder;
    beacon_usb_frame_decoder_init(&decoder);
    uint8_t *completed = NULL;
    size_t completed_length = 0U;
    beacon_usb_frame_result_t result = BEACON_USB_FRAME_INCOMPLETE;
    for (size_t index = 0; index <= BEACON_USB_FRAME_ENCODED_MAX; ++index) {
        result = beacon_usb_frame_decoder_push(&decoder, 1U, &completed,
                                               &completed_length);
    }
    assert(result == BEACON_USB_FRAME_REJECTED);
    assert(beacon_usb_frame_decoder_push(&decoder, 0U, &completed,
                                         &completed_length) ==
           BEACON_USB_FRAME_INCOMPLETE);
    beacon_usb_frame_decoder_reset(&decoder);
}

int main(void)
{
    test_round_trip_with_zero_bytes();
    test_crc_rejection_and_resynchronization();
    test_size_limits_and_oversized_stream();
    puts("usb frame tests passed");
    return 0;
}
