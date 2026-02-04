add_executable(rcv-data rcv-data.c)
target_include_directories(rcv-data PRIVATE ${MICA_INCLUDE_DIRS})
target_link_directories(rcv-data PRIVATE ${MICA_LIB_DIR})
target_link_libraries(rcv-data mica)
