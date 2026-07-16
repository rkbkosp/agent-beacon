#include "beacon_transport.h"

#include <stdlib.h>
#include <string.h>

#include "beacon_usb_frame.h"
#include "driver/usb_serial_jtag.h"
#include "esp_check.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/event_groups.h"
#include "freertos/queue.h"
#include "freertos/task.h"

#define USB_SESSION_ACTIVE_BIT BIT0
#define TRANSPORT_RX_QUEUE_LENGTH 16
#define TRANSPORT_TX_QUEUE_LENGTH 16
#define TRANSPORT_TASK_STACK_SIZE 4096
#define USB_DRIVER_BUFFER_SIZE 8192
#define USB_READ_CHUNK_SIZE 512
#define USB_SESSION_TIMEOUT_MS 12000

static const char *TAG = "beacon_transport";
static EventGroupHandle_t transport_events;
static QueueHandle_t receive_queue;
static QueueHandle_t transmit_queue;
static bool transport_started;
static bool network_available;
static bool usb_available;

static bool usb_session_active(void)
{
    return usb_available && transport_events != NULL &&
           (xEventGroupGetBits(transport_events) & USB_SESSION_ACTIVE_BIT) != 0;
}

static void activate_usb_session(void)
{
    if (usb_session_active()) {
        return;
    }
    xEventGroupSetBits(transport_events, USB_SESSION_ACTIVE_BIT);
    if (network_available) {
        beacon_network_set_suspended(true);
    }
    ESP_LOGI(TAG, "USB business transport selected");
}

static void deactivate_usb_session(const char *reason)
{
    if (!usb_session_active()) {
        return;
    }
    xEventGroupClearBits(transport_events, USB_SESSION_ACTIVE_BIT);
    if (network_available) {
        beacon_network_set_suspended(false);
    }
    ESP_LOGW(TAG, "USB business transport released (%s); Wi-Fi fallback resumed",
             reason != NULL ? reason : "inactive");
}

static void enqueue_received(char *data, size_t length, const char *source)
{
    const beacon_transport_message_t message = {.length = length, .data = data};
    if (xQueueSend(receive_queue, &message, 0) != pdTRUE) {
        ESP_LOGW(TAG, "Protocol receive queue full; dropping %s message", source);
        free(data);
    }
}

static void network_receive_task(void *argument)
{
    (void)argument;
    while (true) {
        beacon_network_message_t incoming = {0};
        if (!beacon_network_receive(&incoming, pdMS_TO_TICKS(100))) {
            continue;
        }
        if (usb_session_active()) {
            beacon_network_message_release(&incoming);
            continue;
        }
        char *data = incoming.data;
        const size_t length = incoming.length;
        incoming.data = NULL;
        incoming.length = 0U;
        enqueue_received(data, length, "Wi-Fi");
    }
}

static bool send_usb_frame(const char *data, size_t length)
{
    const size_t capacity = beacon_usb_frame_wire_size(length);
    if (capacity == 0U || !usb_serial_jtag_is_connected()) {
        return false;
    }
    uint8_t *wire = malloc(capacity);
    if (wire == NULL) {
        return false;
    }
    size_t wire_length = 0U;
    const bool encoded = beacon_usb_frame_encode((const uint8_t *)data, length,
                                                  wire, capacity, &wire_length);
    const int written = encoded
                            ? usb_serial_jtag_write_bytes(wire, wire_length,
                                                          pdMS_TO_TICKS(2000))
                            : 0;
    free(wire);
    return encoded && written == (int)wire_length;
}

static void transmit_task(void *argument)
{
    (void)argument;
    while (true) {
        beacon_transport_message_t outgoing = {0};
        if (xQueueReceive(transmit_queue, &outgoing, portMAX_DELAY) != pdTRUE) {
            continue;
        }
        bool sent = false;
        if (usb_session_active()) {
            sent = send_usb_frame(outgoing.data, outgoing.length);
            if (!sent) {
                deactivate_usb_session("write failed");
            }
        }
        if (!sent && network_available) {
            sent = beacon_network_send(outgoing.data, outgoing.length,
                                       pdMS_TO_TICKS(100));
        }
        if (!sent) {
            ESP_LOGW(TAG, "Dropping outbound protocol message; no transport available");
        }
        beacon_transport_message_release(&outgoing);
    }
}

static void usb_receive_task(void *argument)
{
    (void)argument;
    beacon_usb_frame_decoder_t decoder;
    beacon_usb_frame_decoder_init(&decoder);
    uint8_t chunk[USB_READ_CHUNK_SIZE];
    int64_t last_valid_frame_us = 0;
    while (true) {
        const int count = usb_serial_jtag_read_bytes(chunk, sizeof(chunk),
                                                     pdMS_TO_TICKS(100));
        for (int index = 0; index < count; ++index) {
            uint8_t *payload = NULL;
            size_t payload_length = 0U;
            const beacon_usb_frame_result_t result = beacon_usb_frame_decoder_push(
                &decoder, chunk[index], &payload, &payload_length);
            if (result == BEACON_USB_FRAME_REJECTED) {
                ESP_LOGW(TAG, "Rejected invalid USB business frame");
            } else if (result == BEACON_USB_FRAME_COMPLETE) {
                last_valid_frame_us = esp_timer_get_time();
                activate_usb_session();
                enqueue_received((char *)payload, payload_length, "USB");
            }
        }
        if (!usb_session_active()) {
            continue;
        }
        const int64_t inactive_us = esp_timer_get_time() - last_valid_frame_us;
        if (!usb_serial_jtag_is_connected()) {
            beacon_usb_frame_decoder_reset(&decoder);
            deactivate_usb_session("cable disconnected");
        } else if (inactive_us >= (int64_t)USB_SESSION_TIMEOUT_MS * 1000LL) {
            beacon_usb_frame_decoder_reset(&decoder);
            deactivate_usb_session("heartbeat timeout");
        }
    }
}

