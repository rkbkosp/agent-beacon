#include "beacon_ui_model.h"

#include <stdbool.h>
#include <stddef.h>

static const beacon_page_content_t PAGES[BEACON_PAGE_COUNT] = {
    [BEACON_PAGE_CODEX] = {.title = "CODEX 配额"},
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
