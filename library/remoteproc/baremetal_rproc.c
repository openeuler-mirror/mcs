/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <sys/ioctl.h>
#include <stdio.h>

#include <metal/alloc.h>
#include <metal/io.h>
#include <openamp/remoteproc.h>

#include "memory/shm_pool.h"
#include "remoteproc/remoteproc_module.h"

struct cpu_info {
	uint32_t cpu;
	uint64_t boot_addr;
};

static int mcs_fd;
#define MCS_DEVICE_NAME    "/dev/mcs"
#define IOC_SENDIPI        _IOW('A', 0, int)
#define IOC_CPUON          _IOW('A', 1, int)
#define IOC_AFFINITY_INFO  _IOW('A', 2, int)

/*
 * Listen to events sent from the remote
 *
 * @returns: Returns a non-negative value when an event arrives, -1 on error.
 */
static int rproc_wait_event(void)
{
	int ret;
	struct pollfd fds = {
		.fd = mcs_fd,
		.events = POLLIN
	};

	while (1) {
		ret = poll(&fds, 1, -1);
		if (ret == -1) {
			fprintf(stderr, "%s failed: %s\n", __func__, strerror(errno));
			break;
		}

		if (fds.revents & POLLIN)
			break;
	}

	return ret;
}

static struct remoteproc *rproc_init(struct remoteproc *rproc,
				     const struct remoteproc_ops *ops, void *arg)
{
	int ret;
	struct client_os_inst *client = arg;

	if (!client)
		return NULL;

	rproc->ops = ops;
	rproc->priv = client;

	/* open mcs device for rproc->ops */
	mcs_fd = open(MCS_DEVICE_NAME, O_RDWR | O_SYNC);
	if (mcs_fd < 0) {
		fprintf(stderr, "open %s device failed, err %d\n", MCS_DEVICE_NAME, mcs_fd);
		return NULL;
	}

	/* set up the notification waiter */
	client->wait_event = rproc_wait_event;

	/*
	 * Call rproc->ops->mmap to create shared memory io
	 * TODO:get shared memory from mcs device
	 */
	ret = init_shmem_pool(client, client->static_mem_base, client->static_mem_size);
	if (ret){
		close(mcs_fd);
		fprintf(stderr, "init shared memory pool failed, err %d\n", ret);
		return NULL;
	}

	return rproc;
}

static void rproc_remove(struct remoteproc *rproc)
{
	close(mcs_fd);
	rproc->priv = NULL;
}

static void *rproc_mmap(struct remoteproc *rproc,
			metal_phys_addr_t *pa, metal_phys_addr_t *da,
			size_t size, unsigned int attribute,
			struct metal_io_region **io)
{
	void *va;
	size_t pagesize, aligned_size;
	struct remoteproc_mem *mem;
	metal_phys_addr_t lpa, lda, aligned_addr, offset;
	struct metal_io_region *tmpio;

	lpa = *pa;
	lda = *da;
	if (lpa == METAL_BAD_PHYS && lda == METAL_BAD_PHYS)
		return NULL;
	if (lpa == METAL_BAD_PHYS)
		lpa = lda;
	if (lda == METAL_BAD_PHYS)
		lda = lpa;

	/* align to page boundary */
	pagesize = sysconf(_SC_PAGE_SIZE);
	aligned_addr = (lpa) & ~(pagesize - 1);
	offset = lpa - aligned_addr;
	aligned_size = (offset + size + pagesize - 1) & ~(pagesize - 1);

	va = mmap(NULL, aligned_size, PROT_READ | PROT_WRITE, MAP_SHARED, mcs_fd, aligned_addr);
	if (va == MAP_FAILED) {
		fprintf(stderr, "mmap(0x%lx-0x%lx) failed: %s\n",
			aligned_addr, aligned_addr + aligned_size, strerror(errno));
		return NULL;
	}

	mem = metal_allocate_memory(sizeof(*mem));
	if (!mem)
		goto err_unmap;
	tmpio = metal_allocate_memory(sizeof(*tmpio));
	if (!tmpio) {
		metal_free_memory(mem);
		goto err_unmap;
	}
	remoteproc_init_mem(mem, NULL, lpa, lda, size, tmpio);
	metal_io_init(tmpio, va + offset, &mem->pa, size, -1, attribute, NULL);
	remoteproc_add_mem(rproc, mem);
	*pa = lpa;
	*da = lda;
	if (io)
		*io = tmpio;

	DEBUG_PRINT("mmap succeeded, paddr: 0x%lx, vaddr: 0x%p, size 0x%lx\n",
		     (unsigned long)mem->pa, va + offset, size);
	return metal_io_phys_to_virt(tmpio, mem->pa);

err_unmap:
	munmap(va, aligned_size);
	return NULL;
}

static int rproc_start(struct remoteproc *rproc)
{
	int ret;
	struct client_os_inst *client = rproc->priv;
	struct cpu_info info = {
		.cpu = client->cpu_id,
		.boot_addr = rproc->bootaddr
	};

	ret = ioctl(mcs_fd, IOC_CPUON, &info);
	if (ret < 0) {
		fprintf(stderr, "boot client os on CPU%d failed, err: %d\n", info.cpu, ret);
		return ret;
	}

	return 0;
}

static int rproc_shutdown(struct remoteproc *rproc)
{
	/* TODO:
	 * Delete all the registered remoteproc memories
	 * and tell clientos shut itself down by PSCI
	 */
	printf("shutdown rproc\n");
	return 0;
}

static int rproc_notify(struct remoteproc *rproc, uint32_t id)
{
	int ret;
	struct client_os_inst *client = (struct client_os_inst *)rproc->priv;
	struct cpu_info info = {
		.cpu = client->cpu_id,
	};

	(void)id;
	ret = ioctl(mcs_fd, IOC_SENDIPI, &info);
	if (ret < 0) {
		fprintf(stderr, "send ipi to CPU%d failed, err: %d\n", info.cpu, ret);
		return ret;
	}

	return 0;
}

const struct remoteproc_ops rproc_bare_metal_ops = {
	.init = rproc_init,
	.remove = rproc_remove,
	.start = rproc_start,
	.stop = NULL,
	.shutdown = rproc_shutdown,
	.mmap = rproc_mmap,
	.notify = rproc_notify,
};
