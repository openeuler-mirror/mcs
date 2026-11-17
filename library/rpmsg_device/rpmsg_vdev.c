/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <limits.h>
#include <syslog.h>
#include <metal/alloc.h>
#include <metal/io.h>
#include <openamp/virtio.h>
#include <openamp/remoteproc.h>

#include "mica/mica.h"
#include "memory/shm_pool.h"
#include "rpmsg/rpmsg_vdev.h"
#include "rpmsg/rpmsg_service.h"
#include <remoteproc/mica_rsc.h>

#ifndef ALIGN_UP
#define ALIGN_UP(x, align_to)  (((x) + ((align_to)-1)) & ~((align_to)-1))
#endif

int mica_resolve_rsc_da(metal_phys_addr_t base, size_t bytes,
		      metal_phys_addr_t da, size_t len, metal_phys_addr_t *full)
{
	if (!full || !bytes || !len || (uint64_t)bytes > (uint64_t)UINT32_MAX + 1 ||
	    da == METAL_BAD_PHYS || da == FW_RSC_U32_ADDR_ANY || da > UINT32_MAX ||
	    bytes - 1 > METAL_BAD_PHYS - base)
		return -EINVAL;
	size_t offset = (uint32_t)((uint32_t)da - (uint32_t)base);

	if (offset >= bytes || len > bytes - offset || base + offset == METAL_BAD_PHYS)
		return -EINVAL;
	*full = base + offset;
	return 0;
}

static int vring_geometry(const struct fw_rsc_vdev_vring *ring, size_t *length)
{
	/* These bounds make vring_size's unsigned arithmetic safe on 32/64 bit. */
	if (!ring->num || ring->num > 32768 || (ring->num & (ring->num - 1)) ||
	    !ring->align || ring->align > INT_MAX || (ring->align & (ring->align - 1)))
		return -EINVAL;
	*length = vring_size(ring->num, ring->align);
	return 0;
}

/* Validate before consulting any resource offsets, including the vendor entry.
 * The remoteproc lock is already held when get_mem is called by OpenAMP. */
static int vring_resources(struct mica_client *client, struct fw_rsc_vdev **vdev,
			   struct fw_rsc_vring_offset **offsets)
{
	struct remoteproc *rproc = &client->rproc;
	struct resource_table *table = rproc->rsc_table;
	size_t bytes = rproc->rsc_len;

	*vdev = NULL;
	*offsets = NULL;
	if (!table || bytes < sizeof(*table) ||
	    table->num > (bytes - sizeof(*table)) / sizeof(table->offset[0]))
		return -EINVAL;
	size_t header = sizeof(*table) + table->num * sizeof(table->offset[0]);
	size_t vdev_off = 0, offsets_off = 0;

	for (unsigned int i = 0; i < table->num && (!*vdev || (client->shmem_dynamic && !*offsets)); ++i) {
		size_t off = table->offset[i];
		struct fw_rsc_hdr *hdr;

		if (off < header || off > bytes || sizeof(*hdr) > bytes - off)
			return -EINVAL;
		hdr = (void *)((char *)table + off);
		if (hdr->type == RSC_VDEV && !*vdev) {
			vdev_off = off;
			*vdev = (void *)hdr;
		} else if (client->shmem_dynamic && hdr->type == RSC_VENDOR_VRING_OFFSET && !*offsets) {
			offsets_off = off;
			*offsets = (void *)hdr;
		}
	}
	if (!*vdev || (client->shmem_dynamic && !*offsets) || sizeof(**vdev) > bytes - vdev_off ||
	    !(*vdev)->num_of_vrings ||
	    (client->shmem_dynamic && (*vdev)->num_of_vrings > MAX_NUM_OF_VRINGS) ||
	    (*vdev)->num_of_vrings > (bytes - vdev_off - sizeof(**vdev)) / sizeof((*vdev)->vring[0]))
		return -EINVAL;
	size_t vdev_end = vdev_off + sizeof(**vdev) + (*vdev)->num_of_vrings * sizeof((*vdev)->vring[0]);
	size_t offsets_end = offsets_off + sizeof(uint32_t) * (1 + (*vdev)->num_of_vrings);

