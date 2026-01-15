# 根据 MICA_PED 选择对应的底座实现
if(NOT DEFINED MICA_PED)
    message(FATAL_ERROR "MICA_PED not defined! Please set it in toolchain file.")
endif()

message(STATUS "Building MICA pedestal: ${MICA_PED}")

collect(PROJECT_LIB_SOURCES ped_rsc_table.c)

if(MICA_PED STREQUAL "hetero")
    collect(PROJECT_LIB_SOURCES hetero.c)
    
elseif(MICA_PED STREQUAL "baremetal")
    collect(PROJECT_LIB_SOURCES baremetal.c)
    
else()
    message(FATAL_ERROR "Unknown MICA_PED value: '${MICA_PED}'")
    message(FATAL_ERROR "Supported values: hetero, baremetal")
endif()

message(STATUS "Pedestal sources: ${PROJECT_LIB_SOURCES}")
