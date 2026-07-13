idf_component_register(
    SRCS "beacon_network_policy.c" "beacon_network_frame.c" "beacon_network.c"
    INCLUDE_DIRS "include"
    REQUIRES esp_event esp_netif esp_wifi nvs_flash esp_timer
    PRIV_REQUIRES esp_websocket_client
)

target_compile_options(${COMPONENT_LIB} PRIVATE -Wframe-larger-than=1024)
