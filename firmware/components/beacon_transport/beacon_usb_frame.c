#include "beacon_usb_frame.h"

#include <stdlib.h>
#include <string.h>

#define BEACON_USB_MAGIC_0 0x41U
#define BEACON_USB_MAGIC_1 0x42U
#define BEACON_USB_FRAME_VERSION 1U

static uint32_t read_u32_be(const uint8_t *value)
{
    return ((uint32_t)value[0] << 24U) | ((uint32_t)value[1] << 16U) |
           ((uint32_t)value[2] << 8U) | (uint32_t)value[3];
}

static void write_u32_be(uint8_t *output, uint32_t value)
{
    output[0] = (uint8_t)(value >> 24U);
    output[1] = (uint8_t)(value >> 16U);
    output[2] = (uint8_t)(value >> 8U);
    output[3] = (uint8_t)value;
}

static uint32_t crc32_ieee(const uint8_t *data, size_t length)
{
    uint32_t crc = 0xffffffffU;
    for (size_t index = 0; index < length; ++index) {
        crc ^= data[index];
        for (unsigned int bit = 0; bit < 8U; ++bit) {
            const uint32_t mask = (uint32_t)-(int32_t)(crc & 1U);
            crc = (crc >> 1U) ^ (0xedb88320U & mask);
        }
    }
    return ~crc;
}

static bool cobs_encode(const uint8_t *input, size_t input_length,
                        uint8_t *output, size_t output_capacity,
                        size_t *output_length)
{
    if (output_capacity == 0U) {
        return false;
    }
    size_t code_index = 0U;
    size_t write_index = 1U;
    uint8_t code = 1U;
    for (size_t read_index = 0; read_index < input_length; ++read_index) {
        if (input[read_index] == 0U) {
            if (code_index >= output_capacity || write_index >= output_capacity) {
                return false;
            }
            output[code_index] = code;
            code_index = write_index++;
            code = 1U;
            continue;
        }
        if (write_index >= output_capacity) {
            return false;
        }
        output[write_index++] = input[read_index];
        ++code;
        if (code == 0xffU) {
            if (code_index >= output_capacity || write_index >= output_capacity) {
                return false;
            }
            output[code_index] = code;
            code_index = write_index++;
            code = 1U;
        }
    }
    if (code_index >= output_capacity) {
        return false;
    }
    output[code_index] = code;
    *output_length = write_index;
    return true;
}

static bool cobs_decode_in_place(uint8_t *buffer, size_t encoded_length,
                                 size_t *decoded_length)
{
    size_t read_index = 0U;
    size_t write_index = 0U;
    while (read_index < encoded_length) {
        const uint8_t code = buffer[read_index++];
        if (code == 0U || (size_t)(code - 1U) > encoded_length - read_index) {
            return false;
        }
        for (uint8_t offset = 1U; offset < code; ++offset) {
            buffer[write_index++] = buffer[read_index++];
        }
        if (code != 0xffU && read_index < encoded_length) {
            buffer[write_index++] = 0U;
        }
    }
    *decoded_length = write_index;
    return true;
}

size_t beacon_usb_frame_wire_size(size_t payload_length)
{
    if (payload_length == 0U || payload_length > BEACON_USB_PAYLOAD_MAX) {
        return 0U;
    }
    const size_t raw_length = BEACON_USB_FRAME_HEADER_SIZE + payload_length +
                              BEACON_USB_FRAME_CRC_SIZE;
    return raw_length + (raw_length / 254U) + 2U;
}

bool beacon_usb_frame_encode(const uint8_t *payload, size_t payload_length,
                             uint8_t *output, size_t output_capacity,
                             size_t *output_length)
{
    if (payload == NULL || output == NULL || output_length == NULL ||
        payload_length == 0U || payload_length > BEACON_USB_PAYLOAD_MAX) {
        return false;
    }
    const size_t raw_length = BEACON_USB_FRAME_HEADER_SIZE + payload_length +
                              BEACON_USB_FRAME_CRC_SIZE;
    uint8_t *raw = malloc(raw_length);
    if (raw == NULL) {
        return false;
    }
    raw[0] = BEACON_USB_MAGIC_0;
    raw[1] = BEACON_USB_MAGIC_1;
    raw[2] = BEACON_USB_FRAME_VERSION;
    raw[3] = 0U;
    write_u32_be(raw + 4U, (uint32_t)payload_length);
    memcpy(raw + BEACON_USB_FRAME_HEADER_SIZE, payload, payload_length);
    write_u32_be(raw + BEACON_USB_FRAME_HEADER_SIZE + payload_length,
                 crc32_ieee(raw, BEACON_USB_FRAME_HEADER_SIZE + payload_length));

    size_t encoded_length = 0U;
    const bool encoded = output_capacity > 1U &&
                         cobs_encode(raw, raw_length, output, output_capacity - 1U,
                                     &encoded_length);
    free(raw);
    if (!encoded || encoded_length >= output_capacity) {
        return false;
    }
    output[encoded_length++] = 0U;
    *output_length = encoded_length;
    return true;
}

