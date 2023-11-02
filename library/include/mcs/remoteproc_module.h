/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef REMOTEPROC_MODULE_H
#define REMOTEPROC_MODULE_H

#include <openamp/remoteproc.h>

#if defined __cplusplus
extern "C" {
#endif

#define CPU_STATE_ON          0
#define CPU_STATE_OFF         1
#define CPU_STATE_ON_PENDING  2

struct client_os_inst {
	/* data structure needed by remote proc */
	struct remoteproc rproc;		/* remoteproc instance */

	/* data structure needed by virtio */
	struct virtio_device vdev;		/* vdevice */
	struct rpmsg_virtio_device rvdev;	/* rpmsg virtio dev */
	struct metal_io_region *io;
	struct virtqueue *vq[VRING_COUNT];
	metal_phys_addr_t shm_physmap[1];
	struct virtio_vring_info rvrings[VRING_COUNT];
	struct rpmsg_virtio_shm_pool shpool;
	unsigned long phy_shared_mem;		/* the physical address of shared mem */
	void *virt_shared_mem;			/* the virtual address of shared mem */
	unsigned int shared_mem_size;		/* shared mem size */
	void  *vdev_status_reg;			/*  virtual device status register */
	unsigned int vdev_status_size;
	unsigned int vring_size;
	void *virt_tx_addr;
	void *virt_rx_addr;
	const struct rpmsg_virtio_config *config;

	/* generic data structure */
	char *path;			/* client os firmware path */
	unsigned int cpu_id;     	/* related arg: cpu id */
	int mcs_fd;			/* mcs device fd */
	unsigned long load_address;	/* physical load address */
	unsigned long entry; 		/* physical entry of client os, can be the same as load address */
};

/* create remoteproc */
int create_remoteproc(struct client_os_inst *client);

/* destory remoteproc */
void destory_remoteproc(struct client_os_inst *client);

#if defined __cplusplus
}
#endif

#endif	/* REMOTEPROC_MODULE_H */
