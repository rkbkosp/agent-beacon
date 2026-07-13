#include <assert.h>
#include <stdio.h>
#include <string.h>

#include "beacon_notifications.h"

static beacon_notification_t notification(const char *id, beacon_urgency_t urgency,
                                           uint8_t priority, int64_t expires_at_ms)
{
    beacon_notification_t value = {
        .category = BEACON_CATEGORY_AGENT,
        .urgency = urgency,
        .priority = priority,
        .theme = BEACON_THEME_GREEN,
        .display_ms = 4000,
        .expires_at_ms = expires_at_ms,
    };
    snprintf(value.id, sizeof(value.id), "%s", id);
    snprintf(value.kind, sizeof(value.kind), "agent.done");
    snprintf(value.source, sizeof(value.source), "mock");
    snprintf(value.subject_id, sizeof(value.subject_id), "pane-%s", id);
    snprintf(value.dedupe_key, sizeof(value.dedupe_key), "dedupe-%s", id);
    snprintf(value.title, sizeof(value.title), "Title %s", id);
    return value;
}

static bool has_ack(const beacon_notification_transition_t *transition,
                    const char *id, beacon_ack_status_t status)
{
    for (size_t i = 0; i < transition->ack_count; ++i) {
        if (transition->acks[i].status == status &&
            strcmp(transition->acks[i].notification_id, id) == 0) {
            return true;
        }
    }
    return false;
}

static beacon_notification_disposition_t offer(
    beacon_notification_center_t *center, const beacon_notification_t *item,
    int64_t now_ms, beacon_notification_transition_t *transition)
{
    return beacon_notification_center_offer(center, item, now_ms, transition);
}

static void test_receive_duplicate_and_expired_ack_sequence(void)
{
    beacon_notification_center_t center;
    beacon_notification_center_init(&center);
    beacon_notification_t first = notification("evt-1", BEACON_URGENCY_NORMAL, 50, 10000);

    beacon_notification_transition_t transition;
    offer(&center, &first, 1000, &transition);
    assert(transition.disposition == BEACON_NOTIFICATION_STARTED);
    assert(has_ack(&transition, "evt-1", BEACON_ACK_RECEIVED));
    assert(has_ack(&transition, "evt-1", BEACON_ACK_SHOWN));

    offer(&center, &first, 1001, &transition);
    assert(transition.disposition == BEACON_NOTIFICATION_DUPLICATE);
    assert(transition.ack_count == 1);
    assert(has_ack(&transition, "evt-1", BEACON_ACK_DUPLICATE));

    beacon_notification_t expired = notification("evt-expired", BEACON_URGENCY_NORMAL, 10, 999);
    offer(&center, &expired, 1000, &transition);
    assert(transition.disposition == BEACON_NOTIFICATION_EXPIRED);
    assert(has_ack(&transition, "evt-expired", BEACON_ACK_EXPIRED));
}

static void test_interrupt_guard_and_protocol_error_override(void)
{
    beacon_notification_center_t center;
    beacon_notification_center_init(&center);
    beacon_notification_t current = notification("current", BEACON_URGENCY_NORMAL, 40, 100000);
    beacon_notification_t higher = notification("higher", BEACON_URGENCY_ATTENTION, 75, 100000);
    beacon_notification_transition_t transition;
    assert(offer(&center, &current, 0, &transition) == BEACON_NOTIFICATION_STARTED);

    offer(&center, &higher, 999, &transition);
    assert(transition.disposition == BEACON_NOTIFICATION_QUEUED);
    assert(strcmp(beacon_notification_center_current(&center)->id, "current") == 0);

    beacon_notification_center_t override_center;
    beacon_notification_center_init(&override_center);
    assert(offer(&override_center, &current, 0, &transition) == BEACON_NOTIFICATION_STARTED);
    beacon_notification_t protocol_error = notification("protocol", BEACON_URGENCY_URGENT, 96, 100000);
    protocol_error.category = BEACON_CATEGORY_SYSTEM;
    snprintf(protocol_error.kind, sizeof(protocol_error.kind), "system.protocol_error");
    offer(&override_center, &protocol_error, 1, &transition);
    assert(transition.disposition == BEACON_NOTIFICATION_INTERRUPTED_CURRENT);
    assert(has_ack(&transition, "current", BEACON_ACK_INTERRUPTED));
    assert(has_ack(&transition, "protocol", BEACON_ACK_SHOWN));
}

static void test_ordering_and_same_urgency_priority_delta(void)
{
    beacon_notification_center_t center;
    beacon_notification_center_init(&center);
    beacon_notification_t current = notification("current", BEACON_URGENCY_ATTENTION, 50, 100000);
    beacon_notification_transition_t offer_result;
    assert(offer(&center, &current, 0, &offer_result) == BEACON_NOTIFICATION_STARTED);

    beacon_notification_t delta9 = notification("delta9", BEACON_URGENCY_ATTENTION, 59, 100000);
    assert(offer(&center, &delta9, 1000, &offer_result) == BEACON_NOTIFICATION_QUEUED);
    beacon_notification_t delta10 = notification("delta10", BEACON_URGENCY_ATTENTION, 60, 100000);
    assert(offer(&center, &delta10, 1001, &offer_result) == BEACON_NOTIFICATION_INTERRUPTED_CURRENT);

    beacon_notification_t normal = notification("normal", BEACON_URGENCY_NORMAL, 100, 100000);
    beacon_notification_t urgent_low = notification("urgent", BEACON_URGENCY_URGENT, 1, 100000);
    assert(offer(&center, &normal, 1002, &offer_result) == BEACON_NOTIFICATION_QUEUED);
    assert(offer(&center, &urgent_low, 2001, &offer_result) == BEACON_NOTIFICATION_INTERRUPTED_CURRENT);

    beacon_notification_transition_t transition;
    const beacon_notification_t *next =
        beacon_notification_center_complete_current(&center, 3000, &transition);
    assert(has_ack(&transition, "urgent", BEACON_ACK_COMPLETED));
    assert(next != NULL && strcmp(next->id, "delta9") == 0);
    assert(has_ack(&transition, "delta9", BEACON_ACK_SHOWN));
}