void beacon_usb_frame_decoder_init(beacon_usb_frame_decoder_t *decoder)
{
    if (decoder != NULL) {
        memset(decoder, 0, sizeof(*decoder));
    }
}

void beacon_usb_frame_decoder_reset(beacon_usb_frame_decoder_t *decoder)
{
    if (decoder == NULL) {
        return;
    }
    free(decoder->buffer);
    memset(decoder, 0, sizeof(*decoder));
}

static bool decode_completed_frame(beacon_usb_frame_decoder_t *decoder,
                                   uint8_t **completed_payload,
                                   size_t *completed_length)
{
    size_t raw_length = 0U;
    if (!cobs_decode_in_place(decoder->buffer, decoder->length, &raw_length) ||
        raw_length < BEACON_USB_FRAME_HEADER_SIZE + BEACON_USB_FRAME_CRC_SIZE ||
        decoder->buffer[0] != BEACON_USB_MAGIC_0 ||
        decoder->buffer[1] != BEACON_USB_MAGIC_1 ||
        decoder->buffer[2] != BEACON_USB_FRAME_VERSION || decoder->buffer[3] != 0U) {
        return false;
    }
    const size_t payload_length = read_u32_be(decoder->buffer + 4U);
    if (payload_length == 0U || payload_length > BEACON_USB_PAYLOAD_MAX ||
        raw_length != BEACON_USB_FRAME_HEADER_SIZE + payload_length +
                          BEACON_USB_FRAME_CRC_SIZE) {
        return false;
    }
    const size_t checksum_offset = BEACON_USB_FRAME_HEADER_SIZE + payload_length;
    if (read_u32_be(decoder->buffer + checksum_offset) !=
        crc32_ieee(decoder->buffer, checksum_offset)) {
        return false;
    }
    uint8_t *payload = malloc(payload_length + 1U);
    if (payload == NULL) {
        return false;
    }
    memcpy(payload, decoder->buffer + BEACON_USB_FRAME_HEADER_SIZE, payload_length);
    payload[payload_length] = 0U;
    *completed_payload = payload;
    *completed_length = payload_length;
    return true;
}

beacon_usb_frame_result_t beacon_usb_frame_decoder_push(
    beacon_usb_frame_decoder_t *decoder, uint8_t byte,
    uint8_t **completed_payload, size_t *completed_length)
{
    if (completed_payload != NULL) {
        *completed_payload = NULL;
    }
    if (completed_length != NULL) {
        *completed_length = 0U;
    }
    if (decoder == NULL || completed_payload == NULL || completed_length == NULL) {
        return BEACON_USB_FRAME_REJECTED;
    }
    if (byte != 0U) {
        if (decoder->dropping) {
            return BEACON_USB_FRAME_INCOMPLETE;
        }
        if (decoder->buffer == NULL) {
            decoder->buffer = malloc(BEACON_USB_FRAME_ENCODED_MAX);
            if (decoder->buffer == NULL) {
                decoder->dropping = true;
                return BEACON_USB_FRAME_REJECTED;
            }
        }
        if (decoder->length >= BEACON_USB_FRAME_ENCODED_MAX) {
            free(decoder->buffer);
            decoder->buffer = NULL;
            decoder->length = 0U;
            decoder->dropping = true;
            return BEACON_USB_FRAME_REJECTED;
        }
        decoder->buffer[decoder->length++] = byte;
        return BEACON_USB_FRAME_INCOMPLETE;
    }
    if (decoder->dropping) {
        decoder->dropping = false;
        return BEACON_USB_FRAME_INCOMPLETE;
    }
    if (decoder->length == 0U) {
        return BEACON_USB_FRAME_INCOMPLETE;
    }
    const bool decoded = decode_completed_frame(decoder, completed_payload,
                                                completed_length);
    free(decoder->buffer);
    decoder->buffer = NULL;
    decoder->length = 0U;
    return decoded ? BEACON_USB_FRAME_COMPLETE : BEACON_USB_FRAME_REJECTED;
}
