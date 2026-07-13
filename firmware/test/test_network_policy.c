#include <assert.h>

#include "beacon_network.h"

static void test_websocket_runtime_capacity(void)
{
    assert(BEACON_WEBSOCKET_TASK_STACK_SIZE >= 8192);
    assert(BEACON_NETWORK_MESSAGE_MAX == 64U * 1024U);
    assert(BEACON_WEBSOCKET_BUFFER_SIZE >= 4096U);
    assert(BEACON_WEBSOCKET_BUFFER_SIZE < BEACON_NETWORK_MESSAGE_MAX);
}

static void test_reconnect_backoff(void)
{
    const uint32_t expected[] = {1000, 2000, 4000, 8000, 15000, 30000, 30000};
    for (size_t i = 0; i < sizeof(expected) / sizeof(expected[0]); ++i) {
        assert(beacon_network_backoff_ms(i, 0) == expected[i]);
    }
    assert(beacon_network_backoff_ms(2, 500) == 4500);
    assert(beacon_network_backoff_ms(2, 5000) == 5000);
    assert(beacon_network_backoff_ms(6, 5000) == 30000);
}

static void test_freshness_thresholds(void)
{
    assert(beacon_network_freshness(1000000, 950000) == BEACON_NETWORK_ONLINE);
    assert(beacon_network_freshness(1000000, 880001) == BEACON_NETWORK_ONLINE);
    assert(beacon_network_freshness(1000000, 880000) == BEACON_NETWORK_STALE);
    assert(beacon_network_freshness(1000000, 400001) == BEACON_NETWORK_STALE);
    assert(beacon_network_freshness(1000000, 400000) == BEACON_NETWORK_OFFLINE);
    assert(beacon_network_freshness(1000000, 0) == BEACON_NETWORK_OFFLINE);
}

int main(void)
{
    test_websocket_runtime_capacity();
    test_reconnect_backoff();
    test_freshness_thresholds();
    return 0;
}
