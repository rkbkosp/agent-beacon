#include "beacon_ui_state.h"

#include <stddef.h>

uint32_t beacon_ui_page_interval_ms(beacon_page_t page)
{
    switch (page) {
    case BEACON_PAGE_AGENTS:
        return 6000;
    case BEACON_PAGE_CODEX:
    case BEACON_PAGE_WEATHER:
    default:
        return 8000;
    }
}

static beacon_page_t next_page(beacon_page_t page)
{
    return (beacon_page_t)(((unsigned int)page + 1U) % BEACON_PAGE_COUNT);
}

void beacon_ui_state_init(beacon_ui_state_t *state)
{
    if (state == NULL) {
        return;
    }
    *state = (beacon_ui_state_t) {
        .mode = BEACON_UI_CAROUSEL,
        .saved_mode = BEACON_UI_CAROUSEL,
        .page = BEACON_PAGE_CODEX,
        .saved_page = BEACON_PAGE_CODEX,
        .theme = BEACON_THEME_BLUE,
        .carousel_remaining_ms = 8000,
        .saved_carousel_remaining_ms = 8000,
    };
}

bool beacon_ui_state_tick(beacon_ui_state_t *state, uint32_t elapsed_ms)
{
    if (state == NULL || elapsed_ms == 0 || state->mode == BEACON_UI_DIAGNOSTICS) {
        return false;
    }

    bool changed = false;
    if (state->mode == BEACON_UI_NOTIFICATION) {
        if (elapsed_ms < state->notification_remaining_ms) {
            state->notification_remaining_ms -= elapsed_ms;
            return false;
        }
        elapsed_ms -= state->notification_remaining_ms;
        state->mode = state->saved_mode;
        state->page = state->saved_page;
        state->carousel_remaining_ms = state->saved_carousel_remaining_ms;
        state->notification_remaining_ms = 0;
        changed = true;
        if (state->mode == BEACON_UI_DIAGNOSTICS) {
            return true;
        }
    }

    while (elapsed_ms >= state->carousel_remaining_ms) {
        elapsed_ms -= state->carousel_remaining_ms;
        state->page = next_page(state->page);
        state->carousel_remaining_ms = beacon_ui_page_interval_ms(state->page);
        changed = true;
    }
    state->carousel_remaining_ms -= elapsed_ms;
    return changed;
}

void beacon_ui_state_next_page(beacon_ui_state_t *state)
{
    if (state == NULL || state->mode != BEACON_UI_CAROUSEL) {
        return;
    }
    state->page = next_page(state->page);
    state->carousel_remaining_ms = beacon_ui_page_interval_ms(state->page);
}

void beacon_ui_state_show_notification(beacon_ui_state_t *state, beacon_theme_t theme,
                                       uint32_t display_ms)
{
    if (state == NULL) {
        return;
    }
    if (state->mode != BEACON_UI_NOTIFICATION) {
        state->saved_mode = state->mode;
        state->saved_page = state->page;
        state->saved_carousel_remaining_ms = state->carousel_remaining_ms;
    }
    state->mode = BEACON_UI_NOTIFICATION;
    state->theme = theme;
    state->notification_remaining_ms = display_ms > 0 ? display_ms : 1;
}

void beacon_ui_state_enter_diagnostics(beacon_ui_state_t *state)
{
    if (state != NULL && state->mode == BEACON_UI_CAROUSEL) {
        state->mode = BEACON_UI_DIAGNOSTICS;
    }
}

void beacon_ui_state_exit_diagnostics(beacon_ui_state_t *state)
{
    if (state != NULL && state->mode == BEACON_UI_DIAGNOSTICS) {
        state->mode = BEACON_UI_CAROUSEL;
    }
}
