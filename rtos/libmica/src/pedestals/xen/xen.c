/* SPDX-License-Identifier: MulanPSL-2.0
 * Xen discovery keys and state values follow UniProton 3fe439b8's MICA
 * backend (Copyright Huawei Technologies Co., Ltd. 2024). No duplicate
 * OpenAMP assembly, endpoint manager or domain facilities are implemented here.
 */
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <metal/io.h>
#include <mica/platform/log.h>
#include <mica/platform/lock.h>
#include "../ped_rsc_table.h"
#include "provider.h"
#include "../../mica_internal.h"

#define XEN_PAGE_BYTES 4096u
#define XEN_MAX_GREF 256u
#define XEN_MAX_DOMID 1024u
struct xen_cookie
{
    struct mica_context *ctx;
    const struct mica_xen_provider_ops *provider;
    struct mica_xen_mapping *mapping;
    struct mica_xen_binding *binding;
    /* Lock order: instance notify_lock -> provider event lock. IRQ event()
     * never takes this lock; callers hold no runtime state_lock while waiting. */
    mica_lock_t notify_lock;
    bool notify_lock_acquired;
    struct metal_io_region buffers;
    metal_phys_addr_t wire;
    void *local;
    size_t bytes;
    unsigned int domid;
    atomic_bool pending;
    bool metal_acquired;
};

static int negative(int ret)
{
    return ret > 0 ? -ret : ret;
}

static bool supports(enum mica_service_type type)
{
    return type == MICA_SERVICE_TTY;
}

static int read_number(struct xen_cookie *c, const char *key, uint32_t *out)
{
    char value[32] = {0};

    int ret = negative(c->provider->xs_read(key, value, sizeof(value)));

    if (ret)
        return ret;
    if (!value[0] || value[0] == '-' || value[0] == '+')
        return -EINVAL;
    errno = 0;

    char *end;
    unsigned long number = strtoul(value, &end, 0);

    if (errno || end == value || *end || number > UINT32_MAX)
        return -EINVAL;
    *out = (uint32_t)number;
    return 0;
}

static int discover(struct xen_cookie *c, uint32_t *refs, size_t *count, uint32_t *port)
{
    char path[128], value[32];
    bool found = false;

    for (unsigned int dom = 0; dom < XEN_MAX_DOMID; ++dom)
    {
        snprintf(path, sizeof(path), "/local/domain/%u", dom);

        int ret = negative(c->provider->xs_read(path, value, sizeof(value)));

        if (!ret)
        {
            c->domid = dom;
            found = true;
            break;
        }
        if (ret != -ENOENT && ret != -EACCES && ret != -EPERM)
            return ret;
    }
    if (!found)
        return -ENOENT;
    snprintf(path, sizeof(path), "/local/domain/0/backend/mica/%u/0/gref_num", c->domid);

    uint32_t n;
    int ret = read_number(c, path, &n);

    if (ret)
        return ret;
    if (!n || n > XEN_MAX_GREF)
        return -EINVAL;
    *count = n;
    for (uint32_t i = 0; i < n; ++i)
    {
        snprintf(path, sizeof(path), "/local/domain/0/backend/mica/%u/0/gref_%u", c->domid, i);
        ret = read_number(c, path, &refs[i]);
        if (ret)
            return ret;
    }
    snprintf(path, sizeof(path), "/local/domain/0/backend/mica/%u/0/evtchn_port", c->domid);
    ret = read_number(c, path, port);
    if (ret)
        return ret;
    return *port ? 0 : -EINVAL;
}

static void event(void *arg)
{
    struct xen_cookie *c = arg;

    atomic_store_explicit(&c->pending, true, memory_order_release);
    int ret = mica_sem_post(&c->ctx->receiver_wake);

    if (ret && ret != -EOVERFLOW)
        atomic_store_explicit(&c->ctx->sync_failed, true, memory_order_release);
}