	if (client->shmem_dynamic && offsets_end > bytes)
		return -EINVAL;
	/* Only the extents actually used here must not overlap another start.
	 * Duplicate indices and unrelated payload/config bytes are OpenAMP's domain. */
	for (unsigned int i = 0; i < table->num; ++i) {
		size_t off = table->offset[i];

		if ((off > vdev_off && off < vdev_end) ||
		    (client->shmem_dynamic && off > offsets_off && off < offsets_end))
			return -EINVAL;
	}
	return 0;
}

static int vring_high_pool(struct mica_client *client, bool *high)
{
	*high = false;
	if (!client->rproc.ops || client->rproc.ops->get_mem != mica_vring_get_mem)
		return 0;
	if (!client->shmem_size || client->phys_shmem_start == METAL_BAD_PHYS ||
	    client->shmem_size - 1 > METAL_BAD_PHYS - client->phys_shmem_start)
		return -EINVAL;
	*high = client->phys_shmem_start + (client->shmem_size - 1) > UINT32_MAX;
	return 0;
}

static int vring_address(struct mica_client *client, struct fw_rsc_vdev_vring *ring,
			 struct fw_rsc_vring_offset *offsets, unsigned int index,
			 size_t length, metal_phys_addr_t *full)
{
	int ret = mica_resolve_rsc_da(client->phys_shmem_start, client->shmem_size,
				      ring->da, length, full);

	if (ret)
		return ret;
	if (client->shmem_dynamic && offsets->offset[index] != *full - client->phys_shmem_start)
		return -EINVAL;
	return 0;
}

struct remoteproc_mem *mica_vring_get_mem(struct remoteproc *rproc,
		const char *name, metal_phys_addr_t pa, metal_phys_addr_t da,
		void *va, size_t size, struct remoteproc_mem *buf)
{
	struct fw_rsc_vdev *vdev;
	struct fw_rsc_vring_offset *offsets;
	bool high;

	if (!rproc || name || va || pa != METAL_BAD_PHYS || da == METAL_BAD_PHYS ||
	    da == FW_RSC_U32_ADDR_ANY || da > UINT32_MAX || !size || !buf ||
	    rproc->state != RPROC_RUNNING)
		return NULL;
	struct mica_client *client = metal_container_of(rproc, struct mica_client, rproc);

	if (vring_high_pool(client, &high) || !high || !client->shbuf_io ||
	    vring_resources(client, &vdev, &offsets))
		return NULL;
	/* MCS RUNNING callers with exact DA/vring_size are ring setup only.
	 * Re-audit this identification if future RUNNING callers reuse that pair.
	 * No allocation/list alias, lock recursion or ownership of the borrowed IO. */
	for (unsigned int i = 0; i < vdev->num_of_vrings; ++i) {
		metal_phys_addr_t full;
		size_t length;

		if (vring_geometry(&vdev->vring[i], &length))
			return NULL;
		if (vdev->vring[i].da == da && length == size &&
		    !vring_address(client, &vdev->vring[i], offsets, i, length, &full)) {
			remoteproc_init_mem(buf, NULL, full, da, length, client->shbuf_io);
			return buf;
		}
	}
	return NULL;
}

