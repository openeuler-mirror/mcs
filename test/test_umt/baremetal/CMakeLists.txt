add_executable(baremetal_send-data-2-way send-data-2-way.c)
target_link_libraries(baremetal_send-data-2-way ${MICA_LIB} ${SHARED_LINK_LIBS})
install(TARGETS baremetal_send-data-2-way DESTINATION bin/baremetal RENAME send-data-2-way)
