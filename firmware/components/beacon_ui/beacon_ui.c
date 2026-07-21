#include "beacon_ui.h"

#include <stdio.h>
#include <string.h>

#include "beacon_fonts.h"
#include "beacon_transport.h"
#include "beacon_ui_model.h"
#include "board_ws_147b.h"
#include "board_ws_147b_geometry.h"
#include "esp_check.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "lvgl.h"

#define LVGL_DRAW_ROWS 20
#define LVGL_TICK_PERIOD_MS 2

static const char *TAG = "beacon_ui";
static lv_disp_draw_buf_t draw_buffer;
static lv_disp_drv_t display_driver;
static lv_disp_t *display;
static lv_color_t *draw_buffer_1;
static lv_color_t *draw_buffer_2;
static esp_timer_handle_t tick_timer;
static lv_obj_t *screen;
static beacon_app_state_t app_state;
static beacon_diagnostics_snapshot_t diagnostics_snapshot;
static bool connection_snapshot_ready;

#define FONT_BODY_14 beacon_font_medium_14()
#define FONT_BODY_18 beacon_font_medium_18()
#define FONT_HEADING_14 beacon_font_semibold_14()
#define FONT_HEADING_18 beacon_font_semibold_18()
#define FONT_HEADING_24 beacon_font_semibold_24()

static const uint32_t COLOR_BACKGROUND = 0x101318;
static const uint32_t COLOR_FOREGROUND = 0xf4f7fb;
static const uint32_t COLOR_MUTED = 0x8d98a6;
static const uint32_t COLOR_SUBTLE = 0x252c36;
static const uint32_t COLOR_BLUE = 0x3b82f6;
static const uint32_t COLOR_YELLOW = 0xf5c842;
static const uint32_t COLOR_RED = 0xe5484d;
static const uint32_t COLOR_GREEN = 0x30a46c;

#define TOKEN_RATE_GAUGE_MAX 240
#define TOKEN_RATE_NEEDLE_RETURN_MS 1500U

typedef struct {
    lv_obj_t *meter;
    lv_meter_indicator_t *needle;
    int32_t displayed_value;
    int32_t return_start_value;
    uint32_t return_started_at;
    bool return_pending;
    bool return_active;
} token_rate_gauge_runtime_t;

static token_rate_gauge_runtime_t token_rate_gauge;

static void tick_callback(void *argument)
{
    (void)argument;
    lv_tick_inc(LVGL_TICK_PERIOD_MS);
}

static bool transfer_done_callback(void *user_context)
{
    lv_disp_flush_ready((lv_disp_drv_t *)user_context);
    return false;
}

static void flush_callback(lv_disp_drv_t *driver, const lv_area_t *area, lv_color_t *color_map)
{
    const esp_err_t error = board_display_draw_bitmap_native(
        (uint16_t)area->x1, (uint16_t)area->y1,
        (uint16_t)(area->x2 + 1), (uint16_t)(area->y2 + 1), color_map);
    if (error != ESP_OK) {
        ESP_LOGE(TAG, "flush failed: %s", esp_err_to_name(error));
        lv_disp_flush_ready(driver);
    }
}

static lv_obj_t *create_label(lv_coord_t x, lv_coord_t y, lv_coord_t width, lv_coord_t height,
                              const lv_font_t *font, lv_text_align_t alignment, uint32_t color)
{
    lv_obj_t *label = lv_label_create(screen);
    lv_obj_set_pos(label, x, y);
    lv_obj_set_size(label, width, height);
    lv_label_set_long_mode(label, LV_LABEL_LONG_DOT);
    lv_obj_set_style_text_font(label, font, 0);
    lv_obj_set_style_text_align(label, alignment, 0);
    lv_obj_set_style_text_letter_space(label, 0, 0);
    lv_obj_set_style_text_color(label, lv_color_hex(color), 0);
    return label;
}

