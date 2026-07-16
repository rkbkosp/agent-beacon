#include <stdint.h>
#include <time.h>

#include "beacon_app_state.h"
#include "beacon_button.h"
#include "beacon_diagnostics.h"
#include "beacon_notifications.h"
#include "beacon_protocol.h"
#include "beacon_transport.h"
#include "beacon_ui.h"
#include "beacon_ui_state.h"
#include "board_ws_147b.h"
#include "esp_check.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#if __has_include("config.local.h")
#include "config.local.h"
#define BEACON_HAS_NETWORK_CONFIG 1
#else
#define BEACON_HAS_NETWORK_CONFIG 0
#endif

static const char *TAG = "agent_beacon";
static beacon_notification_center_t notification_center;
static beacon_app_state_t app_state;
static beacon_protocol_message_t protocol_message;
static beacon_ui_state_t ui_state;
static beacon_button_t boot_button;
static beacon_diagnostics_snapshot_t diagnostics_snapshot;
static beacon_notification_transition_t expired_transition;
static beacon_notification_transition_t replay_transition;
static beacon_notification_transition_t completed_transition;
static beacon_notification_transition_t offer_transition;
static bool last_transport_connected;
static beacon_transport_kind_t last_transport_kind = BEACON_TRANSPORT_NONE;
static bool connection_snapshot_ready;

static int64_t wall_clock_ms(void)
{
    return (int64_t)time(NULL) * 1000LL;
}

static bool transition_has_status(const beacon_notification_transition_t *transition,
                                  beacon_ack_status_t status)
{
    for (size_t index = 0; transition != NULL && index < transition->ack_count; ++index) {
        if (transition->acks[index].status == status) {
            return true;
        }
    }
    return false;
}

static void send_transition_acks(const beacon_notification_transition_t *transition)
{
    for (size_t index = 0; transition != NULL && index < transition->ack_count; ++index) {
        (void)beacon_protocol_send_ack(&transition->acks[index]);
    }
}

static void present_current_notification(const beacon_notification_center_t *center,
                                         beacon_ui_state_t *ui_state)
{
    const beacon_notification_t *current = beacon_notification_center_current(center);
    if (current == NULL) {
        return;
    }
    beacon_ui_state_show_notification(ui_state, current->theme, current->display_ms);
    beacon_ui_show_notification(current->theme, current->title, current->detail,
                                current->source_label);
    ESP_LOGI(TAG, "notification id=%s urgency=%d priority=%u display_ms=%lu",
             current->id, current->urgency, current->priority,
             (unsigned long)current->display_ms);
}

static void offer_notification(beacon_ui_state_t *ui_state,
                               const beacon_notification_t *notification)
{
    beacon_notification_center_offer(&notification_center, notification, wall_clock_ms(),
                                     &offer_transition);
    send_transition_acks(&offer_transition);
    if (offer_transition.disposition == BEACON_NOTIFICATION_STARTED ||
        offer_transition.disposition == BEACON_NOTIFICATION_INTERRUPTED_CURRENT) {
        present_current_notification(&notification_center, ui_state);
    }
}

static void show_current_surface(const beacon_ui_state_t *ui_state)
{
    if (ui_state->mode == BEACON_UI_DIAGNOSTICS) {
        beacon_ui_show_diagnostics();
    } else if (ui_state->mode == BEACON_UI_CAROUSEL) {
        beacon_ui_show_page(ui_state->page);
    }
}

static void refresh_current_surface(const beacon_ui_state_t *ui_state)
{
    if (ui_state->mode == BEACON_UI_DIAGNOSTICS) {
        beacon_ui_show_diagnostics();
    } else if (ui_state->mode == BEACON_UI_CAROUSEL) {
        beacon_ui_refresh_page(ui_state->page);
    }
}

static void refresh_on_transport_change(const beacon_ui_state_t *ui_state)
{
    const bool transport_connected = beacon_transport_is_connected();
    const beacon_transport_kind_t transport_kind = beacon_transport_active_kind();
    if (transport_connected == last_transport_connected &&
        transport_kind == last_transport_kind) {
        return;
    }
    last_transport_connected = transport_connected;
    last_transport_kind = transport_kind;
    connection_snapshot_ready = false;
    beacon_ui_set_connection_snapshot_ready(connection_snapshot_ready);
    refresh_current_surface(ui_state);
    const char *transport_name = "disconnected";
    if (transport_kind == BEACON_TRANSPORT_USB) {
        transport_name = "USB";
    } else if (transport_kind == BEACON_TRANSPORT_WIFI) {
        transport_name = "Wi-Fi";
    }
    ESP_LOGI(TAG, "Protocol transport changed to %s; awaiting snapshot",
             transport_name);
}

