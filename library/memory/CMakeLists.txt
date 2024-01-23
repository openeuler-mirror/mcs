aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} memory_srcs)

target_sources(${MICA_LIB} PRIVATE ${memory_srcs})
