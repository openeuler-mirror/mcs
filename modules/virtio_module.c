#include <stdio.h>
#include <fcntl.h>
#include <string.h>
#include <sys/ioctl.h>

#include "openamp_module.h"

static struct virtio_vring_info rvrings[2] = {
	[0] = {
		.info.align = VRING_ALIGNMENT,
	},
	[1] = {
		.info.align = VRING_ALIGNMENT,
	},
};

static struct virtio_device vdev;
static struct rpmsg_virtio_device rvdev;
static struct metal_io_region *io;
struct virtqueue *vq[2];
static metal_phys_addr_t shm_physmap[] = { SHM_START_ADDR };
struct rpmsg_device *rdev;

static unsigned char virtio_get_status(struct virtio_device *vdev)
{
	return VIRTIO_CONFIG_STATUS_DRIVER_OK;
}

static void virtio_set_status(struct virtio_device *vdev, unsigned char status)
{
	*(volatile unsigned int *)VDEVADDR(shmaddr) = (unsigned int)status;
}

static uint32_t virtio_get_features(struct virtio_device *vdev)
{
	return 1 << VIRTIO_RPMSG_F_NS;
}

static void virtio_notify(struct virtqueue *vq)
{
	(void)vq;
	int ret, target_cpu;

	target_cpu = strtol(cpu_id, NULL, STR_TO_DEC);
	ret = ioctl(mcs_dev_fd, IOC_SENDIPI, &target_cpu);
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

static struct rpmsg_virtio_shm_pool shpool;

void virtio_init(void)
{
	int status = 0;

    printf("\nInitialize the virtio, virtqueue and rpmsg device\n");

	io = malloc(sizeof(struct metal_io_region));
	if (!io) {
		printf("malloc io failed\n");
		return;
	}
	metal_io_init(io, SHMEMADDR(shmaddr), shm_physmap, SHM_SIZE, -1, 0, NULL);

	/* setup vdev */
	vq[0] = virtqueue_allocate(VRING_SIZE);
	if (vq[0] == NULL) {
		printf("virtqueue_allocate failed to alloc vq[0]\n");
        free(io);
		return;
	}
	vq[1] = virtqueue_allocate(VRING_SIZE);
	if (vq[1] == NULL) {
		printf("virtqueue_allocate failed to alloc vq[1]\n");
        free(io);
		return;
	}

	vdev.role = RPMSG_HOST;
	vdev.vrings_num = VRING_COUNT;
	vdev.func = &dispatch;
	rvrings[0].io = io;
	rvrings[0].info.vaddr = TXADDR(shmaddr);
	rvrings[0].info.num_descs = VRING_SIZE;
	rvrings[0].info.align = VRING_ALIGNMENT;
	rvrings[0].vq = vq[0];

	rvrings[1].io = io;
	rvrings[1].info.vaddr = RXADDR(shmaddr);
	rvrings[1].info.num_descs = VRING_SIZE;
	rvrings[1].info.align = VRING_ALIGNMENT;
	rvrings[1].vq = vq[1];

	vdev.vrings_info = &rvrings[0];

	/* setup rvdev */
	rpmsg_virtio_init_shm_pool(&shpool, SHMEMADDR(shmaddr), SHM_SIZE);
	status = rpmsg_init_vdev(&rvdev, &vdev, ns_bind_cb, io, &shpool);
	if (status != 0) {
		printf("rpmsg_init_vdev failed %d\n", status);
		free(io);
		return;
	}

	rdev = rpmsg_virtio_get_rpmsg_device(&rvdev);
}

void virtio_deinit(void)
{
    rpmsg_deinit_vdev(&rvdev);

    if (io) {
        free(io);
	}
}
