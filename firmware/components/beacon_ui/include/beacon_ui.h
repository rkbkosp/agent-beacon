#pragma once

#include "beacon_app_state.h"
#include "beacon_ui_state.h"
#include "esp_err.h"

esp_err_t beacon_ui_init(void);
void beacon_ui_process(void);
void beacon_ui_set_app_state(const beacon_app_state_t *state);
void beacon_ui_show_page(beacon_page_t page);
void beacon_ui_show_diagnostics(void);
void beacon_ui_show_notification(beacon_theme_t theme, const char *title, const char *detail,
                                 const char *source_label);
