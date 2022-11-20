#ifndef MCS_COMMON_H
#define MCS_COMMON_H

#define MCS_DEVICE_NAME    "/dev/mcs"

#define IOC_SENDIPI        _IOW('A', 0, int)
#define IOC_CPUON          _IOW('A', 1, int)
#define IOC_AFFINITY_INFO  _IOW('A', 2, int)

#define STR_TO_HEX         16
#define STR_TO_DEC         10

#define PAGE_SIZE          4096
#define PAGE_MASK          (~(PAGE_SIZE - 1))
#define PAGE_ALIGN(addr)   ((addr & PAGE_MASK) + PAGE_SIZE)

#endif /* MCS_COMMON_H */
