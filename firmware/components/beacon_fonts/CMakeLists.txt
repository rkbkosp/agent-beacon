set(BEACON_FONT_FILES
    "font_assets/MiLanPro-Medium-400.ttf"
    "font_assets/MiLanPro-SemiBold-540.ttf"
)

foreach(font_file IN LISTS BEACON_FONT_FILES)
    if(NOT EXISTS "${CMAKE_CURRENT_LIST_DIR}/${font_file}")
        message(FATAL_ERROR
            "Missing user-supplied font: ${font_file}. "
            "Confirm that you have the required font rights, then run "
            "scripts/install-local-fonts.sh from the repository root."
        )
    endif()
endforeach()

idf_component_register(
    SRCS "beacon_fonts.c"
    INCLUDE_DIRS "include"
    REQUIRES heap lvgl__lvgl
    EMBED_FILES ${BEACON_FONT_FILES}
)
