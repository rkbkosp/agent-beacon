#include "beacon_ui_model.h"

#include <stdbool.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>

static const beacon_page_content_t PAGES[BEACON_PAGE_COUNT] = {
    [BEACON_PAGE_CODEX] = {.title = "CODEX 配额"},
    [BEACON_PAGE_TOKEN_RATE] = {.title = "TOKEN 速度"},
    [BEACON_PAGE_AGENTS] = {.title = "智能体"},
    [BEACON_PAGE_WEATHER] = {.title = "天气"},
};

static const beacon_theme_palette_t PALETTES[] = {
    [BEACON_THEME_BLUE] = {.background_rgb = 0x155eef, .foreground_rgb = 0xffffff},
    [BEACON_THEME_YELLOW] = {.background_rgb = 0xf5c842, .foreground_rgb = 0x242424},
    [BEACON_THEME_RED] = {.background_rgb = 0xd92d20, .foreground_rgb = 0xffffff},
    [BEACON_THEME_GREEN] = {.background_rgb = 0x168a50, .foreground_rgb = 0xffffff},
};

const beacon_page_content_t *beacon_ui_page_content(beacon_page_t page)
{
    if (page < 0 || page >= BEACON_PAGE_COUNT) {
        return NULL;
    }
    return &PAGES[page];
}

const beacon_theme_palette_t *beacon_ui_theme_palette(beacon_theme_t theme)
{
    if (theme < BEACON_THEME_BLUE || theme > BEACON_THEME_GREEN) {
        theme = BEACON_THEME_BLUE;
    }
    return &PALETTES[theme];
}

const beacon_app_state_t *beacon_ui_default_app_state(void)
{
    static beacon_app_state_t state;
    static bool initialized;
    if (!initialized) {
        beacon_app_state_init_mock(&state);
        initialized = true;
    }
    return &state;
}

bool beacon_ui_connection_update(beacon_ui_connection_state_t *state,
                                 bool transport_connected,
                                 beacon_transport_kind_t transport_kind,
                                 bool snapshot_received)
{
    if (state == NULL) {
        return false;
    }

    const bool active_transport =
        transport_connected && transport_kind != BEACON_TRANSPORT_NONE;
    const beacon_transport_kind_t active_kind =
        active_transport ? transport_kind : BEACON_TRANSPORT_NONE;
    const bool transport_changed =
        state->transport_connected != active_transport ||
        state->transport_kind != active_kind;

    if (transport_changed) {
        state->transport_connected = active_transport;
        state->transport_kind = active_kind;
        state->snapshot_ready = false;
    }
    if (snapshot_received && active_transport) {
        // A snapshot is itself proof of the current transport session. Recording
        // the transport here makes readiness independent of whether the polling
        // loop or the protocol queue observes a new connection first.
        state->snapshot_ready = true;
    }
    return transport_changed;
}

bool beacon_ui_connection_is_online(bool bridge_online, bool transport_connected,
                                    bool snapshot_ready)
{
    return bridge_online && transport_connected && snapshot_ready;
}

const char *beacon_ui_connection_status_label(bool bridge_online,
                                              bool transport_connected,
                                              bool snapshot_ready,
                                              beacon_freshness_t freshness,
                                              beacon_transport_kind_t transport_kind)
{
    if (!beacon_ui_connection_is_online(bridge_online, transport_connected,
                                        snapshot_ready)) {
        return "○ 离线";
    }
    switch (transport_kind) {
    case BEACON_TRANSPORT_USB:
        return freshness == BEACON_FRESHNESS_STALE ? "USB 部分可用" : "USB 在线";
    case BEACON_TRANSPORT_WIFI:
        return freshness == BEACON_FRESHNESS_STALE ? "WiFi 部分可用" : "WiFi 在线";
    default:
        return "○ 离线";
    }
}

bool beacon_ui_token_rate_drops_to_zero(const beacon_token_rate_state_t *previous,
                                        const beacon_token_rate_state_t *current)
{
    return previous != NULL && current != NULL && previous->available && current->available &&
           (current->freshness == BEACON_FRESHNESS_FRESH ||
            current->freshness == BEACON_FRESHNESS_CACHED) &&
           previous->tokens_per_second > 0.0f && current->tokens_per_second <= 0.0f;
}

void beacon_ui_format_weather_recommendation(const beacon_weather_state_t *weather,
                                             char *output, size_t output_size)
{
    if (output == NULL || output_size == 0U) {
        return;
    }
    output[0] = '\0';
    if (weather == NULL) {
        return;
    }
    const char *label = "出门";
    if (strcmp(weather->next_outing.slot, "lunch") == 0) {
        label = weather->lunch.label[0] != '\0' ? weather->lunch.label : "午饭";
    } else if (strcmp(weather->next_outing.slot, "leave") == 0) {
        label = weather->leave.label[0] != '\0' ? weather->leave.label : "下班";
    }
    const char *decision = "判断未知";
    if (weather->next_outing.umbrella_known) {
        decision = weather->next_outing.umbrella_required ? "需要带伞" : "无需带伞";
    }
    const char *reason = weather->next_outing.reason[0] != '\0'
                             ? weather->next_outing.reason
                             : "数据不足";
    snprintf(output, output_size, "%s·%s·%s", label, decision, reason);
}
