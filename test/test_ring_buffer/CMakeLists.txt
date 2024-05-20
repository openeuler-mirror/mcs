add_executable(test_ring_buffer
	test_ring_buffer.c
	${CMAKE_SOURCE_DIR}/library/rbuf_device/ring_buffer.c
)

include_directories(
	${CMAKE_SOURCE_DIR}/library/include/mcs
	${CMAKE_SOURCE_DIR}/mica/micad/services
)

install(TARGETS test_ring_buffer DESTINATION bin)
