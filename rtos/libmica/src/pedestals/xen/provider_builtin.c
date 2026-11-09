/* SPDX-License-Identifier: Apache-2.0
 * Modified 2026: libmica domain owner and frozen provider adapter.
 * See SOURCES.md. No native adapter or MICA-instance teardown here.
 */
#include "support.h"
#include "debug_console.h"
enum domain_state
{
    COLD, INITIALIZING, NOT_READY, READY
};

static atomic_int state = ATOMIC_VAR_INIT(COLD);
static int init_error;

static int domain_init(unsigned event_irq)
{
    int expected = COLD;

    if (!atomic_compare_exchange_strong_explicit(&state, &expected, INITIALIZING,
        memory_order_acq_rel, memory_order_acquire))
    {
        if (expected == READY)
            return 0;
        if (expected == NOT_READY)
            return init_error;
        return -EBUSY;
    }

    int rc = xen_shared_init();

    if (!rc)
        rc = xen_events_init(event_irq);
    /* Importing grants requires no grant-export table setup. */
    if (!rc)
        rc = xen_xs_init();
    init_error = rc;
    atomic_store_explicit(&state, rc ? NOT_READY : READY, memory_order_release);
    return rc;
}

static bool ready(void)
{
    return atomic_load_explicit(&state, memory_order_acquire) == READY;
}

static int xs_read(const char *key, char *value, size_t capacity)
{
    return ready() ? xen_xs_read(key, value, capacity) : -ENODEV;
}

static int xs_write(const char *key, const char *value)
{
    return ready() ? xen_xs_write(key, value) : -ENODEV;
}

static int map(uint16_t domid, const uint32_t *refs, size_t count, struct mica_xen_mapping **cookie,
    void **local, size_t *bytes)
{
    if (cookie)
        *cookie = NULL;
    if (local)
        *local = NULL;
    if (bytes)
        *bytes = 0;
    return ready() ? xen_grant_map(domid, refs, count, cookie, local, bytes) : -ENODEV;
}

static int unmap(struct mica_xen_mapping *cookie)
{
    return ready() ? xen_grant_unmap(cookie) : -ENODEV;
}

static int bind(uint32_t remote, mica_xen_event_fn fn, void *arg, struct mica_xen_binding **cookie)
{
    if (cookie)
        *cookie = NULL;
    return ready() ? xen_events_bind(remote, fn, arg, cookie) : -ENODEV;
}

static int sync_unbind(struct mica_xen_binding *cookie, uint64_t deadline)
{
    return ready() ? xen_events_unbind(cookie, deadline) : -ENODEV;
}

static int notify(struct mica_xen_binding *cookie)
{
    return ready() ? xen_events_notify(cookie) : -ENODEV;
}

const struct mica_xen_provider_ops *mica_xen_provider_get(void)
{
    static const struct mica_xen_provider_ops ops = {
        .domain_init = domain_init,
        .xs_read = xs_read,
        .xs_write = xs_write,
        .map = map,
        .unmap = unmap,
        .bind = bind,
        .sync_unbind = sync_unbind,
        .notify = notify,
        .debug_init = xen_debug_init,
        .debug_write = xen_debug_write
    };
    return &ops;
}
