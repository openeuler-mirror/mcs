add_library(${MICA_LIB} STATIC "")

add_subdirectory(remoteproc)
add_subdirectory(memory)
add_subdirectory(rpmsg_device)
add_subdirectory(mica)
add_subdirectory(rbuf_device)

install(TARGETS ${MICA_LIB} ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR})
install(DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/include" DESTINATION include)
