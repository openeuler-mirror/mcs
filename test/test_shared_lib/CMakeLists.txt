# sample_app：仅依赖 mica 库和头文件，不依赖 mcs 构建系统、oEE SDK
# 用法： cmake -S . -B build -DMICA_INSTALL_DIR=/path/to/mica-install

cmake_minimum_required(VERSION 3.13)
project(sample_app C)

# 库目录：lib 或 lib64
if(EXISTS "${MICA_INSTALL_DIR}/lib64/libmica.so")
  set(MICA_LIB_DIR "${MICA_INSTALL_DIR}/lib64")
elseif(EXISTS "${MICA_INSTALL_DIR}/lib/libmica.so")
  set(MICA_LIB_DIR "${MICA_INSTALL_DIR}/lib")
else()
  message(FATAL_ERROR "libmica.so 未找到。请先安装 libmica库 到 ${MICA_INSTALL_DIR}/lib64 或 ${MICA_INSTALL_DIR}/lib ，或设置 -DMICA_INSTALL_DIR= 指向正确前缀。")
endif()

set(MICA_INCLUDE_DIRS "")
if(EXISTS "${MICA_INSTALL_DIR}/include/user_msg")
  list(APPEND MICA_INCLUDE_DIRS "${MICA_INSTALL_DIR}/include")
endif()
if(MICA_INCLUDE_DIRS STREQUAL "")
  message(FATAL_ERROR "mica 头文件未找到（需 user_msg）。请确认 mica头文件 已安装到 ${MICA_INSTALL_DIR}/include/ ，或设置 -DMICA_INSTALL_DIR= 指向正确前缀。")
endif()

message(STATUS "sample_app: MICA_INSTALL_DIR=${MICA_INSTALL_DIR}")
message(STATUS "sample_app: MICA_LIB_DIR=${MICA_LIB_DIR}")
message(STATUS "sample_app: MICA_INCLUDE_DIRS=${MICA_INCLUDE_DIRS}")

add_subdirectory(send-data)
add_subdirectory(rcv-data)
add_subdirectory(send-data-2-way)
