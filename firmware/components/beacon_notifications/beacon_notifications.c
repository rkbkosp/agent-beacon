#include "beacon_notifications.h"

#include <limits.h>
#include <string.h>

#define BEACON_INTERRUPT_GUARD_MS 1000

static void copy_string(char *destination, size_t destination_size, const char *source)
{
    if (destination == NULL || destination_size == 0) {
        return;
    }
    size_t length = 0;
    if (source != NULL) {
        while (length + 1U < destination_size && source[length] != '\0') {
            length++;
        }
        memcpy(destination, source, length);
    }
    destination[length] = '\0';
}

static bool is_expired(const beacon_notification_t *notification, int64_t now_ms)
{
    return notification->expires_at_ms > 0 && notification->expires_at_ms <= now_ms;
}

static bool is_protocol_error(const beacon_notification_t *notification)
{
    return strcmp(notification->kind, "system.protocol_error") == 0;
}

static bool is_protected(const beacon_notification_t *notification)
{
    return notification->urgency == BEACON_URGENCY_URGENT ||
           is_protocol_error(notification) ||
           strcmp(notification->kind, "system.bridge_offline") == 0;
}

static bool ranks_before(const beacon_notification_t *left,
                         const beacon_notification_t *right)
{
    if (left->urgency != right->urgency) {
        return left->urgency > right->urgency;
    }
    if (left->priority != right->priority) {
        return left->priority > right->priority;
    }
    if (left->created_at_ms != right->created_at_ms) {
        return left->created_at_ms < right->created_at_ms;
    }
    return strcmp(left->id, right->id) < 0;
}

static void add_ack(beacon_notification_transition_t *transition, const char *id,
                    beacon_ack_status_t status, const char *reason)
{
    if (transition == NULL || transition->ack_count >= BEACON_NOTIFICATION_MAX_ACKS) {
        return;
    }
    beacon_notification_ack_t *ack = &transition->acks[transition->ack_count++];
    memset(ack, 0, sizeof(*ack));
    copy_string(ack->notification_id, sizeof(ack->notification_id), id);
    copy_string(ack->reason, sizeof(ack->reason), reason);
    ack->status = status;
}

static beacon_notification_recent_t *find_recent(beacon_notification_center_t *center,
                                                  const char *id)
{
    for (size_t i = 0; i < center->recent_count; ++i) {
        if (strcmp(center->recent[i].id, id) == 0) {
            return &center->recent[i];
        }
    }
    return NULL;
}

static void remember(beacon_notification_center_t *center,
                     const beacon_notification_t *notification,
                     beacon_ack_status_t status, int64_t now_ms)
{
    beacon_notification_recent_t *recent = find_recent(center, notification->id);
    if (recent == NULL) {
        size_t index;
        if (center->recent_count < BEACON_NOTIFICATION_RECENT_CAPACITY) {
            index = center->recent_count++;
        } else {
            index = center->recent_cursor;
            center->recent_cursor = (center->recent_cursor + 1U) % BEACON_NOTIFICATION_RECENT_CAPACITY;
        }
        recent = &center->recent[index];
        memset(recent, 0, sizeof(*recent));
        copy_string(recent->id, sizeof(recent->id), notification->id);
        copy_string(recent->dedupe_key, sizeof(recent->dedupe_key), notification->dedupe_key);
    }
    recent->result = status;
    if (status == BEACON_ACK_SHOWN) {
        recent->shown_at_ms = now_ms;
    }
}

static bool is_duplicate(const beacon_notification_center_t *center,
                         const beacon_notification_t *notification)
{
    for (size_t i = 0; i < center->recent_count; ++i) {
        const beacon_notification_recent_t *recent = &center->recent[i];
        if (strcmp(recent->id, notification->id) == 0 ||
            (notification->dedupe_key[0] != '\0' &&
             strcmp(recent->dedupe_key, notification->dedupe_key) == 0)) {
            return true;
        }
    }
    return false;
}

static void remove_pending(beacon_notification_center_t *center, size_t index)
{
    if (index >= center->pending_count) {
        return;
    }
    if (index + 1U < center->pending_count) {
        memmove(&center->pending[index], &center->pending[index + 1U],
                (center->pending_count - index - 1U) * sizeof(center->pending[0]));
    }
    center->pending_count--;
}

static size_t count_regular(const beacon_notification_center_t *center)
{
    size_t count = 0;
    for (size_t i = 0; i < center->pending_count; ++i) {
        if (center->pending[i].urgency != BEACON_URGENCY_URGENT) {
            count++;
        }
    }
    return count;
}

static bool has_capacity(const beacon_notification_center_t *center,
                         const beacon_notification_t *notification)
{
    if (center->pending_count >= BEACON_NOTIFICATION_TOTAL_CAPACITY) {
        return false;
    }
    return notification->urgency == BEACON_URGENCY_URGENT ||
           count_regular(center) < BEACON_NOTIFICATION_REGULAR_CAPACITY;
}

