#include "beacon_network_frame.h"

#include <stdlib.h>
#include <string.h>

void beacon_network_frame_assembler_init(beacon_network_frame_assembler_t *assembler)
{
    if (assembler != NULL) {
        memset(assembler, 0, sizeof(*assembler));
    }
}

void beacon_network_frame_assembler_reset(beacon_network_frame_assembler_t *assembler)
{
    if (assembler == NULL) {
        return;
    }
    free(assembler->buffer);
    memset(assembler, 0, sizeof(*assembler));
}

beacon_network_frame_result_t beacon_network_frame_assembler_push(
    beacon_network_frame_assembler_t *assembler, const char *data, size_t data_length,
    size_t payload_offset, size_t payload_length, char **completed_message,
    size_t *completed_length)
{
    if (completed_message != NULL) *completed_message = NULL;
    if (completed_length != NULL) *completed_length = 0;
    if (assembler == NULL || completed_message == NULL || completed_length == NULL ||
        (data == NULL && data_length > 0U)) {
        return BEACON_FRAME_REJECTED;
    }
    if (payload_offset == 0U) {
        beacon_network_frame_assembler_reset(assembler);
        if (payload_length == 0U || payload_length > BEACON_NETWORK_MESSAGE_MAX) {
            return BEACON_FRAME_REJECTED;
        }
        assembler->buffer = malloc(payload_length + 1U);
        if (assembler->buffer == NULL) {
            return BEACON_FRAME_REJECTED;
        }
        assembler->expected_length = payload_length;
    }
    if (assembler->buffer == NULL || payload_length != assembler->expected_length ||
        payload_offset != assembler->received_length ||
        data_length > assembler->expected_length - assembler->received_length) {
        beacon_network_frame_assembler_reset(assembler);
        return BEACON_FRAME_REJECTED;
    }
    memcpy(assembler->buffer + assembler->received_length, data, data_length);
    assembler->received_length += data_length;
    if (assembler->received_length < assembler->expected_length) {
        return BEACON_FRAME_INCOMPLETE;
    }
    assembler->buffer[assembler->expected_length] = '\0';
    *completed_message = assembler->buffer;
    *completed_length = assembler->expected_length;
    assembler->buffer = NULL;
    assembler->expected_length = 0;
    assembler->received_length = 0;
    return BEACON_FRAME_COMPLETE;
}
