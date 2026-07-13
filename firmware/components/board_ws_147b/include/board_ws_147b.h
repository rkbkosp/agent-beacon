#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "esp_err.h"

#define BOARD_WS_147B_DISPLAY_WIDTH 320
#define BOARD_WS_147B_DISPLAY_HEIGHT 172

typedef bool (*board_display_transfer_done_cb_t)(void *user_context);

esp_err_t board_init(void);
esp_err_t board_display_init(void);
esp_err_t board_display_fill_rgb565(uint16_t color);
esp_err_t board_display_draw_bitmap_native(uint16_t x_start, uint16_t y_start,
                                           uint16_t x_end, uint16_t y_end,
                                           const void *color_data);
esp_err_t board_display_set_transfer_done_callback(board_display_transfer_done_cb_t callback,
                                                   void *user_context);
esp_err_t board_backlight_set(uint8_t percent);
bool board_boot_button_pressed(void);
