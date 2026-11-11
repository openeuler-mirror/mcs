/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_DEMO_LOCK_H
#define MICA_DEMO_LOCK_H
#include <demo_lock.h>
typedef demo_mutex_t mica_lock_t;
static inline int mica_lock_init(mica_lock_t *lock)
{
    return demo_mutex_init(lock);
}
static inline int mica_lock_acquire(mica_lock_t *lock, mica_deadline_t deadline)
{
    return demo_mutex_acquire(lock, deadline);
}
static inline int mica_lock_release(mica_lock_t *lock)
{
    return demo_mutex_release(lock);
}
static inline int mica_lock_destroy(mica_lock_t *lock)
{
    return demo_mutex_destroy(lock);
}
#endif
