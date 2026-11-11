/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_PLATFORM_SEM_H
#define MICA_PLATFORM_SEM_H
#include <errno.h>
#include <mica/platform/time.h>
#include <mica/platform/system/@PROJECT_SYSTEM@/sem.h>

/* Native storage, including handle 0, is owned only after successful init.
 * The OS supplies thin inline init/post/pend/destroy with 0/-errno returns.
 * Destroy failure retains the object; callers must keep its acquired flag.
 * pend(0) is an immediate try. The finite tick limit excludes OS FOREVER. */
static inline int mica_sem_wait_until(mica_sem_t *sem, mica_deadline_t deadline)
{
    for (;;) {
        uint64_t now = mica_now_ns();
        uint32_t ticks = deadline > now ?
            mica_timeout_ticks(deadline - now, MICA_SEM_TICK_HZ,
                               MICA_SEM_MAX_FINITE_TICKS) : 0;
        int ret = mica_sem_pend(sem, ticks);
        if (!ret)
            return 0;
        if (!ticks && (ret == -EAGAIN || ret == -ETIMEDOUT))
            return -ETIMEDOUT;
        if (ret != -ETIMEDOUT)
            return ret;
        if (mica_now_ns() >= deadline)
            return -ETIMEDOUT;
    }
}
#endif
