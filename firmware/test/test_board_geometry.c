#include <assert.h>

#include "board_ws_147b_geometry.h"

int main(void)
{
    const board_ws_147b_geometry_t *geometry = board_ws_147b_native_geometry();

    assert(geometry->logical_width == 320);
    assert(geometry->logical_height == 172);
    assert(geometry->panel_width == 172);
    assert(geometry->panel_height == 320);
    assert(geometry->x_gap == 34);
    assert(geometry->y_gap == 0);
    assert(geometry->swap_xy == false);
    assert(geometry->mirror_x == true);
    assert(geometry->mirror_y == false);
    return 0;
}
