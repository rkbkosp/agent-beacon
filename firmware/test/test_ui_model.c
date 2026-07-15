#include <assert.h>
#include <string.h>

#include "beacon_ui_model.h"

int main(void)
{
    static const char *const expected_titles[] = {
        "CODEX 配额",
        "智能体",
        "天气",
    };

    for (size_t page = 0; page < BEACON_PAGE_COUNT; ++page) {
        const beacon_page_content_t *content = beacon_ui_page_content((beacon_page_t)page);
        assert(content != NULL);
        assert(strcmp(content->title, expected_titles[page]) == 0);
    }
    assert(beacon_ui_page_content(BEACON_PAGE_COUNT) == NULL);

    const beacon_app_state_t *state = beacon_ui_default_app_state();
    assert(state != NULL);
    assert(state->codex.home_count == 2);
    assert(strcmp(state->codex.homes[0].label, "MAIN") == 0);
    assert(state->codex.homes[0].weekly_remaining_percent == 18);
    assert(strcmp(state->codex.relay.display, "$14.16") == 0);
    assert(state->agents.item_count == 4);
    assert(state->weather.current.temp_c == 31);
    assert(state->weather.next_outing.umbrella_required == true);

    char recommendation[64];
    beacon_weather_state_t weather = state->weather;
    beacon_ui_format_weather_recommendation(&weather, recommendation, sizeof(recommendation));
    assert(strcmp(recommendation, "下班·需要带伞·有雨") == 0);
    weather.next_outing.umbrella_required = false;
    strcpy(weather.next_outing.reason, "无雨");
    beacon_ui_format_weather_recommendation(&weather, recommendation, sizeof(recommendation));
    assert(strcmp(recommendation, "下班·无需带伞·无雨") == 0);
    weather.next_outing.umbrella_required = true;
    strcpy(weather.next_outing.reason, "遮阳");
    beacon_ui_format_weather_recommendation(&weather, recommendation, sizeof(recommendation));
    assert(strcmp(recommendation, "下班·需要带伞·遮阳") == 0);
    weather.next_outing.umbrella_known = false;
    strcpy(weather.next_outing.reason, "数据不足");
    beacon_ui_format_weather_recommendation(&weather, recommendation, sizeof(recommendation));
    assert(strcmp(recommendation, "下班·判断未知·数据不足") == 0);

    assert(strcmp(beacon_agent_status_label(BEACON_AGENT_BLOCKED), "需交互") == 0);
    assert(strcmp(beacon_agent_status_label(BEACON_AGENT_DONE), "已完成") == 0);
    assert(strcmp(beacon_agent_status_label(BEACON_AGENT_WORKING), "工作中") == 0);
    assert(strcmp(beacon_agent_status_label(BEACON_AGENT_IDLE), "空闲") == 0);
    assert(strcmp(beacon_agent_status_label(BEACON_AGENT_UNKNOWN), "未知") == 0);

    const beacon_theme_palette_t *blue = beacon_ui_theme_palette(BEACON_THEME_BLUE);
    const beacon_theme_palette_t *yellow = beacon_ui_theme_palette(BEACON_THEME_YELLOW);
    const beacon_theme_palette_t *red = beacon_ui_theme_palette(BEACON_THEME_RED);
    const beacon_theme_palette_t *green = beacon_ui_theme_palette(BEACON_THEME_GREEN);
    assert(blue->background_rgb == 0x155eef && blue->foreground_rgb == 0xffffff);
    assert(yellow->background_rgb == 0xf5c842 && yellow->foreground_rgb == 0x242424);
    assert(red->background_rgb == 0xd92d20 && red->foreground_rgb == 0xffffff);
    assert(green->background_rgb == 0x168a50 && green->foreground_rgb == 0xffffff);
    return 0;
}