static int check_vring_mappings(struct mica_client *client, struct virtio_device *actual)
{
	struct fw_rsc_vdev *vdev;
	struct fw_rsc_vring_offset *offsets;
	bool high;

	if (vring_high_pool(client, &high))
		return -EINVAL;
	if (!high)
		return 0;
	if (vring_resources(client, &vdev, &offsets) || !actual->vrings_info ||
	    actual->vrings_num != vdev->num_of_vrings)
		return -EINVAL;
	for (unsigned int i = 0; i < actual->vrings_num; ++i) {
		struct virtio_vring_info *info = &actual->vrings_info[i];
		struct metal_io_region *io = info->io;
		void *va = info->info.vaddr;
		metal_phys_addr_t full;
		size_t length;

		if (vring_geometry(&vdev->vring[i], &length) ||
		    vring_address(client, &vdev->vring[i], offsets, i, length, &full) ||
		    info->info.num_descs != vdev->vring[i].num || info->info.align != vdev->vring[i].align ||
		    !io || !va || va == METAL_BAD_VA || !io->virt || io->virt == METAL_BAD_VA ||
		    (uintptr_t)va % info->info.align || !io->physmap ||
		    io->ops.offset_to_phys || io->ops.phys_to_offset ||
		    !io->size || io->size - 1 > UINTPTR_MAX - (uintptr_t)io->virt ||
		    io->page_shift < sizeof(metal_phys_addr_t) * CHAR_BIT || io->page_mask != METAL_BAD_PHYS)
			return -EINVAL;
		unsigned long off = metal_io_virt_to_offset(io, va);

		if (off == METAL_BAD_OFFSET || off >= io->size || length > io->size - off ||
		    length - 1 > METAL_BAD_PHYS - full || metal_io_virt_to_phys(io, va) != full)
			return -EINVAL;
	}
	return 0;
}

static int setup_vdev(struct mica_client *client)
{
	struct remoteproc *rproc;
	void *buf;
	struct fw_rsc_vdev *vdev_rsc;
	struct fw_rsc_vdev_vring *vring_rsc = NULL;
	struct fw_rsc_vring_offset *vring_offset_rsc = NULL;
	size_t bufsz;
	unsigned int num_vrings, i;
	bool high;
	int ret;

	rproc = &client->rproc;

	/*
	 * Here we just set up vdev 0
	 * TODO: Initialise the corresponding vdev via client.
	 */
	ret = vring_resources(client, &vdev_rsc, &vring_offset_rsc);
	if (ret || vring_high_pool(client, &high))
		return -EINVAL;
	num_vrings = vdev_rsc->num_of_vrings;

	/* alloc vrings */
	for (i = 0; i < num_vrings; i++) {
		metal_phys_addr_t da, pa;
		size_t length;

		vring_rsc = &vdev_rsc->vring[i];
		da = vring_rsc->da;
		if (vring_geometry(vring_rsc, &length) || length > SIZE_MAX - (vring_rsc->align - 1))
			return -EINVAL;
		bufsz = ALIGN_UP(length, vring_rsc->align);

		if (da == FW_RSC_U32_ADDR_ANY) {
			buf = alloc_shmem_region(client, 0, bufsz);
			if (!buf)
				return -ENOMEM;

			pa = shm_pool_virt_to_phys(client, buf);
			da = METAL_BAD_PHYS;
			if (pa == METAL_BAD_PHYS || !remoteproc_mmap(rproc, &pa, &da, bufsz, 0, NULL))
				return -EINVAL;
			if (high && (da != pa || pa < client->phys_shmem_start ||
			    pa - client->phys_shmem_start >= client->shmem_size ||
			    bufsz > client->shmem_size - (pa - client->phys_shmem_start)))
				return -EINVAL;
			DEBUG_PRINT("alloc vring%i: paddr: 0x%lx, daddr: 0x%lx, vaddr: %p, size: 0x%lx\n",
				    i, pa, da, buf, bufsz);

			/* 
			 * For dynamic shmem, mica and client has different base addr (e.g. xen). Use vring offset.
			 * For static shmem, mica and client use the same shmem addr. No need to use vring offset.
			 */
			if (client->shmem_dynamic) {
				if (pa < client->phys_shmem_start || pa - client->phys_shmem_start > UINT32_MAX)
					return -EINVAL;
				vring_offset_rsc->offset[i] = pa - client->phys_shmem_start;
				DEBUG_PRINT("vring%i offset: 0x%x\n", i, vring_offset_rsc->offset[i]);
			}
			vring_rsc->da = da;
		} else {
			if (high && vring_address(client, vring_rsc, vring_offset_rsc, i, bufsz, &da))
				return -EINVAL;
			buf = alloc_shmem_region(client, da, bufsz);
			if (!buf)
				return -ENOMEM;

			pa = METAL_BAD_PHYS;
			if (!remoteproc_mmap(rproc, &pa, &da, bufsz, 0, NULL))
				return -EINVAL;
			DEBUG_PRINT("restore vring%i: paddr: 0x%lx, daddr: 0x%lx, vaddr: %p, size: 0x%lx\n",
				    i, pa, da, buf, bufsz);
		}
	}

	/*
	 * TODO: suppot rpmsg_virtio_config
	 */
	bufsz = 512 * vring_rsc->num * 2;
	buf = alloc_shmem_region(client, 0, bufsz);
	if (!buf)
		return -ENOMEM;

	/* zero descriptor area */
	memset(buf, 0, bufsz);

	DEBUG_PRINT("alloc vdev buffer: paddr: 0x%lx, vaddr: %p, size: 0x%lx\n",
		    shm_pool_virt_to_phys(client, buf), buf, bufsz);

	/* Only RPMsg virtio driver needs to initialize the shared buffers pool */
	rpmsg_virtio_init_shm_pool(&client->vdev_shpool, buf, bufsz);
	return 0;
}

