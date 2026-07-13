#include "beacon_network.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "esp_check.h"
#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "esp_netif_sntp.h"
#include "esp_random.h"
#include "esp_websocket_client.h"
#include "esp_wifi.h"
#include "freertos/event_groups.h"
#include "freertos/queue.h"
#include "freertos/task.h"
#include "nvs_flash.h"

#define WIFI_CONNECTED_BIT BIT0
#define WEBSOCKET_CONNECTED_BIT BIT1
#define NETWORK_RX_QUEUE_LENGTH 16
#define NETWORK_TX_QUEUE_LENGTH 16
#define NETWORK_TASK_STACK_SIZE 6144

static const char *TAG = "beacon_network";
static EventGroupHandle_t connection_events;
static QueueHandle_t receive_queue;
static QueueHandle_t transmit_queue;
static esp_websocket_client_handle_t websocket_client;
static bool network_started;
static beacon_network_frame_assembler_t receive_assembler;

static struct {
    char wifi_ssid[33];
    char wifi_password[65];
    char websocket_uri[256];
    char device_id[65];
    char token[129];
    char websocket_headers[320];
} runtime_config;

static void wifi_event_handler(void *argument, esp_event_base_t event_base,
                               int32_t event_id, void *event_data)
{
    (void)argument;
    (void)event_data;
    if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_STA_DISCONNECTED) {
        xEventGroupClearBits(connection_events, WIFI_CONNECTED_BIT | WEBSOCKET_CONNECTED_BIT);
    } else if (event_base == IP_EVENT && event_id == IP_EVENT_STA_GOT_IP) {
        xEventGroupSetBits(connection_events, WIFI_CONNECTED_BIT);
    }
}

static void websocket_event_handler(void *handler_argument, esp_event_base_t event_base,
                                    int32_t event_id, void *event_data)
{
    (void)handler_argument;
    (void)event_base;
    esp_websocket_event_data_t *data = event_data;
    if (event_id == WEBSOCKET_EVENT_CONNECTED) {
        xEventGroupSetBits(connection_events, WEBSOCKET_CONNECTED_BIT);
        ESP_LOGI(TAG, "WebSocket connected");
    } else if (event_id == WEBSOCKET_EVENT_DISCONNECTED || event_id == WEBSOCKET_EVENT_ERROR) {
        xEventGroupClearBits(connection_events, WEBSOCKET_CONNECTED_BIT);
        beacon_network_frame_assembler_reset(&receive_assembler);
        ESP_LOGW(TAG, "WebSocket disconnected");
    } else if (event_id == WEBSOCKET_EVENT_DATA && data != NULL &&
               (data->op_code == 0x1 || data->op_code == 0x0) && data->data_len > 0) {
        char *completed = NULL;
        size_t completed_length = 0;
        const beacon_network_frame_result_t result = beacon_network_frame_assembler_push(
            &receive_assembler, data->data_ptr, (size_t)data->data_len,
            (size_t)data->payload_offset, (size_t)data->payload_len,
            &completed, &completed_length);
        if (result == BEACON_FRAME_REJECTED) {
            ESP_LOGW(TAG, "Dropping invalid or oversized WebSocket message");
            return;
        }
        if (result != BEACON_FRAME_COMPLETE) {
            return;
        }
        const beacon_network_message_t message = {.length = completed_length, .data = completed};
        if (xQueueSend(receive_queue, &message, 0) != pdTRUE) {
            ESP_LOGW(TAG, "Protocol receive queue full; dropping WebSocket message");
            free(completed);
        }
    }
}

static esp_err_t initialize_wifi(void)
{
    esp_err_t error = nvs_flash_init();
    if (error == ESP_ERR_NVS_NO_FREE_PAGES || error == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_RETURN_ON_ERROR(nvs_flash_erase(), TAG, "NVS erase failed");
        error = nvs_flash_init();
    }
    ESP_RETURN_ON_ERROR(error, TAG, "NVS initialization failed");

    error = esp_netif_init();
    if (error != ESP_OK && error != ESP_ERR_INVALID_STATE) {
        return error;
    }
    error = esp_event_loop_create_default();
    if (error != ESP_OK && error != ESP_ERR_INVALID_STATE) {
        return error;
    }
    ESP_RETURN_ON_FALSE(esp_netif_create_default_wifi_sta() != NULL, ESP_ERR_NO_MEM,
                        TAG, "Wi-Fi station netif creation failed");

    wifi_init_config_t initialization = WIFI_INIT_CONFIG_DEFAULT();
    ESP_RETURN_ON_ERROR(esp_wifi_init(&initialization), TAG, "Wi-Fi initialization failed");
    ESP_RETURN_ON_ERROR(esp_event_handler_register(WIFI_EVENT, ESP_EVENT_ANY_ID,
                                                   wifi_event_handler, NULL),
                        TAG, "Wi-Fi event registration failed");
    ESP_RETURN_ON_ERROR(esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP,
                                                   wifi_event_handler, NULL),
                        TAG, "IP event registration failed");

    wifi_config_t configuration = {0};
    memcpy(configuration.sta.ssid, runtime_config.wifi_ssid, strlen(runtime_config.wifi_ssid));
    memcpy(configuration.sta.password, runtime_config.wifi_password,
           strlen(runtime_config.wifi_password));
    configuration.sta.threshold.authmode = runtime_config.wifi_password[0] == '\0'
                                               ? WIFI_AUTH_OPEN
                                               : WIFI_AUTH_WPA2_PSK;
    configuration.sta.pmf_cfg.capable = true;
    configuration.sta.pmf_cfg.required = false;
    ESP_RETURN_ON_ERROR(esp_wifi_set_mode(WIFI_MODE_STA), TAG, "Wi-Fi mode failed");
    ESP_RETURN_ON_ERROR(esp_wifi_set_config(WIFI_IF_STA, &configuration), TAG, "Wi-Fi config failed");
    ESP_RETURN_ON_ERROR(esp_wifi_start(), TAG, "Wi-Fi start failed");

    esp_sntp_config_t time_config = ESP_NETIF_SNTP_DEFAULT_CONFIG("pool.ntp.org");
    error = esp_netif_sntp_init(&time_config);
    if (error != ESP_OK && error != ESP_ERR_INVALID_STATE) {
        ESP_LOGW(TAG, "SNTP initialization failed: %s", esp_err_to_name(error));
    }
    return ESP_OK;
}

