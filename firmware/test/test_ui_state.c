#include <assert.h>

#include "beacon_app_state.h"
#include "beacon_ui_state.h"

static void test_page_domain_visibility(void)
{
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                               BEACON_STATE_DOMAIN_CODEX, false));
    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                                BEACON_STATE_DOMAIN_AGENTS, false));
    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                                BEACON_STATE_DOMAIN_WEATHER, false));

    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_AGENTS,
                                               BEACON_STATE_DOMAIN_AGENTS, false));
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_WEATHER,
                                               BEACON_STATE_DOMAIN_WEATHER, false));

    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                                BEACON_STATE_DOMAIN_SYSTEM, false));
    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_AGENTS,
                                                BEACON_STATE_DOMAIN_SYSTEM, false));
    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_WEATHER,
                                                BEACON_STATE_DOMAIN_SYSTEM, false));

    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                               BEACON_STATE_DOMAIN_SYSTEM, true));
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_AGENTS,
                                               BEACON_STATE_DOMAIN_SYSTEM, true));
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_WEATHER,
                                               BEACON_STATE_DOMAIN_SYSTEM, true));

    assert(!beacon_ui_page_affected_by_domains(
        BEACON_PAGE_CODEX,
        BEACON_STATE_DOMAIN_AGENTS | BEACON_STATE_DOMAIN_SYSTEM, false));
    assert(beacon_ui_page_affected_by_domains(
        BEACON_PAGE_AGENTS,
        BEACON_STATE_DOMAIN_AGENTS | BEACON_STATE_DOMAIN_SYSTEM, false));

    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                                BEACON_STATE_DOMAIN_CLOCK, false));
    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_COUNT,
                                                BEACON_STATE_DOMAIN_ALL, true));
}

static void test_system_header_change_detection(void)
{
    beacon_system_state_t current = {
        .bridge_online = true,
        .overall_freshness = BEACON_FRESHNESS_FRESH,
        .revision = 10,
    };
    beacon_system_state_t incoming = current;

    incoming.revision = 11;
    assert(!beacon_ui_system_status_changed(&current, &incoming));

    incoming.bridge_online = false;
    assert(beacon_ui_system_status_changed(&current, &incoming));
    incoming = current;
    incoming.overall_freshness = BEACON_FRESHNESS_STALE;
    assert(beacon_ui_system_status_changed(&current, &incoming));

    assert(!beacon_ui_system_status_changed(NULL, &incoming));
    assert(!beacon_ui_system_status_changed(&current, NULL));
}

static void test_online_status_requires_live_transport(void)
{
    bool snapshot_ready = false;

    assert(!beacon_ui_connection_is_online(true, false, snapshot_ready));

    snapshot_ready = beacon_ui_connection_snapshot_ready(snapshot_ready, true, false);
    assert(!snapshot_ready);
    assert(!beacon_ui_connection_is_online(true, true, snapshot_ready));

    snapshot_ready = beacon_ui_connection_snapshot_ready(snapshot_ready, true, true);
    assert(snapshot_ready);
    assert(beacon_ui_connection_is_online(true, true, snapshot_ready));

    snapshot_ready = beacon_ui_connection_snapshot_ready(snapshot_ready, false, false);
    assert(!snapshot_ready);
    assert(!beacon_ui_connection_is_online(true, false, snapshot_ready));

    snapshot_ready = beacon_ui_connection_snapshot_ready(snapshot_ready, true, false);
    assert(!snapshot_ready);
    assert(!beacon_ui_connection_is_online(true, true, snapshot_ready));

    snapshot_ready = beacon_ui_connection_snapshot_ready(snapshot_ready, true, true);
    assert(snapshot_ready);
    assert(beacon_ui_connection_is_online(true, true, snapshot_ready));
    assert(!beacon_ui_connection_is_online(false, true, snapshot_ready));
}

static void test_carousel_and_manual_navigation(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);

    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(state.page == BEACON_PAGE_CODEX);
    assert(state.carousel_remaining_ms == 8000);

    assert(beacon_ui_state_tick(&state, 7999) == false);
    assert(state.page == BEACON_PAGE_CODEX);
    assert(state.carousel_remaining_ms == 1);

    assert(beacon_ui_state_tick(&state, 1) == true);
    assert(state.page == BEACON_PAGE_AGENTS);
    assert(state.carousel_remaining_ms == 6000);

    beacon_ui_state_next_page(&state);
    assert(state.page == BEACON_PAGE_WEATHER);
    assert(state.carousel_remaining_ms == 8000);
}

static void test_notification_restores_interrupted_page(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);
    beacon_ui_state_tick(&state, 2000);

    beacon_ui_state_show_notification(&state, BEACON_THEME_GREEN, 4000);
    assert(state.mode == BEACON_UI_NOTIFICATION);
    assert(state.theme == BEACON_THEME_GREEN);
    assert(state.notification_remaining_ms == 4000);

    assert(beacon_ui_state_tick(&state, 3999) == false);
    assert(state.mode == BEACON_UI_NOTIFICATION);
    assert(beacon_ui_state_tick(&state, 1) == true);
    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(state.page == BEACON_PAGE_CODEX);
    assert(state.carousel_remaining_ms == 6000);

    beacon_ui_state_tick(&state, 6000);
    assert(state.page == BEACON_PAGE_AGENTS);
}

static void test_elapsed_time_carries_across_boundaries(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);
    beacon_ui_state_show_notification(&state, BEACON_THEME_RED, 1000);

    assert(beacon_ui_state_tick(&state, 9100) == true);
    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(state.page == BEACON_PAGE_AGENTS);
    assert(state.carousel_remaining_ms == 5900);

    beacon_ui_state_next_page(&state);
    beacon_ui_state_next_page(&state);
    assert(state.page == BEACON_PAGE_CODEX);
}

static void test_diagnostics_is_not_in_carousel(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);
    beacon_ui_state_enter_diagnostics(&state);
    assert(state.mode == BEACON_UI_DIAGNOSTICS);
    assert(beacon_ui_state_tick(&state, 30000) == false);
    assert(state.page == BEACON_PAGE_CODEX);
    beacon_ui_state_exit_diagnostics(&state);
    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(state.carousel_remaining_ms == 8000);
}

int main(void)
{
    test_page_domain_visibility();
    test_system_header_change_detection();
    test_online_status_requires_live_transport();
    test_carousel_and_manual_navigation();
    test_notification_restores_interrupted_page();
    test_elapsed_time_carries_across_boundaries();
    test_diagnostics_is_not_in_carousel();
    return 0;
}
