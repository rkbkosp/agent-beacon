#include "beacon_protocol.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"

static bool valid_utf8(const unsigned char *data, size_t length)
{
    size_t index = 0;
    while (index < length) {
        const unsigned char first = data[index++];
        if (first <= 0x7fU) {
            continue;
        }
        size_t continuation;
        unsigned char min_second = 0x80U;
        unsigned char max_second = 0xbfU;
        if (first >= 0xc2U && first <= 0xdfU) {
            continuation = 1;
        } else if (first >= 0xe0U && first <= 0xefU) {
            continuation = 2;
            if (first == 0xe0U) min_second = 0xa0U;
            if (first == 0xedU) max_second = 0x9fU;
        } else if (first >= 0xf0U && first <= 0xf4U) {
            continuation = 3;
            if (first == 0xf0U) min_second = 0x90U;
            if (first == 0xf4U) max_second = 0x8fU;
        } else {
            return false;
        }
        if (index + continuation > length || data[index] < min_second || data[index] > max_second) {
            return false;
        }
        index++;
        for (size_t offset = 1; offset < continuation; ++offset, ++index) {
            if (data[index] < 0x80U || data[index] > 0xbfU) {
                return false;
            }
        }
    }
    return true;
}

static bool object_has_only(const cJSON *object, const char *const *names, size_t count)
{
    if (!cJSON_IsObject(object)) {
        return false;
    }
    for (const cJSON *item = object->child; item != NULL; item = item->next) {
        bool allowed = false;
        for (size_t index = 0; index < count; ++index) {
            if (item->string != NULL && strcmp(item->string, names[index]) == 0) {
                allowed = true;
                break;
            }
        }
        if (!allowed) {
            return false;
        }
    }
    return true;
}

static bool copy_item_string(const cJSON *item, char *destination, size_t destination_size)
{
    if (!cJSON_IsString(item) || item->valuestring == NULL ||
        strlen(item->valuestring) >= destination_size) {
        return false;
    }
    memcpy(destination, item->valuestring, strlen(item->valuestring) + 1U);
    return true;
}

static bool copy_json_string(const cJSON *object, const char *name,
                             char *destination, size_t destination_size,
                             bool required)
{
    const cJSON *item = cJSON_GetObjectItemCaseSensitive(object, name);
    if (item == NULL && !required) {
        destination[0] = '\0';
        return true;
    }
    return copy_item_string(item, destination, destination_size);
}

static bool integer_in_range(const cJSON *item, int minimum, int maximum, int *value)
{
    if (!cJSON_IsNumber(item) || item->valuedouble < minimum || item->valuedouble > maximum ||
        item->valuedouble != (double)item->valueint) {
        return false;
    }
    *value = item->valueint;
    return true;
}

static bool parse_freshness_item(const cJSON *item, beacon_freshness_t *freshness)
{
    if (!cJSON_IsString(item) || item->valuestring == NULL) {
        return false;
    }
    if (strcmp(item->valuestring, "fresh") == 0) {
        *freshness = BEACON_FRESHNESS_FRESH;
    } else if (strcmp(item->valuestring, "cached") == 0) {
        *freshness = BEACON_FRESHNESS_CACHED;
    } else if (strcmp(item->valuestring, "stale") == 0) {
        *freshness = BEACON_FRESHNESS_STALE;
    } else if (strcmp(item->valuestring, "unknown") == 0) {
        *freshness = BEACON_FRESHNESS_UNKNOWN;
    } else {
        return false;
    }
    return true;
}

static bool format_timestamp(const cJSON *item, char *destination, size_t destination_size,
                             bool include_date)
{
    int64_t ignored;
    if (!cJSON_IsString(item) || item->valuestring == NULL ||
        !beacon_protocol_parse_rfc3339_ms(item->valuestring, &ignored) ||
        strlen(item->valuestring) < 16U) {
        return false;
    }
    if (include_date) {
        return snprintf(destination, destination_size, "%.2s/%.2s %.5s",
                        item->valuestring + 5, item->valuestring + 8,
                        item->valuestring + 11) > 0;
    }
    return snprintf(destination, destination_size, "%.5s", item->valuestring + 11) > 0;
}

