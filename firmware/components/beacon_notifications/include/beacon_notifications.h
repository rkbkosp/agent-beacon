#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "beacon_ui_state.h"

#define BEACON_NOTIFICATION_REGULAR_CAPACITY 12
#define BEACON_NOTIFICATION_URGENT_RESERVE 2
#define BEACON_NOTIFICATION_TOTAL_CAPACITY \
    (BEACON_NOTIFICATION_REGULAR_CAPACITY + BEACON_NOTIFICATION_URGENT_RESERVE)
#define BEACON_NOTIFICATION_RECENT_CAPACITY 64
#define BEACON_NOTIFICATION_MAX_ACKS 4

typedef enum {
    BEACON_CATEGORY_AGENT = 0,
    BEACON_CATEGORY_QUOTA,
    BEACON_CATEGORY_WEATHER,
    BEACON_CATEGORY_SYSTEM,
} beacon_notification_category_t;

typedef enum {
    BEACON_URGENCY_NORMAL = 0,
    BEACON_URGENCY_ATTENTION,
    BEACON_URGENCY_URGENT,
} beacon_urgency_t;

typedef struct {
    char id[65];
    char kind[65];
    char source[33];
    char subject_id[97];
    char dedupe_key[161];
    char supersede_key[129];
    char group_key[97];
    char title[65];
    char detail[129];
    char source_label[13];
    uint64_t revision;
    beacon_notification_category_t category;
    beacon_urgency_t urgency;
    uint8_t priority;
    beacon_theme_t theme;
    uint32_t display_ms;
    int64_t expires_at_ms;
    int64_t created_at_ms;
    bool sticky_badge;
    bool replay_after_interrupt;
    uint8_t max_replays;
    uint8_t replay_count;
} beacon_notification_t;

typedef enum {
    BEACON_ACK_RECEIVED = 0,
    BEACON_ACK_QUEUED,
    BEACON_ACK_SHOWN,
    BEACON_ACK_COMPLETED,
    BEACON_ACK_INTERRUPTED,
    BEACON_ACK_SUPERSEDED,
    BEACON_ACK_EXPIRED,
    BEACON_ACK_DROPPED,
    BEACON_ACK_INVALID,
    BEACON_ACK_DUPLICATE,
} beacon_ack_status_t;

typedef struct {
    char notification_id[65];
    beacon_ack_status_t status;
    char reason[97];
} beacon_notification_ack_t;

typedef enum {
    BEACON_NOTIFICATION_NONE = 0,
    BEACON_NOTIFICATION_STARTED,
    BEACON_NOTIFICATION_QUEUED,
    BEACON_NOTIFICATION_INTERRUPTED_CURRENT,
    BEACON_NOTIFICATION_DUPLICATE,
    BEACON_NOTIFICATION_EXPIRED,
    BEACON_NOTIFICATION_DROPPED,
} beacon_notification_disposition_t;

typedef struct {
    beacon_notification_disposition_t disposition;
    beacon_notification_ack_t acks[BEACON_NOTIFICATION_MAX_ACKS];
    size_t ack_count;
} beacon_notification_transition_t;

typedef struct {
    char id[65];
    char dedupe_key[161];
    beacon_ack_status_t result;
    int64_t shown_at_ms;
} beacon_notification_recent_t;

typedef struct {
    beacon_notification_t current;
    bool has_current;
    int64_t current_started_at_ms;
    beacon_notification_t pending[BEACON_NOTIFICATION_TOTAL_CAPACITY];
    size_t pending_count;
    beacon_notification_recent_t recent[BEACON_NOTIFICATION_RECENT_CAPACITY];
    size_t recent_count;
    size_t recent_cursor;
    beacon_notification_t latest;
    bool has_latest;
} beacon_notification_center_t;

void beacon_notification_center_init(beacon_notification_center_t *center);
beacon_notification_disposition_t beacon_notification_center_offer(
    beacon_notification_center_t *center, const beacon_notification_t *notification,
    int64_t now_ms, beacon_notification_transition_t *transition);
const beacon_notification_t *beacon_notification_center_current(
    const beacon_notification_center_t *center);
const beacon_notification_t *beacon_notification_center_complete_current(
    beacon_notification_center_t *center, int64_t now_ms,
    beacon_notification_transition_t *transition);
size_t beacon_notification_center_pending_count(const beacon_notification_center_t *center);
bool beacon_notification_center_remove_expired(beacon_notification_center_t *center,
                                               int64_t now_ms,
                                               beacon_notification_transition_t *transition);
bool beacon_notification_center_replay_latest(beacon_notification_center_t *center,
                                              int64_t now_ms,
                                              beacon_notification_transition_t *transition);
