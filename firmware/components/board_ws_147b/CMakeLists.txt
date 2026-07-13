idf_component_register(
    SRCS "board_ws_147b.c" "board_ws_147b_geometry.c"
    INCLUDE_DIRS "include"
    REQUIRES esp_lcd esp_driver_gpio esp_driver_ledc esp_driver_spi heap
)