static void destroy_websocket(void)
{
    if (websocket_client == NULL) {
        return;
    }
    esp_websocket_client_stop(websocket_client);
    esp_websocket_client_destroy(websocket_client);
    websocket_client = NULL;
    xEventGroupClearBits(connection_events, WEBSOCKET_CONNECTED_BIT);
}

static bool start_websocket(void)
{
    const esp_websocket_client_config_t configuration = {
        .uri = runtime_config.websocket_uri,
        .headers = runtime_config.websocket_headers,
        .network_timeout_ms = 10000,
        .reconnect_timeout_ms = 1000,
        .disable_auto_reconnect = true,
        .ping_interval_sec = 20,
        .buffer_size = BEACON_WEBSOCKET_BUFFER_SIZE,
        .task_stack = BEACON_WEBSOCKET_TASK_STACK_SIZE,
    };
    websocket_client = esp_websocket_client_init(&configuration);
    if (websocket_client == NULL) {
        return false;
    }
    if (esp_websocket_register_events(websocket_client, WEBSOCKET_EVENT_ANY,
                                      websocket_event_handler, NULL) != ESP_OK ||
        esp_websocket_client_start(websocket_client) != ESP_OK) {
        destroy_websocket();
        return false;
    }
    const EventBits_t bits = xEventGroupWaitBits(connection_events, WEBSOCKET_CONNECTED_BIT,
                                                 pdFALSE, pdTRUE, pdMS_TO_TICKS(10000));
    return (bits & WEBSOCKET_CONNECTED_BIT) != 0;
}

static void wait_with_jitter(size_t attempt)
{
    const uint32_t base_delay = beacon_network_backoff_ms(attempt, 0);
    const uint32_t jitter_limit = base_delay / 4U;
    const uint32_t jitter = jitter_limit > 0 ? esp_random() % (jitter_limit + 1U) : 0;
    const uint32_t delay = beacon_network_backoff_ms(attempt, jitter);
    ESP_LOGI(TAG, "Reconnect in %lu ms", (unsigned long)delay);
    vTaskDelay(pdMS_TO_TICKS(delay));
}

static void network_task(void *argument)
{
    (void)argument;
    if (initialize_wifi() != ESP_OK) {
        ESP_LOGE(TAG, "Network initialization failed; local UI remains available");
        vTaskDelete(NULL);
        return;
    }

    size_t wifi_attempt = 0;
    size_t websocket_attempt = 0;
    while (true) {
        if ((xEventGroupGetBits(connection_events) & WIFI_CONNECTED_BIT) == 0) {
            ESP_LOGI(TAG, "Connecting Wi-Fi (attempt %u)", (unsigned int)(wifi_attempt + 1U));
            esp_wifi_connect();
            const EventBits_t bits = xEventGroupWaitBits(connection_events, WIFI_CONNECTED_BIT,
                                                         pdFALSE, pdTRUE, pdMS_TO_TICKS(15000));
            if ((bits & WIFI_CONNECTED_BIT) == 0) {
                wait_with_jitter(wifi_attempt++);
                continue;
            }
            wifi_attempt = 0;
            ESP_LOGI(TAG, "Wi-Fi connected");
        }

        if (!start_websocket()) {
            destroy_websocket();
            wait_with_jitter(websocket_attempt++);
            continue;
        }
        websocket_attempt = 0;

        while ((xEventGroupGetBits(connection_events) &
                (WIFI_CONNECTED_BIT | WEBSOCKET_CONNECTED_BIT)) ==
               (WIFI_CONNECTED_BIT | WEBSOCKET_CONNECTED_BIT)) {
            beacon_network_message_t outgoing = {0};
            if (xQueueReceive(transmit_queue, &outgoing, pdMS_TO_TICKS(100)) == pdTRUE) {
                const int sent = esp_websocket_client_send_text(
                    websocket_client, outgoing.data, outgoing.length, pdMS_TO_TICKS(5000));
                beacon_network_message_release(&outgoing);
                if (sent < 0) {
                    ESP_LOGW(TAG, "WebSocket send failed");
                    xEventGroupClearBits(connection_events, WEBSOCKET_CONNECTED_BIT);
                }
            }
        }
        destroy_websocket();
        if ((xEventGroupGetBits(connection_events) & WIFI_CONNECTED_BIT) != 0) {
            wait_with_jitter(websocket_attempt++);
        }
    }
}

