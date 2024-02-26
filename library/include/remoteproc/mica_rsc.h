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

#include <openamp/rpmsg.h>

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
