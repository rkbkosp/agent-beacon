#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "beacon_protocol.h"

static const char *VALID_NOTIFICATION =
    "{\"v\":2,\"id\":\"evt-1\",\"type\":\"notification\","
    "\"ts\":\"2026-07-14T12:00:00+08:00\",\"revision\":7,\"payload\":{"
    "\"category\":\"agent\",\"kind\":\"agent.done\",\"source\":\"herdr\","
    "\"subject_id\":\"w1:p1\",\"theme\":\"green\",\"urgency\":\"normal\","
    "\"priority\":50,\"dedupe_key\":\"agent:1:done\",\"supersede_key\":\"agent:w1:p1\","
    "\"title\":\"Agent completed\",\"detail\":\"All tests passed\",\"source_label\":\"Herdr\","
    "\"display_ms\":4000,\"expires_at\":\"2026-07-14T12:10:00+08:00\","
    "\"sticky_badge\":false,\"replay_after_interrupt\":false,\"max_replays\":0}}";

static const char *VALID_SNAPSHOT =
    "{\"v\":2,\"id\":\"snapshot-1\",\"type\":\"snapshot\","
    "\"ts\":\"2026-07-14T14:31:00+08:00\",\"revision\":301,\"payload\":{"
    "\"clock\":{\"timezone\":\"Asia/Shanghai\",\"server_time\":\"2026-07-14T14:31:00+08:00\"},"
    "\"codex\":{\"homes\":["
      "{\"id\":\"main\",\"label\":\"MAIN\",\"weekly_remaining_percent\":18,"
       "\"weekly_reset_at\":\"2026-07-15T11:39:00+08:00\",\"reset_cards_available\":2,"
       "\"nearest_reset_card_expires_at\":\"2026-07-20T23:59:59+08:00\","
       "\"updated_at\":\"2026-07-14T14:30:00+08:00\",\"freshness\":\"fresh\"},"
      "{\"id\":\"vs\",\"label\":\"VS\",\"weekly_remaining_percent\":64,"
       "\"weekly_reset_at\":\"2026-07-18T09:00:00+08:00\",\"reset_cards_available\":1,"
       "\"nearest_reset_card_expires_at\":\"2026-07-18T23:59:59+08:00\","
       "\"updated_at\":\"2026-07-14T14:30:00+08:00\",\"freshness\":\"fresh\"}],"
      "\"relay\":{\"remaining\":14.16,\"unit\":\"USD\",\"is_valid\":true,"
       "\"updated_at\":\"2026-07-14T14:30:00+08:00\",\"freshness\":\"fresh\"}},"
    "\"agents\":{\"provider\":\"herdr\",\"connected\":true,"
      "\"updated_at\":\"2026-07-14T14:31:00+08:00\",\"items\":["
      "{\"pane_id\":\"p1\",\"display_name\":\"A\",\"status\":\"working\",\"focused\":false,\"revision\":40},"
      "{\"pane_id\":\"p2\",\"display_name\":\"B\",\"status\":\"idle\",\"focused\":false,\"revision\":50},"
      "{\"pane_id\":\"p3\",\"display_name\":\"C\",\"status\":\"unknown\",\"focused\":false,\"revision\":99},"
      "{\"pane_id\":\"p4\",\"display_name\":\"D\",\"status\":\"done\",\"focused\":true,\"revision\":20},"
      "{\"pane_id\":\"p5\",\"display_name\":\"E\",\"status\":\"blocked\",\"custom_status\":\"approval\",\"focused\":false,\"revision\":10}]},"
    "\"weather\":{\"location\":\"Hangzhou\",\"provider\":\"qweather\","
      "\"current\":{\"observed_at\":\"2026-07-14T14:30:00+08:00\",\"temp_c\":31,\"icon\":\"101\",\"text\":\"Cloudy\",\"precip_mm\":0,\"freshness\":\"fresh\"},"
      "\"lunch\":{\"target_at\":\"2026-07-14T12:00:00+08:00\",\"is_past\":true,\"temp_c\":29,\"icon\":\"305\",\"text\":\"Showers\",\"pop\":60,\"precip_mm\":0.5,\"freshness\":\"cached\"},"
      "\"leave\":{\"target_at\":\"2026-07-14T19:00:00+08:00\",\"is_past\":false,\"temp_c\":27,\"icon\":\"305\",\"text\":\"Rain\",\"pop\":70,\"precip_mm\":0.7,\"freshness\":\"fresh\"},"
      "\"next_outing\":{\"slot\":\"leave\",\"target_at\":\"2026-07-14T19:00:00+08:00\",\"umbrella_required\":true,\"confidence\":\"high\",\"reason\":\"rain 70%\"},"
      "\"updated_at\":\"2026-07-14T14:31:00+08:00\"},"
    "\"system\":{\"bridge_online\":true,\"overall_freshness\":\"fresh\"}}}";

static void test_valid_notification(void)
{
    beacon_protocol_message_t message;
    assert(beacon_protocol_decode(VALID_NOTIFICATION, strlen(VALID_NOTIFICATION), &message));
    assert(message.type == BEACON_PROTOCOL_MESSAGE_NOTIFICATION);
    assert(message.revision == 7);
    assert(message.notification.category == BEACON_CATEGORY_AGENT);
    assert(message.notification.urgency == BEACON_URGENCY_NORMAL);
    assert(message.notification.theme == BEACON_THEME_GREEN);
    assert(message.notification.priority == 50);
    assert(strcmp(message.notification.source_label, "Herdr") == 0);
    assert(message.notification.expires_at_ms == 1784002200000LL);
}

