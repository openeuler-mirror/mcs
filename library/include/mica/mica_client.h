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

#define MAX_NAME_LEN			32
#define MAX_FIRMWARE_PATH_LEN	128
#define MAX_CPUSTR_LEN			128
#define MAX_IOMEM_LEN			512
#define MAX_NETWORK_LEN			512

#ifdef DEBUG
#define DEBUG_PRINT(fmt, args...) do { syslog(LOG_DEBUG, "DEBUG: %s:%d:%s(): " fmt, \
				      __FILE_NAME__, __LINE__, __func__, ##args); } while (0)
#else
#define DEBUG_PRINT(fmt, ...) do { } while (0)
#endif

enum pedestal_type {
	BARE_METAL = 0,
	JAILHOUSE = 1,
	XEN = 2,
};

extern struct metal_list g_client_list;

struct pedestal_setup {
	char name[MAX_NAME_LEN];
	char cpu_str[MAX_CPUSTR_LEN];
	unsigned int cpu_id;
	int vcpu_num;
	int max_vcpu_num;
	int cpu_weight;
	int cpu_capacity;
	int memory; /* in MB */
	int max_memory; /* in MB */
	char iomem[MAX_IOMEM_LEN];
	char network[MAX_NETWORK_LEN];
};

struct pedestal_ops {
	int (*set_resource)(struct remoteproc *rproc, char *key, char *value);
};

struct mica_client {
	const struct rpmsg_virtio_config *config;
	/* if the binary supports gdb stub or not */
	bool debug;
	/* mica gdb server pthread_t */
	pthread_t gdb_server_thread;
	#ifdef RPMSG_TTY_USE_CLIENT_NAME
	/* client name */
	char			name[MAX_NAME_LEN];
	#endif
	/* client os firmware path */
	char			path[MAX_FIRMWARE_PATH_LEN];
	/* pedestal configuration */
	char			ped_cfg[MAX_FIRMWARE_PATH_LEN];
	struct pedestal_setup ped_setup;
	/* pedestal customized operations */
	struct pedestal_ops *ped_ops;

	/* The mechanism used to manage the lifecycle of a remoteproc */
	enum			pedestal_type ped;
	/* remoteproc instance */
	struct remoteproc rproc;

	/* shared memory buffer */
	/* TODO: add a lock for client */
	metal_phys_addr_t	phys_shmem_start;
	unsigned int		shmem_size;
	void			*virt_shmem_start;
	void			*virt_shmem_end;
	void			*unused_shmem_start;
	bool			shmem_dynamic;
	/* Metal I/O region of the shared memory buffer */
	struct metal_io_region	*shbuf_io;
	/* virtio buffer */
	struct rpmsg_virtio_shm_pool	vdev_shpool;
	/* rpmsg device */
	struct rpmsg_device		*rdev;

	/* ring buffer device */
	struct rbuf_device *rbuf_dev;

	/* the bound services */
	struct metal_list services;
	/* the client list */
	struct metal_list node;
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
 * @get_match_device: get the devices associated with this service.
 */
struct mica_service {
	struct metal_list node;
	char name[RPROC_MAX_NAME_LEN];
	void *priv;

	/*For user-defined service */
	int (*init)(struct mica_client *client, struct mica_service *svc);
	void (*remove)(struct mica_client *client, struct mica_service *svc);

	/*For rpmsg service */
	bool (*rpmsg_ns_match)(struct rpmsg_device *rdev,
				const char *name,
				uint32_t addr,
				uint32_t dest_addr,
				void *priv);
	void (*rpmsg_ns_bind_cb)(struct rpmsg_device *rdev,
				  const char *name,
				  uint32_t addr,
				  uint32_t dest_addr,
				  void *priv);
	void (*get_match_device)(char *str, size_t size, void *priv);
};

#if defined __cplusplus
}
#endif

#endif	/* MICA_CLIENT_H */
