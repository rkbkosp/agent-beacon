#include <assert.h>
#include <string.h>

#include "beacon_protocol.h"

static void test_enum_and_ack_mapping(void)
{
    beacon_theme_t theme;
    beacon_notification_category_t category;
    beacon_urgency_t urgency;
    assert(beacon_protocol_theme_from_string("blue", &theme) && theme == BEACON_THEME_BLUE);
    assert(!beacon_protocol_theme_from_string("purple", &theme));
    assert(beacon_protocol_category_from_string("weather", &category) &&
           category == BEACON_CATEGORY_WEATHER);
    assert(!beacon_protocol_category_from_string("message", &category));
    assert(beacon_protocol_urgency_from_string("urgent", &urgency) &&
           urgency == BEACON_URGENCY_URGENT);
    assert(!beacon_protocol_urgency_from_string("critical", &urgency));

    assert(strcmp(beacon_protocol_ack_status_string(BEACON_ACK_RECEIVED), "received") == 0);
    assert(strcmp(beacon_protocol_ack_status_string(BEACON_ACK_SHOWN), "shown") == 0);
    assert(strcmp(beacon_protocol_ack_status_string(BEACON_ACK_COMPLETED), "completed") == 0);
    assert(strcmp(beacon_protocol_ack_status_string(BEACON_ACK_DUPLICATE), "duplicate") == 0);
}

static void test_revision_tracker(void)
{
    beacon_revision_tracker_t tracker = {0};
    assert(beacon_revision_tracker_check(&tracker, 1) == BEACON_REVISION_ACCEPTED);
    assert(beacon_revision_tracker_commit_delivery(&tracker, 1, true));
    assert(tracker.current == 1);
    assert(beacon_revision_tracker_check(&tracker, 1) == BEACON_REVISION_DUPLICATE);
    assert(beacon_revision_tracker_check(&tracker, 3) == BEACON_REVISION_GAP);
    assert(tracker.current == 1);
    beacon_revision_tracker_commit(&tracker, 3);
    assert(tracker.current == 3);
    assert(beacon_revision_tracker_check(&tracker, 4) == BEACON_REVISION_ACCEPTED);
    assert(beacon_revision_tracker_commit_delivery(&tracker, 4, true));
}

static void test_revision_commits_only_after_delivery(void)
{
    beacon_revision_tracker_t tracker = {.current = 10};

    assert(beacon_revision_tracker_check(&tracker, 11) == BEACON_REVISION_ACCEPTED);
    assert(tracker.current == 10);
    assert(!beacon_revision_tracker_commit_delivery(&tracker, 11, false));
    assert(tracker.current == 10);
    assert(beacon_revision_tracker_check(&tracker, 12) == BEACON_REVISION_GAP);

    assert(beacon_revision_tracker_commit_delivery(&tracker, 11, true));
    assert(tracker.current == 11);
    assert(beacon_revision_tracker_check(&tracker, 12) == BEACON_REVISION_ACCEPTED);

    // A bridge restart can provide an authoritative revision-zero snapshot.
    // Rejecting that snapshot at a full UI queue must preserve the old tracker.
    assert(!beacon_revision_tracker_commit_delivery(&tracker, 0, false));
    assert(tracker.current == 11);
    assert(beacon_revision_tracker_commit_delivery(&tracker, 0, true));
    assert(tracker.current == 0);
}

static void test_rfc3339_parser(void)
{
    int64_t timestamp_ms = 0;
    assert(beacon_protocol_parse_rfc3339_ms("1970-01-01T00:00:00Z", &timestamp_ms));
    assert(timestamp_ms == 0);
    assert(beacon_protocol_parse_rfc3339_ms("2026-07-14T12:00:00+08:00", &timestamp_ms));
    assert(timestamp_ms == 1784001600000LL);
    assert(beacon_protocol_parse_rfc3339_ms("2026-07-14T04:00:00.125Z", &timestamp_ms));
    assert(timestamp_ms == 1784001600125LL);
    assert(!beacon_protocol_parse_rfc3339_ms("2026/07/14 12:00", &timestamp_ms));
}

int main(void)
{
    test_enum_and_ack_mapping();
    test_revision_tracker();
    test_revision_commits_only_after_delivery();
    test_rfc3339_parser();
    return 0;
}
