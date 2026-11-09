if(NOT PROJECT_SYSTEM)
  string(TOLOWER "${CMAKE_SYSTEM_NAME}" PROJECT_SYSTEM)
endif()
foreach(header io.h irq.h log.h time.h thread.h lock.h sem.h memory.h)
  configure_file("${header}" "include/mica/platform/${header}" @ONLY)
  install(FILES "${CMAKE_CURRENT_BINARY_DIR}/include/mica/platform/${header}"
    DESTINATION "${CMAKE_INSTALL_INCLUDEDIR}/mica/platform")
endforeach()
# Preserve selected in-tree ports; external OS headers remain in the OS tree.
if(IS_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/system/${PROJECT_SYSTEM}")
  foreach(header io.h irq.h log.h time.h thread.h lock.h sem.h memory.h)
    if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/system/${PROJECT_SYSTEM}/${header}")
      configure_file("system/${PROJECT_SYSTEM}/${header}"
        "include/mica/platform/system/${PROJECT_SYSTEM}/${header}" COPYONLY)
    endif()
  endforeach()
  install(DIRECTORY "${CMAKE_CURRENT_BINARY_DIR}/include/mica/platform/system/${PROJECT_SYSTEM}"
    DESTINATION "${CMAKE_INSTALL_INCLUDEDIR}/mica/platform/system")
endif()