static bool parse_notification(const cJSON *root, beacon_protocol_message_t *message)
{
    const cJSON *payload = cJSON_GetObjectItemCaseSensitive(root, "payload");
    const cJSON *category = cJSON_GetObjectItemCaseSensitive(payload, "category");
    const cJSON *theme = cJSON_GetObjectItemCaseSensitive(payload, "theme");
    const cJSON *urgency = cJSON_GetObjectItemCaseSensitive(payload, "urgency");
    const cJSON *priority = cJSON_GetObjectItemCaseSensitive(payload, "priority");
    const cJSON *display_ms = cJSON_GetObjectItemCaseSensitive(payload, "display_ms");
    const cJSON *expires_at = cJSON_GetObjectItemCaseSensitive(payload, "expires_at");
    int parsed_priority;
    int parsed_display_ms;
    if (!cJSON_IsObject(payload) ||
        !beacon_protocol_category_from_string(cJSON_IsString(category) ? category->valuestring : NULL,
                                              &message->notification.category) ||
        !beacon_protocol_theme_from_string(cJSON_IsString(theme) ? theme->valuestring : NULL,
                                           &message->notification.theme) ||
        !beacon_protocol_urgency_from_string(cJSON_IsString(urgency) ? urgency->valuestring : NULL,
                                             &message->notification.urgency) ||
        !integer_in_range(priority, 0, 100, &parsed_priority) ||
        !integer_in_range(display_ms, 1500, 12000, &parsed_display_ms) ||
        !cJSON_IsString(expires_at) || expires_at->valuestring == NULL ||
        !beacon_protocol_parse_rfc3339_ms(expires_at->valuestring,
                                          &message->notification.expires_at_ms)) {
        return false;
    }

    beacon_notification_t *notification = &message->notification;
    if (!copy_json_string(root, "id", notification->id, sizeof(notification->id), true) ||
        !copy_json_string(payload, "kind", notification->kind, sizeof(notification->kind), true) ||
        !copy_json_string(payload, "source", notification->source, sizeof(notification->source), true) ||
        !copy_json_string(payload, "subject_id", notification->subject_id, sizeof(notification->subject_id), true) ||
        !copy_json_string(payload, "dedupe_key", notification->dedupe_key, sizeof(notification->dedupe_key), true) ||
        !copy_json_string(payload, "supersede_key", notification->supersede_key, sizeof(notification->supersede_key), false) ||
        !copy_json_string(payload, "group_key", notification->group_key, sizeof(notification->group_key), false) ||
        !copy_json_string(payload, "title", notification->title, sizeof(notification->title), true) ||
        !copy_json_string(payload, "detail", notification->detail, sizeof(notification->detail), false) ||
        !copy_json_string(payload, "source_label", notification->source_label, sizeof(notification->source_label), false)) {
        return false;
    }
    const size_t category_length = strlen(category->valuestring);
    if (strncmp(notification->kind, category->valuestring, category_length) != 0 ||
        notification->kind[category_length] != '.') {
        return false;
    }
    notification->revision = message->revision;
    notification->priority = (uint8_t)parsed_priority;
    notification->display_ms = (uint32_t)parsed_display_ms;

    const cJSON *sticky = cJSON_GetObjectItemCaseSensitive(payload, "sticky_badge");
    const cJSON *replay = cJSON_GetObjectItemCaseSensitive(payload, "replay_after_interrupt");
    const cJSON *max_replays = cJSON_GetObjectItemCaseSensitive(payload, "max_replays");
    if ((sticky != NULL && !cJSON_IsBool(sticky)) ||
        (replay != NULL && !cJSON_IsBool(replay))) {
        return false;
    }
    notification->sticky_badge = cJSON_IsTrue(sticky);
    notification->replay_after_interrupt = cJSON_IsTrue(replay);
    if (max_replays != NULL) {
        int parsed_max;
        if (!integer_in_range(max_replays, 0, UINT8_MAX, &parsed_max)) {
            return false;
        }
        notification->max_replays = (uint8_t)parsed_max;
    }
    return true;
}

