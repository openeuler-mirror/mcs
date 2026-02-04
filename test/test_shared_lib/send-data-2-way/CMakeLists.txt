add_executable(send-data-2-way send-data-2-way.c)
target_include_directories(send-data-2-way PRIVATE ${MICA_INCLUDE_DIRS})
target_link_directories(send-data-2-way PRIVATE ${MICA_LIB_DIR})
target_link_libraries(send-data-2-way mica)
