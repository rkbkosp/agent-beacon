#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "beacon_app_state.h"

typedef enum {
    BEACON_PAGE_CODEX = 0,
    BEACON_PAGE_AGENTS,
    BEACON_PAGE_WEATHER,
    BEACON_PAGE_COUNT,
} beacon_page_t;

typedef enum {
    BEACON_THEME_BLUE = 0,
    BEACON_THEME_YELLOW,
    BEACON_THEME_RED,
    BEACON_THEME_GREEN,
} beacon_theme_t;

typedef enum {
    BEACON_UI_CAROUSEL = 0,
    BEACON_UI_NOTIFICATION,
    BEACON_UI_DIAGNOSTICS,
} beacon_ui_mode_t;

typedef struct {
    beacon_ui_mode_t mode;
    beacon_ui_mode_t saved_mode;
    beacon_page_t page;
    beacon_page_t saved_page;
    beacon_theme_t theme;
    uint32_t carousel_remaining_ms;
    uint32_t saved_carousel_remaining_ms;
    uint32_t notification_remaining_ms;
} beacon_ui_state_t;

uint32_t beacon_ui_page_interval_ms(beacon_page_t page);
bool beacon_ui_connection_snapshot_ready(bool was_ready, bool transport_connected,
                                         bool snapshot_received);
bool beacon_ui_connection_is_online(bool bridge_online, bool transport_connected,
                                    bool snapshot_ready);
bool beacon_ui_system_status_changed(const beacon_system_state_t *current,
                                     const beacon_system_state_t *incoming);
bool beacon_ui_page_affected_by_domains(beacon_page_t page, uint8_t domains,
                                        bool system_status_changed);
void beacon_ui_state_init(beacon_ui_state_t *state);
bool beacon_ui_state_tick(beacon_ui_state_t *state, uint32_t elapsed_ms);
void beacon_ui_state_next_page(beacon_ui_state_t *state);
void beacon_ui_state_show_notification(beacon_ui_state_t *state, beacon_theme_t theme,
                                       uint32_t display_ms);
void beacon_ui_state_enter_diagnostics(beacon_ui_state_t *state);
void beacon_ui_state_exit_diagnostics(beacon_ui_state_t *state);