static bool parse_clock(const cJSON *object, beacon_system_state_t *system)
{
    return cJSON_IsObject(object) &&
           copy_json_string(object, "timezone", system->timezone, sizeof(system->timezone), true) &&
           format_timestamp(cJSON_GetObjectItemCaseSensitive(object, "server_time"),
                            system->display_time, sizeof(system->display_time), false);
}

static bool parse_codex_home(const cJSON *object, beacon_codex_home_t *home)
{
    int percent;
    int cards;
    const cJSON *weekly_reset = cJSON_GetObjectItemCaseSensitive(object, "weekly_reset_at");
    const cJSON *card_count = cJSON_GetObjectItemCaseSensitive(object, "reset_cards_available");
    const cJSON *card_expiry = cJSON_GetObjectItemCaseSensitive(object, "nearest_reset_card_expires_at");
    if (!cJSON_IsObject(object) ||
        !copy_json_string(object, "id", home->id, sizeof(home->id), true) ||
        !copy_json_string(object, "label", home->label, sizeof(home->label), true) ||
        !integer_in_range(cJSON_GetObjectItemCaseSensitive(object, "weekly_remaining_percent"), 0, 100, &percent) ||
        !parse_freshness_item(cJSON_GetObjectItemCaseSensitive(object, "freshness"), &home->freshness) ||
        !cJSON_IsString(cJSON_GetObjectItemCaseSensitive(object, "updated_at"))) {
        return false;
    }
    home->weekly_remaining_percent = (uint8_t)percent;
    if (cJSON_IsNull(weekly_reset)) {
        snprintf(home->weekly_reset, sizeof(home->weekly_reset), "-");
    } else if (!format_timestamp(weekly_reset, home->weekly_reset, sizeof(home->weekly_reset), true)) {
        return false;
    }
    if (cJSON_IsNull(card_count)) {
        home->reset_cards_available = -1;
    } else if (!integer_in_range(card_count, 0, INT16_MAX, &cards)) {
        return false;
    } else {
        home->reset_cards_available = (int16_t)cards;
    }
    if (cJSON_IsNull(card_expiry)) {
        snprintf(home->nearest_card_expiry, sizeof(home->nearest_card_expiry), "-");
    } else {
        char formatted[16];
        if (!format_timestamp(card_expiry, formatted, sizeof(formatted), true)) {
            return false;
        }
        memcpy(home->nearest_card_expiry, formatted, 5U);
        home->nearest_card_expiry[5] = '\0';
    }
    return true;
}

static bool parse_codex(const cJSON *object, beacon_codex_state_t *codex)
{
    const cJSON *homes = cJSON_GetObjectItemCaseSensitive(object, "homes");
    const cJSON *relay = cJSON_GetObjectItemCaseSensitive(object, "relay");
    const int home_count = cJSON_GetArraySize(homes);
    if (!cJSON_IsObject(object) || !cJSON_IsArray(homes) || home_count < 1 ||
        home_count > BEACON_CODEX_HOME_MAX || !cJSON_IsObject(relay)) {
        return false;
    }
    codex->home_count = (size_t)home_count;
    for (int index = 0; index < home_count; ++index) {
        if (!parse_codex_home(cJSON_GetArrayItem(homes, index), &codex->homes[index])) {
            return false;
        }
    }
    const cJSON *remaining = cJSON_GetObjectItemCaseSensitive(relay, "remaining");
    const cJSON *valid = cJSON_GetObjectItemCaseSensitive(relay, "is_valid");
    if (!cJSON_IsBool(valid) ||
        !copy_json_string(relay, "unit", (char[13]){0}, 13, true) ||
        !cJSON_IsString(cJSON_GetObjectItemCaseSensitive(relay, "updated_at")) ||
        !parse_freshness_item(cJSON_GetObjectItemCaseSensitive(relay, "freshness"),
                              &codex->relay.freshness) ||
        (!cJSON_IsNull(remaining) && !cJSON_IsNumber(remaining))) {
        return false;
    }
    codex->relay.is_valid = cJSON_IsTrue(valid);
    if (!codex->relay.is_valid) {
        snprintf(codex->relay.display, sizeof(codex->relay.display), "凭证无效");
    } else if (cJSON_IsNull(remaining)) {
        snprintf(codex->relay.display, sizeof(codex->relay.display), "-");
    } else {
        snprintf(codex->relay.display, sizeof(codex->relay.display), "$%.2f", remaining->valuedouble);
    }
    return true;
}

