/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_CLIENT_H
#define MICA_CLIENT_H

#include <syslog.h>
#include <openamp/remoteproc.h>
#include <openamp/rpmsg_virtio.h>

#if defined __cplusplus
extern "C" {
#endif

#ifdef DEBUG
#define DEBUG_PRINT(fmt, args...) do{ syslog(LOG_DEBUG, "DEBUG: %s:%d:%s(): " fmt, \
				      __FILE__, __LINE__, __func__, ##args); } while (0)
#else
#define DEBUG_PRINT(fmt, ...) do{ } while (0)
#endif

enum rproc_mode {
	RPROC_MODE_BARE_METAL = 0,
};

struct mica_client {
	const struct rpmsg_virtio_config *config;

	/* The static memory is deprecated and will be removed soon */
	metal_phys_addr_t	static_mem_base;
	unsigned int		static_mem_size;

	/* client os firmware path */
	char			*path;
	/* the target CPU */
	unsigned int		cpu_id;

	/* The mechanism used to manage the lifecycle of a remoteproc */
	enum			rproc_mode mode;
	/* remoteproc instance */
	struct remoteproc rproc;

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
	struct rpmsg_device		*rdev;
	/* notification waiter */
	int				(*wait_event)(void);

	/* the bound services */
	struct metal_list services;
};

/**
 * struct mica_service
 * This structure presents the rpmsg/user-defined service
 *
 * @node:	List of service structures.
 * @name:	service name.
 * @priv:	Private data of the service.
 * @init:	The init() function gets called when the service is registered.
 * @remove:	The remove() function gets called when the client is stopped.
 * @rpmsg_ns_match: A match optional callback for rpmsg service, used to support "dynamic" name service.
 * @rpmsg_ns_bind_cb: rpmsg name service bind callback.
 */
struct mica_service {
	struct metal_list node;
	char name[RPROC_MAX_NAME_LEN];
	void *priv;

	/*For user-defined service */
	int (*init) (void *priv);
	void (*remove) (void *priv);

	/*For rpmsg service */
	bool (*rpmsg_ns_match) (struct rpmsg_device *rdev,
				const char *name,
				uint32_t dest,
				void *priv);
	void (*rpmsg_ns_bind_cb) (struct rpmsg_device *rdev,
				  const char *name,
				  uint32_t dest,
				  void *priv);
};

#if defined __cplusplus
}
#endif

#endif	/* MICA_CLIENT_H */