esp_err_t beacon_transport_start(const beacon_transport_config_t *config)
{
    if (transport_started) {
        return ESP_ERR_INVALID_STATE;
    }
    ESP_RETURN_ON_FALSE(config != NULL, ESP_ERR_INVALID_ARG, TAG,
                        "Transport configuration is required");
    transport_events = xEventGroupCreate();
    receive_queue = xQueueCreate(TRANSPORT_RX_QUEUE_LENGTH,
                                 sizeof(beacon_transport_message_t));
    transmit_queue = xQueueCreate(TRANSPORT_TX_QUEUE_LENGTH,
                                  sizeof(beacon_transport_message_t));
    ESP_RETURN_ON_FALSE(transport_events != NULL && receive_queue != NULL &&
                            transmit_queue != NULL,
                        ESP_ERR_NO_MEM, TAG, "Transport queue allocation failed");

    const esp_err_t network_error = beacon_network_start(&config->network);
    if (network_error == ESP_OK) {
        network_available = true;
        ESP_RETURN_ON_FALSE(xTaskCreate(network_receive_task, "transport_net_rx",
                                        TRANSPORT_TASK_STACK_SIZE, NULL, 5, NULL) == pdPASS,
                            ESP_ERR_NO_MEM, TAG, "Network relay task creation failed");
    } else {
        ESP_LOGW(TAG, "Wi-Fi fallback unavailable: %s", esp_err_to_name(network_error));
    }

    if (config->usb_enabled) {
        usb_serial_jtag_driver_config_t usb_config = {
            .tx_buffer_size = USB_DRIVER_BUFFER_SIZE,
            .rx_buffer_size = USB_DRIVER_BUFFER_SIZE,
        };
        const esp_err_t usb_error = usb_serial_jtag_driver_install(&usb_config);
        if (usb_error == ESP_OK) {
            usb_available = true;
            ESP_RETURN_ON_FALSE(xTaskCreate(usb_receive_task, "transport_usb_rx",
                                            TRANSPORT_TASK_STACK_SIZE, NULL, 6, NULL) == pdPASS,
                                ESP_ERR_NO_MEM, TAG, "USB receive task creation failed");
        } else {
            ESP_LOGW(TAG, "USB business transport unavailable: %s",
                     esp_err_to_name(usb_error));
        }
    }
    ESP_RETURN_ON_FALSE(network_available || usb_available, ESP_FAIL, TAG,
                        "No protocol transport could be started");
    ESP_RETURN_ON_FALSE(xTaskCreate(transmit_task, "transport_tx",
                                    TRANSPORT_TASK_STACK_SIZE, NULL, 5, NULL) == pdPASS,
                        ESP_ERR_NO_MEM, TAG, "Transport transmit task creation failed");
    transport_started = true;
    ESP_LOGI(TAG, "Transport ready (USB primary=%s, Wi-Fi fallback=%s)",
             usb_available ? "yes" : "no", network_available ? "yes" : "no");
    return ESP_OK;
}

bool beacon_transport_is_connected(void)
{
    return transport_started &&
           (usb_session_active() ||
            (network_available && beacon_network_is_connected()));
}

beacon_transport_kind_t beacon_transport_active_kind(void)
{
    if (usb_session_active()) {
        return BEACON_TRANSPORT_USB;
    }
    if (network_available && beacon_network_is_connected()) {
        return BEACON_TRANSPORT_WIFI;
    }
    return BEACON_TRANSPORT_NONE;
}

bool beacon_transport_receive(beacon_transport_message_t *message, TickType_t timeout)
{
    return message != NULL && receive_queue != NULL &&
           xQueueReceive(receive_queue, message, timeout) == pdTRUE;
}

void beacon_transport_message_release(beacon_transport_message_t *message)
{
    if (message != NULL) {
        free(message->data);
        message->data = NULL;
        message->length = 0U;
    }
}

bool beacon_transport_send(const char *data, size_t length, TickType_t timeout)
{
    if (data == NULL || length == 0U || length > BEACON_USB_PAYLOAD_MAX ||
        transmit_queue == NULL) {
        return false;
    }
    beacon_transport_message_t message = {
        .length = length,
        .data = malloc(length + 1U),
    };
    if (message.data == NULL) {
        return false;
    }
    memcpy(message.data, data, length);
    message.data[length] = '\0';
    const bool queued = xQueueSend(transmit_queue, &message, timeout) == pdTRUE;
    if (!queued) {
        beacon_transport_message_release(&message);
    }
    return queued;
}
