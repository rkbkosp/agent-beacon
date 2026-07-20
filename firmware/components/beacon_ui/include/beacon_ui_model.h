#pragma once

#include <stddef.h>
#include <stdint.h>

#include "beacon_app_state.h"
#include "beacon_transport.h"
#include "beacon_ui_state.h"

typedef struct {
    const char *title;
} beacon_page_content_t;

typedef struct {
    uint32_t background_rgb;
    uint32_t foreground_rgb;
} beacon_theme_palette_t;

typedef struct {
    bool transport_connected;
    beacon_transport_kind_t transport_kind;
    bool snapshot_ready;
} beacon_ui_connection_state_t;

const beacon_page_content_t *beacon_ui_page_content(beacon_page_t page);
const beacon_theme_palette_t *beacon_ui_theme_palette(beacon_theme_t theme);
const beacon_app_state_t *beacon_ui_default_app_state(void);
bool beacon_ui_connection_update(beacon_ui_connection_state_t *state,
                                 bool transport_connected,
                                 beacon_transport_kind_t transport_kind,
                                 bool snapshot_received);
bool beacon_ui_connection_is_online(bool bridge_online, bool transport_connected,
                                    bool snapshot_ready);
const char *beacon_ui_connection_status_label(bool bridge_online,
                                              bool transport_connected,
                                              bool snapshot_ready,
                                              beacon_freshness_t freshness,
                                              beacon_transport_kind_t transport_kind);
bool beacon_ui_token_rate_drops_to_zero(const beacon_token_rate_state_t *previous,
                                        const beacon_token_rate_state_t *current);
void beacon_ui_format_weather_recommendation(const beacon_weather_state_t *weather,
                                             char *output, size_t output_size);
