#include "beacon_app_state.h"

#include <string.h>

void beacon_app_state_init_mock(beacon_app_state_t *state)
{
    if (state == NULL) {
        return;
    }
    memset(state, 0, sizeof(*state));
    state->codex.home_count = 2;
    state->codex.homes[0] = (beacon_codex_home_t) {
        .id = "main", .label = "MAIN", .weekly_remaining_percent = 18,
        .weekly_reset = "周三 11:39", .reset_cards_available = 2,
        .nearest_card_expiry = "07/20", .freshness = BEACON_FRESHNESS_FRESH,
    };
    state->codex.homes[1] = (beacon_codex_home_t) {
        .id = "vs", .label = "VS", .weekly_remaining_percent = 64,
        .weekly_reset = "周六 09:00", .reset_cards_available = 1,
        .nearest_card_expiry = "07/18", .freshness = BEACON_FRESHNESS_FRESH,
    };
    state->codex.relay = (beacon_relay_state_t) {
        .display = "$14.16", .is_valid = true, .freshness = BEACON_FRESHNESS_FRESH,
    };

    state->agents.connected = true;
    state->agents.item_count = 4;
    state->agents.items[0] = (beacon_agent_item_t) {
        .pane_id = "w1:p1", .display_name = "Chrome Plugin",
        .secondary = "等待批准", .status = BEACON_AGENT_BLOCKED,
    };
    state->agents.items[1] = (beacon_agent_item_t) {
        .pane_id = "w1:p2", .display_name = "CaseForge",
        .secondary = "执行中", .status = BEACON_AGENT_WORKING,
    };
    state->agents.items[2] = (beacon_agent_item_t) {
        .pane_id = "w1:p3", .display_name = "Docs Agent",
        .secondary = "等待查看", .status = BEACON_AGENT_DONE,
    };
    state->agents.items[3] = (beacon_agent_item_t) {
        .pane_id = "w1:p4", .display_name = "Review Bot",
        .secondary = "空闲", .status = BEACON_AGENT_IDLE,
    };

    strcpy(state->weather.location, "杭州");
    strcpy(state->weather.provider, "和风天气");
    state->weather.current = (beacon_weather_current_t) {
        .observed_time = "14:30", .temp_c = 31, .text = "多云", .icon = "101",
        .freshness = BEACON_FRESHNESS_FRESH,
    };
    state->weather.lunch = (beacon_weather_slot_t) {
        .label = "午饭", .time = "12:00", .temp_c = 29,
        .text = "阵雨", .is_past = true, .freshness = BEACON_FRESHNESS_CACHED,
    };
    state->weather.leave = (beacon_weather_slot_t) {
        .label = "下班", .time = "19:00", .temp_c = 27,
        .text = "小雨", .freshness = BEACON_FRESHNESS_FRESH,
    };
    state->weather.next_outing = (beacon_next_outing_t) {
        .slot = "leave", .time = "19:00", .umbrella_known = true,
        .umbrella_required = true, .reason = "小雨，降水概率 70%",
    };

    strcpy(state->system.timezone, "Asia/Shanghai");
    strcpy(state->system.display_time, "14:42");
    state->system.bridge_online = true;
    state->system.overall_freshness = BEACON_FRESHNESS_FRESH;
}

void beacon_app_state_apply(beacon_app_state_t *destination,
                            const beacon_app_state_t *patch,
                            uint8_t domains, uint64_t revision)
{
    if (destination == NULL || patch == NULL) {
        return;
    }
    if ((domains & BEACON_STATE_DOMAIN_CLOCK) != 0U) {
        memcpy(destination->system.timezone, patch->system.timezone,
               sizeof(destination->system.timezone));
        memcpy(destination->system.display_time, patch->system.display_time,
               sizeof(destination->system.display_time));
    }
    if ((domains & BEACON_STATE_DOMAIN_CODEX) != 0U) {
        destination->codex = patch->codex;
    }
    if ((domains & BEACON_STATE_DOMAIN_AGENTS) != 0U) {
        destination->agents = patch->agents;
    }
    if ((domains & BEACON_STATE_DOMAIN_WEATHER) != 0U) {
        destination->weather = patch->weather;
    }
    if ((domains & BEACON_STATE_DOMAIN_SYSTEM) != 0U) {
        destination->system.bridge_online = patch->system.bridge_online;
        destination->system.overall_freshness = patch->system.overall_freshness;
    }
    destination->system.revision = revision;
}

const char *beacon_agent_status_label(beacon_agent_status_t status)
{
    switch (status) {
    case BEACON_AGENT_WORKING:
        return "工作中";
    case BEACON_AGENT_BLOCKED:
        return "需交互";
    case BEACON_AGENT_DONE:
        return "已完成";
    case BEACON_AGENT_IDLE:
        return "空闲";
    case BEACON_AGENT_UNKNOWN:
    default:
        return "未知";
    }
}
