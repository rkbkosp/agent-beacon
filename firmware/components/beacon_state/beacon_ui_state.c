#include "beacon_ui_state.h"

#include <stddef.h>

#include "beacon_app_state.h"

uint32_t beacon_ui_page_interval_ms(beacon_page_t page)
{
    switch (page) {
    case BEACON_PAGE_AGENTS:
        return 6000;
    case BEACON_PAGE_CODEX:
        return 15000;
    case BEACON_PAGE_WEATHER:
    default:
        return 8000;
    }
}

bool beacon_ui_connection_snapshot_ready(bool was_ready, bool transport_connected,
                                         bool snapshot_received)
{
    return transport_connected && (was_ready || snapshot_received);
}

bool beacon_ui_connection_is_online(bool bridge_online, bool transport_connected,
                                    bool snapshot_ready)
{
    return bridge_online && transport_connected && snapshot_ready;
}

bool beacon_ui_system_status_changed(const beacon_system_state_t *current,
                                     const beacon_system_state_t *incoming)
{
    return current != NULL && incoming != NULL &&
           (current->bridge_online != incoming->bridge_online ||
            current->overall_freshness != incoming->overall_freshness);
}

bool beacon_ui_page_affected_by_domains(beacon_page_t page, uint8_t domains,
                                        bool system_status_changed)
{
    uint8_t page_domain;
    switch (page) {
    case BEACON_PAGE_CODEX:
        page_domain = BEACON_STATE_DOMAIN_CODEX;
        break;
    case BEACON_PAGE_AGENTS:
        page_domain = BEACON_STATE_DOMAIN_AGENTS;
        break;
    case BEACON_PAGE_WEATHER:
        page_domain = BEACON_STATE_DOMAIN_WEATHER;
        break;
    default:
        return false;
    }

    if ((domains & page_domain) != 0U) {
        return true;
    }

    // Connection/freshness status appears in every carousel page header, but
    // provider patches may carry an unchanged system object.
    return system_status_changed && (domains & BEACON_STATE_DOMAIN_SYSTEM) != 0U;
}

static beacon_page_t next_page(beacon_page_t page, bool codex_active)
{
    switch (page) {
    case BEACON_PAGE_CODEX:
        return BEACON_PAGE_AGENTS;
    case BEACON_PAGE_AGENTS:
        return BEACON_PAGE_WEATHER;
    case BEACON_PAGE_WEATHER:
    default:
        return codex_active ? BEACON_PAGE_CODEX : BEACON_PAGE_AGENTS;
    }
}

void beacon_ui_state_init(beacon_ui_state_t *state)
{
    if (state == NULL) {
        return;
    }
    *state = (beacon_ui_state_t) {
        .mode = BEACON_UI_CAROUSEL,
        .saved_mode = BEACON_UI_CAROUSEL,
        .page = BEACON_PAGE_AGENTS,
        .saved_page = BEACON_PAGE_AGENTS,
        .theme = BEACON_THEME_BLUE,
        .codex_active = false,
        .carousel_remaining_ms = 6000,
        .saved_carousel_remaining_ms = 6000,
    };
}

bool beacon_ui_state_set_codex_active(beacon_ui_state_t *state, bool active)
{
    if (state == NULL || state->codex_active == active) {
        return false;
    }

    state->codex_active = active;
    if (active) {
        if (state->mode == BEACON_UI_NOTIFICATION) {
            state->saved_page = BEACON_PAGE_CODEX;
            state->saved_carousel_remaining_ms =
                beacon_ui_page_interval_ms(BEACON_PAGE_CODEX);
            return false;
        }

        const bool visible_page_changed =
            state->mode == BEACON_UI_CAROUSEL && state->page != BEACON_PAGE_CODEX;
        state->page = BEACON_PAGE_CODEX;
        state->carousel_remaining_ms = beacon_ui_page_interval_ms(BEACON_PAGE_CODEX);
        return visible_page_changed;
    }

    if (state->mode == BEACON_UI_NOTIFICATION) {
        if (state->saved_page == BEACON_PAGE_CODEX) {
            state->saved_page = BEACON_PAGE_AGENTS;
            state->saved_carousel_remaining_ms =
                beacon_ui_page_interval_ms(BEACON_PAGE_AGENTS);
        }
        return false;
    }

    if (state->page != BEACON_PAGE_CODEX) {
        return false;
    }
    state->page = BEACON_PAGE_AGENTS;
    state->carousel_remaining_ms = beacon_ui_page_interval_ms(BEACON_PAGE_AGENTS);
    return state->mode == BEACON_UI_CAROUSEL;
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
        state->page = next_page(state->page, state->codex_active);
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
    state->page = next_page(state->page, state->codex_active);
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