static lv_obj_t *create_box(lv_coord_t x, lv_coord_t y, lv_coord_t width, lv_coord_t height,
                            uint32_t color)
{
    lv_obj_t *box = lv_obj_create(screen);
    lv_obj_set_pos(box, x, y);
    lv_obj_set_size(box, width, height);
    lv_obj_clear_flag(box, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_set_style_border_width(box, 0, 0);
    lv_obj_set_style_pad_all(box, 0, 0);
    lv_obj_set_style_radius(box, 3, 0);
    lv_obj_set_style_bg_color(box, lv_color_hex(color), 0);
    lv_obj_set_style_bg_opa(box, LV_OPA_COVER, 0);
    return box;
}

static void set_text(lv_obj_t *label, const char *text)
{
    lv_label_set_text(label, text != NULL ? text : "-");
}

static void begin_screen(uint32_t background)
{
    lv_obj_clean(screen);
    token_rate_gauge.meter = NULL;
    token_rate_gauge.needle = NULL;
    lv_obj_set_style_bg_color(screen, lv_color_hex(background), 0);
    lv_obj_set_style_bg_opa(screen, LV_OPA_COVER, 0);
}

static void create_header_with_title_width(const char *title, const char *suffix,
                                           lv_coord_t title_width)
{
    set_text(create_label(8, 3, title_width, 18, FONT_HEADING_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_FOREGROUND), title);
    set_text(create_label(12 + title_width, 3, 300 - title_width, 18, FONT_BODY_14,
                          LV_TEXT_ALIGN_RIGHT,
                          COLOR_MUTED), suffix);
    lv_obj_t *line = create_box(8, 22, 304, 1, COLOR_SUBTLE);
    lv_obj_set_style_radius(line, 0, 0);
}

static void create_header(const char *title, const char *suffix)
{
    create_header_with_title_width(title, suffix, 176);
}

static const char *connection_suffix(void)
{
    return beacon_ui_connection_status_label(
        app_state.system.bridge_online, beacon_transport_is_connected(),
        connection_snapshot_ready, app_state.system.overall_freshness,
        beacon_transport_active_kind());
}

static uint32_t quota_color(const beacon_codex_home_t *home)
{
    if (home->freshness == BEACON_FRESHNESS_STALE) {
        return COLOR_MUTED;
    }
    if (home->weekly_remaining_percent < 5) {
        return COLOR_RED;
    }
    if (home->weekly_remaining_percent <= 30) {
        return COLOR_YELLOW;
    }
    return COLOR_BLUE;
}

static void create_codex_home(const beacon_codex_home_t *home, lv_coord_t y)
{
    char value[24];
    set_text(create_label(8, y, 64, 18, FONT_HEADING_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_FOREGROUND), home->label);
    lv_snprintf(value, sizeof(value), "%u%%", home->weekly_remaining_percent);
    set_text(create_label(70, y - 3, 60, 25, FONT_HEADING_24, LV_TEXT_ALIGN_LEFT,
                          quota_color(home)), value);
    set_text(create_label(158, y, 154, 18, FONT_BODY_14, LV_TEXT_ALIGN_RIGHT,
                          COLOR_MUTED), home->weekly_reset);

    lv_obj_t *bar = lv_bar_create(screen);
    lv_obj_set_pos(bar, 8, y + 25);
    lv_obj_set_size(bar, 142, 7);
    lv_bar_set_range(bar, 0, 100);
    lv_bar_set_value(bar, home->weekly_remaining_percent, LV_ANIM_OFF);
    lv_obj_set_style_bg_color(bar, lv_color_hex(COLOR_SUBTLE), LV_PART_MAIN);
    lv_obj_set_style_bg_opa(bar, LV_OPA_COVER, LV_PART_MAIN);
    lv_obj_set_style_bg_color(bar, lv_color_hex(quota_color(home)), LV_PART_INDICATOR);
    lv_obj_set_style_radius(bar, 2, LV_PART_MAIN | LV_PART_INDICATOR);

    if (home->reset_cards_available < 0) {
        lv_snprintf(value, sizeof(value), "重置卡 - | -");
    } else {
        lv_snprintf(value, sizeof(value), "重置卡 %d | %s", home->reset_cards_available,
                    home->nearest_card_expiry);
    }
    set_text(create_label(158, y + 20, 154, 18, FONT_BODY_14, LV_TEXT_ALIGN_RIGHT,
                          COLOR_MUTED), value);
}

static void show_codex_quota_page(void)
{
    begin_screen(COLOR_BACKGROUND);
    create_header("CODEX 配额", connection_suffix());
    for (size_t index = 0; index < app_state.codex.home_count && index < 2; ++index) {
        create_codex_home(&app_state.codex.homes[index], index == 0 ? 29 : 78);
    }
    create_box(8, 128, 304, 36, COLOR_SUBTLE);
    set_text(create_label(16, 137, 150, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_MUTED), "0-0 中转");
    set_text(create_label(172, 133, 132, 24, FONT_HEADING_18, LV_TEXT_ALIGN_RIGHT,
                          app_state.codex.relay.is_valid ? COLOR_FOREGROUND : COLOR_YELLOW),
             app_state.codex.relay.display);
}

static int32_t token_rate_gauge_value(const beacon_token_rate_state_t *rate)
{
    if (!rate->available || rate->tokens_per_second <= 0.0f) {
        return 0;
    }
    if (rate->tokens_per_second >= TOKEN_RATE_GAUGE_MAX) {
        return TOKEN_RATE_GAUGE_MAX;
    }
    return (int32_t)(rate->tokens_per_second + 0.5f);
}

static uint32_t token_rate_color(const beacon_token_rate_state_t *rate)
{
    if (!rate->available || rate->freshness == BEACON_FRESHNESS_STALE ||
        rate->freshness == BEACON_FRESHNESS_UNKNOWN) {
        return COLOR_MUTED;
    }
    if (rate->freshness == BEACON_FRESHNESS_CACHED) {
        return COLOR_YELLOW;
    }
    return COLOR_BLUE;
}

static bool token_rate_value_is_usable(const beacon_token_rate_state_t *rate)
{
    return rate->available && (rate->freshness == BEACON_FRESHNESS_FRESH ||
                               rate->freshness == BEACON_FRESHNESS_CACHED);
}

static void set_token_rate_needle_value(void *meter, int32_t value)
{
    if (meter != token_rate_gauge.meter || token_rate_gauge.needle == NULL) {
        return;
    }
    token_rate_gauge.displayed_value = value;
    lv_meter_set_indicator_value(meter, token_rate_gauge.needle, value);
}

static void finish_token_rate_needle_return(lv_anim_t *animation)
{
    if (animation->var == token_rate_gauge.meter) {
        token_rate_gauge.displayed_value = 0;
        token_rate_gauge.return_active = false;
    }
}

static void cancel_token_rate_needle_return(void)
{
    token_rate_gauge.return_pending = false;
    token_rate_gauge.return_active = false;
}

static void create_token_rate_meter(const beacon_token_rate_state_t *rate,
                                    bool animate_zero_return)
{
    const uint32_t color = token_rate_color(rate);
    const int32_t gauge_value = token_rate_gauge_value(rate);
    int32_t needle_value = gauge_value;
    uint32_t needle_animation_ms = 0U;

    if (animate_zero_return && token_rate_value_is_usable(rate) && gauge_value == 0) {
        if (token_rate_gauge.return_pending) {
            needle_value = token_rate_gauge.return_start_value;
            token_rate_gauge.return_started_at = lv_tick_get();
            token_rate_gauge.return_pending = false;
            token_rate_gauge.return_active = needle_value > 0;
            if (token_rate_gauge.return_active) {
                needle_animation_ms = TOKEN_RATE_NEEDLE_RETURN_MS;
            }
        } else if (token_rate_gauge.return_active) {
            // A live state refresh rebuilds the meter; keep the original deadline.
            const uint32_t elapsed_ms = lv_tick_elaps(token_rate_gauge.return_started_at);
            if (elapsed_ms < TOKEN_RATE_NEEDLE_RETURN_MS &&
                token_rate_gauge.displayed_value > 0) {
                needle_value = token_rate_gauge.displayed_value;
                needle_animation_ms = TOKEN_RATE_NEEDLE_RETURN_MS - elapsed_ms;
            } else {
                token_rate_gauge.return_active = false;
            }
        }
    } else {
        cancel_token_rate_needle_return();
    }

    lv_obj_t *meter = lv_meter_create(screen);
    lv_obj_set_pos(meter, 9, 24);
    lv_obj_set_size(meter, 150, 146);
    lv_obj_clear_flag(meter, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_set_style_bg_opa(meter, LV_OPA_TRANSP, 0);
    lv_obj_set_style_border_width(meter, 0, 0);
    lv_obj_set_style_pad_all(meter, 10, 0);
    lv_obj_set_style_text_font(meter, FONT_BODY_14, LV_PART_TICKS);
    lv_obj_set_style_text_color(meter, lv_color_hex(COLOR_MUTED), LV_PART_TICKS);

    lv_meter_scale_t *scale = lv_meter_add_scale(meter);
    lv_meter_set_scale_ticks(meter, scale, 13, 1, 5, lv_color_hex(COLOR_MUTED));
    lv_meter_set_scale_major_ticks(meter, scale, 3, 2, 8,
                                   lv_color_hex(COLOR_FOREGROUND), 3);
    lv_meter_set_scale_range(meter, scale, 0, TOKEN_RATE_GAUGE_MAX, 240, 150);

    lv_meter_indicator_t *progress = lv_meter_add_arc(
        meter, scale, 3, lv_color_hex(color), 0);
    lv_meter_set_indicator_start_value(meter, progress, 0);
    lv_meter_set_indicator_end_value(meter, progress, gauge_value);
    lv_meter_indicator_t *needle = lv_meter_add_needle_line(
        meter, scale, 3, lv_color_hex(color), -12);
    token_rate_gauge.meter = meter;
    token_rate_gauge.needle = needle;
    token_rate_gauge.displayed_value = needle_value;
    lv_meter_set_indicator_value(meter, needle, needle_value);

    if (needle_animation_ms > 0U) {
        lv_anim_t animation;
        lv_anim_init(&animation);
        lv_anim_set_var(&animation, meter);
        lv_anim_set_exec_cb(&animation, set_token_rate_needle_value);
        lv_anim_set_values(&animation, needle_value, 0);
        lv_anim_set_time(&animation, needle_animation_ms);
        lv_anim_set_path_cb(&animation, lv_anim_path_ease_out);
        lv_anim_set_ready_cb(&animation, finish_token_rate_needle_return);
        lv_anim_start(&animation);
    }

    char value[24];
    if (!rate->available) {
        lv_snprintf(value, sizeof(value), "--");
    } else if (rate->tokens_per_second < 1000.0f) {
        lv_snprintf(value, sizeof(value), "%.1f", (double)rate->tokens_per_second);
    } else {
        lv_snprintf(value, sizeof(value), "%.0f", (double)rate->tokens_per_second);
    }
    set_text(create_label(29, 75, 110, 29, FONT_HEADING_24, LV_TEXT_ALIGN_CENTER,
                          color), value);
    set_text(create_label(34, 103, 100, 18, FONT_BODY_14, LV_TEXT_ALIGN_CENTER,
                          COLOR_MUTED), "估算 tok/s");

    char activity[32];
    if (rate->freshness == BEACON_FRESHNESS_STALE) {
        lv_snprintf(activity, sizeof(activity), "速度数据已过期");
    } else if (!rate->available) {
        lv_snprintf(activity, sizeof(activity), "等待速度数据");
    } else {
        lv_snprintf(activity, sizeof(activity), "%u 会话 · %u 流",
                    (unsigned)rate->active_sessions, (unsigned)rate->active_streams);
    }
    set_text(create_label(16, 139, 136, 18, FONT_BODY_14, LV_TEXT_ALIGN_CENTER,
                          COLOR_MUTED), activity);
}

static void create_codex_fuel(const beacon_codex_home_t *home, lv_coord_t y)
{
    char label[16];
    char value[24];
    char reset[16];
    char cards[24];
    if (home->freshness == BEACON_FRESHNESS_STALE) {
        lv_snprintf(label, sizeof(label), "%s !", home->label);
    } else {
        lv_snprintf(label, sizeof(label), "%s", home->label);
    }
    set_text(create_label(178, y, 56, 18, FONT_HEADING_14, LV_TEXT_ALIGN_LEFT,
                          home->freshness == BEACON_FRESHNESS_STALE ? COLOR_MUTED
                                                                   : COLOR_FOREGROUND),
             label);
    lv_snprintf(value, sizeof(value), "%u%%", home->weekly_remaining_percent);
    set_text(create_label(234, y - 4, 78, 25, FONT_HEADING_24, LV_TEXT_ALIGN_RIGHT,
                          quota_color(home)), value);

    lv_obj_t *bar = lv_bar_create(screen);
    lv_obj_set_pos(bar, 178, y + 22);
    lv_obj_set_size(bar, 134, 6);
    lv_bar_set_range(bar, 0, 100);
    lv_bar_set_value(bar, home->weekly_remaining_percent, LV_ANIM_OFF);
    lv_obj_set_style_bg_color(bar, lv_color_hex(COLOR_SUBTLE), LV_PART_MAIN);
    lv_obj_set_style_bg_opa(bar, LV_OPA_COVER, LV_PART_MAIN);
    lv_obj_set_style_bg_color(bar, lv_color_hex(quota_color(home)), LV_PART_INDICATOR);
    lv_obj_set_style_radius(bar, 2, LV_PART_MAIN | LV_PART_INDICATOR);

    if (strcmp(home->weekly_reset, "-") == 0) {
        lv_snprintf(reset, sizeof(reset), "重 -");
    } else {
        lv_snprintf(reset, sizeof(reset), "重 %.5s", home->weekly_reset);
    }
    if (home->reset_cards_available < 0) {
        lv_snprintf(cards, sizeof(cards), "卡 -");
    } else if (strcmp(home->nearest_card_expiry, "-") == 0) {
        lv_snprintf(cards, sizeof(cards), "卡%d · -", home->reset_cards_available);
    } else {
        lv_snprintf(cards, sizeof(cards), "卡%d · %.5s", home->reset_cards_available,
                    home->nearest_card_expiry);
    }
    set_text(create_label(178, y + 30, 58, 16, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_MUTED), reset);
    set_text(create_label(236, y + 30, 76, 16, FONT_BODY_14, LV_TEXT_ALIGN_RIGHT,
                          COLOR_MUTED), cards);
}

static void show_token_rate_page(bool animate_zero_return)
{
    begin_screen(COLOR_BACKGROUND);
    create_header("TOKEN 速度", connection_suffix());
    create_token_rate_meter(&app_state.codex.token_rate, animate_zero_return);
    create_box(169, 29, 1, 135, COLOR_SUBTLE);
    set_text(create_label(178, 27, 134, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_MUTED), "油量 · 周配额");
    for (size_t index = 0; index < app_state.codex.home_count && index < 2; ++index) {
        create_codex_fuel(&app_state.codex.homes[index], index == 0 ? 43 : 91);
    }
    create_box(176, 141, 136, 24, COLOR_SUBTLE);
    set_text(create_label(182, 144, 44, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_MUTED), "0-0");
    set_text(create_label(226, 142, 80, 22, FONT_HEADING_18, LV_TEXT_ALIGN_RIGHT,
                          app_state.codex.relay.is_valid ? COLOR_FOREGROUND : COLOR_YELLOW),
             app_state.codex.relay.display);
}

static uint32_t agent_color(beacon_agent_status_t status)
{
    switch (status) {
    case BEACON_AGENT_BLOCKED:
        return COLOR_YELLOW;
    case BEACON_AGENT_DONE:
        return COLOR_GREEN;
    case BEACON_AGENT_WORKING:
        return COLOR_BLUE;
    default:
        return COLOR_MUTED;
    }
}

static const char *agent_symbol(beacon_agent_status_t status)
{
    switch (status) {
    case BEACON_AGENT_BLOCKED:
        return "!";
    case BEACON_AGENT_DONE:
        return "√";
    case BEACON_AGENT_WORKING:
        return "●";
    case BEACON_AGENT_IDLE:
        return "·";
    default:
        return "?";
    }
}

static void show_agents_page(void)
{
    begin_screen(COLOR_BACKGROUND);
    char header[64];
    size_t working = 0;
    size_t blocked = 0;
    size_t done = 0;
    for (size_t index = 0; index < app_state.agents.item_count; ++index) {
        working += app_state.agents.items[index].status == BEACON_AGENT_WORKING;
        blocked += app_state.agents.items[index].status == BEACON_AGENT_BLOCKED;
        done += app_state.agents.items[index].status == BEACON_AGENT_DONE;
    }
    if (app_state.agents.hidden_count > 0) {
        lv_snprintf(header, sizeof(header), "智能体 %u 工 %u 待 %u 完 +%u", (unsigned)working,
                    (unsigned)blocked, (unsigned)done,
                    (unsigned)app_state.agents.hidden_count);
    } else {
        lv_snprintf(header, sizeof(header), "智能体 %u 工 %u 待 %u 完", (unsigned)working,
                    (unsigned)blocked, (unsigned)done);
    }
    create_header(header, connection_suffix());

    for (size_t index = 0; index < app_state.agents.item_count && index < BEACON_AGENT_ITEM_MAX; ++index) {
        const beacon_agent_item_t *item = &app_state.agents.items[index];
        const lv_coord_t y = 27 + (lv_coord_t)index * 35;
        set_text(create_label(8, y, 16, 18, FONT_HEADING_14, LV_TEXT_ALIGN_CENTER,
                              agent_color(item->status)), agent_symbol(item->status));
        set_text(create_label(28, y, 178, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                              COLOR_FOREGROUND), item->display_name);
        set_text(create_label(210, y, 102, 18, FONT_HEADING_14, LV_TEXT_ALIGN_RIGHT,
                              agent_color(item->status)), beacon_agent_status_label(item->status));
        set_text(create_label(28, y + 17, 284, 15, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                              COLOR_MUTED), item->secondary);
    }
}

static void create_weather_column(const char *heading, int16_t temp_c, const char *text,
                                  lv_coord_t x, bool is_muted)
{
    char temperature[16];
    const uint32_t value_color = is_muted ? COLOR_MUTED : COLOR_FOREGROUND;

    create_box(x, 30, 96, 76, COLOR_SUBTLE);
    set_text(create_label(x + 4, 33, 88, 17, FONT_HEADING_14, LV_TEXT_ALIGN_CENTER,
                          COLOR_MUTED), heading);
    lv_snprintf(temperature, sizeof(temperature), "%d°", temp_c);
    set_text(create_label(x + 4, 51, 88, 30, FONT_HEADING_24, LV_TEXT_ALIGN_CENTER,
                          value_color), temperature);
    set_text(create_label(x + 4, 82, 88, 21, FONT_BODY_18, LV_TEXT_ALIGN_CENTER,
                          value_color), text);
}

static void create_weather_slot(const beacon_weather_slot_t *slot, lv_coord_t x)
{
    char heading[24];
    lv_snprintf(heading, sizeof(heading), "%s%s", slot->label,
                slot->is_past ? " 已过" : "");
    create_weather_column(heading, slot->temp_c, slot->text, x, slot->is_past);
}

static void show_weather_page(void)
{
    begin_screen(COLOR_BACKGROUND);
    char header[64];
    char recommendation[64];
    const lv_font_t *recommendation_font = FONT_HEADING_18;
    uint32_t recommendation_color = COLOR_FOREGROUND;
    bool show_recommendation_background = true;

    lv_snprintf(header, sizeof(header), "%s · %s · %s 更新", app_state.weather.location,
                app_state.weather.provider, app_state.weather.current.observed_time);
    create_header_with_title_width(header, connection_suffix(), 210);

    create_weather_column("当前", app_state.weather.current.temp_c,
                          app_state.weather.current.text, 8,
                          app_state.weather.current.freshness == BEACON_FRESHNESS_STALE);
    create_weather_slot(&app_state.weather.lunch, 112);
    create_weather_slot(&app_state.weather.leave, 216);
    beacon_ui_format_weather_recommendation(&app_state.weather, recommendation,
                                            sizeof(recommendation));

    uint32_t color = COLOR_YELLOW;
    if (!app_state.weather.next_outing.umbrella_known) {
        recommendation_color = COLOR_BACKGROUND;
    } else if (app_state.weather.next_outing.umbrella_required) {
        color = COLOR_RED;
        recommendation_font = FONT_HEADING_24;
    } else {
        show_recommendation_background = false;
    }
    if (show_recommendation_background) {
        create_box(8, 113, 304, 54, color);
    } else {
        lv_obj_t *line = create_box(8, 113, 304, 1, COLOR_SUBTLE);
        lv_obj_set_style_radius(line, 0, 0);
    }
    set_text(create_label(14, 126, 292, 30, recommendation_font, LV_TEXT_ALIGN_CENTER,
                          recommendation_color), recommendation);
}

esp_err_t beacon_ui_init(void)
{
    const board_ws_147b_geometry_t *geometry = board_ws_147b_native_geometry();
    lv_init();
    ESP_RETURN_ON_ERROR(beacon_fonts_init(), TAG, "Milan font initialization failed");
    beacon_app_state_init_mock(&app_state);

    const size_t draw_pixels = geometry->panel_width * LVGL_DRAW_ROWS;
    const uint32_t capabilities = MALLOC_CAP_INTERNAL | MALLOC_CAP_DMA | MALLOC_CAP_8BIT;
    draw_buffer_1 = heap_caps_malloc(draw_pixels * sizeof(lv_color_t), capabilities);
    draw_buffer_2 = heap_caps_malloc(draw_pixels * sizeof(lv_color_t), capabilities);
    ESP_RETURN_ON_FALSE(draw_buffer_1 != NULL && draw_buffer_2 != NULL, ESP_ERR_NO_MEM,
                        TAG, "LVGL draw buffer allocation failed");
    lv_disp_draw_buf_init(&draw_buffer, draw_buffer_1, draw_buffer_2, draw_pixels);

    lv_disp_drv_init(&display_driver);
    display_driver.hor_res = geometry->panel_width;
    display_driver.ver_res = geometry->panel_height;
    display_driver.flush_cb = flush_callback;
    display_driver.draw_buf = &draw_buffer;
    display_driver.sw_rotate = 1;
    display_driver.rotated = LV_DISP_ROT_90;
    ESP_RETURN_ON_ERROR(
        board_display_set_transfer_done_callback(transfer_done_callback, &display_driver),
        TAG, "LVGL transfer callback registration failed");
    display = lv_disp_drv_register(&display_driver);
    ESP_RETURN_ON_FALSE(display != NULL, ESP_FAIL, TAG, "LVGL display registration failed");

    const esp_timer_create_args_t timer_arguments = {.callback = tick_callback, .name = "lvgl_tick"};
    ESP_RETURN_ON_ERROR(esp_timer_create(&timer_arguments, &tick_timer), TAG, "LVGL timer creation failed");
    ESP_RETURN_ON_ERROR(esp_timer_start_periodic(tick_timer, LVGL_TICK_PERIOD_MS * 1000),
                        TAG, "LVGL timer start failed");

    screen = lv_obj_create(NULL);
    lv_obj_clear_flag(screen, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_set_style_border_width(screen, 0, 0);
    lv_obj_set_style_pad_all(screen, 0, 0);
    lv_obj_set_style_radius(screen, 0, 0);
    lv_scr_load(screen);
    ESP_LOGI(TAG, "LVGL 8 ready: logical=%dx%d software rotation=90",
             lv_disp_get_hor_res(display), lv_disp_get_ver_res(display));
    return ESP_OK;
}

void beacon_ui_process(void)
{
    lv_timer_handler();
}

void beacon_ui_set_app_state(const beacon_app_state_t *state)
{
    if (state != NULL) {
        if (beacon_ui_token_rate_drops_to_zero(&app_state.codex.token_rate,
                                               &state->codex.token_rate)) {
            if (token_rate_gauge.meter != NULL) {
                token_rate_gauge.return_start_value = token_rate_gauge.displayed_value;
            } else {
                token_rate_gauge.return_start_value =
                    token_rate_gauge_value(&app_state.codex.token_rate);
            }
            token_rate_gauge.return_pending = true;
            token_rate_gauge.return_active = false;
        } else if (!token_rate_value_is_usable(&state->codex.token_rate) ||
                   state->codex.token_rate.tokens_per_second > 0.0f) {
            cancel_token_rate_needle_return();
        }
        app_state = *state;
    }
}

void beacon_ui_set_diagnostics(const beacon_diagnostics_snapshot_t *snapshot)
{
    if (snapshot != NULL) {
        diagnostics_snapshot = *snapshot;
    }
}

void beacon_ui_set_connection_snapshot_ready(bool ready)
{
    connection_snapshot_ready = ready;
}

static bool render_page(beacon_page_t page, bool animate_zero_return)
{
    switch (page) {
    case BEACON_PAGE_CODEX:
        show_codex_quota_page();
        break;
    case BEACON_PAGE_TOKEN_RATE:
        show_token_rate_page(animate_zero_return);
        break;
    case BEACON_PAGE_AGENTS:
        show_agents_page();
        break;
    case BEACON_PAGE_WEATHER:
        show_weather_page();
        break;
    default:
        return false;
    }
    return true;
}

void beacon_ui_show_page(beacon_page_t page)
{
    if (screen == NULL) {
        return;
    }
    lv_anim_del(screen, NULL);
    if (!render_page(page, false)) {
        return;
    }
    lv_obj_set_style_opa(screen, LV_OPA_0, 0);
    lv_obj_fade_in(screen, 160, 0);
}

void beacon_ui_refresh_page(beacon_page_t page)
{
    if (screen == NULL) {
        return;
    }
    lv_anim_del(screen, NULL);
    if (!render_page(page, page == BEACON_PAGE_TOKEN_RATE)) {
        return;
    }
    lv_obj_set_style_opa(screen, LV_OPA_COVER, 0);
}

void beacon_ui_show_diagnostics(void)
{
    if (screen == NULL) {
        return;
    }
    cancel_token_rate_needle_return();
    begin_screen(COLOR_BACKGROUND);
    create_header("诊断", connection_suffix());
    char line[64];
    lv_snprintf(line, sizeof(line), "固件 M2 · 协议 2 · REV %llu",
                (unsigned long long)app_state.system.revision);
    set_text(create_label(10, 31, 300, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_FOREGROUND), line);
    char temperature[16] = "--";
    char cpu_usage[8] = "--";
    if (diagnostics_snapshot.soc_temperature_available) {
        const int temperature_tenths = diagnostics_snapshot.soc_temperature_tenths_c;
        const unsigned int magnitude =
            (unsigned int)(temperature_tenths < 0 ? -temperature_tenths : temperature_tenths);
        lv_snprintf(temperature, sizeof(temperature), "%s%u.%u°C",
                    temperature_tenths < 0 ? "-" : "", magnitude / 10U,
                    magnitude % 10U);
    }
    if (diagnostics_snapshot.cpu_usage_available) {
        lv_snprintf(cpu_usage, sizeof(cpu_usage), "%u%%",
                    diagnostics_snapshot.cpu_usage_percent);
    }
    lv_snprintf(line, sizeof(line), "SoC %s · CPU %s", temperature, cpu_usage);
    set_text(create_label(10, 56, 300, 18, FONT_HEADING_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_FOREGROUND), line);
    set_text(create_label(10, 81, 300, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_MUTED), "网络 / WS / 堆 / PSRAM");
    set_text(create_label(10, 106, 300, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_MUTED), "CODEX 主/副 · HERDR · 和风天气");
    set_text(create_label(10, 145, 300, 18, FONT_BODY_14, LV_TEXT_ALIGN_CENTER,
                          COLOR_MUTED), "长按 BOOT 退出");
}

void beacon_ui_show_notification(beacon_theme_t theme, const char *title, const char *detail,
                                 const char *source_label)
{
    if (screen == NULL || title == NULL || detail == NULL) {
        return;
    }
    const beacon_theme_palette_t *palette = beacon_ui_theme_palette(theme);
    cancel_token_rate_needle_return();
    begin_screen(palette->background_rgb);
    set_text(create_label(12, 10, 296, 18, FONT_HEADING_14, LV_TEXT_ALIGN_CENTER,
                          palette->foreground_rgb), source_label != NULL ? source_label : "AGENT BEACON");
    set_text(create_label(12, 54, 296, 36, FONT_HEADING_24, LV_TEXT_ALIGN_CENTER,
                          palette->foreground_rgb), title);
    set_text(create_label(20, 101, 280, 42, FONT_BODY_18, LV_TEXT_ALIGN_CENTER,
                          palette->foreground_rgb), detail);
    set_text(create_label(12, 151, 296, 16, FONT_BODY_14, LV_TEXT_ALIGN_CENTER,
                          palette->foreground_rgb), "自动返回");
    lv_obj_set_style_opa(screen, LV_OPA_0, 0);
    lv_obj_fade_in(screen, 180, 0);
}
