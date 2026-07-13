#include "board_ws_147b.h"
#include "board_ws_147b_geometry.h"

#include <stddef.h>

#include "driver/gpio.h"
#include "driver/ledc.h"
#include "driver/spi_master.h"
#include "esp_check.h"
#include "esp_heap_caps.h"
#include "esp_lcd_panel_commands.h"
#include "esp_lcd_panel_io.h"
#include "esp_lcd_panel_ops.h"
#include "esp_lcd_panel_vendor.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define LCD_HOST SPI3_HOST
#define LCD_PIXEL_CLOCK_HZ (12 * 1000 * 1000)
#define LCD_PIN_SCLK 40
#define LCD_PIN_MOSI 45
#define LCD_PIN_DC 41
#define LCD_PIN_RST 39
#define LCD_PIN_CS 42
#define LCD_PIN_BACKLIGHT 46
#define BOOT_PIN 0

#define BACKLIGHT_TIMER LEDC_TIMER_0
#define BACKLIGHT_MODE LEDC_LOW_SPEED_MODE
#define BACKLIGHT_CHANNEL LEDC_CHANNEL_0
#define BACKLIGHT_DUTY_RES LEDC_TIMER_13_BIT
#define BACKLIGHT_FREQUENCY_HZ 4000
#define BACKLIGHT_MAX_DUTY ((1U << 13) - 1U)

typedef struct {
    uint8_t command;
    uint8_t data[14];
    uint8_t data_size;
    uint16_t delay_ms;
} lcd_init_command_t;

// Adapted from Espressif's Apache-2.0 ST7789T example; see THIRD_PARTY_NOTICES.md.
static const lcd_init_command_t LCD_INIT_COMMANDS[] = {
    {.command = 0x36, .data = {0x00}, .data_size = 1},
    {.command = 0x3a, .data = {0x55}, .data_size = 1},
    {.command = 0xb0, .data = {0x00, 0xe8}, .data_size = 2},
    {.command = 0xb2, .data = {0x0c, 0x0c, 0x00, 0x33, 0x33}, .data_size = 5},
    {.command = 0xb7, .data = {0x75}, .data_size = 1},
    {.command = 0xbb, .data = {0x1a}, .data_size = 1},
    {.command = 0xc0, .data = {0x80}, .data_size = 1},
    {.command = 0xc2, .data = {0x01, 0xff}, .data_size = 2},
    {.command = 0xc3, .data = {0x13}, .data_size = 1},
    {.command = 0xc4, .data = {0x20}, .data_size = 1},
    {.command = 0xc6, .data = {0x0f}, .data_size = 1},
    {.command = 0xd0, .data = {0xa4}, .data_size = 1},
    {.command = 0xe0,
     .data = {0xd0, 0x0d, 0x14, 0x0d, 0x0d, 0x09, 0x38, 0x44, 0x4e, 0x3a, 0x17, 0x18, 0x2f, 0x30},
     .data_size = 14},
    {.command = 0xe1,
     .data = {0xd0, 0x09, 0x0f, 0x08, 0x07, 0x14, 0x37, 0x44, 0x4d, 0x38, 0x15, 0x16, 0x2c, 0x2e},
     .data_size = 14},
    {.command = LCD_CMD_INVON},
    {.command = LCD_CMD_DISPON},
    {.command = LCD_CMD_RAMWR},
};

static const char *TAG = "board_ws_147b";
static esp_lcd_panel_handle_t panel_handle;
static esp_lcd_panel_io_handle_t panel_io;
static uint16_t *frame_buffer;
static board_display_transfer_done_cb_t transfer_done_callback;
static void *transfer_done_context;

static bool panel_transfer_done(esp_lcd_panel_io_handle_t io,
                                esp_lcd_panel_io_event_data_t *event_data,
                                void *user_context)
{
    (void)io;
    (void)event_data;
    (void)user_context;
    if (transfer_done_callback == NULL) {
        return false;
    }
    return transfer_done_callback(transfer_done_context);
}

