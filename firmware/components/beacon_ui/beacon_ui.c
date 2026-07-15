#include "beacon_ui.h"

#include <stdio.h>
#include <string.h>

#include "beacon_fonts.h"
#include "beacon_network.h"
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
    if (!beacon_ui_connection_is_online(app_state.system.bridge_online,
                                        beacon_network_is_connected(),
                                        connection_snapshot_ready)) {
        return "○ 离线";
    }
    if (app_state.system.overall_freshness == BEACON_FRESHNESS_STALE) {
        return "△ 部分可用";
    }
    return "● 在线";
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

static void show_codex_page(void)
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

static const char *next_outing_label(const beacon_weather_state_t *weather)
{
    if (strcmp(weather->next_outing.slot, "lunch") == 0) {
        return weather->lunch.label[0] != '\0' ? weather->lunch.label : "午饭";
    }
    if (strcmp(weather->next_outing.slot, "leave") == 0) {
        return weather->leave.label[0] != '\0' ? weather->leave.label : "下班";
    }
    return "出门";
}

static void show_weather_page(void)
{
    begin_screen(COLOR_BACKGROUND);
    char header[64];
    char recommendation[64];
    const char *outing_label = next_outing_label(&app_state.weather);
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

    uint32_t color = COLOR_YELLOW;
    if (!app_state.weather.next_outing.umbrella_known) {
        recommendation_color = COLOR_BACKGROUND;
        lv_snprintf(recommendation, sizeof(recommendation), "%s · 判断未知", outing_label);
    } else if (app_state.weather.next_outing.umbrella_required) {
        color = COLOR_RED;
        recommendation_font = FONT_HEADING_24;
        lv_snprintf(recommendation, sizeof(recommendation), "%s · 需要带伞", outing_label);
    } else {
        show_recommendation_background = false;
        lv_snprintf(recommendation, sizeof(recommendation), "%s · 无需带伞", outing_label);
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
        app_state = *state;
    }
}

void beacon_ui_set_connection_snapshot_ready(bool ready)
{
    connection_snapshot_ready = ready;
}

static bool render_page(beacon_page_t page)
{
    switch (page) {
    case BEACON_PAGE_CODEX:
        show_codex_page();
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
    if (!render_page(page)) {
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
    if (!render_page(page)) {
        return;
    }
    lv_obj_set_style_opa(screen, LV_OPA_COVER, 0);
}

void beacon_ui_show_diagnostics(void)
{
    if (screen == NULL) {
        return;
    }
    begin_screen(COLOR_BACKGROUND);
    create_header("诊断", connection_suffix());
    char line[64];
    lv_snprintf(line, sizeof(line), "固件 M2 · 协议 2 · REV %llu",
                (unsigned long long)app_state.system.revision);
    set_text(create_label(10, 31, 300, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_FOREGROUND), line);
    set_text(create_label(10, 57, 300, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_MUTED), "网络 / WS / 堆 / PSRAM");
    set_text(create_label(10, 83, 300, 18, FONT_BODY_14, LV_TEXT_ALIGN_LEFT,
                          COLOR_MUTED), "CODEX 主/副 · HERDR · 和风天气");
    set_text(create_label(10, 138, 300, 18, FONT_BODY_14, LV_TEXT_ALIGN_CENTER,
                          COLOR_MUTED), "长按 BOOT 退出");
}

void beacon_ui_show_notification(beacon_theme_t theme, const char *title, const char *detail,
                                 const char *source_label)
{
    if (screen == NULL || title == NULL || detail == NULL) {
        return;
    }
    const beacon_theme_palette_t *palette = beacon_ui_theme_palette(theme);
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
