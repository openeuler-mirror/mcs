# src/CMakeLists.txt

# ========== 调试：验证变量传递 ==========
message(STATUS "=== src/CMakeLists.txt - Variable Check ===")
message(STATUS "LIBMETAL_PATH = ${LIBMETAL_PATH}")
message(STATUS "OPENAMP_PATH = ${OPENAMP_PATH}")

if(NOT LIBMETAL_PATH OR "${LIBMETAL_PATH}" STREQUAL "")
    message(FATAL_ERROR "LIBMETAL_PATH not available in src/CMakeLists.txt")
endif()

if(NOT OPENAMP_PATH OR "${OPENAMP_PATH}" STREQUAL "")
    message(FATAL_ERROR "OPENAMP_PATH not available in src/CMakeLists.txt")
endif()

# ========== 设置编译标志 ==========
set_property (GLOBAL PROPERTY "PROJECT_LIB_EXTRA_CFLAGS")

# ========== 收集 include 路径 ==========
collect (PROJECT_LIB_DIRS "${CMAKE_CURRENT_BINARY_DIR}")

# 添加 MICA 自己的 include 路径
collect (PROJECT_INC_DIRS "${CMAKE_CURRENT_SOURCE_DIR}/include")
collect (PROJECT_INC_DIRS "${CMAKE_CURRENT_BINARY_DIR}/../lib/include")

# ========== 收集源文件 ==========
collect (PROJECT_LIB_SOURCES mica_init.c)
collect (PROJECT_LIB_SOURCES mica_service.c)

# ========== 添加子目录 ==========
add_subdirectory (pedestals)
add_subdirectory (services)

# ========== 添加依赖库（使用正确的路径） ==========
set(LIBMETAL_LIB_DIR "${LIBMETAL_PATH}/output/usr/local/lib")
set(OPENAMP_LIB_DIR "${OPENAMP_PATH}/output/usr/local/lib")

message(STATUS "Checking for libmetal at: ${LIBMETAL_LIB_DIR}/libmetal.a")
if(EXISTS "${LIBMETAL_LIB_DIR}/libmetal.a")
    message(STATUS "  Found libmetal")
    collect (PROJECT_LIB_DEPS metal)
else()
    message(WARNING "  libmetal.a not found")
endif()

message(STATUS "Checking for OpenAMP at: ${OPENAMP_LIB_DIR}/libopen_amp.a")
if(EXISTS "${OPENAMP_LIB_DIR}/libopen_amp.a")
    message(STATUS "  Found OpenAMP")
    collect (PROJECT_LIB_DEPS open_amp)
else()
    message(WARNING "  libopen_amp.a not found")
endif()

# ========== 收集所有变量 ==========
collector_list (_include PROJECT_INC_DIRS)
collector_list (_deps PROJECT_LIB_DEPS)
collector_list (_sources PROJECT_LIB_SOURCES)

# ========== 调试：打印 include 路径 ==========
message(STATUS "Project specific include directories:")
foreach(inc ${_include})
    message(STATUS "  ${inc}")
endforeach()

message(STATUS "Library dependencies: ${_deps}")

# ========== 获取继承的 include 路径 ==========
get_directory_property(inherited_includes INCLUDE_DIRECTORIES)
message(STATUS "Inherited include directories:")
foreach(inc ${inherited_includes})
    message(STATUS "  ${inc}")
endforeach()

message(STATUS "===========================================")

# ========== 追加项目特定的 include 路径 ==========
foreach(inc ${_include})
    include_directories(${inc})
endforeach()

# ========== 获取额外的编译标志 ==========
get_property (_ecflags GLOBAL PROPERTY "PROJECT_LIB_EXTRA_CFLAGS")
set_property (SOURCE ${_sources}
  APPEND_STRING PROPERTY COMPILE_FLAGS " ${_ecflags}")

# ========== 创建静态库 ==========
set (_lib ${MICA_LIB}-static)
add_library (${_lib} STATIC ${_sources})

# 使用 target_include_directories 确保路径正确
target_include_directories(${_lib} PRIVATE ${_include})

target_link_libraries (${_lib} ${_deps})

install (TARGETS ${_lib} ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR})
set_target_properties (${_lib} PROPERTIES
  OUTPUT_NAME       "${MICA_LIB}"
)

# ========== 安装公共头文件 ==========
install (DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}/include/mica" 
         DESTINATION include
         FILES_MATCHING PATTERN "*.h")

# vim: expandtab:ts=2:sw=2:smartindent