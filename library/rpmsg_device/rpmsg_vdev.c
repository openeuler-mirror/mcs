/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <metal/alloc.h>
#include <metal/io.h>
#include <openamp/virtio.h>
#include <openamp/remoteproc.h>
#include <openamp/rsc_table_parser.h>

#include "mica/mica.h"
#include "memory/shm_pool.h"
#include "rpmsg/rpmsg_vdev.h"
#include "rpmsg/rpmsg_endpoint.h"

static int setup_vdev(struct client_os_inst *client)
{
	struct remoteproc *rproc;
	void *rsc_table, *buf;
	struct fw_rsc_vdev *vdev_rsc;
	struct fw_rsc_vdev_vring *vring_rsc;
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
		bufsz = vring_size(num_descs, align);
		buf = get_free_shmem(client, bufsz);
		if (!buf)
			return -ENOMEM;

		if (da == FW_RSC_U32_ADDR_ANY) {
			pa = shm_pool_virt_to_phys(client, buf);
			da = METAL_BAD_PHYS;
			(void *)remoteproc_mmap(rproc, &pa, &da, bufsz, 0, NULL);
			DEBUG_PRINT("alloc vring%i: paddr: 0x%lx, daddr: 0x%lx, vaddr: 0x%p, size: 0x%lx\n",
				    i, pa, da, buf, bufsz);
			vring_rsc->da = da;
		}
	}

	/*
	 * TODO: suppot rpmsg_virtio_config
	 */
	bufsz = 512 * vring_rsc->num * 2;
	buf = get_free_shmem(client, bufsz);
	if (!buf)
		return -ENOMEM;

	DEBUG_PRINT("alloc vdev buffer: paddr: 0x%lx, vaddr: 0x%p, size: 0x%lx\n",
		    shm_pool_virt_to_phys(client, buf), buf, bufsz);

	/* Only RPMsg virtio driver needs to initialize the shared buffers pool */
	rpmsg_virtio_init_shm_pool(&client->vdev_shpool, buf, bufsz);
	return 0;
}

int create_rpmsg_device(struct client_os_inst *client)
{
	int ret;
	struct rpmsg_virtio_device *rpmsg_vdev;
	struct virtio_device *vdev;

	rpmsg_vdev = metal_allocate_memory(sizeof(*rpmsg_vdev));
	if (!rpmsg_vdev)
		return -ENOMEM;

	ret = setup_vdev(client);
	if (ret != 0) {
		fprintf(stderr, "setup virtio device failed, err: %d\n", ret);
		goto err1;
	}

	/*
	 * Here we just use vdev 0
	 * TODO: create the corresponding vdev via client
	 */
	vdev = remoteproc_create_virtio(&client->rproc, 0, VIRTIO_DEV_DRIVER, NULL);
	if (!vdev) {
		fprintf(stderr, "create virtio device failed\n");
		ret = -EINVAL;
		goto err1;
	}

	ret =  rpmsg_init_vdev(rpmsg_vdev, vdev, ns_bind_cb,
			       client->shbuf_io,
			       &client->vdev_shpool);
	if (ret) {
		fprintf(stderr, "init rpmsg device failed, err: %d\n", ret);
		goto err2;
	}
	client->rpdev = rpmsg_virtio_get_rpmsg_device(rpmsg_vdev);
	return ret;

err2:
	remoteproc_remove_virtio(&client->rproc, vdev);
err1:
	metal_free_memory(rpmsg_vdev);
	return ret;
}