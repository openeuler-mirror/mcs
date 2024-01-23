aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} remoteproc_srcs)

target_sources(${MICA_LIB} PRIVATE ${remoteproc_srcs})