static int notify(void *arg, uint32_t vqid)
{
    (void)vqid;

    struct xen_cookie *c = arg;

    if (!c->notify_lock_acquired ||
        atomic_load_explicit(&c->ctx->sync_failed, memory_order_acquire))
        return -ESHUTDOWN;
    int ret = mica_lock_acquire(&c->notify_lock, mica_now_ns() + MICA_STOP_BUDGET_NS);

    if (ret)
        return negative(ret);
    ret = c->binding ? negative(c->provider->notify(c->binding)) : -ESHUTDOWN;
    int released = mica_lock_release(&c->notify_lock);

    if (released)
        atomic_store_explicit(&c->ctx->sync_failed, true, memory_order_release);
    if (!ret)
        ret = released;
    return ret;
}

static int cancelled(struct mica_context *ctx)
{
    int ret = mica_context_lock(ctx);

    if (ret)
        return ret;
    bool stop = ctx->cancel;

    ret = mica_context_unlock(ctx);
    return ret ? ret : stop ? -ECANCELED : 0;
}

static int make_layout(struct xen_cookie *c, struct mica_rpmsg_layout *out)
{
    struct fw_resource_table *t = c->local;
    /* No DA/offset is read until the driver's status release below. */
    mica_deadline_t deadline = mica_now_ns() + MICA_STOP_BUDGET_NS;
    volatile uint8_t *status = &t->vdev.status;

    while (!(*status & VIRTIO_CONFIG_STATUS_DRIVER_OK))
    {
        int ret = cancelled(c->ctx);

        if (ret)
            return ret;
        uint64_t now = mica_now_ns();

        if (now >= deadline)
            return -ETIMEDOUT;
        (void)mica_sem_wait_until(&c->ctx->receiver_wake, now + 1000000ULL);
    }
    atomic_thread_fence(memory_order_acquire);

    int ret = cancelled(c->ctx);

    if (ret)
        return ret;
    if (t->ver != 1 || t->num != RSC_TABLE_NUM_ENTRY ||
        t->offset[RSC_TABLE_EPT_TABLE_ENTRY] != offsetof(struct fw_resource_table, ept_table) ||
        t->offset[RSC_TABLE_VDEV_ENTRY] != offsetof(struct fw_resource_table, vdev) ||
        t->offset[RSC_TABLE_VRING_OFFSET_ENTRY] != offsetof(struct fw_resource_table, vring_offset) ||
        t->vdev.type != RSC_VDEV || t->vdev.id != VIRTIO_ID_RPMSG || t->vdev.num_of_vrings != 2 ||
        t->vring_offset.type != RSC_VENDOR_VRING_OFFSET)
        return -EINVAL;
    size_t offset0 = t->vring_offset.offset[0];

    if (offset0 < sizeof(*t) || offset0 >= c->bytes)
        return -EINVAL;
    c->wire = t->vring0.da;
    metal_io_init(&c->buffers, (char *)c->local + offset0, &c->wire, c->bytes - offset0, -1, 0, NULL);
    struct mica_rpmsg_layout layout = {
        .runtime_table = t,
        .table_bytes = sizeof(*t),
        .buffer_io = &c->buffers,
        .notify = notify,
        .notify_cookie = c
    };
    const struct fw_rsc_vdev_vring *rings[2] = {
        &t->vring0,
        &t->vring1
    };
    for (unsigned int i = 0; i < 2; ++i)
    {
        const struct fw_rsc_vdev_vring *r = rings[i];
        size_t offset = t->vring_offset.offset[i];

        if (!r->num || r->num > RPROC_MAX_VRING_DESC || (r->num & (r->num - 1)) || !r->align ||
            r->align > 65536 || (r->align & (r->align - 1)) || offset < offset0 || offset > c->bytes)
            return -EINVAL;
        size_t length = (size_t)vring_size(r->num, r->align);

        if (length > c->bytes - offset || offset % r->align ||
            (uint64_t)r->da != (uint64_t)c->wire + (offset - offset0))
            return -EINVAL;
        layout.vring_local[i] = (char *)c->local + offset;
        layout.vring_bytes[i] = length;
    }
    *out = layout;
    return 0;
}

