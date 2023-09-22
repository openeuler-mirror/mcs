
add_library(${MCS_LIB} STATIC
		openamp_module.c
		remoteproc_module.c
		virtio_module.c
		rpmsg_endpoint.c
		rpmsg_rpc_service.c
		rpmsg_sys_service.c
		mica_debug_common.c
)

include_directories(
	include/mcs
)

install(TARGETS ${MCS_LIB} ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR})
install(DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/include/mcs" DESTINATION include)
