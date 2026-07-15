#pragma once

#include <stddef.h>
#include <stdint.h>

#include "beacon_app_state.h"
#include "beacon_ui_state.h"

typedef struct {
    const char *title;
} beacon_page_content_t;

typedef struct {
    uint32_t background_rgb;
    uint32_t foreground_rgb;
} beacon_theme_palette_t;

const beacon_page_content_t *beacon_ui_page_content(beacon_page_t page);
const beacon_theme_palette_t *beacon_ui_theme_palette(beacon_theme_t theme);
const beacon_app_state_t *beacon_ui_default_app_state(void);
void beacon_ui_format_weather_recommendation(const beacon_weather_state_t *weather,
                                             char *output, size_t output_size);
