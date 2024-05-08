/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <metal/cache.h>
#include <openamp/rsc_table_parser.h>

#include <remoteproc/mica_rsc.h>
#include <rpmsg/rpmsg_service.h>

#include <syslog.h>

int handle_mica_rsc(struct remoteproc *rproc, void *rsc, size_t len)
{
	int i;
	uint32_t rsc_type;

	rsc_type = ((struct fw_rsc_vendor *)rsc)->type;
	switch (rsc_type) {
	case RSC_VENDOR_EPT_TABLE:
	{
		struct fw_rsc_ept *ept_rsc = rsc;
		struct ept_info *ept;

		for (i = 0; i < ept_rsc->num_of_epts; i++) {
			ept = &ept_rsc->endpoints[i];
			if (ept->addr != 0)
				register_remote_ept(ept->name, ept->addr, ept->dest_addr);
		}
		break;
	}
	default:
		break;
	}

	return 0;
}

int rsc_update_ept_table(struct remoteproc *rproc, struct rpmsg_device *rdev)
{
	void *rsc_table = rproc->rsc_table;
	size_t ept_rsc_offset;
	struct fw_rsc_ept *ept_rsc;
	struct metal_list *node;
	struct rpmsg_endpoint *ept;
	uint32_t i;

	ept_rsc_offset = find_rsc(rsc_table, RSC_VENDOR_EPT_TABLE, 0);
	/* If there is no ept table, do nothing */
	if (ept_rsc_offset == 0)
		return 0;

	ept_rsc = (struct fw_rsc_ept *)(rsc_table + ept_rsc_offset);
	memset(ept_rsc, 0, sizeof(*ept_rsc));
	ept_rsc->type = RSC_VENDOR_EPT_TABLE;

	metal_list_for_each(&rdev->endpoints, node) {
		ept = metal_container_of(node, struct rpmsg_endpoint, node);

		/* just process the bound endpoints */
		if (ept->addr == RPMSG_ADDR_ANY || ept->dest_addr == RPMSG_ADDR_ANY)
			continue;

		i = ept_rsc->num_of_epts;
		if (i >= MAX_NUM_OF_EPTS)
			return -ENOSPC;

		/*
		 * Note that we are walking through the endpoint of the host,
		 * we need to store the remote endpoint information in RSC_VENDOR_EPT_TABLE.
		 * So the addresses of dest and src need to be exchanged.
		 */
		ept_rsc->endpoints[i].addr = ept->dest_addr;
		ept_rsc->endpoints[i].dest_addr = ept->addr;
		strlcpy(ept_rsc->endpoints[i].name, ept->name, RPMSG_NAME_SIZE);
		ept_rsc->num_of_epts++;
	}

	metal_cache_flush(ept_rsc, sizeof(*ept_rsc));
	return 0;
}
