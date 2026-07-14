#include <assert.h>

#include "beacon_app_state.h"
#include "beacon_ui_state.h"

static void test_page_domain_visibility(void)
{
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                               BEACON_STATE_DOMAIN_CODEX));
    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                                BEACON_STATE_DOMAIN_AGENTS));
    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                                BEACON_STATE_DOMAIN_WEATHER));

    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_AGENTS,
                                               BEACON_STATE_DOMAIN_AGENTS));
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_WEATHER,
                                               BEACON_STATE_DOMAIN_WEATHER));

    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                               BEACON_STATE_DOMAIN_SYSTEM));
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_AGENTS,
                                               BEACON_STATE_DOMAIN_SYSTEM));
    assert(beacon_ui_page_affected_by_domains(BEACON_PAGE_WEATHER,
                                               BEACON_STATE_DOMAIN_SYSTEM));

    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_CODEX,
                                                BEACON_STATE_DOMAIN_CLOCK));
    assert(!beacon_ui_page_affected_by_domains(BEACON_PAGE_COUNT,
                                                BEACON_STATE_DOMAIN_ALL));
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
    test_carousel_and_manual_navigation();
    test_notification_restores_interrupted_page();
    test_elapsed_time_carries_across_boundaries();
    test_diagnostics_is_not_in_carousel();
    return 0;
}
