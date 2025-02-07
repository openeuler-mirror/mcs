add_executable(send-data
	send-data.c
)

target_link_libraries(send-data ${MICA_LIB} ${SHARED_LINK_LIBS})

install(TARGETS send-data DESTINATION bin)