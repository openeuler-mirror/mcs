/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_DEMO_THREAD_H
#define MICA_DEMO_THREAD_H
#include <demo_thread.h>
typedef demo_thread_t mica_thread_t;
typedef void *(*mica_thread_entry_t)(void *);
static inline int mica_thread_create(mica_thread_t *out,
                                     mica_thread_entry_t entry, void *arg)
{
    return demo_thread_create(out, entry, arg);
}
static inline int mica_thread_is_self(mica_thread_t thread)
{
    return demo_thread_is_self(thread);
}
static inline int mica_thread_join(mica_thread_t thread, mica_deadline_t deadline)
{
    return demo_thread_join(thread, deadline);
}
#endif
