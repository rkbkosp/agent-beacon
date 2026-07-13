#include "beacon_protocol.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "beacon_network.h"
#include "cJSON.h"
#include "esp_check.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/queue.h"
#include "freertos/task.h"

#define PROTOCOL_TASK_STACK_SIZE 8192
#define PROTOCOL_UI_QUEUE_LENGTH 16

static const char *TAG = "beacon_protocol";
static QueueHandle_t ui_message_queue;
static beacon_revision_tracker_t revision_tracker;
static char device_id[65];
static bool protocol_started;

static void add_timestamp(cJSON *root)
{
    char timestamp[32] = "1970-01-01T00:00:00Z";
    const time_t now = time(NULL);
    struct tm utc_time;
    if (gmtime_r(&now, &utc_time) != NULL) {
        strftime(timestamp, sizeof(timestamp), "%Y-%m-%dT%H:%M:%SZ", &utc_time);
    }
    cJSON_AddStringToObject(root, "ts", timestamp);
}

static void add_envelope_fields(cJSON *root, const char *id, const char *type,
                                uint64_t revision)
{
    cJSON_AddNumberToObject(root, "v", BEACON_PROTOCOL_VERSION);
    cJSON_AddStringToObject(root, "id", id);
    cJSON_AddStringToObject(root, "type", type);
    add_timestamp(root);
    cJSON_AddNumberToObject(root, "revision", (double)revision);
}

static bool send_json(cJSON *root)
{
    if (root == NULL) {
        return false;
    }
    char *output = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    if (output == NULL) {
        return false;
    }
    const bool sent = beacon_network_send(output, strlen(output), pdMS_TO_TICKS(100));
    free(output);
    return sent;
}

static bool send_hello(void)
{
    char id[65];
    snprintf(id, sizeof(id), "hello-%lld", (long long)esp_timer_get_time());
    cJSON *root = cJSON_CreateObject();
    cJSON *payload = cJSON_CreateObject();
    if (root == NULL || payload == NULL) {
        cJSON_Delete(root);
        cJSON_Delete(payload);
        return false;
    }
    add_envelope_fields(root, id, "hello", revision_tracker.current);
    cJSON_AddStringToObject(payload, "role", "device");
    cJSON_AddStringToObject(payload, "device_id", device_id);
    cJSON_AddNumberToObject(payload, "protocol_version", BEACON_PROTOCOL_VERSION);
    cJSON_AddStringToObject(payload, "firmware_version", "m2-v2");
    cJSON_AddItemToObject(root, "payload", payload);
    return send_json(root);
}

static bool send_get_snapshot(const char *reason)
{
    char id[65];
    snprintf(id, sizeof(id), "get-snapshot-%lld", (long long)esp_timer_get_time());
    cJSON *root = cJSON_CreateObject();
    cJSON *payload = cJSON_CreateObject();
    if (root == NULL || payload == NULL) {
        cJSON_Delete(root);
        cJSON_Delete(payload);
        return false;
    }
    add_envelope_fields(root, id, "get_snapshot", revision_tracker.current);
    if (reason != NULL) {
        cJSON_AddStringToObject(payload, "reason", reason);
    }
    cJSON_AddItemToObject(root, "payload", payload);
    return send_json(root);
}

bool beacon_protocol_send_ack(const beacon_notification_ack_t *ack)
{
    if (ack == NULL || ack->notification_id[0] == '\0' || !beacon_network_is_connected()) {
        return false;
    }
    cJSON *root = cJSON_CreateObject();
    if (root == NULL) {
        return false;
    }
    cJSON_AddNumberToObject(root, "v", BEACON_PROTOCOL_VERSION);
    cJSON_AddStringToObject(root, "type", "ack");
    cJSON_AddStringToObject(root, "id", ack->notification_id);
    cJSON_AddStringToObject(root, "device_id", device_id);
    cJSON_AddStringToObject(root, "status", beacon_protocol_ack_status_string(ack->status));
    add_timestamp(root);
    cJSON *timestamp = cJSON_DetachItemFromObjectCaseSensitive(root, "ts");
    if (timestamp != NULL) {
        cJSON_AddItemToObject(root, "at", timestamp);
    }
    if (ack->reason[0] != '\0') {
        cJSON_AddStringToObject(root, "reason", ack->reason);
    }
    return send_json(root);
}

