idf_component_register(
    SRCS "beacon_protocol_types.c" "beacon_protocol_json.c" "beacon_protocol_runtime.c"
    INCLUDE_DIRS "include"
    REQUIRES beacon_notifications beacon_state beacon_transport esp_timer json
)
