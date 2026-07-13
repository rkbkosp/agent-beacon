idf_component_register(
    SRCS "app_main.c"
    INCLUDE_DIRS "."
    PRIV_INCLUDE_DIRS "${PROJECT_DIR}"
    REQUIRES board_ws_147b beacon_input beacon_network beacon_notifications
             beacon_protocol beacon_state beacon_ui esp_timer
)

target_compile_options(${COMPONENT_LIB} PRIVATE -Werror=frame-larger-than=512)
