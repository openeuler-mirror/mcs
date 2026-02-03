aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} memory_srcs)

target_sources(${MICA_STATIC_LIB} PRIVATE ${memory_srcs})
target_sources(${MICA_SHARED_LIB} PRIVATE ${memory_srcs})
