#
# Copyright (c) 2025 Huawei Technologies Co., Ltd. All rights reserved.
#
# SPDX-License-Identifier: BSD-3-Clause
#

# Collector functions for CMake
# 用于在多个 CMakeLists.txt 之间收集变量

# 创建一个全局 collector
# @param name: collector 名称
# @param base: 基础路径（用于相对路径转换，可为空）
function (collector_create name base)
  set_property (GLOBAL PROPERTY "COLLECT_${name}_LIST")
  set_property (GLOBAL PROPERTY "COLLECT_${name}_BASE" "${base}")
endfunction (collector_create)

# 获取 collector 中收集的列表
# @param var: 输出变量名
# @param name: collector 名称
function (collector_list var name)
  get_property (_list GLOBAL PROPERTY "COLLECT_${name}_LIST")
  set (${var} "${_list}" PARENT_SCOPE)
endfunction (collector_list)

# 获取 collector 的基础路径
# @param var: 输出变量名
# @param name: collector 名称
function (collector_base var name)
  get_property (_base GLOBAL PROPERTY "COLLECT_${name}_BASE")
  set (${var} "${_base}" PARENT_SCOPE)
endfunction (collector_base)

# 向 collector 中添加项
# @param name: collector 名称
# @param ARGN: 要添加的项列表
function (collect name)
  set (_list)
  foreach (s IN LISTS ARGN)
    # 判断是否已经是绝对路径
    if (IS_ABSOLUTE "${s}")
      # 已经是绝对路径，直接使用
      list (APPEND _list "${s}")
    else()
      # 相对路径，转换为绝对路径
      get_filename_component (s "${s}" ABSOLUTE)
      list (APPEND _list "${s}")
    endif()
  endforeach ()
  
  set_property (GLOBAL APPEND PROPERTY "COLLECT_${name}_LIST" "${_list}")
endfunction (collect)

# ========== 创建全局 collectors ==========
collector_create (PROJECT_INC_DIRS    "")
collector_create (PROJECT_LIB_DIRS    "")
collector_create (PROJECT_LIB_DEPS    "")
collector_create (PROJECT_LIB_SOURCES "")
collector_create (PROJECT_LIB_HEADERS "")

# vim: expandtab:ts=2:sw=2:smartindent