/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_CLIENT_H
#define MICA_CLIENT_H

#include <openamp/remoteproc.h>
#include <openamp/rpmsg_virtio.h>

#if defined __cplusplus
extern "C" {
#endif

#ifdef DEBUG
#define DEBUG_PRINT(fmt, ...) do{ fprintf(stderr, "%s:%d:%s():\n\t" fmt, __FILE__, \
				     __LINE__, __func__, __VA_ARGS__); } while (0)
#else
#define DEBUG_PRINT(fmt, ...) do{ } while (0)
#endif

enum rproc_mode {
	RPROC_MODE_BARE_METAL = 0,
};

struct client_os_inst {
	const struct rpmsg_virtio_config *config;

	/* The static memory is deprecated and will be removed soon */
	metal_phys_addr_t	static_mem_base;
	unsigned int		static_mem_size;

	/* generic data structure */
	char			*path;	/* client os firmware path */
	unsigned int		cpu_id;	/* related arg: cpu id */
	enum			rproc_mode mode;/* The mechanism used to manage the lifecycle of a remoteproc */

	/* data structure needed by remote proc */
	struct remoteproc rproc;		/* remoteproc instance */

	/* shared memory buffer */
	/* TODO: add a lock for client */
	metal_phys_addr_t	phys_shmem_start;
	unsigned int		shmem_size;
	void			*virt_shmem_start;
	void			*virt_shmem_end;
	void			*unused_shmem_start;
	/* Metal I/O region of the shared memory buffer */
	struct metal_io_region	*shbuf_io;
	/* virtio buffer */
	struct rpmsg_virtio_shm_pool	vdev_shpool;
	/* rpmsg device */
	struct rpmsg_device		*rpdev;
	/* notification waiter */
	int				(*wait_event)(void);
};

#if defined __cplusplus
}
#endif

#endif	/* MICA_CLIENT_H */
