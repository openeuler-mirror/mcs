cmake_minimum_required(VERSION 3.19)

project(openeuler_mcs_demo)

# append -pthread flags
add_compile_options(-pthread)

message(STATUS "DEMO_TARGET=${DEMO_TARGET}")


set(MCS_LIB mcs)

set(SHARED_LINK_LIBS
	libopen_amp.so
	libmetal.so
	libsysfs.so
)

add_subdirectory(library)

set(SRC_FILE
	./${DEMO_TARGET}/rpmsg_main.c
)

if(DEMO_TARGET MATCHES "rpmsg_pty_demo")
	list(APPEND SRC_FILE "./${DEMO_TARGET}/rpmsg_pty.c")
else()
	message(FATAL_ERROR "DEMO_TARGET: choose a target to build.
		[rpmsg_pty_demo]")
endif()

include_directories(
	${DEMO_TARGET}
	library/include/mcs
)

add_executable(rpmsg_main ${SRC_FILE})
target_link_libraries(rpmsg_main ${MCS_LIB} ${SHARED_LINK_LIBS})

install(TARGETS rpmsg_main DESTINATION bin)
