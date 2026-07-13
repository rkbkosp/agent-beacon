#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define BEACON_CODEX_HOME_MAX 2
#define BEACON_AGENT_ITEM_MAX 4

#define BEACON_STATE_DOMAIN_CLOCK (1U << 0)
#define BEACON_STATE_DOMAIN_CODEX (1U << 1)
#define BEACON_STATE_DOMAIN_AGENTS (1U << 2)
#define BEACON_STATE_DOMAIN_WEATHER (1U << 3)
#define BEACON_STATE_DOMAIN_SYSTEM (1U << 4)
#define BEACON_STATE_DOMAIN_ALL \
    (BEACON_STATE_DOMAIN_CLOCK | BEACON_STATE_DOMAIN_CODEX | \
     BEACON_STATE_DOMAIN_AGENTS | BEACON_STATE_DOMAIN_WEATHER | \
     BEACON_STATE_DOMAIN_SYSTEM)

typedef enum {
    BEACON_FRESHNESS_FRESH = 0,
    BEACON_FRESHNESS_CACHED,
    BEACON_FRESHNESS_STALE,
    BEACON_FRESHNESS_UNKNOWN,
} beacon_freshness_t;

typedef enum {
    BEACON_AGENT_WORKING = 0,
    BEACON_AGENT_BLOCKED,
    BEACON_AGENT_DONE,
    BEACON_AGENT_IDLE,
    BEACON_AGENT_UNKNOWN,
} beacon_agent_status_t;

typedef struct {
    char id[16];
    char label[12];
    uint8_t weekly_remaining_percent;
    char weekly_reset[16];
    int16_t reset_cards_available;
    char nearest_card_expiry[16];
    beacon_freshness_t freshness;
} beacon_codex_home_t;

typedef struct {
    char display[16];
    bool is_valid;
    beacon_freshness_t freshness;
} beacon_relay_state_t;

typedef struct {
    size_t home_count;
    beacon_codex_home_t homes[BEACON_CODEX_HOME_MAX];
    beacon_relay_state_t relay;
} beacon_codex_state_t;

typedef struct {
    char pane_id[24];
    char display_name[28];
    char secondary[36];
    beacon_agent_status_t status;
    bool focused;
    uint64_t revision;
} beacon_agent_item_t;

typedef struct {
    bool connected;
    size_t item_count;
    size_t hidden_count;
    beacon_agent_item_t items[BEACON_AGENT_ITEM_MAX];
} beacon_agents_state_t;

typedef struct {
    int16_t temp_c;
    char text[16];
    char icon[8];
    beacon_freshness_t freshness;
} beacon_weather_current_t;

typedef struct {
    char label[12];
    char time[8];
    int16_t temp_c;
    char text[16];
    bool is_past;
    beacon_freshness_t freshness;
} beacon_weather_slot_t;

typedef struct {
    char slot[12];
    char time[8];
    bool umbrella_known;
    bool umbrella_required;
    char reason[36];
} beacon_next_outing_t;

typedef struct {
    char location[16];
    char provider[16];
    beacon_weather_current_t current;
    beacon_weather_slot_t lunch;
    beacon_weather_slot_t leave;
    beacon_next_outing_t next_outing;
} beacon_weather_state_t;

typedef struct {
    char timezone[32];
    char display_time[8];
    bool bridge_online;
    beacon_freshness_t overall_freshness;
    uint64_t revision;
} beacon_system_state_t;

typedef struct {
    beacon_codex_state_t codex;
    beacon_agents_state_t agents;
    beacon_weather_state_t weather;
    beacon_system_state_t system;
} beacon_app_state_t;

void beacon_app_state_init_mock(beacon_app_state_t *state);
void beacon_app_state_apply(beacon_app_state_t *destination,
                            const beacon_app_state_t *patch,
                            uint8_t domains, uint64_t revision);
const char *beacon_agent_status_label(beacon_agent_status_t status);
