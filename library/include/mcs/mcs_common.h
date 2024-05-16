/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MCS_COMMON_H
#define MCS_COMMON_H

#include <stdint.h>

#define MCS_DEVICE_NAME    "/dev/mcs"


#define STR_TO_HEX         16
#define STR_TO_DEC         10

#define PAGE_SIZE          4096
#define PAGE_MASK          (~(PAGE_SIZE - 1))
#define PAGE_ALIGN(addr)   ((addr & PAGE_MASK) + PAGE_SIZE)

/* common definitions for static  vring and virt dev */
/* normally, one virt device has two vrings */
#define VRING_COUNT                2
/* the size of virt device status register, 16 KB aligned */
#define VDEV_STATUS_SIZE           0x4000
/* the alignment inside vring */
#define VRING_ALIGNMENT            4
/* vring size, one item of vring can hold RING_BUFFER(512) bytes */
#define VRING_SIZE                 16

#endif /* MCS_COMMON_H */
