/* SPDX-License-Identifier: Apache-2.0
 * Copyright (c) 2021 EPAM Systems
 * Copyright (c) 2022 Arm Limited (or its affiliates). All rights reserved.
 * Modified 2026 from Zephyr v4.2.0 drivers/xen/events.c: OS IRQ binding,
 * permanent dispatch slots, atomic entry fence and deadline-aware revoke.
 * No kernel API or callback in a worker substitute. See SOURCES.md.
 */
#include "support.h"
#include <stdlib.h>
#define DISABLED (UINT32_C(1) << 31)
struct event_slot
{
    atomic_uint gate;
    mica_xen_event_fn fn;
    void *arg;
    bool owned; /* task lock only */
};

struct mica_xen_binding
{
    uint32_t port;
    bool closed;
    bool installed;
};

static struct event_slot slots[XEN_PORTS];
static mica_lock_t event_lock;
static bool event_lock_acquired;
static atomic_bool sync_failed;
static mica_sem_t pause_sem;
static bool pause_sem_acquired;
static unsigned domain_irq;
static bool domain_irq_acquired;

static void mask(uint32_t port)
{
    __atomic_fetch_or(&mica_xen_shared->evtchn_mask[port / 64], UINT64_C(1) <<(port % 64),
        __ATOMIC_SEQ_CST);
}

static void dispatch(uint32_t port)
{
    struct event_slot *slot = &slots[port];
    unsigned gate = atomic_load_explicit(&slot->gate, memory_order_acquire);

    for (;;)
    {
        if (gate & DISABLED)
            return;
        if (atomic_compare_exchange_weak_explicit(&slot->gate, &gate, gate + 1, memory_order_acquire,
            memory_order_relaxed))
            break;
    }
    slot->fn(slot->arg); /* contractual pending+wake only */
    atomic_fetch_sub_explicit(&slot->gate, 1, memory_order_release);
}

static void events_irq(void *arg)
{
    (void)arg;

    struct xen_vcpu_info *vcpu = &mica_xen_shared->vcpu_info[0];

    __atomic_store_n(&vcpu->evtchn_upcall_pending, 0, __ATOMIC_RELAXED);
    atomic_thread_fence(memory_order_seq_cst);

    uint64_t selector = __atomic_exchange_n(&vcpu->evtchn_pending_sel, 0, __ATOMIC_SEQ_CST);

    while (selector)
    {
        unsigned word = (unsigned)__builtin_ctzll(selector);

        selector &= selector - 1;

        uint64_t pending = __atomic_load_n(&mica_xen_shared->evtchn_pending[word],
            __ATOMIC_ACQUIRE) & ~__atomic_load_n(&mica_xen_shared->evtchn_mask[word], __ATOMIC_ACQUIRE);

        while (pending)
        {
            unsigned bit = (unsigned)__builtin_ctzll(pending);

            pending &= pending - 1;
            __atomic_fetch_and(&mica_xen_shared->evtchn_pending[word], ~(UINT64_C(1) << bit),
                __ATOMIC_SEQ_CST);
            dispatch(word * 64 + bit);
        }
    }
}

int xen_event_send(uint32_t port)
{
    struct xen_evt_port op = {
        .port = port
    };
    return(int)HYPERVISOR_event_channel_op(EVT_SEND, &op);
}

int xen_events_init(unsigned event_irq)
{
    if (!mica_xen_shared)
        return -ENODEV;
    for (size_t n = 0; n < XEN_PORTS; ++n)
        atomic_init(&slots[n].gate, DISABLED);
    for (size_t n = 0; n < 64; ++n)
        __atomic_store_n(&mica_xen_shared->evtchn_mask[n], UINT64_MAX, __ATOMIC_RELEASE);
    int rc = mica_lock_init(&event_lock);

    if (rc)
        return rc;
    event_lock_acquired = true;
    if (!pause_sem_acquired)
    {
        rc = mica_sem_init(&pause_sem, 0);
        if (rc)
            return rc;
        pause_sem_acquired = true;
    }
    rc = mica_request_irq(event_irq, events_irq, NULL);
    if (rc)
        return rc;
    domain_irq = event_irq;
    domain_irq_acquired = true;
    return mica_unmask_irq(domain_irq);
}

