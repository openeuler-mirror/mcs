/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_PLATFORM_SEM_H
#error "Include mica/platform/sem.h instead"
#endif
#ifndef MICA_DEMO_SEM_H
#define MICA_DEMO_SEM_H
#include <stdint.h>
/* Porting template, not a runnable OS implementation. demo_sem.h must define
 * demo_sem_t, DEMO_SEM_TICK_HZ, DEMO_SEM_MAX_FINITE_TICKS and the four demo_sem_*
 * primitives below. Native 0 is valid. Primitives return 0/-errno; init failure
 * leaves no live object, destroy failure retains it. post must be IRQ-safe
 * (overflow may coalesce); pend(0) tries once, finite timeout is -ETIMEDOUT.
 * The finite maximum must exclude the OS infinite-wait sentinel. */
#include <demo_sem.h>
typedef demo_sem_t mica_sem_t;
#define MICA_SEM_TICK_HZ DEMO_SEM_TICK_HZ
#define MICA_SEM_MAX_FINITE_TICKS DEMO_SEM_MAX_FINITE_TICKS
static inline int mica_sem_init(mica_sem_t *sem, unsigned int count)
{
    return demo_sem_init(sem, count);
}

static inline int mica_sem_post(mica_sem_t *sem)
{
    return demo_sem_post(sem);
}

static inline int mica_sem_pend(mica_sem_t *sem, uint32_t ticks)
{
    return demo_sem_pend(sem, ticks);
}

static inline int mica_sem_destroy(mica_sem_t *sem)
{
    return demo_sem_destroy(sem);
}

#endif
