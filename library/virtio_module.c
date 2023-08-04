/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <fcntl.h>
#include <string.h>
#include <sys/ioctl.h>

#include "openamp_module.h"

static unsigned char virtio_get_status(struct virtio_device *vdev)
{
	return VIRTIO_CONFIG_STATUS_DRIVER_OK;
}

static void virtio_set_status(struct virtio_device *vdev, unsigned char status)
{
	struct client_os_inst *client = metal_container_of(vdev, struct client_os_inst, vdev);

	*(volatile unsigned char *)(client->vdev_status_reg) = status;
}

static uint32_t virtio_get_features(struct virtio_device *vdev)
{
#ifndef RPMSG_RPC_DEMO
	return 1 << VIRTIO_RPMSG_F_NS;
#else
	return 0;
#endif
}

static void virtio_notify(struct virtqueue *vq)
{
	struct client_os_inst *client = metal_container_of(vq->vq_dev, struct client_os_inst, vdev);
	int ret;

	ret = ioctl(client->mcs_fd, IOC_SENDIPI, &client->cpu_id);
	if (ret) {
		printf("send ipi tp second os failed\n");
	}

	return;
}

struct virtio_dispatch dispatch = {
	.get_status = virtio_get_status,
	.set_status = virtio_set_status,
	.get_features = virtio_get_features,
	.notify = virtio_notify,
};

void virtio_init(struct client_os_inst *client)
{
	int status = 0;
	void *share_mem_start;

    printf("\nInitialize the virtio, virtqueue and rpmsg device\n");

	client->io = malloc(sizeof(struct metal_io_region));
	if (!client->io) {
		printf("malloc io failed\n");
		return;
	}

	share_mem_start = client->virt_shared_mem + client->vdev_status_size;
	client->shm_physmap[0] = client->phy_shared_mem + client->vdev_status_size;

	metal_io_init(client->io, share_mem_start, client->shm_physmap,
		client->shared_mem_size - client->vdev_status_size, -1, 0, NULL);

	printf("virt add:%p, status_reg:%p, tx:%p, rx:%p, mempool:%p\n",
    	client->virt_shared_mem, client->vdev_status_reg, client->virt_tx_addr,
    	client->virt_rx_addr, share_mem_start);

	/* setup vdev */
	client->vq[0] = virtqueue_allocate(client->vring_size);
	if (client->vq[0] == NULL) {
		printf("virtqueue_allocate failed to alloc vq[0]\n");
        free(client->io);
		return;
	}
	client->vq[1] = virtqueue_allocate(client->vring_size);
	if (client->vq[1] == NULL) {
		printf("virtqueue_allocate failed to alloc vq[1]\n");
        free(client->io);
		return;
	}

	client->vdev.role = RPMSG_HOST;
	client->vdev.vrings_num = VRING_COUNT;
	client->vdev.func = &dispatch;

	client->rvrings[0].io = client->io;
	client->rvrings[0].info.vaddr = client->virt_tx_addr;
	client->rvrings[0].info.num_descs = client->vring_size;
	client->rvrings[0].info.align = VRING_ALIGNMENT;
	client->rvrings[0].vq = client->vq[0];

	client->rvrings[1].io = client->io;
	client->rvrings[1].info.vaddr = client->virt_rx_addr;
	client->rvrings[1].info.num_descs = client->vring_size;
	client->rvrings[1].info.align = VRING_ALIGNMENT;
	client->rvrings[1].vq = client->vq[1];

	client->vdev.vrings_info = &client->rvrings[0];

	/* setup rvdev */
	rpmsg_virtio_init_shm_pool(&client->shpool, share_mem_start,
			client->shared_mem_size - client->vdev_status_size);
#ifndef RPMSG_RPC_DEMO
	status = rpmsg_init_vdev(&client->rvdev, &client->vdev, ns_bind_cb,
			 client->io, &client->shpool);
#else
	status = rpmsg_init_vdev(&client->rvdev, &client->vdev, NULL,
			 client->io, &client->shpool);
#endif
	if (status != 0) {
		printf("rpmsg_init_vdev failed %d\n", status);
		free(client->io);
		return;
	}
}

void virtio_deinit(struct client_os_inst *client)
{
	/* currently virtio_deinit is called in openamp_deinit
	 * and after destory_remoteproc. Becasue destory_remoteproc
	 * will reset the remote processor, so no need to call
	 * rpmsg_deinit_vdev to avoid extra ipi interrupts
	 */
	/* rpmsg_deinit_vdev(&client->rvdev); */

    if (client->io) {
        free(client->io);
	}
}
