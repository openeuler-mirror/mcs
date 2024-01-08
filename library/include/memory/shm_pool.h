/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef SHM_POOL_H
#define SHM_POOL_H

#include <metal/io.h>

#include "mica/mica_client.h"

#if defined __cplusplus
extern "C" {
#endif

/**
 * Use the client's shbuf_io to convert a physical address to virtual address.
 *
 * @param[in]	client	The client OS that associated with shm_pool.
 * @param[in]	phys	Physical address.
 * @return	NULL if out of range, or corresponding virtual address.
 */
void *shm_pool_phys_to_virt(struct client_os_inst *client, metal_phys_addr_t phys);

/**
 * Use the client's shbuf_io to convert a virtual address to physical address.
 *
 * @param[in]	client	The client OS that associated with shm_pool.
 * @param[in]	va	Virtual address within segment.
 * @return	METAL_BAD_PHYS if out of range, or corresponding physical address.
 */
metal_phys_addr_t shm_pool_virt_to_phys(struct client_os_inst *client, void *va);

/**
 * Initialize a shared memory pool for a client.
 *
 * @param[in]	client	The client OS that associated with shm_pool.
 * @param[in]	pa	The physical starting address of shared memory pool.
 * @param[in]	size	The size of shared memory pool.
 * @return	Return 0 on success, negative errno on failure.
 */
int init_shmem_pool(struct client_os_inst *client, metal_phys_addr_t pa, size_t size);

/**
 * get a free shared memory region from the client's shared memory pool.
 *
 * @param[in]	client	The client OS that associated with shm_pool.
 * @param[in]	size	The desired size of the shared memory to be obtained.
 * @return	NULL if out of range, or the virtual starting address of shared memory pool.
 */
void *get_free_shmem(struct client_os_inst *client, size_t size);

#if defined __cplusplus
}
#endif

#endif	/* SHM_POOL_H */