int create_rpmsg_device(struct mica_client *client)
{
	int ret;
	struct rpmsg_virtio_device *rpmsg_vdev;
	struct virtio_device *vdev;

	rpmsg_vdev = metal_allocate_memory(sizeof(*rpmsg_vdev));
	if (!rpmsg_vdev)
		return -ENOMEM;

	ret = setup_vdev(client);
	if (ret != 0) {
		syslog(LOG_ERR, "setup virtio device failed, err: %d\n", ret);
		goto err1;
	}

	/*
	 * Here we just use vdev 0
	 * TODO: create the corresponding vdev via client
	 */
	vdev = remoteproc_create_virtio(&client->rproc, 0, VIRTIO_DEV_DRIVER, NULL);
	if (!vdev) {
		syslog(LOG_ERR, "create virtio device failed\n");
		ret = -EINVAL;
		goto err1;
	}

	/* OpenAMP rereads u32 DAs and searches mems before get_mem. Verify its
	 * actual result before rpmsg_init_vdev can write any ring content. */
	ret = check_vring_mappings(client, vdev);
	if (ret)
		goto err2;
	ret = rpmsg_init_vdev(rpmsg_vdev, vdev, mica_ns_bind_cb,
			      client->shbuf_io,
			      &client->vdev_shpool);
	if (ret) {
		syslog(LOG_ERR, "init rpmsg device failed, err: %d\n", ret);
		goto err2;
	}
	client->rdev = rpmsg_virtio_get_rpmsg_device(rpmsg_vdev);
	return ret;

err2:
	remoteproc_remove_virtio(&client->rproc, vdev);
err1:
	metal_free_memory(rpmsg_vdev);
	return ret;
}

void release_rpmsg_device(struct mica_client *client)
{
	struct rpmsg_virtio_device *rpmsg_vdev;
	struct virtio_device *vdev;

	if (!client->rdev)
		return;

	rpmsg_vdev = metal_container_of(client->rdev, struct rpmsg_virtio_device, rdev);

	/*
	 * rpmsg_deinit_vdev() clears rpmsg_vdev->vdev, so save it first.
	 * Otherwise remoteproc_remove_virtio() is a no-op and the virtio
	 * device stays in rproc->vdevs with already freed virtqueues:
	 * the next create_rpmsg_device() returns that dead vdev and frees
	 * the dangling virtqueues again.
	 */
	vdev = rpmsg_vdev->vdev;

	/* destroy all epts */
	rpmsg_deinit_vdev(rpmsg_vdev);

	remoteproc_remove_virtio(&client->rproc, vdev);
	metal_free_memory(rpmsg_vdev);
	client->rdev = NULL;
}
