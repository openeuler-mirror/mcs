aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} MICAD_SRCS)
aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR}/services/pty PTY_SERVICES_SRCS)
aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR}/services/rpc RPC_SERVICES_SRCS)
aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR}/services/debug DEBUG_SERVICES_SRCS)
aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR}/services/umt UMT_SERVICES_SRCS)

add_executable(micad ${MICAD_SRCS} ${PTY_SERVICES_SRCS} ${RPC_SERVICES_SRCS} ${DEBUG_SERVICES_SRCS} ${UMT_SERVICES_SRCS})

target_include_directories(micad PUBLIC
	${CMAKE_CURRENT_SOURCE_DIR}
)

target_link_libraries(micad ${MICA_LIB} ${SHARED_LINK_LIBS})

install(TARGETS micad
	RUNTIME DESTINATION bin
	PERMISSIONS OWNER_READ OWNER_EXECUTE GROUP_READ GROUP_EXECUTE
)
