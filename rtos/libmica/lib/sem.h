/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_SEM__H__
#define __MICA_SEM__H__

#include <mica/platform/system/@PROJECT_SYSTEM@/sem.h>

#ifdef __cplusplus
extern "C" {
#endif

/** \defgroup Sem Interfaces
 *  @{
 */

/**
 * @brief      initialize a semaphore
 *             return a pointer to the semaphore
 *
 * @param[in]  value       Initial number of available semaphores.
 * @param[out]  sem        ID/Pointer of the semaphore handle initialized.
 */
static inline int mica_sem_init(mica_sem_t *sem, unsigned int value);

/**
 * @brief      delete the semaphore
 *
 * @param[in]  sem         Semaphore handle
 */
static inline void mica_sem_destroy(mica_sem_t sem);

/**
 * @brief      signal the semaphore
 *
 * @param[in]  sem         Semaphore handle
 */
static inline unsigned int mica_sem_post(mica_sem_t sem);

/**
 * @brief      wait on the semaphore
 *
 * @param[in]  sem         Semaphore handle
 */
static inline unsigned int mica_sem_wait(mica_sem_t sem);

/** @} */

#ifdef __cplusplus
}
#endif

#endif /* __MICA_SEM__H__ */
