add_executable(${TEST_DEBUG_BACKEND}
	test_ring_buffer.c
)

include_directories(
	${CMAKE_SOURCE_DIR}/library/include/mcs
)
