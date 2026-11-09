/* SPDX-License-Identifier: MulanPSL-2.0
 * Original output-only PV console producer; public MIT ABI in abi.h. */
#include <errno.h>
#include <limits.h>
#include <stdbool.h>
#include <stdatomic.h>
#include "abi.h"
#include "debug_console.h"
#include <mica/platform/memory.h>
#define MICA_XEN_PAGE_SIZE 4096u
extern int xen_hvm_get(uint32_t index, uint64_t *value);

static atomic_bool gate = ATOMIC_VAR_INIT(false);
static struct xencons_interface *ring;
static void *view;
static uint32_t event_port;
static bool ready;
static unsigned long notify_failures;

static bool enter(void)
{
    bool expected = false;
    /* ONE weak CAS attempt, no spin loop or sleeping lock. ARM build below
     * pins baseline LL/SC and disables outline atomics (no direct LSE). */
    return atomic_compare_exchange_weak_explicit(&gate, &expected, true, memory_order_acquire,
        memory_order_relaxed);
}

static void leave(void)
{
    atomic_store_explicit(&gate, false, memory_order_release);
}

static bool valid(struct xencons_interface *p)
{
    uint32_t cons = __atomic_load_n(&p->out_cons, __ATOMIC_ACQUIRE);
    uint32_t prod = __atomic_load_n(&p->out_prod, __ATOMIC_RELAXED);
    uint32_t ic = __atomic_load_n(&p->in_cons, __ATOMIC_RELAXED);
    uint32_t ip = __atomic_load_n(&p->in_prod, __ATOMIC_ACQUIRE);

    return prod - cons <= sizeof(p->out) && ip - ic <= sizeof(p->in);
}

int xen_debug_init(void)
{
    if (!enter())
        return -EAGAIN;
    int rc = 0;

    if (ready)
        goto done;
    /* A failed protocol validation/unmap keeps the acquired view domain-owned.
     * Never overwrite/lose it or publish a partially usable view. */
    if (view)
    {
        rc = mica_mem_unmap(view, MICA_XEN_PAGE_SIZE);
        if (rc)
            goto done;
        view = NULL;
    }

    uint64_t pfn = 0, port = 0;

    rc = xen_hvm_get(HVM_CONSOLE_PFN, &pfn);
    if (!rc)
        rc = xen_hvm_get(HVM_CONSOLE_EVTCHN, &port);
    if (rc)
        goto done;
    if (!pfn || pfn > (UINT64_MAX - MICA_XEN_PAGE_SIZE) / MICA_XEN_PAGE_SIZE || !port ||
        port > UINT32_MAX)
    {
        rc = -EINVAL;
        goto done;
    }
    /* Own-domain trusted HVM special GFN, NOT an allocated page or Dom0 PA.
     * Port supplies actual local view/attributes (Uni M0C0), no pointer cast.
     * No shared_info/events/xenstore init or second domain owner. */
    void *local = NULL;

    rc = mica_mem_map(pfn*MICA_XEN_PAGE_SIZE, MICA_XEN_PAGE_SIZE, &local);
    if (rc)
        goto done;
    view = local;
    if (!valid(local))
        rc = -EPROTO;
    if (rc)
    {
        if (view && !mica_mem_unmap(view, MICA_XEN_PAGE_SIZE))
            view = NULL;
        goto done;
    }
    ring = local;
    event_port = (uint32_t)port;
    ready = true;
done:
    leave();
    return rc;
}

int xen_debug_write(const void *bytes, size_t len)
{
    if (!len)
        return 0;
    if (!bytes)
        return -EINVAL;
    if (len > INT_MAX)
        return -EOVERFLOW;
    if (!enter())
        return -EAGAIN;
    int rc;

    if (!ready)
    {
        rc = -ENODEV;
        goto done;
    }
    if (!valid(ring))
    {
        rc = -EPROTO;
        goto done;
    }

    uint32_t cons = __atomic_load_n(&ring->out_cons, __ATOMIC_ACQUIRE);
    uint32_t prod = __atomic_load_n(&ring->out_prod, __ATOMIC_RELAXED);
    uint32_t used = prod - cons;

    if (used > sizeof(ring->out))
    {
        rc = -EPROTO;
        goto done;
    }

    size_t n = sizeof(ring->out) - used;

    if (n > len)
        n = len;
    if (!n)
    {
        rc = -EAGAIN;
        goto done;
    }

    const unsigned char *source = bytes;

    for (size_t i = 0; i < n; i++)
        ring->out[(prod + (uint32_t)i) & (sizeof(ring->out) - 1)] = (char)source[i];
    __atomic_store_n(&ring->out_prod, prod + (uint32_t)n, __ATOMIC_RELEASE);
    /* Release producer cursor before backend notification, no implicit NUL.
     * Keep gate across non-waiting notify so failure accounting is serialized.
     * A published prefix is irrevocable even when event send fails. */
    struct xen_evt_port notify = {
        .port = event_port
    };
    if (HYPERVISOR_event_channel_op(EVT_SEND, &notify))
        notify_failures++;
    rc = (int)n;
done:
    leave();
    return rc;
}
