#include <assert.h>
#include <stdint.h>

#include "beacon_diagnostics.h"

int main(void)
{
    assert(beacon_diagnostics_cpu_usage_percent(1000000U, 0U, 2U) == 100U);
    assert(beacon_diagnostics_cpu_usage_percent(1000000U, 500000U, 2U) == 75U);
    assert(beacon_diagnostics_cpu_usage_percent(1000000U, 1000000U, 2U) == 50U);
    assert(beacon_diagnostics_cpu_usage_percent(1000000U, 1500000U, 2U) == 25U);
    assert(beacon_diagnostics_cpu_usage_percent(1000000U, 2000000U, 2U) == 0U);
    assert(beacon_diagnostics_cpu_usage_percent(1000000U, 2100000U, 2U) == 0U);
    assert(beacon_diagnostics_cpu_usage_percent(0U, 0U, 2U) == 0U);
    assert(beacon_diagnostics_cpu_usage_percent(1000000U, 0U, 0U) == 0U);
    return 0;
}