static bool append_pending(beacon_notification_center_t *center,
                           const beacon_notification_t *notification)
{
    if (!has_capacity(center, notification)) {
        return false;
    }
    center->pending[center->pending_count++] = *notification;
    return true;
}

static size_t highest_pending(const beacon_notification_center_t *center)
{
    size_t selected = 0;
    for (size_t i = 1; i < center->pending_count; ++i) {
        if (ranks_before(&center->pending[i], &center->pending[selected])) {
            selected = i;
        }
    }
    return selected;
}

static size_t worst_evictable(const beacon_notification_center_t *center)
{
    size_t selected = SIZE_MAX;
    for (size_t i = 0; i < center->pending_count; ++i) {
        if (is_protected(&center->pending[i])) {
            continue;
        }
        if (selected == SIZE_MAX || ranks_before(&center->pending[selected], &center->pending[i])) {
            selected = i;
        }
    }
    return selected;
}

static void mark_shown(beacon_notification_center_t *center, int64_t now_ms,
                       beacon_notification_transition_t *transition)
{
    center->current_started_at_ms = now_ms;
    center->latest = center->current;
    center->has_latest = true;
    remember(center, &center->current, BEACON_ACK_SHOWN, now_ms);
    add_ack(transition, center->current.id, BEACON_ACK_SHOWN, NULL);
}

static const beacon_notification_t *promote_next(beacon_notification_center_t *center,
                                                  int64_t now_ms,
                                                  beacon_notification_transition_t *transition)
{
    if (center->pending_count == 0) {
        center->has_current = false;
        return NULL;
    }
    const size_t index = highest_pending(center);
    center->current = center->pending[index];
    center->has_current = true;
    remove_pending(center, index);
    mark_shown(center, now_ms, transition);
    return &center->current;
}

static bool should_interrupt(const beacon_notification_center_t *center,
                             const beacon_notification_t *notification,
                             int64_t now_ms)
{
    if (is_protocol_error(notification)) {
        return true;
    }
    if (now_ms - center->current_started_at_ms < BEACON_INTERRUPT_GUARD_MS) {
        return false;
    }
    return notification->urgency > center->current.urgency ||
           (notification->urgency == center->current.urgency &&
            notification->priority >= (uint16_t)center->current.priority + 10U);
}

void beacon_notification_center_init(beacon_notification_center_t *center)
{
    if (center != NULL) {
        memset(center, 0, sizeof(*center));
    }
}

beacon_notification_disposition_t beacon_notification_center_offer(
    beacon_notification_center_t *center, const beacon_notification_t *notification,
    int64_t now_ms, beacon_notification_transition_t *transition)
{
    if (transition == NULL) {
        return BEACON_NOTIFICATION_DROPPED;
    }
    memset(transition, 0, sizeof(*transition));
    if (center == NULL || notification == NULL || notification->id[0] == '\0') {
        transition->disposition = BEACON_NOTIFICATION_DROPPED;
        return transition->disposition;
    }
    if (is_expired(notification, now_ms)) {
        transition->disposition = BEACON_NOTIFICATION_EXPIRED;
        remember(center, notification, BEACON_ACK_EXPIRED, now_ms);
        add_ack(transition, notification->id, BEACON_ACK_EXPIRED, "expired_before_queue");
        return transition->disposition;
    }
    if (is_duplicate(center, notification)) {
        transition->disposition = BEACON_NOTIFICATION_DUPLICATE;
        add_ack(transition, notification->id, BEACON_ACK_DUPLICATE, "duplicate_id_or_key");
        return transition->disposition;
    }

    beacon_notification_t accepted = *notification;
    accepted.created_at_ms = now_ms;
    remember(center, &accepted, BEACON_ACK_RECEIVED, now_ms);
    add_ack(transition, accepted.id, BEACON_ACK_RECEIVED, NULL);

    if (accepted.supersede_key[0] != '\0') {
        for (size_t i = 0; i < center->pending_count;) {
            if (strcmp(center->pending[i].supersede_key, accepted.supersede_key) == 0) {
                remember(center, &center->pending[i], BEACON_ACK_SUPERSEDED, now_ms);
                add_ack(transition, center->pending[i].id, BEACON_ACK_SUPERSEDED, "newer_state");
                remove_pending(center, i);
            } else {
                i++;
            }
        }
    }

    if (!center->has_current) {
        center->current = accepted;
        center->has_current = true;
        transition->disposition = BEACON_NOTIFICATION_STARTED;
        mark_shown(center, now_ms, transition);
        return transition->disposition;
    }

    if (accepted.supersede_key[0] != '\0' &&
        strcmp(center->current.supersede_key, accepted.supersede_key) == 0) {
        remember(center, &center->current, BEACON_ACK_SUPERSEDED, now_ms);
        add_ack(transition, center->current.id, BEACON_ACK_SUPERSEDED, "newer_state");
        center->current = accepted;
        transition->disposition = BEACON_NOTIFICATION_INTERRUPTED_CURRENT;
        mark_shown(center, now_ms, transition);
        return transition->disposition;
    }

    if (should_interrupt(center, &accepted, now_ms)) {
        beacon_notification_t interrupted = center->current;
        remember(center, &interrupted, BEACON_ACK_INTERRUPTED, now_ms);
        add_ack(transition, interrupted.id, BEACON_ACK_INTERRUPTED, NULL);
        if (interrupted.replay_after_interrupt && interrupted.replay_count < interrupted.max_replays &&
            !is_expired(&interrupted, now_ms)) {
            interrupted.replay_count++;
            interrupted.created_at_ms = now_ms;
            (void)append_pending(center, &interrupted);
        }
        center->current = accepted;
        transition->disposition = BEACON_NOTIFICATION_INTERRUPTED_CURRENT;
        mark_shown(center, now_ms, transition);
        return transition->disposition;
    }

    if (append_pending(center, &accepted)) {
        transition->disposition = BEACON_NOTIFICATION_QUEUED;
        remember(center, &accepted, BEACON_ACK_QUEUED, now_ms);
        add_ack(transition, accepted.id, BEACON_ACK_QUEUED, NULL);
        return transition->disposition;
    }

    const size_t evict = worst_evictable(center);
    if (evict != SIZE_MAX && ranks_before(&accepted, &center->pending[evict])) {
        const beacon_notification_t evicted = center->pending[evict];
        remove_pending(center, evict);
        (void)append_pending(center, &accepted);
        remember(center, &evicted, BEACON_ACK_DROPPED, now_ms);
        add_ack(transition, evicted.id, BEACON_ACK_DROPPED, "queue_evicted");
        remember(center, &accepted, BEACON_ACK_QUEUED, now_ms);
        add_ack(transition, accepted.id, BEACON_ACK_QUEUED, NULL);
        transition->disposition = BEACON_NOTIFICATION_QUEUED;
        return transition->disposition;
    }

    remember(center, &accepted, BEACON_ACK_DROPPED, now_ms);
    add_ack(transition, accepted.id, BEACON_ACK_DROPPED, "queue_full_lower_priority");
    transition->disposition = BEACON_NOTIFICATION_DROPPED;
    return transition->disposition;
}