static esp_err_t configure_backlight(void)
{
    const ledc_timer_config_t timer_config = {
        .speed_mode = BACKLIGHT_MODE,
        .duty_resolution = BACKLIGHT_DUTY_RES,
        .timer_num = BACKLIGHT_TIMER,
        .freq_hz = BACKLIGHT_FREQUENCY_HZ,
        .clk_cfg = LEDC_AUTO_CLK,
    };
    ESP_RETURN_ON_ERROR(ledc_timer_config(&timer_config), TAG, "backlight timer configuration failed");

    const ledc_channel_config_t channel_config = {
        .gpio_num = LCD_PIN_BACKLIGHT,
        .speed_mode = BACKLIGHT_MODE,
        .channel = BACKLIGHT_CHANNEL,
        .intr_type = LEDC_INTR_DISABLE,
        .timer_sel = BACKLIGHT_TIMER,
        .duty = 0,
        .hpoint = 0,
    };
    return ledc_channel_config(&channel_config);
}

static esp_err_t apply_official_panel_commands(void)
{
    ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_param(panel_io, LCD_CMD_SLPOUT, NULL, 0),
                        TAG, "LCD sleep-out failed");
    vTaskDelay(pdMS_TO_TICKS(100));

    for (size_t index = 0; index < sizeof(LCD_INIT_COMMANDS) / sizeof(LCD_INIT_COMMANDS[0]); ++index) {
        const lcd_init_command_t *entry = &LCD_INIT_COMMANDS[index];
        const void *data = entry->data_size > 0 ? entry->data : NULL;
        ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_param(panel_io, entry->command, data, entry->data_size),
                            TAG, "LCD command 0x%02x failed", entry->command);
        if (entry->delay_ms > 0) {
            vTaskDelay(pdMS_TO_TICKS(entry->delay_ms));
        }
    }
    return ESP_OK;
}

