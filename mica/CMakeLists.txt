aux_source_directory(${CMAKE_CURRENT_SOURCE_DIR} MICA_SRCS)

add_executable(mica_main ${MICA_SRCS})

target_include_directories(mica_main PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}
)

target_link_libraries(mica_main ${MICA_LIB} ${SHARED_LINK_LIBS})
