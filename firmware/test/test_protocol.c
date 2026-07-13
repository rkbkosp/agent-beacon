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
    assert(beacon_revision_tracker_message(&tracker, 1) == BEACON_REVISION_ACCEPTED);
    assert(tracker.current == 1);
    assert(beacon_revision_tracker_message(&tracker, 1) == BEACON_REVISION_DUPLICATE);
    assert(beacon_revision_tracker_message(&tracker, 3) == BEACON_REVISION_GAP);
    assert(tracker.current == 1);
    beacon_revision_tracker_snapshot(&tracker, 3);
    assert(tracker.current == 3);
    assert(beacon_revision_tracker_message(&tracker, 4) == BEACON_REVISION_ACCEPTED);
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
    test_rfc3339_parser();
    return 0;
}
