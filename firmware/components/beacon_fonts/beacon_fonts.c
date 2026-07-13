#include "beacon_fonts.h"

#include <stddef.h>
#include <stdint.h>

#include "esp_heap_caps.h"
#include "esp_log.h"
#include "src/extra/libs/tiny_ttf/lv_tiny_ttf.h"

extern const uint8_t milan_medium_start[]
    asm("_binary_MiLanPro_Medium_400_ttf_start");
extern const uint8_t milan_medium_end[]
    asm("_binary_MiLanPro_Medium_400_ttf_end");
extern const uint8_t milan_semibold_start[]
    asm("_binary_MiLanPro_SemiBold_540_ttf_start");
extern const uint8_t milan_semibold_end[]
    asm("_binary_MiLanPro_SemiBold_540_ttf_end");

static const char *TAG = "beacon_fonts";
static const size_t GLYPH_CACHE_BYTES = 4 * 1024;

static lv_font_t *medium_14;
static lv_font_t *medium_18;
static lv_font_t *semibold_14;
static lv_font_t *semibold_18;
static lv_font_t *semibold_24;

static lv_font_t *init_font(const uint8_t *start, const uint8_t *end,
                            uint16_t size)
{
    return lv_tiny_ttf_create_data_ex(start, (size_t)(end - start), size,
                                      GLYPH_CACHE_BYTES);
}

esp_err_t beacon_fonts_init(void)
{
    const size_t psram_before = heap_caps_get_free_size(MALLOC_CAP_SPIRAM);
    medium_14 = init_font(milan_medium_start, milan_medium_end, 14);
    medium_18 = init_font(milan_medium_start, milan_medium_end, 18);
    semibold_14 = init_font(milan_semibold_start, milan_semibold_end, 14);
    semibold_18 = init_font(milan_semibold_start, milan_semibold_end, 18);
    semibold_24 = init_font(milan_semibold_start, milan_semibold_end, 24);
    if (!medium_14 || !medium_18 || !semibold_14 || !semibold_18 ||
        !semibold_24) {
        ESP_LOGE(TAG, "Milan TinyTTF initialization failed");
        return ESP_FAIL;
    }

    const size_t psram_after = heap_caps_get_free_size(MALLOC_CAP_SPIRAM);
    ESP_LOGI(TAG,
             "Milan TinyTTF ready: medium=%u bytes semibold=%u bytes PSRAM=%u -> %u",
             (unsigned)(milan_medium_end - milan_medium_start),
             (unsigned)(milan_semibold_end - milan_semibold_start),
             (unsigned)psram_before, (unsigned)psram_after);
    return ESP_OK;
}

const lv_font_t *beacon_font_medium_14(void) { return medium_14; }
const lv_font_t *beacon_font_medium_18(void) { return medium_18; }
const lv_font_t *beacon_font_semibold_14(void) { return semibold_14; }
const lv_font_t *beacon_font_semibold_18(void) { return semibold_18; }
const lv_font_t *beacon_font_semibold_24(void) { return semibold_24; }
