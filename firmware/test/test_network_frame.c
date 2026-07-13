#include <assert.h>
#include <stdlib.h>
#include <string.h>

#include "beacon_network_frame.h"

static void test_reassembles_fragmented_text_frame(void)
{
    beacon_network_frame_assembler_t assembler;
    beacon_network_frame_assembler_init(&assembler);
    char *message = NULL;
    size_t length = 0;
    assert(beacon_network_frame_assembler_push(&assembler, "hello ", 6, 0, 11,
                                               &message, &length) == BEACON_FRAME_INCOMPLETE);
    assert(message == NULL);
    assert(beacon_network_frame_assembler_push(&assembler, "world", 5, 6, 11,
                                               &message, &length) == BEACON_FRAME_COMPLETE);
    assert(length == 11 && strcmp(message, "hello world") == 0);
    free(message);
    beacon_network_frame_assembler_reset(&assembler);
}

static void test_rejects_oversized_and_out_of_order_frames_then_recovers(void)
{
    beacon_network_frame_assembler_t assembler;
    beacon_network_frame_assembler_init(&assembler);
    char *message = NULL;
    size_t length = 0;
    assert(beacon_network_frame_assembler_push(&assembler, "x", 1, 0,
                                               BEACON_NETWORK_MESSAGE_MAX + 1U,
                                               &message, &length) == BEACON_FRAME_REJECTED);
    assert(beacon_network_frame_assembler_push(&assembler, "abc", 3, 0, 6,
                                               &message, &length) == BEACON_FRAME_INCOMPLETE);
    assert(beacon_network_frame_assembler_push(&assembler, "def", 3, 2, 6,
                                               &message, &length) == BEACON_FRAME_REJECTED);
    assert(beacon_network_frame_assembler_push(&assembler, "ok", 2, 0, 2,
                                               &message, &length) == BEACON_FRAME_COMPLETE);
    assert(length == 2 && strcmp(message, "ok") == 0);
    free(message);
    beacon_network_frame_assembler_reset(&assembler);
}

int main(void)
{
    test_reassembles_fragmented_text_frame();
    test_rejects_oversized_and_out_of_order_frames_then_recovers();
    return 0;
}