esp_err_t beacon_network_start(const beacon_network_config_t *config)
{
    if (network_started) {
        return ESP_ERR_INVALID_STATE;
    }
    ESP_RETURN_ON_FALSE(config != NULL && config->wifi_ssid != NULL &&
                            config->websocket_uri != NULL && config->device_id != NULL &&
                            config->token != NULL && config->wifi_ssid[0] != '\0' &&
                            config->websocket_uri[0] != '\0' && config->device_id[0] != '\0' &&
                            config->token[0] != '\0',
                        ESP_ERR_INVALID_ARG, TAG, "Wi-Fi, WebSocket, device ID, and token are required");
    ESP_RETURN_ON_FALSE(strlen(config->wifi_ssid) <= 32 &&
                            (config->wifi_password == NULL || strlen(config->wifi_password) <= 63) &&
                            strlen(config->websocket_uri) < sizeof(runtime_config.websocket_uri) &&
                            strlen(config->device_id) < sizeof(runtime_config.device_id) &&
                            strlen(config->token) < sizeof(runtime_config.token),
                        ESP_ERR_INVALID_SIZE, TAG, "Network configuration value is too long");
    snprintf(runtime_config.wifi_ssid, sizeof(runtime_config.wifi_ssid), "%s", config->wifi_ssid);
    snprintf(runtime_config.wifi_password, sizeof(runtime_config.wifi_password), "%s",
             config->wifi_password != NULL ? config->wifi_password : "");
    snprintf(runtime_config.websocket_uri, sizeof(runtime_config.websocket_uri), "%s",
             config->websocket_uri);
    snprintf(runtime_config.device_id, sizeof(runtime_config.device_id), "%s", config->device_id);
    snprintf(runtime_config.token, sizeof(runtime_config.token), "%s", config->token);
    snprintf(runtime_config.websocket_headers, sizeof(runtime_config.websocket_headers),
             "X-Agent-Beacon-Device-ID: %s\r\nX-Agent-Beacon-Token: %s\r\n"
             "X-Agent-Beacon-Protocol: 2\r\n",
             runtime_config.device_id, runtime_config.token);

    beacon_network_frame_assembler_init(&receive_assembler);
    connection_events = xEventGroupCreate();
    receive_queue = xQueueCreate(NETWORK_RX_QUEUE_LENGTH, sizeof(beacon_network_message_t));
    transmit_queue = xQueueCreate(NETWORK_TX_QUEUE_LENGTH, sizeof(beacon_network_message_t));
    ESP_RETURN_ON_FALSE(connection_events != NULL && receive_queue != NULL && transmit_queue != NULL,
                        ESP_ERR_NO_MEM, TAG, "Network queue allocation failed");
    ESP_RETURN_ON_FALSE(xTaskCreate(network_task, "network_task", NETWORK_TASK_STACK_SIZE, NULL, 5, NULL) == pdPASS,
                        ESP_ERR_NO_MEM, TAG, "Network task creation failed");
    network_started = true;
    return ESP_OK;
}

bool beacon_network_is_connected(void)
{
    return network_started &&
           (xEventGroupGetBits(connection_events) &
            (WIFI_CONNECTED_BIT | WEBSOCKET_CONNECTED_BIT)) ==
               (WIFI_CONNECTED_BIT | WEBSOCKET_CONNECTED_BIT);
}

bool beacon_network_receive(beacon_network_message_t *message, TickType_t timeout)
{
    return message != NULL && receive_queue != NULL &&
           xQueueReceive(receive_queue, message, timeout) == pdTRUE;
}

void beacon_network_message_release(beacon_network_message_t *message)
{
    if (message != NULL) {
        free(message->data);
        message->data = NULL;
        message->length = 0;
    }
}

bool beacon_network_send(const char *data, size_t length, TickType_t timeout)
{
    if (data == NULL || length == 0 || length > BEACON_NETWORK_MESSAGE_MAX || transmit_queue == NULL) {
        return false;
    }
    beacon_network_message_t message = {.length = length, .data = malloc(length + 1U)};
    if (message.data == NULL) {
        return false;
    }
    memcpy(message.data, data, length);
    message.data[length] = '\0';
    const bool queued = xQueueSend(transmit_queue, &message, timeout) == pdTRUE;
    if (!queued) {
        beacon_network_message_release(&message);
    }
    return queued;
}
