idf_component_register(
    SRCS "beacon_transport.c" "beacon_usb_frame.c"
    INCLUDE_DIRS "include"
    REQUIRES beacon_network esp_driver_usb_serial_jtag esp_timer
)

target_compile_options(${COMPONENT_LIB} PRIVATE -Wframe-larger-than=1024)
