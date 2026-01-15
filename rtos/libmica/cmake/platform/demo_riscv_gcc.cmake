# ========================================================================
# DEMO OS RISC-V 交叉编译工具链配置
# ========================================================================

# -------------------- 用户配置区 --------------------

# 1. 交叉编译器前缀
set(CROSS_PREFIX "riscv32-linux-musl-")

# 2. 组件路径配置
set(COMPONENTS_PATH "/path/to/dev" CACHE PATH "")

set(LIBMETAL_PATH "${COMPONENTS_PATH}/libmetal" CACHE PATH "")
set(OPENAMP_PATH "${COMPONENTS_PATH}/open-amp" CACHE PATH "")
set(CLIENT_OS_PATH "${COMPONENTS_PATH}/demo" CACHE PATH "")

# 3. 平台头文件 Include 路径
set(PLATFORM_INCLUDE_DIRS
    ${CLIENT_OS_PATH}/xxx/include
    CACHE STRING ""
)

# 4. 平台信息
set(CMAKE_SYSTEM_NAME demo)
set(CMAKE_SYSTEM_PROCESSOR riscv)
set(PROJECT_SYSTEM "demo" CACHE STRING "") # 要和 lib/system 下系统目录名一致
set(MICA_PED "hetero" CACHE STRING "")

# -------------------- 用户配置区 END --------------------

# ========================================================================
# 以下为自动配置，一般不需要修改
# ========================================================================

set(CMAKE_C_COMPILER "${CROSS_PREFIX}gcc")
set(CMAKE_C_COMPILER_WORKS 1)
set(CMAKE_CXX_COMPILER_WORKS 1)

set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE NEVER)

# -------------------- 路径自动检测 --------------------
if(NOT COMPONENTS_PATH OR "${COMPONENTS_PATH}" STREQUAL "")
    message(FATAL_ERROR "COMPONENTS_PATH not set! Please set environment variable or edit this file.")
endif()

if(NOT LIBMETAL_PATH OR "${LIBMETAL_PATH}" STREQUAL "")
    message(FATAL_ERROR "LIBMETAL_PATH not set! Please set environment variable or edit this file.")
endif()

if(NOT OPENAMP_PATH OR "${OPENAMP_PATH}" STREQUAL "")
    message(FATAL_ERROR "OPENAMP_PATH not set! Please set environment variable or edit this file.")
endif()

if(NOT EXISTS "${CLIENT_OS_PATH}")
    message(FATAL_ERROR "Client OS not found at: ${CLIENT_OS_PATH}")
endif()

# -------------------- 依赖库路径 --------------------
set(PLATFORM_DEP_INCLUDE_DIRS
    ${LIBMETAL_PATH}/output/usr/local/include
    ${OPENAMP_PATH}/output/usr/local/include
    CACHE STRING ""
)

set(PLATFORM_LINK_DIRS
    ${LIBMETAL_PATH}/output/usr/local/lib
    ${OPENAMP_PATH}/output/usr/local/lib
    CACHE STRING ""
)

# -------------------- 调试信息 --------------------
message(STATUS "=== Toolchain Configuration ===")
message(STATUS "Compiler:    ${CMAKE_C_COMPILER}")
message(STATUS "Components:  ${COMPONENTS_PATH}")
message(STATUS "Demo OS:      ${CLIENT_OS_PATH}")
message(STATUS "libmetal:    ${LIBMETAL_PATH}")
message(STATUS "OpenAMP:     ${OPENAMP_PATH}")
message(STATUS "===============================")
