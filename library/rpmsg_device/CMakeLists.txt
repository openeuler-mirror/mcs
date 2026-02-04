aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} rpmsg_srcs)

target_sources(${MICA_STATIC_LIB} PRIVATE ${rpmsg_srcs})
target_sources(${MICA_SHARED_LIB} PRIVATE ${rpmsg_srcs})