static int prepare(struct mica_context *ctx, struct mica_rpmsg_layout *layout)
{
    if (!ctx || !layout || !ctx->receiver_wake_acquired || !ctx->state_lock_acquired)
        return -EINVAL;
    if (ctx->pedestal_cookie)
        return -EBUSY;
    memset(layout, 0, sizeof(*layout));

    const struct mica_xen_provider_ops *p = mica_xen_provider_get();

    if (!p || !p->xs_read || !p->xs_write || !p->map || !p->unmap || !p->bind || !p->sync_unbind ||
        !p->notify)
        return -ENODEV;
    struct xen_cookie *c = calloc(1, sizeof(*c));

    if (!c)
        return -ENOMEM;
    c->ctx = ctx;
    c->provider = p;
    atomic_init(&c->pending, false);
    ctx->pedestal_cookie = c;
    ctx->pedestal_acquired = true;

    int ret = mica_lock_init(&c->notify_lock);

    if (ret)
        return negative(ret);
    c->notify_lock_acquired = true;
    /* domain_init belongs exclusively to OS startup, never attach/remove. */
    struct metal_init_params params = METAL_INIT_DEFAULTS;

    ret = negative(metal_init(&params));
    if (ret)
        return ret;
    c->metal_acquired = true;

    uint32_t refs[XEN_MAX_GREF], port;
    size_t count = 0;

    ret = discover(c, refs, &count, &port);
    if (ret)
        return ret;
    ret = negative(p->map(0, refs, count, &c->mapping, &c->local, &c->bytes));
    if (ret)
        return ret; /* even failed map's residual cookie stays reachable */
    if (!c->mapping || !c->local || c->bytes != count*XEN_PAGE_BYTES ||
        c->bytes < sizeof(struct fw_resource_table))
        return -EINVAL;
    ret = negative(p->bind(port, event, c, &c->binding));
    if (ret)
        return ret;
    if (!c->binding)
        return -EINVAL;
    return make_layout(c, layout);
}

static int quiesce(struct mica_context *ctx, uint64_t deadline)
{
    struct xen_cookie *c = ctx->pedestal_cookie;

    (void)mica_context_cancel(ctx);
    if (!c)
        return 0;
    if (atomic_load_explicit(&ctx->sync_failed, memory_order_acquire))
        return -EIO;
    if (!c->notify_lock_acquired)
        return c->binding ? -EIO : 0;
    int ret = mica_lock_acquire(&c->notify_lock, deadline);

    if (ret)
        return negative(ret);
    if (c->binding)
    {
        ret = negative(c->provider->sync_unbind(c->binding, deadline));
        if (!ret)
            c->binding = NULL;
    }
    int released = mica_lock_release(&c->notify_lock);

    if (released)
        atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
    if (!ret)
        ret = released;
    return ret;
}

static int detach(struct mica_context *ctx)
{
    struct xen_cookie *c = ctx->pedestal_cookie;

    if (!c)
        return 0;
    if (atomic_load_explicit(&ctx->sync_failed, memory_order_acquire))
        return -EIO;
    if (c->binding || ctx->receiver_acquired || ctx->openamp_acquired)
        return -EBUSY;
    if (c->mapping)
    {
        int ret = negative(c->provider->unmap(c->mapping));

        if (ret)
            return ret;
        c->mapping = NULL;
        c->local = NULL;
        c->bytes = 0;
    }
    /* Do not publish Closed: guest teardown cannot prove Dom0 stopped using
     * the grants. Existing master/watch lifecycle remains authoritative. */
    /* detach is reached only after helper/receiver/users have exited; failures
     * above retain this lock with the cookie for the same cleanup owner. */
    if (c->notify_lock_acquired)
    {
        int ret = mica_lock_destroy(&c->notify_lock);

        if (ret)
            return ret;
        c->notify_lock_acquired = false;
    }
    if (c->metal_acquired)
        metal_finish();
    free(c);
    ctx->pedestal_cookie = NULL;
    ctx->pedestal_acquired = false;
    return 0;
}

static const struct mica_pedestal_ops ops = {
    .supports = supports,
    .prepare = prepare,
    .quiesce = quiesce,
    .detach = detach
};

const struct mica_pedestal_ops *mica_get_ped_ops(void)
{
    return &ops;
}
