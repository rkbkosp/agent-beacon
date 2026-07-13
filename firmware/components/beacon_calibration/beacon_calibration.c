#include "beacon_calibration.h"

static const beacon_calibration_step_t STEPS[] = {
    {.name = "RED", .rgb565 = 0xf800, .hold_ms = 2000},
    {.name = "YELLOW", .rgb565 = 0xffe0, .hold_ms = 2000},
    {.name = "BLUE", .rgb565 = 0x001f, .hold_ms = 2000},
    {.name = "GREEN", .rgb565 = 0x07e0, .hold_ms = 2000},
};

size_t beacon_calibration_count(void)
{
    return sizeof(STEPS) / sizeof(STEPS[0]);
}

const beacon_calibration_step_t *beacon_calibration_step(size_t index)
{
    if (index >= beacon_calibration_count()) {
        return NULL;
    }
    return &STEPS[index];
}

size_t beacon_calibration_next(size_t current_index)
{
    if (current_index >= beacon_calibration_count()) {
        return 0;
    }
    return (current_index + 1) % beacon_calibration_count();
}
