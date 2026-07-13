#pragma once

#include "esp_err.h"
#include "lvgl.h"

esp_err_t beacon_fonts_init(void);

const lv_font_t *beacon_font_medium_14(void);
const lv_font_t *beacon_font_medium_18(void);
const lv_font_t *beacon_font_semibold_14(void);
const lv_font_t *beacon_font_semibold_18(void);
const lv_font_t *beacon_font_semibold_24(void);
