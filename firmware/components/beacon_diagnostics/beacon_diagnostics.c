#include "beacon_diagnostics.h"

#include <string.h>

#include "driver/temperature_sensor.h"
#include "esp_err.h"
#include "esp_ipc.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "sdkconfig.h"

#if defined(CONFIG_FREERTOS_GENERATE_RUN_TIME_STATS) && !CONFIG_FREERTOS_SMP
#include "freertos/idf_additions.h"
#define BEACON_CPU_USAGE_SUPPORTED 1
#else
#define BEACON_CPU_USAGE_SUPPORTED 0
#endif

static const char *TAG = "beacon_diagnostics";
static temperature_sensor_handle_t temperature_sensor;
static bool first_snapshot_logged;

#if BEACON_CPU_USAGE_SUPPORTED
static configRUN_TIME_COUNTER_TYPE previous_idle_runtime[CONFIG_FREERTOS_NUMBER_OF_CORES];
static int64_t previous_cpu_sample_us;
static bool cpu_baseline_ready;

typedef struct {
    BaseType_t core;
    configRUN_TIME_COUNTER_TYPE idle_runtime;
} idle_runtime_request_t;

static void read_idle_runtime_on_core(void *argument)
{
    idle_runtime_request_t *request = argument;
    request->idle_runtime = ulTaskGetIdleRunTimeCounterForCore(request->core);
}

static bool read_idle_runtimes(configRUN_TIME_COUNTER_TYPE *idle_runtimes)
{
    const BaseType_t current_core = xPortGetCoreID();
    for (BaseType_t core = 0; core < CONFIG_FREERTOS_NUMBER_OF_CORES; ++core) {
        if (core == current_core) {
            idle_runtimes[core] = ulTaskGetIdleRunTimeCounterForCore(core);
            continue;
        }

        idle_runtime_request_t request = {.core = core};
        if (esp_ipc_call_blocking((uint32_t)core, read_idle_runtime_on_core,
                                  &request) != ESP_OK) {
            return false;
        }
        idle_runtimes[core] = request.idle_runtime;
    }
    return true;
}
#endif

static int16_t temperature_to_tenths(float celsius)
{
    const float scaled = celsius * 10.0f;
    return (int16_t)(scaled + (scaled >= 0.0f ? 0.5f : -0.5f));
}

void beacon_diagnostics_init(void)
{
    const temperature_sensor_config_t config =
        TEMPERATURE_SENSOR_CONFIG_DEFAULT(-10, 80);
    esp_err_t error = temperature_sensor_install(&config, &temperature_sensor);
    if (error == ESP_OK) {
        error = temperature_sensor_enable(temperature_sensor);
    }
    if (error != ESP_OK) {
        ESP_LOGW(TAG, "SoC temperature sensor unavailable: %s",
                 esp_err_to_name(error));
        if (temperature_sensor != NULL) {
            (void)temperature_sensor_uninstall(temperature_sensor);
            temperature_sensor = NULL;
        }
    }

#if BEACON_CPU_USAGE_SUPPORTED
    if (read_idle_runtimes(previous_idle_runtime)) {
        previous_cpu_sample_us = esp_timer_get_time();
        cpu_baseline_ready = true;
    } else {
        ESP_LOGW(TAG, "CPU usage baseline unavailable");
    }
#else
    ESP_LOGW(TAG, "CPU usage unavailable: FreeRTOS run-time stats are disabled");
#endif
}

void beacon_diagnostics_sample(beacon_diagnostics_snapshot_t *snapshot)
{
    if (snapshot == NULL) {
        return;
    }
    memset(snapshot, 0, sizeof(*snapshot));

    if (temperature_sensor != NULL) {
        float celsius = 0.0f;
        if (temperature_sensor_get_celsius(temperature_sensor, &celsius) == ESP_OK) {
            snapshot->soc_temperature_available = true;
            snapshot->soc_temperature_tenths_c = temperature_to_tenths(celsius);
        }
    }

#if BEACON_CPU_USAGE_SUPPORTED
    configRUN_TIME_COUNTER_TYPE current_idle_runtime[CONFIG_FREERTOS_NUMBER_OF_CORES];
    if (!read_idle_runtimes(current_idle_runtime)) {
        return;
    }
    const int64_t current_time_us = esp_timer_get_time();
    uint64_t idle_elapsed_us = 0U;
    for (BaseType_t core = 0; core < CONFIG_FREERTOS_NUMBER_OF_CORES; ++core) {
        idle_elapsed_us +=
            (uint64_t)(current_idle_runtime[core] - previous_idle_runtime[core]);
        previous_idle_runtime[core] = current_idle_runtime[core];
    }

    if (cpu_baseline_ready && current_time_us > previous_cpu_sample_us) {
        snapshot->cpu_usage_available = true;
        snapshot->cpu_usage_percent = beacon_diagnostics_cpu_usage_percent(
            (uint64_t)(current_time_us - previous_cpu_sample_us), idle_elapsed_us,
            CONFIG_FREERTOS_NUMBER_OF_CORES);
    }
    previous_cpu_sample_us = current_time_us;
    cpu_baseline_ready = true;
#endif

    if (!first_snapshot_logged && snapshot->soc_temperature_available &&
        snapshot->cpu_usage_available) {
        const int temperature_tenths = snapshot->soc_temperature_tenths_c;
        const unsigned int magnitude =
            (unsigned int)(temperature_tenths < 0 ? -temperature_tenths : temperature_tenths);
        ESP_LOGI(TAG, "Metrics ready: SoC=%s%u.%u C CPU=%u%%",
                 temperature_tenths < 0 ? "-" : "", magnitude / 10U,
                 magnitude % 10U, (unsigned int)snapshot->cpu_usage_percent);
        first_snapshot_logged = true;
    }
}