static void test_valid_snapshot_and_patch(void)
{
    beacon_protocol_message_t message;
    assert(beacon_protocol_decode(VALID_SNAPSHOT, strlen(VALID_SNAPSHOT), &message));
    assert(message.type == BEACON_PROTOCOL_MESSAGE_SNAPSHOT);
    assert(message.state_domains == BEACON_STATE_DOMAIN_ALL);
    assert(message.state.codex.home_count == 2);
    assert(message.state.codex.homes[0].weekly_remaining_percent == 18);
    assert(strcmp(message.state.codex.homes[0].weekly_reset, "07/15 11:39") == 0);
    assert(strcmp(message.state.codex.relay.display, "$14.16") == 0);
    assert(message.state.agents.item_count == 4 && message.state.agents.hidden_count == 1);
    assert(message.state.agents.items[0].status == BEACON_AGENT_BLOCKED);
    assert(message.state.agents.items[1].status == BEACON_AGENT_DONE);
    assert(message.state.agents.items[2].status == BEACON_AGENT_WORKING);
    assert(message.state.agents.items[3].status == BEACON_AGENT_IDLE);
    assert(message.state.weather.next_outing.umbrella_required);
    assert(strcmp(message.state.weather.provider, "和风天气") == 0);
    assert(strcmp(message.state.weather.lunch.label, "午饭") == 0);
    assert(strcmp(message.state.weather.leave.label, "下班") == 0);
    assert(strcmp(message.state.system.display_time, "14:31") == 0);

    const char *patch =
        "{\"v\":2,\"id\":\"patch-1\",\"type\":\"state_patch\","
        "\"ts\":\"2026-07-14T14:32:00+08:00\",\"revision\":302,\"payload\":{"
        "\"system\":{\"bridge_online\":false,\"overall_freshness\":\"stale\"}}}";
    assert(beacon_protocol_decode(patch, strlen(patch), &message));
    assert(message.type == BEACON_PROTOCOL_MESSAGE_STATE_PATCH);
    assert(message.state_domains == BEACON_STATE_DOMAIN_SYSTEM);
    assert(!message.state.system.bridge_online);

    const char *invalid_relay_patch =
        "{\"v\":2,\"id\":\"patch-2\",\"type\":\"state_patch\","
        "\"ts\":\"2026-07-14T14:33:00+08:00\",\"revision\":303,\"payload\":{"
        "\"codex\":{\"homes\":[{\"id\":\"main\",\"label\":\"MAIN\","
        "\"weekly_remaining_percent\":18,\"weekly_reset_at\":null,"
        "\"reset_cards_available\":0,\"nearest_reset_card_expires_at\":null,"
        "\"updated_at\":\"2026-07-14T14:30:00+08:00\",\"freshness\":\"fresh\"}],"
        "\"relay\":{\"remaining\":null,\"unit\":\"USD\",\"is_valid\":false,"
        "\"updated_at\":\"2026-07-14T14:30:00+08:00\",\"freshness\":\"fresh\"}}}}";
    assert(beacon_protocol_decode(invalid_relay_patch, strlen(invalid_relay_patch), &message));
    assert(strcmp(message.state.codex.relay.display, "凭证无效") == 0);
}

static void test_invalid_messages(void)
{
    beacon_protocol_message_t message;
    const char *v1 = "{\"v\":1,\"id\":\"evt\",\"type\":\"hello\",\"ts\":\"2026-07-14T12:00:00Z\",\"revision\":0,\"payload\":{}}";
    assert(!beacon_protocol_decode(v1, strlen(v1), &message));

    char forbidden_category[2048];
    snprintf(forbidden_category, sizeof(forbidden_category), "%s", VALID_NOTIFICATION);
    char *agent = strstr(forbidden_category, "agent");
    memcpy(agent, "email", 5);
    assert(!beacon_protocol_decode(forbidden_category, strlen(forbidden_category), &message));

    const char *legacy_top_level =
        "{\"v\":2,\"id\":\"snapshot\",\"type\":\"snapshot\",\"ts\":\"2026-07-14T12:00:00Z\",\"revision\":1,"
        "\"payload\":{\"clock\":{},\"codex\":{},\"agents\":{},\"weather\":{},\"system\":{},\"tasks\":{}}}";
    assert(!beacon_protocol_decode(legacy_top_level, strlen(legacy_top_level), &message));

    char invalid_utf8[] = {'{', '"', 'x', '"', ':', '"', (char)0xff, '"', '}', '\0'};
    assert(!beacon_protocol_decode(invalid_utf8, strlen(invalid_utf8), &message));

    char *oversized = calloc(BEACON_PROTOCOL_MAX_MESSAGE_BYTES + 2U, 1);
    assert(oversized != NULL);
    memset(oversized, ' ', BEACON_PROTOCOL_MAX_MESSAGE_BYTES + 1U);
    assert(!beacon_protocol_decode(oversized, BEACON_PROTOCOL_MAX_MESSAGE_BYTES + 1U, &message));
    free(oversized);
}

int main(void)
{
    test_valid_notification();
    test_valid_snapshot_and_patch();
    test_invalid_messages();
    return 0;
}
