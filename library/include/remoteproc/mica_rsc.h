/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_RSC_H
#define MICA_RSC_H

#if defined __cplusplus
extern "C" {
#endif

#include <stddef.h>
#include <openamp/rpmsg.h>
#include <openamp/remoteproc.h>

/**
 * mica_find_rsc - Find the offset of a resource type in the resource table
 * @rsc_table: Pointer to the resource table
 * @rsc_type: Type of the resource
 * @index: Index of the resource of the specified type
 *
 * Return the offset of the resource on success, or 0 if it is not found.
 *
 * OpenAMP no longer installs its private rsc_table_parser.h since 2026.04,
 * so keep a local copy here. The implementation is derived from OpenAMP's
 * lib/remoteproc/rsc_table_parser.c:
 * Copyright (c) 2014, Mentor Graphics Corporation
 * Copyright (c) 2018, Xilinx Inc.
 * SPDX-License-Identifier: BSD-3-Clause
 */
static inline size_t mica_find_rsc(void *rsc_table, unsigned int rsc_type,
				   unsigned int index)
{
	struct resource_table *r_table = rsc_table;
	struct fw_rsc_hdr *hdr;
	unsigned int i, rsc_index = 0;

	if (!r_table)
		return 0;

	for (i = 0; i < r_table->num; i++) {
		hdr = (void *)((char *)r_table + r_table->offset[i]);
		if (hdr->type != rsc_type)
			continue;
		if (rsc_index++ == index)
			return r_table->offset[i];
	}

	return 0;
}

/**
 * enum mica_resource_type - types of mica resource entries
 *
 * Range: [RSC_VENDOR_START, RSC_VENDOR_END]
 * @RSC_VENDOR_EPT_TABLE: List of bound endpoints
 *
 * For more details regarding a specific resource type, please see its
 * dedicated structure below.
 *
 * Please note that these values are used as indices to the handle_mica_rsc()
 * lookup table, so please keep them sane.
 *
 * These request entries should precede other shared resource entries
 * such as vdevs, vrings.
 */
enum mica_resource_type {
	RSC_VENDOR_EPT_TABLE = 128,
	RSC_VENDOR_RBUF_PAIR = 129,
	RSC_VENDOR_VRING_OFFSET = 130,
	RSC_VENDOR_XENSTORE = 131,
};

/**
 * struct fw_rsc_ept - List of bound endpoints
 * @num_of_epts: indicates how many bound endpoints
 * @endpoints: an array of @num_of_epts entries of 'struct ept_info'.

 * After binding the rpmsg endpoint, the host records the port addr of
 * that endpoint in the rsctable. if the host crashes, rpmsg communication
 * can be restored based on this information.
 */
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

/*
 * struct fw_rsc_rbuf_pair - Ring buffer resource
 * @type: resource type
 * @da: device address of the ring buffer
 * @pa: physical address of the ring buffer
 * @rb_len: length of the resource
 * @rb_num: number of ring buffers
 * @flags: IOMMU protection flags
 * @state: states like if the data is ready, and if the data has special meaning
 */
METAL_PACKED_BEGIN
struct fw_rsc_rbuf_pair {
	uint32_t type;
	uint32_t flags;
	uint64_t da;
	uint64_t pa;
	uint64_t len;
	volatile uint8_t state;
	uint8_t reserved[7];
} METAL_PACKED_END;

enum rbuf_state {
	RBUF_STATE_UNINIT = 0,
	RBUF_STATE_INIT = 1,
	RBUF_STATE_ORDINARY_DATA = 2,
	RBUF_STATE_CTRL_C = 3,
	RBUF_STATE_CPU_STOP = 4,
};

/**
 * struct vring_offset - Offset of vrings from shmem base addr
 * @type: resource type
 * @vring1_offset: offset of vring1
 */
#define MAX_NUM_OF_VRINGS 8
METAL_PACKED_BEGIN
struct fw_rsc_vring_offset {
	uint32_t type;
	uint32_t offset[MAX_NUM_OF_VRINGS];
} METAL_PACKED_END;

/**
 * handle_mica_rsc - Process our custom rsctable entries
 */
int handle_mica_rsc(struct remoteproc *rproc, void *rsc, size_t len);

/**
 * rsc_update_ept_table - Lookup the rpmsg_device and update RSC_VENDOR_EPT_TABLE
 */
int rsc_update_ept_table(struct remoteproc *rproc, struct rpmsg_device *rdev);

#if defined __cplusplus
}
#endif

#endif	/* MICA_RSC_H */
