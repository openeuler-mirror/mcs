cmake_minimum_required(VERSION 3.19)

project(openeuler_mcs_demo)

add_compile_options(-pthread -Werror=implicit-function-declaration)

message(STATUS "DEMO_TARGET=${DEMO_TARGET}")

set(MCS_LIB mcs)

set(SHARED_LINK_LIBS
	libopen_amp.so
	libmetal.so
	libsysfs.so
)

set(CMAKE_C_FLAGS_DEBUG "${CMAKE_C_FLAGS_DEBUG} -DDEBUG")

add_subdirectory(library)

set(SRC_FILE
	./${DEMO_TARGET}/mica_main.c
)

if(DEMO_TARGET MATCHES "openamp_demo")
	list(APPEND SRC_FILE "./rpmsg_pty_demo/rpmsg_pty.c")
	list(APPEND SRC_FILE "./${DEMO_TARGET}/rpmsg_matrix_multiply.c")
	list(APPEND SRC_FILE "./${DEMO_TARGET}/rpmsg_ping.c")
elseif(DEMO_TARGET MATCHES "mica_demo")
	if(CONFIG_RING_BUFFER MATCHES "y")
		add_definitions(-DCONFIG_RING_BUFFER)
		set(TEST_DEBUG_BACKEND test_ring_buffer)
	else()
		message(STATUS "choose a messaging mechanism to build.
			[-DCONFIG_RING_BUFFER=y]")
	endif()
	# add source file for mica_demo
	list(APPEND SRC_FILE "${CMAKE_CURRENT_SOURCE_DIR}/${DEMO_TARGET}/rpmsg_pty.c")
	list(APPEND SRC_FILE "${CMAKE_CURRENT_SOURCE_DIR}/${DEMO_TARGET}/mica_debug.c")
	# enable debug log
	if(DEFINED MICA_DEBUG_LOG)
		add_definitions(-DMICA_DEBUG_LOG)
	endif()
	# compile testing usecase
	# add_subdirectory(${CMAKE_CURRENT_SOURCE_DIR}/test/${TEST_DEBUG_BACKEND})
	install(TARGETS ${TEST_DEBUG_BACKEND} DESTINATION bin)
elseif(DEMO_TARGET MATCHES "benchmark_demo")
	list(APPEND SRC_FILE "./${DEMO_TARGET}/benchmark.c")
	list(APPEND SRC_FILE "./${DEMO_TARGET}/rpmsg_pty.c")
	# compile benchmark

	add_executable(pty_test "./benchmark_demo/pty_test.c")
	target_link_libraries(pty_test ${MCS_LIB} ${SHARED_LINK_LIBS})
else()
	message(FATAL_ERROR "DEMO_TARGET: choose a target to build.
		[mica_demo|mica_pty_demo]")
endif()

include_directories(
	${CMAKE_SOURCE_DIR}/${DEMO_TARGET}
	${CMAKE_SOURCE_DIR}/library/include
)

add_executable(mica_main ${SRC_FILE})
target_link_libraries(mica_main ${MCS_LIB} ${SHARED_LINK_LIBS})

install(TARGETS mica_main DESTINATION bin)
