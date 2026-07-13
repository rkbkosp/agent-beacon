#pragma once

#include <stdbool.h>
#include <stdint.h>

typedef enum {
    BEACON_BUTTON_NONE = 0,
    BEACON_BUTTON_SHORT_PRESS,
    BEACON_BUTTON_DOUBLE_PRESS,
    BEACON_BUTTON_LONG_2S,
    BEACON_BUTTON_LONG_5S,
} beacon_button_event_t;

typedef struct {
    bool stable_pressed;
    bool candidate_pressed;
    bool click_pending;
    bool long_2s_emitted;
    bool long_5s_emitted;
    uint32_t debounce_ms;
    uint32_t double_click_ms;
    uint32_t long_2s_ms;
    uint32_t long_5s_ms;
    uint32_t candidate_elapsed_ms;
    uint32_t pressed_elapsed_ms;
    uint32_t click_elapsed_ms;
} beacon_button_t;

void beacon_button_init(beacon_button_t *button, uint32_t debounce_ms,
                        uint32_t double_click_ms, uint32_t long_2s_ms,
                        uint32_t long_5s_ms);
beacon_button_event_t beacon_button_update(beacon_button_t *button, bool raw_pressed,
                                           uint32_t elapsed_ms);
