/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __PED_RESOURCE_TABLE_H__
#define __PED_RESOURCE_TABLE_H__

#include <stddef.h>
#include <openamp/rpmsg.h>
#include <openamp/remoteproc.h>
#include "ped_openamp.h"

#ifdef __cplusplus
extern "C" {
#endif

#define RSC_VENDOR_EPT_TABLE    128 /* List of bound endpoints */
#define RSC_VENDOR_VRING_OFFSET 130 /* Offset of vrings from shmem base addr */

enum rsc_table_entries {
    RSC_TABLE_EPT_TABLE_ENTRY,
#ifdef OS_GDB_STUB
    RSC_TABLE_RBUF_ENTRY,
#endif
    RSC_TABLE_VDEV_ENTRY,
#ifdef OS_OPTION_XEN
    RSC_TABLE_VRING_OFFSET_ENTRY,
#endif
    RSC_TABLE_NUM_ENTRY
};

METAL_PACKED_BEGIN
struct ept_info {
    char name[RPMSG_NAME_SIZE];
    uint32_t addr;
    uint32_t dest_addr;
} METAL_PACKED_END;

#define MAX_NUM_OF_EPTS 64

METAL_PACKED_BEGIN
struct fw_rsc_ept {
    uint32_t type;
    uint32_t num_of_epts;
    struct ept_info endpoints[MAX_NUM_OF_EPTS];
} METAL_PACKED_END;

#ifdef OS_OPTION_XEN
#define MAX_NUM_OF_VRINGS 8
METAL_PACKED_BEGIN
struct fw_rsc_vring_offset {
	uint32_t type;
	uint32_t offset[MAX_NUM_OF_VRINGS];
} METAL_PACKED_END;
#endif

#ifdef OS_GDB_STUB
#define RSC_VENDOR_RINGBUFFER   129
#define RINGBUFFER_TOTAL_SIZE   0x2000

METAL_PACKED_BEGIN
struct fw_rsc_rbuf_pair {
	uint32_t type;
	uint32_t flags;
	uint64_t da;
	uint64_t pa;
	uint64_t len;
	uint8_t state;
	uint8_t reserved[7];
} METAL_PACKED_END;

enum rbuf_state {
	RBUF_STATE_UNINIT = 0,
	RBUF_STATE_INIT = 1,
	RBUF_STATE_ORDINARY_DATA = 2,
	RBUF_STATE_CTRL_C = 3,
	RBUF_STATE_RESTART = 4,
};

extern uint8_t get_rbuf_state(void);

#endif

METAL_PACKED_BEGIN
struct fw_resource_table {
    unsigned int ver;
    unsigned int num;
    unsigned int reserved[2];
    unsigned int offset[RSC_TABLE_NUM_ENTRY];

    struct fw_rsc_ept ept_table;
#ifdef OS_GDB_STUB
    struct fw_rsc_rbuf_pair rbufs;
#endif
    struct fw_rsc_vdev vdev;
    struct fw_rsc_vdev_vring vring0;
    struct fw_rsc_vdev_vring vring1;
#ifdef OS_OPTION_XEN
    struct fw_rsc_vring_offset vring_offset;
#endif

} METAL_PACKED_END;



void rsc_table_get(void **table_ptr, int *length);

#ifdef __cplusplus
}
#endif

#endif /* __PED_RESOURCE_TABLE_H__ */
