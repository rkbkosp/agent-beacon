#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "beacon_app_state.h"
#include "beacon_notifications.h"

#define BEACON_PROTOCOL_VERSION 2
#define BEACON_PROTOCOL_MAX_MESSAGE_BYTES (64U * 1024U)

typedef enum {
    BEACON_REVISION_ACCEPTED = 0,
    BEACON_REVISION_DUPLICATE,
    BEACON_REVISION_GAP,
} beacon_revision_result_t;

typedef struct {
    uint64_t current;
} beacon_revision_tracker_t;

bool beacon_protocol_theme_from_string(const char *value, beacon_theme_t *theme);
bool beacon_protocol_category_from_string(const char *value,
                                          beacon_notification_category_t *category);
bool beacon_protocol_urgency_from_string(const char *value, beacon_urgency_t *urgency);
const char *beacon_protocol_ack_status_string(beacon_ack_status_t status);
beacon_revision_result_t beacon_revision_tracker_check(const beacon_revision_tracker_t *tracker,
                                                       uint64_t incoming_revision);
void beacon_revision_tracker_commit(beacon_revision_tracker_t *tracker, uint64_t revision);
bool beacon_revision_tracker_commit_delivery(beacon_revision_tracker_t *tracker,
                                             uint64_t revision, bool delivered);
bool beacon_protocol_parse_rfc3339_ms(const char *value, int64_t *timestamp_ms);

typedef enum {
    BEACON_PROTOCOL_MESSAGE_HELLO = 0,
    BEACON_PROTOCOL_MESSAGE_SNAPSHOT,
    BEACON_PROTOCOL_MESSAGE_STATE_PATCH,
    BEACON_PROTOCOL_MESSAGE_NOTIFICATION,
    BEACON_PROTOCOL_MESSAGE_HEARTBEAT,
    BEACON_PROTOCOL_MESSAGE_ERROR,
} beacon_protocol_message_type_t;

typedef struct {
    beacon_protocol_message_type_t type;
    uint64_t revision;
    uint8_t state_domains;
    beacon_app_state_t state;
    beacon_notification_t notification;
} beacon_protocol_message_t;

bool beacon_protocol_decode(const char *json, size_t length,
                            beacon_protocol_message_t *message);

#ifdef ESP_PLATFORM

#include "esp_err.h"
#include "freertos/FreeRTOS.h"

typedef struct {
    const char *device_id;
    const char *token;
} beacon_protocol_config_t;

esp_err_t beacon_protocol_start(const beacon_protocol_config_t *config);
bool beacon_protocol_receive(beacon_protocol_message_t *message, TickType_t timeout);
bool beacon_protocol_send_ack(const beacon_notification_ack_t *ack);

#endif