static bool parse_agent_status(const cJSON *item, beacon_agent_status_t *status)
{
    if (!cJSON_IsString(item) || item->valuestring == NULL) return false;
    if (strcmp(item->valuestring, "working") == 0) *status = BEACON_AGENT_WORKING;
    else if (strcmp(item->valuestring, "blocked") == 0) *status = BEACON_AGENT_BLOCKED;
    else if (strcmp(item->valuestring, "done") == 0) *status = BEACON_AGENT_DONE;
    else if (strcmp(item->valuestring, "idle") == 0) *status = BEACON_AGENT_IDLE;
    else if (strcmp(item->valuestring, "unknown") == 0) *status = BEACON_AGENT_UNKNOWN;
    else return false;
    return true;
}

static const char *agent_status_text(beacon_agent_status_t status)
{
    return beacon_agent_status_label(status);
}

static uint8_t agent_status_priority(beacon_agent_status_t status)
{
    switch (status) {
    case BEACON_AGENT_BLOCKED: return 5;
    case BEACON_AGENT_DONE: return 4;
    case BEACON_AGENT_WORKING: return 3;
    case BEACON_AGENT_IDLE: return 2;
    case BEACON_AGENT_UNKNOWN:
    default: return 1;
    }
}

static bool agent_sorts_before(const beacon_agent_item_t *left,
                               const beacon_agent_item_t *right)
{
    const uint8_t left_priority = agent_status_priority(left->status);
    const uint8_t right_priority = agent_status_priority(right->status);
    if (left_priority != right_priority) {
        return left_priority > right_priority;
    }
    if (left->revision != right->revision) {
        return left->revision > right->revision;
    }
    if (left->focused != right->focused) {
        return left->focused;
    }
    return strcmp(left->display_name, right->display_name) < 0;
}

static void insert_agent(beacon_agents_state_t *agents,
                         const beacon_agent_item_t *candidate)
{
    size_t position = 0;
    while (position < agents->item_count &&
           !agent_sorts_before(candidate, &agents->items[position])) {
        position++;
    }
    if (position >= BEACON_AGENT_ITEM_MAX) {
        return;
    }

    const size_t old_count = agents->item_count;
    const size_t new_count = old_count < BEACON_AGENT_ITEM_MAX ? old_count + 1 : old_count;
    if (new_count > position + 1) {
        memmove(&agents->items[position + 1], &agents->items[position],
                (new_count - position - 1) * sizeof(agents->items[0]));
    }
    agents->items[position] = *candidate;
    agents->item_count = new_count;
}

