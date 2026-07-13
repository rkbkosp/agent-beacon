#include <assert.h>
#include <stdint.h>
#include <string.h>

#include "beacon_calibration.h"

int main(void)
{
    static const char *const expected_names[] = {
        "RED",
        "YELLOW",
        "BLUE",
        "GREEN",
    };
    static const uint16_t expected_colors[] = {
        0xf800,
        0xffe0,
        0x001f,
        0x07e0,
    };

    assert(beacon_calibration_count() == 4);
    for (size_t index = 0; index < beacon_calibration_count(); ++index) {
        const beacon_calibration_step_t *step = beacon_calibration_step(index);
        assert(step != NULL);
        assert(strcmp(step->name, expected_names[index]) == 0);
        assert(step->rgb565 == expected_colors[index]);
        assert(step->hold_ms == 2000);
        assert(beacon_calibration_next(index) == (index + 1) % 4);
    }

    assert(beacon_calibration_step(4) == NULL);
    assert(beacon_calibration_next(99) == 0);
    return 0;
}
