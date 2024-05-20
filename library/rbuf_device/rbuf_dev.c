/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <syslog.h>
#include <metal/alloc.h>
#include <metal/io.h>
#include <openamp/virtio.h>
#include <openamp/remoteproc.h>
#include <openamp/rsc_table_parser.h>

#include "mica/mica.h"
#include "memory/shm_pool.h"
#include "rbuf_device/rbuf_dev.h"
#include "rbuf_device/ring_buffer.h"
#include "remoteproc/mica_rsc.h"

#ifndef ALIGN_UP
#define ALIGN_UP(x, align_to)  (((x) + ((align_to)-1)) & ~((align_to)-1))
#endif

static int setup_rbuf_dev(struct mica_client *client)
{
	struct remoteproc *rproc;
	void *rsc_table;
	struct fw_rsc_rbuf_pair *rbuf_rsc;

	rproc = &client->rproc;
	rsc_table = rproc->rsc_table;

	size_t rbuf_rsc_offset = find_rsc(rsc_table, RSC_VENDOR_RBUF_PAIR, 0);
	if (!rbuf_rsc_offset)
		return -ENODEV;

	rbuf_rsc = (struct fw_rsc_rbuf_pair *)(rsc_table + rbuf_rsc_offset);
    void *rb_va = alloc_shmem_region(client, 0, rbuf_rsc->len);
	if (!rb_va)
		return -ENOMEM;

    struct rbuf_device *rbuf_dev = client->rbuf_dev;
    rbuf_dev->rx_va = rb_va;
    rbuf_dev->tx_va = rb_va + rbuf_rsc->len / 2;

    rbuf_rsc->pa = shm_pool_virt_to_phys(client, rb_va);
    /* for now, we do not support IOMMU, so the da should be equal to pa */
    rbuf_rsc->da = rbuf_rsc->pa;

    DEBUG_PRINT("alloc debug ring buffer: paddr: 0x%lx, vaddr: %p, size: 0x%lx\n",
		    rbuf_rsc->pa, rb_va, rbuf_rsc->len);

	/* init ring buffer */
	ring_buffer_pair_init(rbuf_dev->rx_va, rbuf_dev->tx_va, rbuf_dev->rbuf_len);
    rbuf_rsc->state = RBUF_STATE_INIT;

	return 0;
}

int create_rbuf_device(struct mica_client *client)
{
	int ret;
	struct rbuf_device *rbuf_dev;

	rbuf_dev = metal_allocate_memory(sizeof(*rbuf_dev));
	if (!rbuf_dev)
		return -ENOMEM;

    client->rbuf_dev = rbuf_dev;
	ret = setup_rbuf_dev(client);
	if (ret != 0) {
		syslog(LOG_ERR, "setup ring buffer device failed, err: %d\n", ret);
		goto err_free_rbuf_dev;
	}

	return 0;

err_free_rbuf_dev:
	metal_free_memory(rbuf_dev);
	return ret;
}

void destroy_rbuf_device(struct mica_client *client)
{
	struct rbuf_device *rbuf_dev;

	rbuf_dev = client->rbuf_dev;

    metal_free_memory(rbuf_dev);
}
