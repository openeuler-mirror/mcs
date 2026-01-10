# 收集 services 目录下的源文件
collect (PROJECT_LIB_SOURCES tty_service.c)
collect (PROJECT_LIB_SOURCES umt_service.c)

if (SUPPORT_RPC)
    collect (PROJECT_LIB_SOURCES rpc_service.c)
    add_subdirectory (rpc_proxy)
endif (SUPPORT_RPC)
