aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} mica_lib_srcs)

target_sources(${MICA_STATIC_LIB} PRIVATE ${mica_lib_srcs})
target_sources(${MICA_SHARED_LIB} PRIVATE ${mica_lib_srcs})
