#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "beacon_network_frame.h"

typedef enum {
    BEACON_NETWORK_ONLINE = 0,
    BEACON_NETWORK_STALE,
    BEACON_NETWORK_OFFLINE,
} beacon_network_freshness_t;

#define BEACON_WEBSOCKET_BUFFER_SIZE 4096
#define BEACON_WEBSOCKET_TASK_STACK_SIZE 8192

uint32_t beacon_network_backoff_ms(size_t attempt, uint32_t jitter_ms);
beacon_network_freshness_t beacon_network_freshness(int64_t now_ms, int64_t last_update_ms);

#ifdef ESP_PLATFORM

#include "esp_err.h"
#include "freertos/FreeRTOS.h"

typedef struct {
    const char *wifi_ssid;
    const char *wifi_password;
    const char *websocket_uri;
    const char *device_id;
    const char *token;
} beacon_network_config_t;

typedef struct {
    size_t length;
    char *data;
} beacon_network_message_t;

esp_err_t beacon_network_start(const beacon_network_config_t *config);
bool beacon_network_is_connected(void);
bool beacon_network_receive(beacon_network_message_t *message, TickType_t timeout);
void beacon_network_message_release(beacon_network_message_t *message);
bool beacon_network_send(const char *data, size_t length, TickType_t timeout);

#endif
