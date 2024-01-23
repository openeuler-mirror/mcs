/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <syslog.h>
#include <metal/atomic.h>
#include <metal/alloc.h>

#include "memory/shm_pool.h"

void *shm_pool_phys_to_virt(struct mica_client *client, metal_phys_addr_t phys)
{
	struct metal_io_region *io = client->shbuf_io;

	return metal_io_phys_to_virt(io, phys);
}

metal_phys_addr_t shm_pool_virt_to_phys(struct mica_client *client, void *va)
{
	struct metal_io_region *io = client->shbuf_io;

	return metal_io_virt_to_phys(io, va);
}

int init_shmem_pool(struct mica_client *client, metal_phys_addr_t pa, size_t size)
{
	void *va;

	if (client->phys_shmem_start != 0) {
		syslog(LOG_ERR, "%s failed: the shared memory of this client has been registered\n",
			__func__);
		return -EPERM;
	}

	va = remoteproc_mmap(&client->rproc, &pa, NULL, size, 0, &client->shbuf_io);
	if (!va)
		return -EPERM;

	client->phys_shmem_start = pa;
	client->shmem_size = size;
	client->virt_shmem_start = va;
	client->virt_shmem_end = va + size;
	client->unused_shmem_start = va;
	DEBUG_PRINT("init shmem pool, pa: 0x%lx, size: 0x%x, va: 0x%p - 0x%p\n",
		     (unsigned long)pa, (unsigned int)size, va, va + size);
	return 0;
}

void *get_free_shmem(struct mica_client *client, size_t size)
{
	void *va;

	if (client->unused_shmem_start + size > client->virt_shmem_end) {
		syslog(LOG_ERR, "%s failed: no free shared memory found\n", __func__);
		DEBUG_PRINT("free shmem: 0x%p - 0x%p (size: 0x%lx), alloc size: 0x%lx\n",
			     client->unused_shmem_start, client->virt_shmem_end,
			     (size_t)(client->virt_shmem_end - client->unused_shmem_start), size);
		return NULL;
	}

	va = client->unused_shmem_start;
	client->unused_shmem_start += size;
	DEBUG_PRINT("alloc shmem: 0x%p - 0x%p\n", va, va + size);
	return va;
}
