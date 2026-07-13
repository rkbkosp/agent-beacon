#pragma once

#include <stddef.h>

#define BEACON_NETWORK_MESSAGE_MAX (64U * 1024U)

typedef enum {
    BEACON_FRAME_REJECTED = 0,
    BEACON_FRAME_INCOMPLETE,
    BEACON_FRAME_COMPLETE,
} beacon_network_frame_result_t;

typedef struct {
    char *buffer;
    size_t expected_length;
    size_t received_length;
} beacon_network_frame_assembler_t;

void beacon_network_frame_assembler_init(beacon_network_frame_assembler_t *assembler);
void beacon_network_frame_assembler_reset(beacon_network_frame_assembler_t *assembler);
beacon_network_frame_result_t beacon_network_frame_assembler_push(
    beacon_network_frame_assembler_t *assembler, const char *data, size_t data_length,
    size_t payload_offset, size_t payload_length, char **completed_message,
    size_t *completed_length);
