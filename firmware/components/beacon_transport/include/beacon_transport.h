#pragma once

#include <stdbool.h>
#include <stddef.h>

typedef enum {
    BEACON_TRANSPORT_NONE = 0,
    BEACON_TRANSPORT_WIFI,
    BEACON_TRANSPORT_USB,
} beacon_transport_kind_t;

#ifdef ESP_PLATFORM

#include "beacon_network.h"
#include "esp_err.h"
#include "freertos/FreeRTOS.h"

typedef struct {
    beacon_network_config_t network;
    bool usb_enabled;
} beacon_transport_config_t;

typedef struct {
    size_t length;
    char *data;
} beacon_transport_message_t;

esp_err_t beacon_transport_start(const beacon_transport_config_t *config);
bool beacon_transport_is_connected(void);
beacon_transport_kind_t beacon_transport_active_kind(void);
bool beacon_transport_receive(beacon_transport_message_t *message, TickType_t timeout);
void beacon_transport_message_release(beacon_transport_message_t *message);
bool beacon_transport_send(const char *data, size_t length, TickType_t timeout);

#endif
