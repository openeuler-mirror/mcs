/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_SEM__H__
#error "Include mica/platform/sem.h instead of mica/platform/system/demo/sem.h"
#endif

#ifndef __MICA_DEMO_SEM__H__
#define __MICA_DEMO_SEM__H__

#ifdef __cplusplus
extern "C" {
#endif

typedef int mica_sem_t;

static inline int mica_sem_init(mica_sem_t *sem, unsigned int value)
{
	return demo_sem_init(value, sem);
}

static inline void mica_sem_destroy(mica_sem_t sem)
{
	demo_sem_destroy(sem);
}

static inline unsigned int mica_sem_post(mica_sem_t sem)
{
	return demo_sem_post(sem);
}

static inline unsigned int mica_sem_wait(mica_sem_t sem)
{
	return demo_sem_wait(sem);
}

#ifdef __cplusplus
}
#endif

#endif /* __MICA_DEMO_SEM__H__ */
