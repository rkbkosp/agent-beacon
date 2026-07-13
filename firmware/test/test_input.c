#include <assert.h>

#include "beacon_button.h"

static beacon_button_event_t sample(beacon_button_t *button, bool pressed, uint32_t elapsed_ms)
{
    return beacon_button_update(button, pressed, elapsed_ms);
}

static void press_and_release(beacon_button_t *button)
{
    assert(sample(button, true, 20) == BEACON_BUTTON_NONE);
    assert(sample(button, true, 20) == BEACON_BUTTON_NONE);
    assert(sample(button, false, 20) == BEACON_BUTTON_NONE);
    assert(sample(button, false, 20) == BEACON_BUTTON_NONE);
}

static void test_short_press_waits_for_double_click_window(void)
{
    beacon_button_t button;
    beacon_button_init(&button, 30, 350, 2000, 5000);
    press_and_release(&button);
    assert(sample(&button, false, 349) == BEACON_BUTTON_NONE);
    assert(sample(&button, false, 1) == BEACON_BUTTON_SHORT_PRESS);
}

static void test_double_press_fires_without_short_press(void)
{
    beacon_button_t button;
    beacon_button_init(&button, 30, 350, 2000, 5000);
    press_and_release(&button);
    assert(sample(&button, false, 100) == BEACON_BUTTON_NONE);
    assert(sample(&button, true, 20) == BEACON_BUTTON_NONE);
    assert(sample(&button, true, 20) == BEACON_BUTTON_NONE);
    assert(sample(&button, false, 20) == BEACON_BUTTON_NONE);
    assert(sample(&button, false, 20) == BEACON_BUTTON_DOUBLE_PRESS);
    assert(sample(&button, false, 400) == BEACON_BUTTON_NONE);
}

static void test_two_and_five_second_holds_fire_once(void)
{
    beacon_button_t button;
    beacon_button_init(&button, 30, 350, 2000, 5000);
    sample(&button, true, 30);
    sample(&button, true, 30);
    assert(sample(&button, true, 1969) == BEACON_BUTTON_NONE);
    assert(sample(&button, true, 1) == BEACON_BUTTON_LONG_2S);
    assert(sample(&button, true, 2999) == BEACON_BUTTON_NONE);
    assert(sample(&button, true, 1) == BEACON_BUTTON_LONG_5S);
    assert(sample(&button, true, 1000) == BEACON_BUTTON_NONE);
    sample(&button, false, 30);
    assert(sample(&button, false, 30) == BEACON_BUTTON_NONE);
}

static void test_bounce_does_not_create_event(void)
{
    beacon_button_t button;
    beacon_button_init(&button, 30, 350, 2000, 5000);
    assert(sample(&button, true, 10) == BEACON_BUTTON_NONE);
    assert(sample(&button, false, 10) == BEACON_BUTTON_NONE);
    assert(sample(&button, true, 10) == BEACON_BUTTON_NONE);
    assert(sample(&button, false, 400) == BEACON_BUTTON_NONE);
}

int main(void)
{
    test_short_press_waits_for_double_click_window();
    test_double_press_fires_without_short_press();
    test_two_and_five_second_holds_fire_once();
    test_bounce_does_not_create_event();
    return 0;
}