static void apply_protocol_state(const beacon_protocol_message_t *message,
                                 beacon_ui_state_t *ui_state)
{
    const bool system_status_changed =
        (message->state_domains & BEACON_STATE_DOMAIN_SYSTEM) != 0U &&
        beacon_ui_system_status_changed(&app_state.system, &message->state.system);
    beacon_app_state_apply(&app_state, &message->state, message->state_domains,
                           message->revision);
    const bool page_changed =
        (message->state_domains & BEACON_STATE_DOMAIN_AGENTS) != 0U &&
        beacon_ui_state_set_codex_active(ui_state, app_state.agents.codex_active);
    connection_snapshot_ready = beacon_ui_connection_snapshot_ready(
        connection_snapshot_ready, beacon_transport_is_connected(),
        message->type == BEACON_PROTOCOL_MESSAGE_SNAPSHOT);
    beacon_ui_set_app_state(&app_state);
    beacon_ui_set_connection_snapshot_ready(connection_snapshot_ready);
    if (page_changed) {
        beacon_ui_show_page(ui_state->page);
    } else if (ui_state->mode == BEACON_UI_DIAGNOSTICS) {
        beacon_ui_show_diagnostics();
    } else if (ui_state->mode == BEACON_UI_CAROUSEL &&
               beacon_ui_page_affected_by_domains(ui_state->page,
                                                  message->state_domains,
                                                  system_status_changed)) {
        beacon_ui_refresh_page(ui_state->page);
    }
    ESP_LOGI(TAG, "state type=%d domains=0x%02x revision=%llu", message->type,
             message->state_domains, (unsigned long long)message->revision);
}

static void start_transport_if_configured(void)
{
#if BEACON_HAS_NETWORK_CONFIG
    const beacon_transport_config_t transport_config = {
        .network = {
            .wifi_ssid = BEACON_WIFI_SSID,
            .wifi_password = BEACON_WIFI_PASSWORD,
            .websocket_uri = BEACON_WEBSOCKET_URI,
            .device_id = BEACON_DEVICE_ID,
            .token = BEACON_BRIDGE_TOKEN,
        },
        .usb_enabled = true,
    };
    const esp_err_t transport_error = beacon_transport_start(&transport_config);
    if (transport_error != ESP_OK) {
        ESP_LOGE(TAG, "Protocol transport disabled: %s",
                 esp_err_to_name(transport_error));
        return;
    }
    const beacon_protocol_config_t protocol_config = {
        .device_id = BEACON_DEVICE_ID,
        .token = BEACON_BRIDGE_TOKEN,
    };
    const esp_err_t protocol_error = beacon_protocol_start(&protocol_config);
    if (protocol_error != ESP_OK) {
        ESP_LOGE(TAG, "Protocol disabled: %s", esp_err_to_name(protocol_error));
    }
#else
    ESP_LOGW(TAG, "Network config absent; run scripts/configure-network.sh");
#endif
}