static void test_supersede_and_bounded_replay(void)
{
    beacon_notification_center_t center;
    beacon_notification_center_init(&center);
    beacon_notification_t active = notification("active", BEACON_URGENCY_URGENT, 90, 100000);
    beacon_notification_transition_t transition;
    assert(offer(&center, &active, 0, &transition) == BEACON_NOTIFICATION_STARTED);

    beacon_notification_t blocked = notification("blocked", BEACON_URGENCY_ATTENTION, 75, 100000);
    blocked.replay_after_interrupt = true;
    blocked.max_replays = 1;
    snprintf(blocked.supersede_key, sizeof(blocked.supersede_key), "agent:pane-1");
    assert(offer(&center, &blocked, 1, &transition) == BEACON_NOTIFICATION_QUEUED);

    beacon_notification_t done = notification("done", BEACON_URGENCY_NORMAL, 50, 100000);
    snprintf(done.supersede_key, sizeof(done.supersede_key), "agent:pane-1");
    offer(&center, &done, 2, &transition);
    assert(transition.disposition == BEACON_NOTIFICATION_QUEUED);
    assert(has_ack(&transition, "blocked", BEACON_ACK_SUPERSEDED));
    assert(beacon_notification_center_pending_count(&center) == 1);

    beacon_notification_center_t replay_center;
    beacon_notification_center_init(&replay_center);
    assert(offer(&replay_center, &blocked, 0, &transition) == BEACON_NOTIFICATION_STARTED);
    beacon_notification_t critical = notification("critical", BEACON_URGENCY_URGENT, 90, 100000);
    assert(offer(&replay_center, &critical, 1000, &transition) == BEACON_NOTIFICATION_INTERRUPTED_CURRENT);
    assert(beacon_notification_center_pending_count(&replay_center) == 1);
    const beacon_notification_t *next = beacon_notification_center_complete_current(&replay_center, 9000, &transition);
    assert(next != NULL && strcmp(next->id, "blocked") == 0);
    beacon_notification_t second_critical = notification("critical-2", BEACON_URGENCY_URGENT, 100, 100000);
    assert(offer(&replay_center, &second_critical, 10000, &transition) == BEACON_NOTIFICATION_INTERRUPTED_CURRENT);
    assert(beacon_notification_center_pending_count(&replay_center) == 0);
}

static void test_regular_capacity_and_urgent_reserve(void)
{
    beacon_notification_center_t center;
    beacon_notification_center_init(&center);
    beacon_notification_t active = notification("active", BEACON_URGENCY_URGENT, 100, 100000);
    beacon_notification_transition_t transition;
    assert(offer(&center, &active, 0, &transition) == BEACON_NOTIFICATION_STARTED);

    char id[32];
    for (size_t i = 0; i < BEACON_NOTIFICATION_REGULAR_CAPACITY; ++i) {
        snprintf(id, sizeof(id), "normal-%02zu", i);
        beacon_notification_t item = notification(id, BEACON_URGENCY_NORMAL, (uint8_t)(20 + i), 100000);
        assert(offer(&center, &item, (int64_t)i + 1, &transition) == BEACON_NOTIFICATION_QUEUED);
    }
    assert(beacon_notification_center_pending_count(&center) == 12);

    beacon_notification_t reserve1 = notification("reserve-1", BEACON_URGENCY_URGENT, 80, 100000);
    beacon_notification_t reserve2 = notification("reserve-2", BEACON_URGENCY_URGENT, 81, 100000);
    assert(offer(&center, &reserve1, 20, &transition) == BEACON_NOTIFICATION_QUEUED);
    assert(offer(&center, &reserve2, 21, &transition) == BEACON_NOTIFICATION_QUEUED);
    assert(beacon_notification_center_pending_count(&center) == BEACON_NOTIFICATION_TOTAL_CAPACITY);

    beacon_notification_t low = notification("too-low", BEACON_URGENCY_NORMAL, 1, 100000);
    offer(&center, &low, 22, &transition);
    assert(transition.disposition == BEACON_NOTIFICATION_DROPPED);
    assert(has_ack(&transition, "too-low", BEACON_ACK_DROPPED));
}

static void test_latest_unexpired_can_be_replayed(void)
{
    beacon_notification_center_t center;
    beacon_notification_center_init(&center);
    beacon_notification_t item = notification("latest", BEACON_URGENCY_NORMAL, 50, 10000);
    beacon_notification_transition_t transition;
    assert(offer(&center, &item, 0, &transition) == BEACON_NOTIFICATION_STARTED);
    assert(beacon_notification_center_complete_current(&center, 4000, &transition) == NULL);
    assert(beacon_notification_center_replay_latest(&center, 5000, &transition));
    assert(strcmp(beacon_notification_center_current(&center)->id, "latest") == 0);
    assert(!beacon_notification_center_replay_latest(&center, 10000, &transition));
}

int main(void)
{
    test_receive_duplicate_and_expired_ack_sequence();
    test_interrupt_guard_and_protocol_error_override();
    test_ordering_and_same_urgency_priority_delta();
    test_supersede_and_bounded_replay();
    test_regular_capacity_and_urgent_reserve();
    test_latest_unexpired_can_be_replayed();
    return 0;
}
