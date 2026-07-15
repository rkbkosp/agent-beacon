#include "beacon_protocol.h"

#include <stddef.h>
#include <string.h>

static bool parse_digits(const char *value, size_t count, int *result)
{
    int parsed = 0;
    for (size_t i = 0; i < count; ++i) {
        if (value[i] < '0' || value[i] > '9') {
            return false;
        }
        parsed = parsed * 10 + (value[i] - '0');
    }
    *result = parsed;
    return true;
}

static bool is_leap_year(int year)
{
    return year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
}

static int days_in_month(int year, int month)
{
    static const int DAYS[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
    if (month == 2 && is_leap_year(year)) {
        return 29;
    }
    return DAYS[month - 1];
}

static int64_t days_from_civil(int year, unsigned int month, unsigned int day)
{
    year -= month <= 2;
    const int era = (year >= 0 ? year : year - 399) / 400;
    const unsigned int year_of_era = (unsigned int)(year - era * 400);
    const unsigned int day_of_year =
        (153U * (month + (month > 2 ? (unsigned int)-3 : 9U)) + 2U) / 5U + day - 1U;
    const unsigned int day_of_era = year_of_era * 365U + year_of_era / 4U -
                                    year_of_era / 100U + day_of_year;
    return (int64_t)era * 146097LL + (int64_t)day_of_era - 719468LL;
}

bool beacon_protocol_theme_from_string(const char *value, beacon_theme_t *theme)
{
    if (value == NULL || theme == NULL) {
        return false;
    }
    if (strcmp(value, "blue") == 0) {
        *theme = BEACON_THEME_BLUE;
    } else if (strcmp(value, "yellow") == 0) {
        *theme = BEACON_THEME_YELLOW;
    } else if (strcmp(value, "red") == 0) {
        *theme = BEACON_THEME_RED;
    } else if (strcmp(value, "green") == 0) {
        *theme = BEACON_THEME_GREEN;
    } else {
        return false;
    }
    return true;
}

bool beacon_protocol_category_from_string(const char *value,
                                          beacon_notification_category_t *category)
{
    if (value == NULL || category == NULL) {
        return false;
    }
    if (strcmp(value, "agent") == 0) {
        *category = BEACON_CATEGORY_AGENT;
    } else if (strcmp(value, "quota") == 0) {
        *category = BEACON_CATEGORY_QUOTA;
    } else if (strcmp(value, "weather") == 0) {
        *category = BEACON_CATEGORY_WEATHER;
    } else if (strcmp(value, "system") == 0) {
        *category = BEACON_CATEGORY_SYSTEM;
    } else {
        return false;
    }
    return true;
}

bool beacon_protocol_urgency_from_string(const char *value, beacon_urgency_t *urgency)
{
    if (value == NULL || urgency == NULL) {
        return false;
    }
    if (strcmp(value, "normal") == 0) {
        *urgency = BEACON_URGENCY_NORMAL;
    } else if (strcmp(value, "attention") == 0) {
        *urgency = BEACON_URGENCY_ATTENTION;
    } else if (strcmp(value, "urgent") == 0) {
        *urgency = BEACON_URGENCY_URGENT;
    } else {
        return false;
    }
    return true;
}

const char *beacon_protocol_ack_status_string(beacon_ack_status_t status)
{
    switch (status) {
    case BEACON_ACK_RECEIVED: return "received";
    case BEACON_ACK_QUEUED: return "queued";
    case BEACON_ACK_SHOWN: return "shown";
    case BEACON_ACK_COMPLETED: return "completed";
    case BEACON_ACK_INTERRUPTED: return "interrupted";
    case BEACON_ACK_SUPERSEDED: return "superseded";
    case BEACON_ACK_EXPIRED: return "expired";
    case BEACON_ACK_DROPPED: return "dropped";
    case BEACON_ACK_INVALID: return "invalid";
    case BEACON_ACK_DUPLICATE: return "duplicate";
    default: return "invalid";
    }
}

beacon_revision_result_t beacon_revision_tracker_check(const beacon_revision_tracker_t *tracker,
                                                       uint64_t incoming_revision)
{
    if (tracker == NULL || incoming_revision == 0 || incoming_revision <= tracker->current) {
        return BEACON_REVISION_DUPLICATE;
    }
    if (tracker->current != 0 && incoming_revision != tracker->current + 1U) {
        return BEACON_REVISION_GAP;
    }
    return BEACON_REVISION_ACCEPTED;
}

void beacon_revision_tracker_commit(beacon_revision_tracker_t *tracker, uint64_t revision)
{
    if (tracker != NULL) {
        tracker->current = revision;
    }
}

bool beacon_revision_tracker_commit_delivery(beacon_revision_tracker_t *tracker,
                                             uint64_t revision, bool delivered)
{
    if (tracker == NULL || !delivered) {
        return false;
    }
    beacon_revision_tracker_commit(tracker, revision);
    return true;
}

bool beacon_protocol_parse_rfc3339_ms(const char *value, int64_t *timestamp_ms)
{
    if (value == NULL || timestamp_ms == NULL || strlen(value) < 20 ||
        value[4] != '-' || value[7] != '-' || value[10] != 'T' ||
        value[13] != ':' || value[16] != ':') {
        return false;
    }

    int year, month, day, hour, minute, second;
    if (!parse_digits(value, 4, &year) || !parse_digits(value + 5, 2, &month) ||
        !parse_digits(value + 8, 2, &day) || !parse_digits(value + 11, 2, &hour) ||
        !parse_digits(value + 14, 2, &minute) || !parse_digits(value + 17, 2, &second)) {
        return false;
    }
    if (year < 1970 || month < 1 || month > 12 || day < 1 ||
        day > days_in_month(year, month) || hour > 23 || minute > 59 || second > 60) {
        return false;
    }

    const char *cursor = value + 19;
    int milliseconds = 0;
    if (*cursor == '.') {
        cursor++;
        int digits = 0;
        while (*cursor >= '0' && *cursor <= '9') {
            if (digits < 3) {
                milliseconds = milliseconds * 10 + (*cursor - '0');
            }
            digits++;
            cursor++;
        }
        if (digits == 0) {
            return false;
        }
        while (digits < 3) {
            milliseconds *= 10;
            digits++;
        }
    }

    int timezone_offset_seconds = 0;
    if (*cursor == 'Z' && cursor[1] == '\0') {
        cursor++;
    } else if ((*cursor == '+' || *cursor == '-') && strlen(cursor) == 6 && cursor[3] == ':') {
        int timezone_hour, timezone_minute;
        if (!parse_digits(cursor + 1, 2, &timezone_hour) ||
            !parse_digits(cursor + 4, 2, &timezone_minute) ||
            timezone_hour > 23 || timezone_minute > 59) {
            return false;
        }
        timezone_offset_seconds = timezone_hour * 3600 + timezone_minute * 60;
        if (*cursor == '-') {
            timezone_offset_seconds = -timezone_offset_seconds;
        }
        cursor += 6;
    } else {
        return false;
    }
    if (*cursor != '\0') {
        return false;
    }

    const int64_t days = days_from_civil(year, (unsigned int)month, (unsigned int)day);
    const int64_t seconds = days * 86400LL + hour * 3600LL + minute * 60LL + second -
                            timezone_offset_seconds;
    *timestamp_ms = seconds * 1000LL + milliseconds;
    return true;
}
