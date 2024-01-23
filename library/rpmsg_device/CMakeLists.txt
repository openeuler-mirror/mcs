aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} rpmsg_srcs)

target_sources(${MICA_LIB} PRIVATE ${rpmsg_srcs})