const beacon_notification_t *beacon_notification_center_current(
    const beacon_notification_center_t *center)
{
    return center != NULL && center->has_current ? &center->current : NULL;
}

const beacon_notification_t *beacon_notification_center_complete_current(
    beacon_notification_center_t *center, int64_t now_ms,
    beacon_notification_transition_t *transition)
{
    if (transition != NULL) {
        memset(transition, 0, sizeof(*transition));
    }
    if (center == NULL || !center->has_current) {
        return NULL;
    }
    remember(center, &center->current, BEACON_ACK_COMPLETED, now_ms);
    add_ack(transition, center->current.id, BEACON_ACK_COMPLETED, NULL);
    center->has_current = false;
    return promote_next(center, now_ms, transition);
}

size_t beacon_notification_center_pending_count(const beacon_notification_center_t *center)
{
    return center == NULL ? 0 : center->pending_count;
}

bool beacon_notification_center_remove_expired(beacon_notification_center_t *center,
                                               int64_t now_ms,
                                               beacon_notification_transition_t *transition)
{
    if (transition != NULL) {
        memset(transition, 0, sizeof(*transition));
    }
    if (center == NULL) {
        return false;
    }
    if (center->has_current && is_expired(&center->current, now_ms)) {
        remember(center, &center->current, BEACON_ACK_EXPIRED, now_ms);
        add_ack(transition, center->current.id, BEACON_ACK_EXPIRED, "expired_while_shown");
        center->has_current = false;
        (void)promote_next(center, now_ms, transition);
        return true;
    }
    for (size_t i = 0; i < center->pending_count; ++i) {
        if (is_expired(&center->pending[i], now_ms)) {
            remember(center, &center->pending[i], BEACON_ACK_EXPIRED, now_ms);
            add_ack(transition, center->pending[i].id, BEACON_ACK_EXPIRED, "expired_in_queue");
            remove_pending(center, i);
            return true;
        }
    }
    return false;
}

bool beacon_notification_center_replay_latest(beacon_notification_center_t *center,
                                              int64_t now_ms,
                                              beacon_notification_transition_t *transition)
{
    if (transition != NULL) {
        memset(transition, 0, sizeof(*transition));
    }
    if (center == NULL || center->has_current || !center->has_latest ||
        is_expired(&center->latest, now_ms)) {
        return false;
    }
    center->current = center->latest;
    center->current.created_at_ms = now_ms;
    center->has_current = true;
    if (transition != NULL) {
        transition->disposition = BEACON_NOTIFICATION_STARTED;
    }
    mark_shown(center, now_ms, transition);
    return true;
}
