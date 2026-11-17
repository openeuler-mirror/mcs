/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RPMSG_VDEV_H
#define RPMSG_VDEV_H

#include "mica/mica_client.h"

#if defined __cplusplus
extern "C" {
#endif

int create_rpmsg_device(struct mica_client *client);
void release_rpmsg_device(struct mica_client *client);
/* Internal resource-table ring contract, not an arbitrary PA/DA translator. */
int mica_resolve_rsc_da(metal_phys_addr_t base, size_t bytes,
		      metal_phys_addr_t da, size_t len, metal_phys_addr_t *full);
struct remoteproc_mem *mica_vring_get_mem(struct remoteproc *rproc,
		const char *name, metal_phys_addr_t pa, metal_phys_addr_t da,
		void *va, size_t size, struct remoteproc_mem *buf);

#if defined __cplusplus
}
#endif

#endif	/* RPMSG_VDEV_H */
