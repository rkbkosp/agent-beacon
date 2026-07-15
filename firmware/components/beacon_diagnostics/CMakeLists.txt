idf_component_register(
    SRCS "beacon_diagnostics.c" "beacon_diagnostics_math.c"
    INCLUDE_DIRS "include"
    REQUIRES esp_driver_tsens esp_system esp_timer freertos
)
