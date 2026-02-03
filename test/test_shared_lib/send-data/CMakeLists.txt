add_executable(send-data send-data.c)
target_include_directories(send-data PRIVATE ${MICA_INCLUDE_DIRS})
target_link_directories(send-data PRIVATE ${MICA_LIB_DIR})
target_link_libraries(send-data mica)