static void send_simple_ack(const beacon_notification_t *notification,
                            beacon_ack_status_t status, const char *reason)
{
    beacon_notification_ack_t ack = {.status = status};
    snprintf(ack.notification_id, sizeof(ack.notification_id), "%s", notification->id);
    if (reason != NULL) {
        snprintf(ack.reason, sizeof(ack.reason), "%s", reason);
    }
    (void)beacon_protocol_send_ack(&ack);
}

static bool queue_for_ui(const beacon_protocol_message_t *message)
{
    return xQueueSend(ui_message_queue, message, 0) == pdTRUE;
}

static void protocol_task(void *argument)
{
    (void)argument;
    bool was_connected = false;
    while (true) {
        const bool connected = beacon_network_is_connected();
        if (connected && !was_connected) {
            ESP_LOGI(TAG, "Protocol transport connected; waiting for server hello");
        }
        was_connected = connected;

        beacon_network_message_t incoming = {0};
        if (!beacon_network_receive(&incoming, pdMS_TO_TICKS(100))) {
            continue;
        }
        beacon_protocol_message_t message;
        const bool decoded = beacon_protocol_decode(incoming.data, incoming.length, &message);
        beacon_network_message_release(&incoming);
        if (!decoded) {
            ESP_LOGW(TAG, "Rejected invalid protocol v2 message");
            continue;
        }

        if (message.type == BEACON_PROTOCOL_MESSAGE_HELLO) {
            (void)send_hello();
            continue;
        }
        if (message.type == BEACON_PROTOCOL_MESSAGE_SNAPSHOT) {
            beacon_revision_tracker_snapshot(&revision_tracker, message.revision);
            if (!queue_for_ui(&message)) {
                ESP_LOGW(TAG, "UI state queue full after snapshot");
            }
            ESP_LOGI(TAG, "Snapshot revision=%llu", (unsigned long long)message.revision);
            continue;
        }
        if (message.type != BEACON_PROTOCOL_MESSAGE_STATE_PATCH &&
            message.type != BEACON_PROTOCOL_MESSAGE_NOTIFICATION) {
            continue;
        }

        const beacon_revision_result_t revision_result =
            beacon_revision_tracker_message(&revision_tracker, message.revision);
        if (revision_result == BEACON_REVISION_DUPLICATE) {
            if (message.type == BEACON_PROTOCOL_MESSAGE_NOTIFICATION) {
                send_simple_ack(&message.notification, BEACON_ACK_DUPLICATE, "revision_duplicate");
            }
            continue;
        }
        if (revision_result == BEACON_REVISION_GAP) {
            ESP_LOGW(TAG, "Revision gap current=%llu incoming=%llu; requesting snapshot",
                     (unsigned long long)revision_tracker.current,
                     (unsigned long long)message.revision);
            (void)send_get_snapshot("revision_gap");
            continue;
        }
        if (!queue_for_ui(&message)) {
            if (message.type == BEACON_PROTOCOL_MESSAGE_NOTIFICATION) {
                send_simple_ack(&message.notification, BEACON_ACK_DROPPED, "ui_queue_full");
            } else {
                (void)send_get_snapshot("ui_queue_full");
            }
        }
    }
}

esp_err_t beacon_protocol_start(const beacon_protocol_config_t *config)
{
    if (protocol_started) {
        return ESP_ERR_INVALID_STATE;
    }
    ESP_RETURN_ON_FALSE(config != NULL && config->device_id != NULL &&
                            config->device_id[0] != '\0' && strlen(config->device_id) < sizeof(device_id),
                        ESP_ERR_INVALID_ARG, TAG, "Device ID is required or too long");
    snprintf(device_id, sizeof(device_id), "%s", config->device_id);
    ui_message_queue = xQueueCreate(PROTOCOL_UI_QUEUE_LENGTH, sizeof(beacon_protocol_message_t));
    ESP_RETURN_ON_FALSE(ui_message_queue != NULL, ESP_ERR_NO_MEM,
                        TAG, "Protocol UI queue allocation failed");
    ESP_RETURN_ON_FALSE(xTaskCreate(protocol_task, "protocol_task", PROTOCOL_TASK_STACK_SIZE,
                                    NULL, 5, NULL) == pdPASS,
                        ESP_ERR_NO_MEM, TAG, "Protocol task creation failed");
    protocol_started = true;
    return ESP_OK;
}

bool beacon_protocol_receive(beacon_protocol_message_t *message, TickType_t timeout)
{
    return message != NULL && ui_message_queue != NULL &&
           xQueueReceive(ui_message_queue, message, timeout) == pdTRUE;
}
