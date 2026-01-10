cmake_minimum_required (VERSION 3.0)

project (mica C)

# ========== 版本信息 ==========
set(PROJECT_VERSION_MAJOR 1)
set(PROJECT_VERSION_MINOR 0)
set(PROJECT_VERSION_PATCH 0)
set(PROJECT_VERSION "${PROJECT_VERSION_MAJOR}.${PROJECT_VERSION_MINOR}.${PROJECT_VERSION_PATCH}")

# ========== 包含自定义CMake模块 ==========
list(APPEND CMAKE_MODULE_PATH "${CMAKE_CURRENT_SOURCE_DIR}/cmake")
include(collect)

# ========== 包含配置文件（如果存在） ==========
if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/config.cmake")
    include(config.cmake)
endif()

# ========== 配置选项 ==========
option (WITH_SHARED_LIB "Build with shared library" OFF)
option (WITH_PROXY "Build with proxy" OFF)

# ========== 库名称 ==========
set (MICA_LIB mica)

# ========== 检查必需的平台变量（toolchain 应该已设置） ==========
if(NOT PLATFORM_INCLUDE_DIRS)
    message(FATAL_ERROR 
        "PLATFORM_INCLUDE_DIRS not set. "
        "Please specify a toolchain file with -DCMAKE_TOOLCHAIN_FILE=...")
endif()

if(NOT PLATFORM_DEP_INCLUDE_DIRS)
    message(FATAL_ERROR 
        "PLATFORM_DEP_INCLUDE_DIRS not set. "
        "Please specify a toolchain file with -DCMAKE_TOOLCHAIN_FILE=...")
endif()

# ========== 验证关键路径（toolchain 应该已设置） ==========
if(NOT LIBMETAL_PATH OR "${LIBMETAL_PATH}" STREQUAL "")
    message(FATAL_ERROR "LIBMETAL_PATH not set by toolchain file")
endif()

if(NOT OPENAMP_PATH OR "${OPENAMP_PATH}" STREQUAL "")
    message(FATAL_ERROR "OPENAMP_PATH not set by toolchain file")
endif()

# ========== 应用平台配置 ==========
include_directories(${PLATFORM_INCLUDE_DIRS})
include_directories(${PLATFORM_DEP_INCLUDE_DIRS})

if(PLATFORM_LINK_DIRS)
    link_directories(${PLATFORM_LINK_DIRS})
endif()

# ========== 调试信息 ==========
message(STATUS "==================== Build Configuration ====================")
message(STATUS "Project: ${PROJECT_NAME} ${PROJECT_VERSION}")
message(STATUS "System: ${CMAKE_SYSTEM_NAME}")
message(STATUS "Processor: ${CMAKE_SYSTEM_PROCESSOR}")
message(STATUS "Compiler: ${CMAKE_C_COMPILER}")
message(STATUS "Build Type: ${CMAKE_BUILD_TYPE}")
message(STATUS "Platform System: ${PROJECT_SYSTEM}")
message(STATUS "MICA Pedestal: ${MICA_PED}")
message(STATUS "LIBMETAL_PATH: ${LIBMETAL_PATH}")
message(STATUS "OPENAMP_PATH: ${OPENAMP_PATH}")
message(STATUS "============================================================")

message(STATUS "Platform Include Dirs:")
foreach(dir ${PLATFORM_INCLUDE_DIRS})
    message(STATUS "  ${dir}")
endforeach()

message(STATUS "Dependency Include Dirs:")
foreach(dir ${PLATFORM_DEP_INCLUDE_DIRS})
    message(STATUS "  ${dir}")
endforeach()

# 验证关键头文件
set(OPENAMP_HEADER "${OPENAMP_PATH}/output/usr/local/include/openamp/open_amp.h")
if(NOT EXISTS "${OPENAMP_HEADER}")
    message(FATAL_ERROR "OpenAMP header not found: ${OPENAMP_HEADER}")
endif()

set(LIBMETAL_HEADER "${LIBMETAL_PATH}/output/usr/local/include/metal/sys.h")
if(NOT EXISTS "${LIBMETAL_HEADER}")
    message(FATAL_ERROR "libmetal header not found: ${LIBMETAL_HEADER}")
endif()

message(STATUS "Dependency headers verified")
message(STATUS "============================================================")

# ========== 添加子目录 ==========
add_subdirectory (lib)
add_subdirectory (src)

# vim: expandtab:ts=2:sw=2:smartindent