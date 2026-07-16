idf_component_register(
    SRCS "beacon_ui.c" "beacon_ui_model.c"
    INCLUDE_DIRS "include"
    REQUIRES board_ws_147b beacon_diagnostics beacon_fonts beacon_state beacon_transport esp_timer heap lvgl__lvgl
)
