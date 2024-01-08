add_library(${MCS_LIB} STATIC 
	mica_debug_common.c
	mica_gdb_server.c
)

add_subdirectory(remoteproc)
add_subdirectory(memory)
add_subdirectory(rpmsg_device)
add_subdirectory(mica)

include_directories(
	${CMAKE_CURRENT_SOURCE_DIR}/include
)

install(TARGETS ${MCS_LIB} ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR})
install(DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/include" DESTINATION include)
