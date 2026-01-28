add_executable(riscv_send-data send-data.c)
target_link_libraries(riscv_send-data ${MICA_LIB} ${SHARED_LINK_LIBS})
install(TARGETS riscv_send-data DESTINATION bin/riscv RENAME send-data)

add_executable(riscv_rcv-data rcv-data.c)
target_link_libraries(riscv_rcv-data ${MICA_LIB} ${SHARED_LINK_LIBS})
install(TARGETS riscv_rcv-data DESTINATION bin/riscv RENAME rcv-data)

add_executable(riscv_send-data-2-way send-data-2-way.c)
target_link_libraries(riscv_send-data-2-way ${MICA_LIB} ${SHARED_LINK_LIBS})
install(TARGETS riscv_send-data-2-way DESTINATION bin/riscv RENAME send-data-2-way)
