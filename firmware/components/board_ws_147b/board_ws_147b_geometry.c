#include "board_ws_147b_geometry.h"

static const board_ws_147b_geometry_t NATIVE_GEOMETRY = {
    .logical_width = 320,
    .logical_height = 172,
    .panel_width = 172,
    .panel_height = 320,
    .x_gap = 34,
    .y_gap = 0,
    .swap_xy = false,
    .mirror_x = true,
    .mirror_y = false,
};

const board_ws_147b_geometry_t *board_ws_147b_native_geometry(void)
{
    return &NATIVE_GEOMETRY;
}
