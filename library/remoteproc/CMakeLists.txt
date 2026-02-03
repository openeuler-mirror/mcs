aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} remoteproc_srcs)

target_sources(${MICA_STATIC_LIB} PRIVATE ${remoteproc_srcs})
target_sources(${MICA_SHARED_LIB} PRIVATE ${remoteproc_srcs})
