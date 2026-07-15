#include "beacon_diagnostics.h"

#include <stdint.h>

uint8_t beacon_diagnostics_cpu_usage_percent(uint64_t elapsed_us,
                                             uint64_t idle_elapsed_us,
                                             size_t core_count)
{
    if (elapsed_us == 0U || core_count == 0U ||
        elapsed_us > UINT64_MAX / core_count) {
        return 0;
    }

    const uint64_t available_cpu_us = elapsed_us * core_count;
    if (idle_elapsed_us >= available_cpu_us) {
        return 0;
    }

    const uint64_t busy_cpu_us = available_cpu_us - idle_elapsed_us;
    const uint64_t rounded_percent =
        (busy_cpu_us * 100U + available_cpu_us / 2U) / available_cpu_us;
    return rounded_percent > 100U ? 100U : (uint8_t)rounded_percent;
}
