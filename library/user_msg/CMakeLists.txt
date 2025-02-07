aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} user_msg_srcs)

target_sources(${MICA_LIB} PRIVATE ${user_msg_srcs})