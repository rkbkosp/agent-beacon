#pragma once

#include <stdbool.h>
#include <stdint.h>

typedef struct {
    uint16_t logical_width;
    uint16_t logical_height;
    uint16_t panel_width;
    uint16_t panel_height;
    uint16_t x_gap;
    uint16_t y_gap;
    bool swap_xy;
    bool mirror_x;
    bool mirror_y;
} board_ws_147b_geometry_t;

const board_ws_147b_geometry_t *board_ws_147b_native_geometry(void);