static int revoke(struct mica_xen_binding *binding, uint64_t deadline)
{
    struct event_slot *slot = binding->installed ? &slots[binding->port] : NULL;

    if (slot)
    {
        mask(binding->port);
        atomic_fetch_or_explicit(&slot->gate, DISABLED, memory_order_acq_rel);
    }
    if (!binding->closed)
    {
        struct xen_evt_port op = {
            .port = binding->port
        };

        int rc = (int)HYPERVISOR_event_channel_op(EVT_CLOSE, &op);

        if (rc)
            return rc;
        binding->closed = true;
    }
    while (slot && (atomic_load_explicit(&slot->gate, memory_order_acquire) & ~DISABLED))
    {
        uint64_t now = mica_now_ns();

        if (now >= deadline)
            return -ETIMEDOUT;
        uint64_t until = deadline - now > UINT64_C(1000000) ? now + UINT64_C(1000000) : deadline;
        int rc = mica_sem_wait_until(&pause_sem, until);

        if (rc && rc != -ETIMEDOUT)
            return rc;
    }
    if (slot)
    {
        slot->fn = NULL;
        slot->arg = NULL;
        slot->owned = false;
    }
    return 0;
}

int xen_events_bind(uint32_t remote, mica_xen_event_fn fn, void *arg, struct mica_xen_binding **out)
{
    if (out)
        *out = NULL;
    if (!out || !fn)
        return -EINVAL;
    if (!event_lock_acquired || !domain_irq_acquired ||
        atomic_load_explicit(&sync_failed, memory_order_acquire))
        return -ENODEV;
    struct mica_xen_binding *binding = calloc(1, sizeof(*binding));

    if (!binding)
        return -ENOMEM;
    int rc = mica_lock_acquire(&event_lock, xen_deadline());

    if (rc)
    {
        free(binding);
        return rc;
    }
    struct xen_evt_bind op = {
        .remote_dom = 0,
        .remote_port = remote
    };
    rc = (int)HYPERVISOR_event_channel_op(EVT_BIND, &op);
    if (rc)
    {
        free(binding);
        goto done;
    }
    binding->port = op.local_port;
    if (op.local_port >= XEN_PORTS)
    {
        rc = -ERANGE;
        goto undo;
    }

    struct event_slot *slot = &slots[op.local_port];

    if (slot->owned)
    {
        rc = -EBUSY;
        goto undo;
    }
    mask(op.local_port);
    slot->fn = fn;
    slot->arg = arg;
    slot->owned = true;
    binding->installed = true;
    atomic_store_explicit(&slot->gate, 0, memory_order_release);
    struct xen_evt_port unmask = {
        .port = op.local_port
    };
    /* Xen unmask includes delivery of a pending edge set while masked. */
    rc = (int)HYPERVISOR_event_channel_op(EVT_UNMASK, &unmask);
    if (rc)
        goto undo;
    *out = binding;
    goto done;
undo:
    if (revoke(binding, xen_deadline()))
        *out = binding;
    else
        free(binding);
done:
    {
        int released = mica_lock_release(&event_lock);

        if (released)
        {
            atomic_store_explicit(&sync_failed, true, memory_order_release);
            if (!rc)
                rc = released;
        }
    }
    return rc;
}

int xen_events_unbind(struct mica_xen_binding *binding, uint64_t deadline)
{
    if (!binding)
        return -EINVAL;
    if (atomic_load_explicit(&sync_failed, memory_order_acquire))
        return -EIO;
    int rc = mica_lock_acquire(&event_lock, deadline);

    if (rc)
        return rc;
    /* Revoke consumes the binding; an unlock failure must not leave the caller
     * retaining a dangling cookie. Keep ownership until the release succeeds. */
    rc = revoke(binding, deadline);
    int released = mica_lock_release(&event_lock);

    if (released)
        atomic_store_explicit(&sync_failed, true, memory_order_release);
    if (!rc)
        rc = released;
    if (!rc)
        free(binding);
    return rc;
}

int xen_events_notify(struct mica_xen_binding *binding)
{
    if (!binding)
        return -EINVAL;
    if (atomic_load_explicit(&sync_failed, memory_order_acquire))
        return -EIO;
    int rc = mica_lock_acquire(&event_lock, xen_deadline());

    if (rc)
        return rc;
    rc = binding->closed || !binding->installed || (atomic_load_explicit(&slots[binding->port].gate,
        memory_order_acquire) & DISABLED) ? -ENODEV : xen_event_send(binding->port);
    int released = mica_lock_release(&event_lock);

    if (released)
        atomic_store_explicit(&sync_failed, true, memory_order_release);
    if (!rc)
        rc = released;
    return rc;
}
