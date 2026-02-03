aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} rbuf_srcs)

target_sources(${MICA_STATIC_LIB} PRIVATE ${rbuf_srcs})
target_sources(${MICA_SHARED_LIB} PRIVATE ${rbuf_srcs})
