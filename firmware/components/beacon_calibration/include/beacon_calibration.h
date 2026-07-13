#pragma once

#include <stddef.h>
#include <stdint.h>

typedef struct {
    const char *name;
    uint16_t rgb565;
    uint32_t hold_ms;
} beacon_calibration_step_t;

size_t beacon_calibration_count(void);
const beacon_calibration_step_t *beacon_calibration_step(size_t index);
size_t beacon_calibration_next(size_t current_index);
