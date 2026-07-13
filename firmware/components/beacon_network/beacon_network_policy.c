#include "beacon_network.h"

uint32_t beacon_network_backoff_ms(size_t attempt, uint32_t jitter_ms)
{
    static const uint32_t BACKOFF_MS[] = {1000, 2000, 4000, 8000, 15000, 30000};
    const size_t last = sizeof(BACKOFF_MS) / sizeof(BACKOFF_MS[0]) - 1U;
    const uint32_t base = BACKOFF_MS[attempt < last ? attempt : last];
    const uint32_t maximum_jitter = base / 4U;
    const uint32_t bounded_jitter = jitter_ms < maximum_jitter ? jitter_ms : maximum_jitter;
    const uint32_t delay = base + bounded_jitter;
    return delay < 30000U ? delay : 30000U;
}

beacon_network_freshness_t beacon_network_freshness(int64_t now_ms, int64_t last_update_ms)
{
    if (last_update_ms <= 0 || now_ms < last_update_ms) {
        return BEACON_NETWORK_OFFLINE;
    }
    const int64_t age_ms = now_ms - last_update_ms;
    if (age_ms < 120000) {
        return BEACON_NETWORK_ONLINE;
    }
    if (age_ms < 600000) {
        return BEACON_NETWORK_STALE;
    }
    return BEACON_NETWORK_OFFLINE;
}