static bool parse_agents(const cJSON *object, beacon_agents_state_t *agents)
{
    char provider[16];
    const cJSON *connected = cJSON_GetObjectItemCaseSensitive(object, "connected");
    const cJSON *items = cJSON_GetObjectItemCaseSensitive(object, "items");
    if (!cJSON_IsObject(object) ||
        !copy_json_string(object, "provider", provider, sizeof(provider), true) ||
        strcmp(provider, "herdr") != 0 || !cJSON_IsBool(connected) || !cJSON_IsArray(items) ||
        !cJSON_IsString(cJSON_GetObjectItemCaseSensitive(object, "updated_at"))) {
        return false;
    }
    agents->connected = cJSON_IsTrue(connected);
    const int count = cJSON_GetArraySize(items);
    agents->item_count = 0;
    agents->hidden_count = 0;
    for (int index = 0; index < count; ++index) {
        const cJSON *item = cJSON_GetArrayItem(items, index);
        const cJSON *focused = cJSON_GetObjectItemCaseSensitive(item, "focused");
        const cJSON *revision = cJSON_GetObjectItemCaseSensitive(item, "revision");
        beacon_agent_status_t status;
        if (!cJSON_IsObject(item) ||
            !parse_agent_status(cJSON_GetObjectItemCaseSensitive(item, "status"), &status) ||
            !cJSON_IsBool(focused) || !cJSON_IsNumber(revision) ||
            revision->valuedouble < 0 || revision->valuedouble > 9007199254740991.0 ||
            (double)(uint64_t)revision->valuedouble != revision->valuedouble) {
            return false;
        }
        beacon_agent_item_t candidate = {
            .status = status,
            .focused = cJSON_IsTrue(focused),
            .revision = (uint64_t)revision->valuedouble,
        };
        if (!copy_json_string(item, "pane_id", candidate.pane_id, sizeof(candidate.pane_id), true) ||
            !copy_json_string(item, "display_name", candidate.display_name, sizeof(candidate.display_name), true)) {
            return false;
        }
        if (!copy_json_string(item, "custom_status", candidate.secondary, sizeof(candidate.secondary), false) ||
            candidate.secondary[0] == '\0') {
            if (!copy_json_string(item, "title", candidate.secondary, sizeof(candidate.secondary), false)) {
                return false;
            }
        }
        if (candidate.secondary[0] == '\0') {
            snprintf(candidate.secondary, sizeof(candidate.secondary), "%s",
                     agent_status_text(candidate.status));
        }
        insert_agent(agents, &candidate);
    }
    agents->hidden_count = (size_t)count - agents->item_count;
    return true;
}

static bool parse_weather_current(const cJSON *object, beacon_weather_current_t *current)
{
    int temp;
    return cJSON_IsObject(object) &&
           cJSON_IsString(cJSON_GetObjectItemCaseSensitive(object, "observed_at")) &&
           integer_in_range(cJSON_GetObjectItemCaseSensitive(object, "temp_c"), -80, 80, &temp) &&
           (current->temp_c = (int16_t)temp, true) &&
           copy_json_string(object, "icon", current->icon, sizeof(current->icon), true) &&
           copy_json_string(object, "text", current->text, sizeof(current->text), true) &&
           cJSON_IsNumber(cJSON_GetObjectItemCaseSensitive(object, "precip_mm")) &&
           parse_freshness_item(cJSON_GetObjectItemCaseSensitive(object, "freshness"),
                                &current->freshness);
}

static bool parse_weather_slot(const cJSON *object, const char *label,
                               beacon_weather_slot_t *slot)
{
    int temp;
    int pop;
    const cJSON *past = cJSON_GetObjectItemCaseSensitive(object, "is_past");
    snprintf(slot->label, sizeof(slot->label), "%s", label);
    return cJSON_IsObject(object) &&
           format_timestamp(cJSON_GetObjectItemCaseSensitive(object, "target_at"),
                            slot->time, sizeof(slot->time), false) &&
           cJSON_IsBool(past) && (slot->is_past = cJSON_IsTrue(past), true) &&
           integer_in_range(cJSON_GetObjectItemCaseSensitive(object, "temp_c"), -80, 80, &temp) &&
           (slot->temp_c = (int16_t)temp, true) &&
           cJSON_IsString(cJSON_GetObjectItemCaseSensitive(object, "icon")) &&
           copy_json_string(object, "text", slot->text, sizeof(slot->text), true) &&
           integer_in_range(cJSON_GetObjectItemCaseSensitive(object, "pop"), 0, 100, &pop) &&
           cJSON_IsNumber(cJSON_GetObjectItemCaseSensitive(object, "precip_mm")) &&
           parse_freshness_item(cJSON_GetObjectItemCaseSensitive(object, "freshness"),
                                &slot->freshness);
}

