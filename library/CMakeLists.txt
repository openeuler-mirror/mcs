# Static lib libmica.a: does not link metal/open_amp; app must link -lmetal -lopen_amp
add_library(${MICA_STATIC_LIB} STATIC "")
set_target_properties(${MICA_STATIC_LIB} PROPERTIES OUTPUT_NAME mica)

# Shared lib libmica.so: links metal/open_amp internally; app only needs -lmica
add_library(${MICA_SHARED_LIB} SHARED "")
set_target_properties(${MICA_SHARED_LIB} PROPERTIES OUTPUT_NAME mica)
target_link_libraries(${MICA_SHARED_LIB} PRIVATE ${SHARED_LINK_LIBS})

add_subdirectory(remoteproc)
add_subdirectory(memory)
add_subdirectory(rpmsg_device)
add_subdirectory(mica)
add_subdirectory(rbuf_device)
add_subdirectory(user_msg)

install(TARGETS ${MICA_STATIC_LIB} ${MICA_SHARED_LIB}
	LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
	ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}
)
install(DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/include" DESTINATION include)
