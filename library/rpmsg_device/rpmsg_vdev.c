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
#include "rpmsg/rpmsg_vdev.h"
#include "rpmsg/rpmsg_service.h"

#ifndef ALIGN_UP
#define ALIGN_UP(x, align_to)  (((x) + ((align_to)-1)) & ~((align_to)-1))
#endif

static int setup_vdev(struct mica_client *client)
{
	struct remoteproc *rproc;
	void *rsc_table, *buf;
	struct fw_rsc_vdev *vdev_rsc;
	struct fw_rsc_vdev_vring *vring_rsc = NULL;
	size_t vdev_rsc_offset, bufsz;
	unsigned int num_vrings, i;

	rproc = &client->rproc;
	rsc_table = rproc->rsc_table;

	/*
	 * Here we just set up vdev 0
	 * TODO: Initialise the corresponding vdev via client.
	 */
	vdev_rsc_offset = find_rsc(rsc_table, RSC_VDEV, 0);
	if (!vdev_rsc_offset)
		return -ENODEV;

	vdev_rsc = (struct fw_rsc_vdev *)(rsc_table + vdev_rsc_offset);
	num_vrings = vdev_rsc->num_of_vrings;

	/* alloc vrings */
	for (i = 0; i < num_vrings; i++) {
		metal_phys_addr_t da, pa;
		unsigned int num_descs, align;

		vring_rsc = &vdev_rsc->vring[i];
		da = vring_rsc->da;
		num_descs = vring_rsc->num;
		align = vring_rsc->align;
		bufsz = ALIGN_UP(vring_size(num_descs, align), align);

		if (da == FW_RSC_U32_ADDR_ANY) {
			buf = alloc_shmem_region(client, 0, bufsz);
			if (!buf)
				return -ENOMEM;

			pa = shm_pool_virt_to_phys(client, buf);
			da = METAL_BAD_PHYS;
			(void *)remoteproc_mmap(rproc, &pa, &da, bufsz, 0, NULL);
			DEBUG_PRINT("alloc vring%i: paddr: 0x%lx, daddr: 0x%lx, vaddr: %p, size: 0x%lx\n",
				    i, pa, da, buf, bufsz);
			vring_rsc->da = da;
		} else {
			buf = alloc_shmem_region(client, da, bufsz);
			if (!buf)
				return -ENOMEM;

			pa = METAL_BAD_PHYS;
			(void *)remoteproc_mmap(rproc, &pa, &da, bufsz, 0, NULL);
			DEBUG_PRINT("restore vring%i: paddr: 0x%lx, daddr: 0x%lx, vaddr: %p, size: 0x%lx\n",
				    i, pa, da, buf, bufsz);
		}
	}

	/*
	 * TODO: suppot rpmsg_virtio_config
	 */
	if (!vring_rsc)
		return -ENODEV;
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

	rpmsg_vdev = metal_container_of(client->rdev, struct rpmsg_virtio_device, rdev);

	/* destroy all epts */
	rpmsg_deinit_vdev(rpmsg_vdev);

	remoteproc_remove_virtio(&client->rproc, rpmsg_vdev->vdev);
}