void app_main(void)
{
    ESP_ERROR_CHECK(board_init());
    ESP_ERROR_CHECK(beacon_ui_init());
    beacon_diagnostics_init();

    beacon_ui_state_init(&ui_state);
    beacon_button_init(&boot_button, 30, 350, 2000, 5000);
    beacon_notification_center_init(&notification_center);
    beacon_app_state_init_mock(&app_state);
    beacon_ui_set_app_state(&app_state);
    beacon_ui_show_page(ui_state.page);
    start_transport_if_configured();

    int64_t previous_time_us = esp_timer_get_time();
    int64_t previous_diagnostics_sample_us = previous_time_us;
    ESP_LOGI(TAG, "M2 v2 UI ready: Codex / Agents / Weather");

    while (true) {
        beacon_ui_process();
        vTaskDelay(pdMS_TO_TICKS(10));
        refresh_on_transport_change(&ui_state);

        const int64_t current_time_us = esp_timer_get_time();
        uint64_t elapsed_ms_64 = (uint64_t)(current_time_us - previous_time_us) / 1000U;
        previous_time_us = current_time_us;
        if (elapsed_ms_64 == 0U) {
            elapsed_ms_64 = 1U;
        }
        const uint32_t elapsed_ms = elapsed_ms_64 > UINT32_MAX ? UINT32_MAX : (uint32_t)elapsed_ms_64;

        if (current_time_us - previous_diagnostics_sample_us >= 1000000LL) {
            beacon_diagnostics_sample(&diagnostics_snapshot);
            beacon_ui_set_diagnostics(&diagnostics_snapshot);
            previous_diagnostics_sample_us = current_time_us;
            if (ui_state.mode == BEACON_UI_DIAGNOSTICS) {
                beacon_ui_show_diagnostics();
            }
        }

        while (beacon_protocol_receive(&protocol_message, 0)) {
            if (protocol_message.type == BEACON_PROTOCOL_MESSAGE_SNAPSHOT ||
                protocol_message.type == BEACON_PROTOCOL_MESSAGE_STATE_PATCH) {
                apply_protocol_state(&protocol_message, &ui_state);
            } else if (protocol_message.type == BEACON_PROTOCOL_MESSAGE_NOTIFICATION) {
                offer_notification(&ui_state, &protocol_message.notification);
            }
        }

        while (beacon_notification_center_remove_expired(&notification_center,
                                                         wall_clock_ms(),
                                                         &expired_transition)) {
            send_transition_acks(&expired_transition);
            if (transition_has_status(&expired_transition, BEACON_ACK_SHOWN)) {
                present_current_notification(&notification_center, &ui_state);
            } else if (beacon_notification_center_current(&notification_center) == NULL &&
                       ui_state.mode == BEACON_UI_NOTIFICATION) {
                (void)beacon_ui_state_tick(&ui_state, ui_state.notification_remaining_ms);
                show_current_surface(&ui_state);
            }
        }

        const beacon_button_event_t button_event =
            beacon_button_update(&boot_button, board_boot_button_pressed(), elapsed_ms);
        if (button_event == BEACON_BUTTON_SHORT_PRESS && ui_state.mode == BEACON_UI_CAROUSEL) {
            if (ui_state.carousel_paused) {
                beacon_ui_state_resume_carousel(&ui_state);
            } else {
                beacon_ui_state_next_page(&ui_state);
            }
            beacon_ui_show_page(ui_state.page);
        } else if (button_event == BEACON_BUTTON_DOUBLE_PRESS &&
                   ui_state.mode == BEACON_UI_CAROUSEL) {
            if (beacon_notification_center_replay_latest(&notification_center,
                                                         wall_clock_ms(),
                                                         &replay_transition)) {
                send_transition_acks(&replay_transition);
                present_current_notification(&notification_center, &ui_state);
            }
        } else if (button_event == BEACON_BUTTON_TRIPLE_PRESS &&
                   ui_state.mode == BEACON_UI_CAROUSEL) {
            beacon_ui_state_pin_token_rate(&ui_state);
            beacon_ui_show_page(ui_state.page);
        } else if (button_event == BEACON_BUTTON_LONG_2S) {
            if (ui_state.mode == BEACON_UI_DIAGNOSTICS) {
                beacon_ui_state_exit_diagnostics(&ui_state);
                beacon_ui_show_page(ui_state.page);
            } else if (ui_state.mode == BEACON_UI_CAROUSEL) {
                beacon_ui_state_enter_diagnostics(&ui_state);
                beacon_ui_show_diagnostics();
            }
        } else if (button_event == BEACON_BUTTON_LONG_5S) {
            ESP_LOGW(TAG, "Provisioning gesture received; SoftAP is scheduled after M2");
        }

        const bool was_notification = ui_state.mode == BEACON_UI_NOTIFICATION;
        const bool ui_changed = beacon_ui_state_tick(&ui_state, elapsed_ms);
        if (was_notification && ui_state.mode != BEACON_UI_NOTIFICATION) {
            const beacon_notification_t *next = beacon_notification_center_complete_current(
                &notification_center, wall_clock_ms(), &completed_transition);
            send_transition_acks(&completed_transition);
            if (next != NULL) {
                present_current_notification(&notification_center, &ui_state);
            } else {
                show_current_surface(&ui_state);
            }
        } else if (ui_changed && ui_state.mode == BEACON_UI_CAROUSEL) {
            beacon_ui_show_page(ui_state.page);
        }
    }
}
