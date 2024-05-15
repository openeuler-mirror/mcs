cmake_minimum_required(VERSION 3.19)

project(openeuler_mica)

## set common FLAGS
set(CMAKE_SHARED_LINKER_FLAGS "-pthread")
set(CMAKE_C_FLAGS "-fstack-protector-all -O2 -Wall")
set(CMAKE_C_FLAGS_DEBUG "${CMAKE_C_FLAGS_DEBUG} -DDEBUG")

## Add dependencies
set(SHARED_LINK_LIBS
	metal
	open_amp
)

## Add headers
include_directories(
	${CMAKE_SOURCE_DIR}/library/include
)

## rpc support multi workers
if(DEFINED MULTI_WORKERS)
	add_definitions(-DMULTI_WORKERS)
endif()

## build MICA library
set(MICA_LIB mica)
add_subdirectory(${CMAKE_SOURCE_DIR}/library)

## build MICA client
add_subdirectory(${CMAKE_SOURCE_DIR}/mica)

## build testcase for debugging module
add_subdirectory(${CMAKE_SOURCE_DIR}/test/test_ring_buffer)
