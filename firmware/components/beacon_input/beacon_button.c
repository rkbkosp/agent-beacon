#include "beacon_button.h"

#include <stddef.h>

static uint32_t add_saturated(uint32_t value, uint32_t increment)
{
    return UINT32_MAX - value < increment ? UINT32_MAX : value + increment;
}

void beacon_button_init(beacon_button_t *button, uint32_t debounce_ms,
                        uint32_t double_click_ms, uint32_t long_2s_ms,
                        uint32_t long_5s_ms)
{
    if (button == NULL) {
        return;
    }
    *button = (beacon_button_t) {
        .debounce_ms = debounce_ms > 0U ? debounce_ms : 1U,
        .double_click_ms = double_click_ms > 0U ? double_click_ms : 1U,
        .long_2s_ms = long_2s_ms > 0U ? long_2s_ms : 1U,
        .long_5s_ms = long_5s_ms > long_2s_ms ? long_5s_ms : long_2s_ms + 1U,
    };
}

beacon_button_event_t beacon_button_update(beacon_button_t *button, bool raw_pressed,
                                           uint32_t elapsed_ms)
{
    if (button == NULL || elapsed_ms == 0U) {
        return BEACON_BUTTON_NONE;
    }

    if (raw_pressed != button->candidate_pressed) {
        button->candidate_pressed = raw_pressed;
        button->candidate_elapsed_ms = elapsed_ms;
    } else {
        button->candidate_elapsed_ms = add_saturated(button->candidate_elapsed_ms, elapsed_ms);
    }

    if (button->candidate_pressed != button->stable_pressed &&
        button->candidate_elapsed_ms >= button->debounce_ms) {
        button->stable_pressed = button->candidate_pressed;
        if (button->stable_pressed) {
            button->pressed_elapsed_ms = 0;
            button->long_2s_emitted = false;
            button->long_5s_emitted = false;
            return BEACON_BUTTON_NONE;
        }

        if (!button->long_2s_emitted && button->pressed_elapsed_ms > 0U) {
            button->click_pending = true;
            button->click_elapsed_ms = 0;
            button->click_count++;
            if (button->click_count >= 3U) {
                button->click_pending = false;
                button->click_count = 0;
                return BEACON_BUTTON_TRIPLE_PRESS;
            }
        } else {
            button->click_pending = false;
            button->click_count = 0;
            button->click_elapsed_ms = 0;
        }
        return BEACON_BUTTON_NONE;
    }

    if (button->stable_pressed) {
        button->pressed_elapsed_ms = add_saturated(button->pressed_elapsed_ms, elapsed_ms);
        if (!button->long_2s_emitted && button->pressed_elapsed_ms >= button->long_2s_ms) {
            button->long_2s_emitted = true;
            button->click_pending = false;
            button->click_count = 0;
            button->click_elapsed_ms = 0;
            return BEACON_BUTTON_LONG_2S;
        }
        if (!button->long_5s_emitted && button->pressed_elapsed_ms >= button->long_5s_ms) {
            button->long_5s_emitted = true;
            return BEACON_BUTTON_LONG_5S;
        }
        return BEACON_BUTTON_NONE;
    }

    if (button->click_pending && !raw_pressed) {
        button->click_elapsed_ms = add_saturated(button->click_elapsed_ms, elapsed_ms);
        if (button->click_elapsed_ms >= button->double_click_ms) {
            const beacon_button_event_t event = button->click_count == 2U
                                                    ? BEACON_BUTTON_DOUBLE_PRESS
                                                    : BEACON_BUTTON_SHORT_PRESS;
            button->click_pending = false;
            button->click_count = 0;
            button->click_elapsed_ms = 0;
            return event;
        }
    }
    return BEACON_BUTTON_NONE;
}
