#include <assert.h>

#include "beacon_app_state.h"
#include "beacon_ui_state.h"

static void test_page_domain_visibility(void)
{
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                               BEACON_STATE_DOMAIN_CODEX, false));
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_TOKEN_RATE,
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

static void test_carousel_and_manual_navigation(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);

    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(!state.codex_active);
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
    beacon_ui_state_next_page(&state);
    assert(state.page == BEACON_PAGE_CODEX);
    assert(state.carousel_remaining_ms == 8000);
}

static void test_codex_activation_jumps_for_fifteen_seconds(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);
    assert(beacon_ui_state_tick(&state, 8000));
    assert(state.page == BEACON_PAGE_AGENTS);
    assert(beacon_ui_state_tick(&state, 6000));
    assert(state.page == BEACON_PAGE_WEATHER);

    assert(beacon_ui_state_set_codex_active(&state, true));
    assert(state.codex_active);
    assert(state.page == BEACON_PAGE_TOKEN_RATE);
    assert(state.carousel_remaining_ms == 15000);

    assert(!beacon_ui_state_tick(&state, 14999));
    assert(state.page == BEACON_PAGE_TOKEN_RATE);
    assert(state.carousel_remaining_ms == 1);
    assert(!beacon_ui_state_set_codex_active(&state, true));
    assert(state.carousel_remaining_ms == 1);

    assert(beacon_ui_state_tick(&state, 1));
    assert(state.page == BEACON_PAGE_AGENTS);
    assert(state.carousel_remaining_ms == 6000);
    beacon_ui_state_next_page(&state);
    assert(state.page == BEACON_PAGE_WEATHER);
    beacon_ui_state_next_page(&state);
    assert(state.page == BEACON_PAGE_TOKEN_RATE);
    assert(state.carousel_remaining_ms == 15000);
}

static void test_codex_deactivation_restores_quota_dashboard(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);
    assert(beacon_ui_state_set_codex_active(&state, true));
    assert(beacon_ui_state_set_codex_active(&state, false));
    assert(!state.codex_active);
    assert(state.page == BEACON_PAGE_CODEX);
    assert(state.carousel_remaining_ms == 8000);

    assert(beacon_ui_state_set_codex_active(&state, true));
    assert(beacon_ui_state_tick(&state, 15000));
    assert(state.page == BEACON_PAGE_AGENTS);
    assert(beacon_ui_state_tick(&state, 6000));
    assert(state.page == BEACON_PAGE_WEATHER);
    assert(!beacon_ui_state_set_codex_active(&state, false));
    assert(state.page == BEACON_PAGE_WEATHER);
    assert(state.carousel_remaining_ms == 8000);
    beacon_ui_state_next_page(&state);
    assert(state.page == BEACON_PAGE_CODEX);
    assert(state.carousel_remaining_ms == 8000);
}

static void test_token_rate_pin_stops_and_resumes_carousel(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);

    beacon_ui_state_pin_token_rate(&state);
    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(state.carousel_paused);
    assert(state.page == BEACON_PAGE_TOKEN_RATE);
    assert(state.carousel_remaining_ms == 15000);

    assert(!beacon_ui_state_tick(&state, 60000));
    assert(state.page == BEACON_PAGE_TOKEN_RATE);
    assert(state.carousel_remaining_ms == 15000);
    beacon_ui_state_next_page(&state);
    assert(state.page == BEACON_PAGE_TOKEN_RATE);

    assert(!beacon_ui_state_set_codex_active(&state, true));
    assert(!beacon_ui_state_set_codex_active(&state, false));
    assert(state.page == BEACON_PAGE_TOKEN_RATE);

    beacon_ui_state_resume_carousel(&state);
    assert(!state.carousel_paused);
    assert(state.page == BEACON_PAGE_CODEX);
    assert(state.carousel_remaining_ms == 8000);
    assert(!beacon_ui_state_tick(&state, 7999));
    assert(state.page == BEACON_PAGE_CODEX);
    assert(beacon_ui_state_tick(&state, 1));
    assert(state.page == BEACON_PAGE_AGENTS);
    assert(state.carousel_remaining_ms == 6000);
}

static void test_notification_restores_pinned_token_rate_page(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);
    beacon_ui_state_pin_token_rate(&state);
    beacon_ui_state_show_notification(&state, BEACON_THEME_BLUE, 1000);

    assert(beacon_ui_state_tick(&state, 1000));
    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(state.carousel_paused);
    assert(state.page == BEACON_PAGE_TOKEN_RATE);
    assert(state.carousel_remaining_ms == 15000);
}

static void test_notification_restores_interrupted_page(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);
    assert(beacon_ui_state_set_codex_active(&state, true));
    beacon_ui_state_tick(&state, 2000);

    beacon_ui_state_show_notification(&state, BEACON_THEME_GREEN, 4000);
    assert(state.mode == BEACON_UI_NOTIFICATION);
    assert(state.theme == BEACON_THEME_GREEN);
    assert(state.notification_remaining_ms == 4000);

    assert(beacon_ui_state_tick(&state, 3999) == false);
    assert(state.mode == BEACON_UI_NOTIFICATION);
    assert(beacon_ui_state_tick(&state, 1) == true);
    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(state.page == BEACON_PAGE_TOKEN_RATE);
    assert(state.carousel_remaining_ms == 13000);

    beacon_ui_state_tick(&state, 13000);
    assert(state.page == BEACON_PAGE_AGENTS);
}

static void test_activation_waits_for_notification_overlay(void)
{
    beacon_ui_state_t state;
    beacon_ui_state_init(&state);
    beacon_ui_state_show_notification(&state, BEACON_THEME_GREEN, 4000);

    assert(!beacon_ui_state_set_codex_active(&state, true));
    assert(state.mode == BEACON_UI_NOTIFICATION);
    assert(state.saved_page == BEACON_PAGE_TOKEN_RATE);
    assert(state.saved_carousel_remaining_ms == 15000);
    assert(beacon_ui_state_tick(&state, 4000));
    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(state.page == BEACON_PAGE_TOKEN_RATE);
    assert(state.carousel_remaining_ms == 15000);

    beacon_ui_state_show_notification(&state, BEACON_THEME_GREEN, 4000);
    assert(!beacon_ui_state_set_codex_active(&state, false));
    assert(state.saved_page == BEACON_PAGE_CODEX);
    assert(state.saved_carousel_remaining_ms == 8000);
    assert(beacon_ui_state_tick(&state, 4000));
    assert(state.page == BEACON_PAGE_CODEX);
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
    assert(!beacon_ui_state_set_codex_active(&state, true));
    assert(state.page == BEACON_PAGE_TOKEN_RATE);
    beacon_ui_state_exit_diagnostics(&state);
    assert(state.mode == BEACON_UI_CAROUSEL);
    assert(state.carousel_remaining_ms == 15000);
}

int main(void)
{
    test_page_domain_visibility();
    test_system_header_change_detection();
    test_carousel_and_manual_navigation();
    test_codex_activation_jumps_for_fifteen_seconds();
    test_codex_deactivation_restores_quota_dashboard();
    test_token_rate_pin_stops_and_resumes_carousel();
    test_notification_restores_pinned_token_rate_page();
    test_notification_restores_interrupted_page();
    test_activation_waits_for_notification_overlay();
    test_elapsed_time_carries_across_boundaries();
    test_diagnostics_is_not_in_carousel();
    return 0;
}
