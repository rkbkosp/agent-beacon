#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

typedef struct {
    bool soc_temperature_available;
    int16_t soc_temperature_tenths_c;
    bool cpu_usage_available;
    uint8_t cpu_usage_percent;
} beacon_diagnostics_snapshot_t;

void beacon_diagnostics_init(void);
void beacon_diagnostics_sample(beacon_diagnostics_snapshot_t *snapshot);

uint8_t beacon_diagnostics_cpu_usage_percent(uint64_t elapsed_us,
                                             uint64_t idle_elapsed_us,
                                             size_t core_count);
