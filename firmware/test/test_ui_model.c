#include <assert.h>
#include <string.h>

#include "beacon_ui_model.h"

int main(void)
{
    static const char *const expected_titles[] = {
        "TOKEN 速度",
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
    assert(state->codex.token_rate.available);
    assert(state->codex.token_rate.tokens_per_second > 42.6f &&
           state->codex.token_rate.tokens_per_second < 42.8f);
    assert(state->codex.token_rate.active_sessions == 2);
    assert(state->codex.token_rate.active_streams == 3);

    assert(strcmp(beacon_ui_connection_status_label(
                      true, true, true, BEACON_FRESHNESS_FRESH,
                      BEACON_TRANSPORT_USB),
                  "USB 在线") == 0);
    assert(strcmp(beacon_ui_connection_status_label(
                      true, true, true, BEACON_FRESHNESS_CACHED,
                      BEACON_TRANSPORT_WIFI),
                  "WiFi 在线") == 0);
    assert(strcmp(beacon_ui_connection_status_label(
                      true, true, true, BEACON_FRESHNESS_STALE,
                      BEACON_TRANSPORT_USB),
                  "USB 部分可用") == 0);
    assert(strcmp(beacon_ui_connection_status_label(
                      true, true, true, BEACON_FRESHNESS_STALE,
                      BEACON_TRANSPORT_WIFI),
                  "WiFi 部分可用") == 0);
    assert(strcmp(beacon_ui_connection_status_label(
                      true, false, true, BEACON_FRESHNESS_FRESH,
                      BEACON_TRANSPORT_USB),
                  "○ 离线") == 0);
    assert(strcmp(beacon_ui_connection_status_label(
                      true, true, true, BEACON_FRESHNESS_FRESH,
                      BEACON_TRANSPORT_NONE),
                  "○ 离线") == 0);

    beacon_token_rate_state_t previous_rate = state->codex.token_rate;
    beacon_token_rate_state_t current_rate = previous_rate;
    current_rate.tokens_per_second = 0.0f;
    assert(beacon_ui_token_rate_drops_to_zero(&previous_rate, &current_rate));
    assert(!beacon_ui_token_rate_drops_to_zero(&current_rate, &current_rate));
    current_rate.freshness = BEACON_FRESHNESS_STALE;
    assert(!beacon_ui_token_rate_drops_to_zero(&previous_rate, &current_rate));
    current_rate.freshness = BEACON_FRESHNESS_FRESH;
    current_rate.available = false;
    assert(!beacon_ui_token_rate_drops_to_zero(&previous_rate, &current_rate));
    current_rate = previous_rate;
    current_rate.tokens_per_second = 18.0f;
    assert(!beacon_ui_token_rate_drops_to_zero(&previous_rate, &current_rate));
    previous_rate.available = false;
    current_rate.tokens_per_second = 0.0f;
    assert(!beacon_ui_token_rate_drops_to_zero(&previous_rate, &current_rate));
    assert(!beacon_ui_token_rate_drops_to_zero(NULL, &current_rate));
    assert(!beacon_ui_token_rate_drops_to_zero(&previous_rate, NULL));

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
