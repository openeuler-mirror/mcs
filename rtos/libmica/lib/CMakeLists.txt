# lib/ 目录只处理头文件的配置和安装，不创建库

collect (PROJECT_LIB_DIRS "${CMAKE_CURRENT_BINARY_DIR}")
collect (PROJECT_INC_DIRS "${CMAKE_CURRENT_BINARY_DIR}/include")

# ========== 定义要导出的平台抽象头文件 ==========
set(PLATFORM_HEADERS
    barrier.h
    delay.h
    io.h
    irq.h
    log.h
    macro.h
    securec.h
    sem.h
)

# ========== 从 toolchain 文件获取系统名称 ==========
if(NOT PROJECT_SYSTEM)
    string(TOLOWER ${CMAKE_SYSTEM_NAME} PROJECT_SYSTEM)
endif()

message(STATUS "PROJECT_SYSTEM = ${PROJECT_SYSTEM}")

# ========== 配置和安装平台抽象头文件 ==========
foreach (f ${PLATFORM_HEADERS})
  # 1. 配置到 build/lib/include/mica/platform/（注意路径变化）
  configure_file (${f} include/mica/platform/${f} @ONLY)
  
  # 2. 安装到 output/usr/local/include/mica/platform/
  install (FILES ${CMAKE_CURRENT_BINARY_DIR}/include/mica/platform/${f}
           DESTINATION include/mica/platform)
endforeach (f)

# ========== 安装系统特定的头文件 ==========
if (EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/system/${PROJECT_SYSTEM}")
    file(GLOB SYSTEM_HEADERS "${CMAKE_CURRENT_SOURCE_DIR}/system/${PROJECT_SYSTEM}/*.h")
    foreach(header ${SYSTEM_HEADERS})
        get_filename_component(header_name ${header} NAME)
        
        # 配置到 build/lib/include/mica/platform/system/PROJECT_SYSTEM/
        configure_file(${header} 
                       include/mica/platform/system/${PROJECT_SYSTEM}/${header_name} 
                       @ONLY)
        
        # 安装到 output/usr/local/include/mica/platform/system/PROJECT_SYSTEM/
        install(FILES ${CMAKE_CURRENT_BINARY_DIR}/include/mica/platform/system/${PROJECT_SYSTEM}/${header_name}
                DESTINATION include/mica/platform/system/${PROJECT_SYSTEM})
    endforeach()
else()
    message(WARNING "System specific headers not found at: ${CMAKE_CURRENT_SOURCE_DIR}/system/${PROJECT_SYSTEM}")
endif()

# ========== 添加 include 路径 ==========
include_directories (${CMAKE_CURRENT_BINARY_DIR}/include)

# vim: expandtab:ts=2:sw=2:smartindent