static bool parse_weather(const cJSON *object, beacon_weather_state_t *weather)
{
    char provider[16];
    const cJSON *outing = cJSON_GetObjectItemCaseSensitive(object, "next_outing");
    if (!cJSON_IsObject(object) ||
        !copy_json_string(object, "location", weather->location, sizeof(weather->location), true) ||
        !copy_json_string(object, "provider", provider, sizeof(provider), true) ||
        strcmp(provider, "qweather") != 0 ||
        !parse_weather_current(cJSON_GetObjectItemCaseSensitive(object, "current"), &weather->current) ||
        !parse_weather_slot(cJSON_GetObjectItemCaseSensitive(object, "lunch"), "午饭", &weather->lunch) ||
        !parse_weather_slot(cJSON_GetObjectItemCaseSensitive(object, "leave"), "下班", &weather->leave) ||
        !cJSON_IsObject(outing) ||
        !copy_json_string(outing, "slot", weather->next_outing.slot, sizeof(weather->next_outing.slot), true) ||
        !copy_json_string(outing, "reason", weather->next_outing.reason, sizeof(weather->next_outing.reason), true) ||
        !cJSON_IsString(cJSON_GetObjectItemCaseSensitive(outing, "confidence")) ||
        !cJSON_IsString(cJSON_GetObjectItemCaseSensitive(object, "updated_at"))) {
        return false;
    }
    snprintf(weather->provider, sizeof(weather->provider), "和风天气");
    const cJSON *target = cJSON_GetObjectItemCaseSensitive(outing, "target_at");
    if (cJSON_IsNull(target)) {
        weather->next_outing.time[0] = '\0';
    } else if (!format_timestamp(target, weather->next_outing.time,
                                 sizeof(weather->next_outing.time), false)) {
        return false;
    }
    const cJSON *umbrella = cJSON_GetObjectItemCaseSensitive(outing, "umbrella_required");
    if (cJSON_IsNull(umbrella)) {
        weather->next_outing.umbrella_known = false;
    } else if (cJSON_IsBool(umbrella)) {
        weather->next_outing.umbrella_known = true;
        weather->next_outing.umbrella_required = cJSON_IsTrue(umbrella);
    } else {
        return false;
    }
    return true;
}

static bool parse_system(const cJSON *object, beacon_system_state_t *system)
{
    const cJSON *online = cJSON_GetObjectItemCaseSensitive(object, "bridge_online");
    return cJSON_IsObject(object) && cJSON_IsBool(online) &&
           (system->bridge_online = cJSON_IsTrue(online), true) &&
           parse_freshness_item(cJSON_GetObjectItemCaseSensitive(object, "overall_freshness"),
                                &system->overall_freshness);
}

static bool parse_state(const cJSON *payload, bool snapshot, beacon_protocol_message_t *message)
{
    static const char *const ALLOWED[] = {"clock", "codex", "agents", "weather", "system"};
    if (!object_has_only(payload, ALLOWED, sizeof(ALLOWED) / sizeof(ALLOWED[0]))) {
        return false;
    }
    const cJSON *clock = cJSON_GetObjectItemCaseSensitive(payload, "clock");
    const cJSON *codex = cJSON_GetObjectItemCaseSensitive(payload, "codex");
    const cJSON *agents = cJSON_GetObjectItemCaseSensitive(payload, "agents");
    const cJSON *weather = cJSON_GetObjectItemCaseSensitive(payload, "weather");
    const cJSON *system = cJSON_GetObjectItemCaseSensitive(payload, "system");
    if (clock != NULL) {
        if (!parse_clock(clock, &message->state.system)) return false;
        message->state_domains |= BEACON_STATE_DOMAIN_CLOCK;
    }
    if (codex != NULL) {
        if (!parse_codex(codex, &message->state.codex)) return false;
        message->state_domains |= BEACON_STATE_DOMAIN_CODEX;
    }
    if (agents != NULL) {
        if (!parse_agents(agents, &message->state.agents)) return false;
        message->state_domains |= BEACON_STATE_DOMAIN_AGENTS;
    }
    if (weather != NULL) {
        if (!parse_weather(weather, &message->state.weather)) return false;
        message->state_domains |= BEACON_STATE_DOMAIN_WEATHER;
    }
    if (system != NULL) {
        if (!parse_system(system, &message->state.system)) return false;
        message->state_domains |= BEACON_STATE_DOMAIN_SYSTEM;
    }
    return snapshot ? message->state_domains == BEACON_STATE_DOMAIN_ALL : message->state_domains != 0U;
}

