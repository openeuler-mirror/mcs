set(MICA_SOURCES
  "${CMAKE_CURRENT_SOURCE_DIR}/mica_debug.c"
  "${CMAKE_CURRENT_SOURCE_DIR}/mica_init.c"
  "${CMAKE_CURRENT_SOURCE_DIR}/mica_service.c"
  "${CMAKE_CURRENT_SOURCE_DIR}/mica_context.c")
add_subdirectory(pedestals)
add_subdirectory(services)
add_library(mica-static STATIC ${MICA_SOURCES})
add_library(mica::mica ALIAS mica-static)
set_target_properties(mica-static PROPERTIES OUTPUT_NAME mica)
target_compile_features(mica-static PRIVATE c_std_11)
if(PROJECT_SYSTEM STREQUAL "uniproton")
  # Required before the first libc header for the native finite tryjoin map.
  target_compile_definitions(mica-static PRIVATE _GNU_SOURCE)
endif()
target_include_directories(mica-static PUBLIC
  "${CMAKE_CURRENT_SOURCE_DIR}/include"
  "${CMAKE_CURRENT_BINARY_DIR}/../lib/include"
  ${PLATFORM_INCLUDE_DIRS} ${PLATFORM_DEP_INCLUDE_DIRS}
  PRIVATE "${CMAKE_CURRENT_SOURCE_DIR}" "${CMAKE_CURRENT_SOURCE_DIR}/pedestals"
  "${CMAKE_CURRENT_SOURCE_DIR}/services")
target_include_directories(mica-static SYSTEM PUBLIC
  "${LIBMETAL_PATH}/output/usr/local/include"
  "${OPENAMP_PATH}/output/usr/local/include")
target_link_libraries(mica-static INTERFACE
  "${OPENAMP_PATH}/output/usr/local/lib/libopen_amp.a"
  "${LIBMETAL_PATH}/output/usr/local/lib/libmetal.a")
if(MICA_PED STREQUAL "xen")
  target_compile_definitions(mica-static PRIVATE "OS_OPTION_XEN=")
endif()
if(MICA_PED STREQUAL "xen" AND MICA_XEN_PROVIDER STREQUAL "builtin")
  # One weak LL/SC producer try on ARMv8.0/A53, no direct LSE RMW.
  set_source_files_properties("${CMAKE_CURRENT_SOURCE_DIR}/pedestals/xen/debug_console.c"
    PROPERTIES COMPILE_OPTIONS "-march=armv8-a;-mno-outline-atomics")
endif()
# Static consumers retain the resource-table member and section under GC.
file(WRITE "${CMAKE_CURRENT_BINARY_DIR}/mica-resource-table.ld"
  "SECTIONS { .resource_table : { KEEP(*(.resource_table)) } } INSERT AFTER .data;\n")
target_link_options(mica-static INTERFACE "LINKER:--undefined=rsc_table_get"
  "LINKER:-T,${CMAKE_CURRENT_BINARY_DIR}/mica-resource-table.ld")
install(FILES "${CMAKE_CURRENT_BINARY_DIR}/mica-resource-table.ld"
  DESTINATION "${CMAKE_INSTALL_LIBDIR}")
install(TARGETS mica-static ARCHIVE DESTINATION "${CMAKE_INSTALL_LIBDIR}")
install(FILES include/mica/mica.h include/mica/service.h include/mica/debug.h
  DESTINATION "${CMAKE_INSTALL_INCLUDEDIR}/mica")
install(FILES pedestals/xen/provider.h
  DESTINATION "${CMAKE_INSTALL_INCLUDEDIR}/mica/platform/xen")
if(MICA_PED STREQUAL "xen" AND MICA_XEN_PROVIDER STREQUAL "builtin")
  include(pedestals/xen/distribution.cmake)
  foreach(text IN LISTS MICA_XEN_DISTRIBUTION_FILES)
    get_filename_component(subdir "${text}" DIRECTORY)
    install(FILES "${CMAKE_CURRENT_SOURCE_DIR}/pedestals/xen/${text}"
      DESTINATION "${CMAKE_INSTALL_DATADIR}/licenses/mica/xen/${subdir}")
  endforeach()
endif()
