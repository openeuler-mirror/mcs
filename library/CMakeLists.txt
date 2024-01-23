add_library(${MICA_LIB} STATIC "")

target_sources(${MICA_LIB} PUBLIC
	mica_debug_common.c
	mica_gdb_server.c
)

add_subdirectory(remoteproc)
add_subdirectory(memory)
add_subdirectory(rpmsg_device)
add_subdirectory(mica)

install(TARGETS ${MICA_LIB} ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR})
install(DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/include" DESTINATION include)