bool beacon_protocol_decode(const char *json, size_t length,
                            beacon_protocol_message_t *message)
{
    if (json == NULL || length == 0U || length > BEACON_PROTOCOL_MAX_MESSAGE_BYTES ||
        message == NULL || !valid_utf8((const unsigned char *)json, length)) {
        return false;
    }
    char *terminated = malloc(length + 1U);
    if (terminated == NULL) {
        return false;
    }
    memcpy(terminated, json, length);
    terminated[length] = '\0';
    const char *parse_end = NULL;
    cJSON *root = cJSON_ParseWithLengthOpts(terminated, length + 1U, &parse_end, true);
    free(terminated);
    if (root == NULL || !cJSON_IsObject(root)) {
        cJSON_Delete(root);
        return false;
    }

    memset(message, 0, sizeof(*message));
    bool result = false;
    const cJSON *version = cJSON_GetObjectItemCaseSensitive(root, "v");
    const cJSON *id = cJSON_GetObjectItemCaseSensitive(root, "id");
    const cJSON *type = cJSON_GetObjectItemCaseSensitive(root, "type");
    const cJSON *timestamp = cJSON_GetObjectItemCaseSensitive(root, "ts");
    const cJSON *revision = cJSON_GetObjectItemCaseSensitive(root, "revision");
    const cJSON *payload = cJSON_GetObjectItemCaseSensitive(root, "payload");
    int64_t ignored_timestamp;
    if (!cJSON_IsNumber(version) || version->valueint != BEACON_PROTOCOL_VERSION ||
        !cJSON_IsString(id) || id->valuestring == NULL || id->valuestring[0] == '\0' ||
        strlen(id->valuestring) >= sizeof(message->notification.id) ||
        !cJSON_IsString(type) || type->valuestring == NULL ||
        !cJSON_IsString(timestamp) || timestamp->valuestring == NULL ||
        !beacon_protocol_parse_rfc3339_ms(timestamp->valuestring, &ignored_timestamp) ||
        !cJSON_IsNumber(revision) || revision->valuedouble < 0 || !cJSON_IsObject(payload)) {
        goto cleanup;
    }
    message->revision = (uint64_t)revision->valuedouble;

    if (strcmp(type->valuestring, "hello") == 0) {
        const cJSON *role = cJSON_GetObjectItemCaseSensitive(payload, "role");
        const cJSON *protocol_version = cJSON_GetObjectItemCaseSensitive(payload, "protocol_version");
        if (cJSON_IsString(role) && strcmp(role->valuestring, "server") == 0 &&
            cJSON_IsNumber(protocol_version) && protocol_version->valueint == BEACON_PROTOCOL_VERSION) {
            message->type = BEACON_PROTOCOL_MESSAGE_HELLO;
            result = true;
        }
    } else if (strcmp(type->valuestring, "snapshot") == 0 && parse_state(payload, true, message)) {
        message->type = BEACON_PROTOCOL_MESSAGE_SNAPSHOT;
        result = true;
    } else if (strcmp(type->valuestring, "state_patch") == 0 && parse_state(payload, false, message)) {
        message->type = BEACON_PROTOCOL_MESSAGE_STATE_PATCH;
        result = true;
    } else if (strcmp(type->valuestring, "notification") == 0 && parse_notification(root, message)) {
        message->type = BEACON_PROTOCOL_MESSAGE_NOTIFICATION;
        result = true;
    } else if (strcmp(type->valuestring, "heartbeat") == 0) {
        message->type = BEACON_PROTOCOL_MESSAGE_HEARTBEAT;
        result = true;
    } else if (strcmp(type->valuestring, "error") == 0) {
        message->type = BEACON_PROTOCOL_MESSAGE_ERROR;
        result = true;
    }

cleanup:
    cJSON_Delete(root);
    return result;
}