esp_err_t board_display_init(void)
{
    const board_ws_147b_geometry_t *geometry = board_ws_147b_native_geometry();
    ESP_RETURN_ON_ERROR(configure_backlight(), TAG, "backlight initialization failed");

    const spi_bus_config_t bus_config = {
        .mosi_io_num = LCD_PIN_MOSI,
        .miso_io_num = -1,
        .sclk_io_num = LCD_PIN_SCLK,
        .quadwp_io_num = -1,
        .quadhd_io_num = -1,
        .max_transfer_sz = BOARD_WS_147B_DISPLAY_WIDTH * BOARD_WS_147B_DISPLAY_HEIGHT * sizeof(uint16_t),
    };
    ESP_RETURN_ON_ERROR(spi_bus_initialize(LCD_HOST, &bus_config, SPI_DMA_CH_AUTO),
                        TAG, "LCD SPI bus initialization failed");

    const esp_lcd_panel_io_spi_config_t io_config = {
        .cs_gpio_num = LCD_PIN_CS,
        .dc_gpio_num = LCD_PIN_DC,
        .spi_mode = 0,
        .pclk_hz = LCD_PIXEL_CLOCK_HZ,
        .trans_queue_depth = 1,
        .lcd_cmd_bits = 8,
        .lcd_param_bits = 8,
    };
    ESP_RETURN_ON_ERROR(
        esp_lcd_new_panel_io_spi((esp_lcd_spi_bus_handle_t)LCD_HOST, &io_config, &panel_io),
        TAG, "LCD panel IO initialization failed");

    const esp_lcd_panel_dev_config_t panel_config = {
        .reset_gpio_num = LCD_PIN_RST,
        .rgb_ele_order = LCD_RGB_ELEMENT_ORDER_BGR,
        .data_endian = LCD_RGB_DATA_ENDIAN_LITTLE,
        .bits_per_pixel = 16,
    };
    ESP_RETURN_ON_ERROR(esp_lcd_new_panel_st7789(panel_io, &panel_config, &panel_handle),
                        TAG, "ST7789 panel allocation failed");
    ESP_RETURN_ON_ERROR(esp_lcd_panel_reset(panel_handle), TAG, "LCD reset failed");
    ESP_RETURN_ON_ERROR(esp_lcd_panel_init(panel_handle), TAG, "LCD base initialization failed");
    ESP_RETURN_ON_ERROR(apply_official_panel_commands(), TAG, "LCD vendor initialization failed");
    ESP_RETURN_ON_ERROR(esp_lcd_panel_set_gap(panel_handle, geometry->x_gap, geometry->y_gap),
                        TAG, "LCD gap configuration failed");
    ESP_RETURN_ON_ERROR(esp_lcd_panel_swap_xy(panel_handle, geometry->swap_xy),
                        TAG, "LCD axis configuration failed");
    ESP_RETURN_ON_ERROR(esp_lcd_panel_mirror(panel_handle, geometry->mirror_x, geometry->mirror_y),
                        TAG, "LCD mirror configuration failed");

    const size_t frame_buffer_size = geometry->panel_width * geometry->panel_height * sizeof(uint16_t);
    frame_buffer = heap_caps_malloc(frame_buffer_size, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (frame_buffer == NULL) {
        frame_buffer = heap_caps_malloc(frame_buffer_size, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    }
    ESP_RETURN_ON_FALSE(frame_buffer != NULL, ESP_ERR_NO_MEM, TAG, "LCD frame buffer allocation failed");

    ESP_RETURN_ON_ERROR(board_display_fill_rgb565(0x0000), TAG, "initial LCD clear failed");
    vTaskDelay(pdMS_TO_TICKS(100));
    ESP_RETURN_ON_ERROR(board_backlight_set(80), TAG, "backlight enable failed");
    ESP_LOGI(TAG, "LCD ready: panel=%ux%u logical=%ux%u SPI3 12 MHz gap=(%u,%u)",
             geometry->panel_width, geometry->panel_height,
             geometry->logical_width, geometry->logical_height,
             geometry->x_gap, geometry->y_gap);
    return ESP_OK;
}

esp_err_t board_display_fill_rgb565(uint16_t color)
{
    const board_ws_147b_geometry_t *geometry = board_ws_147b_native_geometry();
    ESP_RETURN_ON_FALSE(panel_handle != NULL && frame_buffer != NULL, ESP_ERR_INVALID_STATE,
                        TAG, "LCD is not initialized");
    const size_t pixel_count = geometry->panel_width * geometry->panel_height;
    for (size_t index = 0; index < pixel_count; ++index) {
        frame_buffer[index] = color;
    }
    return board_display_draw_bitmap_native(0, 0, geometry->panel_width, geometry->panel_height,
                                            frame_buffer);
}

esp_err_t board_display_draw_bitmap_native(uint16_t x_start, uint16_t y_start,
                                           uint16_t x_end, uint16_t y_end,
                                           const void *color_data)
{
    const board_ws_147b_geometry_t *geometry = board_ws_147b_native_geometry();
    ESP_RETURN_ON_FALSE(panel_handle != NULL, ESP_ERR_INVALID_STATE, TAG, "LCD is not initialized");
    ESP_RETURN_ON_FALSE(color_data != NULL, ESP_ERR_INVALID_ARG, TAG, "LCD color data is null");
    ESP_RETURN_ON_FALSE(x_start < x_end && y_start < y_end &&
                        x_end <= geometry->panel_width && y_end <= geometry->panel_height,
                        ESP_ERR_INVALID_ARG, TAG, "LCD native draw area is out of range");
    return esp_lcd_panel_draw_bitmap(panel_handle, x_start, y_start, x_end, y_end, color_data);
}

esp_err_t board_display_set_transfer_done_callback(board_display_transfer_done_cb_t callback,
                                                   void *user_context)
{
    ESP_RETURN_ON_FALSE(panel_io != NULL, ESP_ERR_INVALID_STATE, TAG, "LCD IO is not initialized");
    transfer_done_callback = callback;
    transfer_done_context = user_context;
    const esp_lcd_panel_io_callbacks_t callbacks = {
        .on_color_trans_done = panel_transfer_done,
    };
    return esp_lcd_panel_io_register_event_callbacks(panel_io, &callbacks, NULL);
}

esp_err_t board_backlight_set(uint8_t percent)
{
    ESP_RETURN_ON_FALSE(percent <= 100, ESP_ERR_INVALID_ARG, TAG, "backlight percent must be 0..100");
    const uint32_t duty = (BACKLIGHT_MAX_DUTY * percent) / 100;
    ESP_RETURN_ON_ERROR(ledc_set_duty(BACKLIGHT_MODE, BACKLIGHT_CHANNEL, duty),
                        TAG, "backlight duty update failed");
    return ledc_update_duty(BACKLIGHT_MODE, BACKLIGHT_CHANNEL);
}

bool board_boot_button_pressed(void)
{
    return gpio_get_level(BOOT_PIN) == 0;
}

esp_err_t board_init(void)
{
    const gpio_config_t button_config = {
        .pin_bit_mask = 1ULL << BOOT_PIN,
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    ESP_RETURN_ON_ERROR(gpio_config(&button_config), TAG, "BOOT button configuration failed");
    return board_display_init();
}
