/* SPDX-License-Identifier: MulanPSL-2.0 */
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <metal/io.h>
#include <metal/cache.h>
#include <openamp/rpmsg_virtio.h>
#include <openamp/remoteproc_virtio.h>
#include "mica_ped.h"
#include "../mica_internal.h"

/* Exact guest OpenAMP 2022.10 internal symbol is declared narrowly in our
 * private contract, NOT by including its source-tree rpmsg_internal.h.
 * No private NS encoder/consumer is duplicated here. */
#define MICA_NS_CREATE 0UL
#define MICA_NS_DESTROY 1u

struct mica_openamp
{
    struct rpmsg_virtio_device rvdev;
    struct virtio_device *vdev;
    struct metal_io_region table_io;
    metal_phys_addr_t table_phys;
    rpmsg_ept_cb original_ns;
    void *original_ns_priv;
    struct mica_context *ctx;
    atomic_bool resetting; /* RESET admission is checked under each service gate. */
};

static void ignore_bind(struct rpmsg_device *rdev, const char *name, uint32_t dest)
{
    (void)rdev;
    (void)name;
    (void)dest;
    /* Fixed local service slots only. NS must never create a service. */
}

bool mica_openamp_resetting(struct mica_openamp *helper)
{
    return helper && atomic_load_explicit(&helper->resetting, memory_order_acquire);
}

static int receive_ns(struct rpmsg_endpoint *ept, void *data, size_t len, uint32_t src, void *priv)
{
    struct mica_openamp *helper = priv;
    char name[RPMSG_NAME_SIZE];
    uint32_t remote, flags;

    if (!data || len != RPMSG_NAME_SIZE + 2 * sizeof(uint32_t))
        return 0;
    mica_service_drain_returns(helper->ctx);
    /* OpenAMP invokes callbacks without its device mutex; its original NS
     * handler releases that mutex before any of our service gates are taken. */
    (void)helper->original_ns(ept, data, len, src, helper->original_ns_priv);
    memcpy(name, data, sizeof(name));
    memcpy(&remote, (char *)data + sizeof(name), sizeof(remote));
    memcpy(&flags, (char *)data + sizeof(name) + sizeof(remote), sizeof(flags));
    for (unsigned int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *slot = &helper->ctx->services[i];

        if (mica_service_lock(slot))
        {
            atomic_store_explicit(&helper->ctx->sync_failed, true, memory_order_release);
            return 0;
        }
        if (slot->published_once && slot->name && !strncmp(slot->name, name, sizeof(name)))
        {
            slot->peer.local_addr = slot->endpoint.addr;
            slot->peer.remote_addr = remote;
            slot->peer.bound = !(flags & MICA_NS_DESTROY);
        }
        if (mica_service_unlock(slot))
            return 0;
    }
    return 0; /* RX callback errors must not reach OpenAMP's assertion. */
}

static int receive_data(struct rpmsg_endpoint *ept, void *data, size_t len, uint32_t src, void *priv)
{
    struct mica_service_slot *slot = priv;
    struct mica_context *ctx = slot->ctx;

    if (mica_service_lock(slot))
    {
        atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
        return 0;
    }
    if (slot->state != MICA_SERVICE_RUNNING || slot->failed_retained || slot->cancelled ||
        atomic_load_explicit(&ctx->cancel, memory_order_acquire) ||
        mica_openamp_resetting(ctx->openamp))
    {
        mica_service_unlock(slot);
        return 0;
    }
    slot->peer.local_addr = ept->addr;
    slot->peer.remote_addr = src;
    slot->peer.bound = true;
    rpmsg_ept_cb callback = slot->rx_callback;
    if (mica_service_unlock(slot))
        return 0;
    mica_service_drain_returns(ctx);
    if (callback)
        (void)callback(ept, data, len, src, slot);
    return 0;
}

static struct fw_rsc_vdev *find_vdev(const struct mica_rpmsg_layout *layout)
{
    const uint32_t *table = layout->runtime_table;
    uint32_t version, count, offset, type;

    if (!table || layout->table_bytes < 4 * sizeof(uint32_t))
        return NULL;
    memcpy(&version, table, sizeof(version));
    memcpy(&count, table + 1, sizeof(count));
    if (version != 1 || count > (layout->table_bytes - 16) / 4)
        return NULL;
    for (uint32_t i = 0; i < count; ++i)
    {
        memcpy(&offset, table + 4 + i, sizeof(offset));
        if (offset > layout->table_bytes || layout->table_bytes - offset < sizeof(struct fw_rsc_vdev))
            continue;
        memcpy(&type, (char *)table + offset, sizeof(type));
        if (type == RSC_VDEV && offset % _Alignof(uint32_t) == 0 &&
            layout->table_bytes - offset >= sizeof(struct fw_rsc_vdev) + 2 * sizeof(struct fw_rsc_vdev_vring))
            return(struct fw_rsc_vdev *)((char *)layout->runtime_table + offset);
    }
    return NULL;
}

int mica_openamp_create(const struct mica_rpmsg_layout *layout, struct mica_openamp **out)
{
    if (!out)
        return -EINVAL;
    *out = NULL;
    if (!layout || !layout->buffer_io || !layout->notify)
        return -EINVAL;
    struct fw_rsc_vdev *rsc = find_vdev(layout);

    if (!rsc || rsc->id != VIRTIO_ID_RPMSG || rsc->num_of_vrings != 2)
        return -EINVAL;
    struct fw_rsc_vdev_vring *vr = (void *)((char *)rsc + sizeof(*rsc));

    for (unsigned int i = 0; i < 2; ++i)
    {
        if (!layout->vring_local[i] || !vr[i].num || vr[i].num > RPROC_MAX_VRING_DESC ||
            !vr[i].align || (vr[i].align & (vr[i].align - 1)) || vr[i].align > 65536 ||
            (vr[i].num & (vr[i].num - 1)) || (size_t)vring_size(vr[i].num,
            vr[i].align) > layout->vring_bytes[i])
            return -EINVAL;
    }

    struct mica_context *ctx = mica_context_get();
    /* Avoid OpenAMP's unbounded wait_remote_ready. Layout producers must not
     * publish DA/offset-dependent views before readiness; this is a final
     * local ready/cancel gate, never a second Xen discovery implementation. */
    mica_deadline_t deadline = mica_now_ns() + MICA_STOP_BUDGET_NS;
    volatile uint8_t *status = &rsc->status;

    while (!(*status & VIRTIO_CONFIG_STATUS_DRIVER_OK))
    {
        int ret = mica_context_lock(ctx);

        if (ret)
            return ret;
        bool cancel = ctx->cancel;

        ret = mica_context_unlock(ctx);
        if (ret)
            return ret;
        if (cancel)
            return -ECANCELED;
        if (mica_now_ns() >= deadline)
            return -ETIMEDOUT;
        (void)mica_sem_wait_until(&ctx->receiver_wake, mica_now_ns() + 1000000ULL);
    }
    atomic_thread_fence(memory_order_acquire);

    struct mica_openamp *helper = calloc(1, sizeof(*helper));

    if (!helper)
        return -ENOMEM;
    helper->ctx = ctx;
    atomic_init(&helper->resetting, false);
    helper->table_phys = (uintptr_t)layout->runtime_table;
    metal_io_init(&helper->table_io, layout->runtime_table, &helper->table_phys, layout->table_bytes,
        -1, 0, NULL);
    helper->vdev = rproc_virtio_create_vdev(VIRTIO_DEV_DEVICE, 0, rsc, &helper->table_io,
        layout->notify_cookie, layout->notify, NULL);
    if (!helper->vdev)
    {
        free(helper);
        return -ENOMEM;
    }

    int ret = 0;

    for (unsigned int i = 0; i < 2 && !ret; ++i)
        ret = rproc_virtio_init_vring(helper->vdev, i, vr[i].notifyid, layout->vring_local[i],
            layout->buffer_io, vr[i].num, vr[i].align);
    if (!ret)
        ret = rpmsg_init_vdev(&helper->rvdev, helper->vdev, ignore_bind, layout->buffer_io, NULL);
    if (ret)
    {
        rproc_virtio_remove_vdev(helper->vdev);
        free(helper);
        return ret < 0 ? ret : -ret;
    }

    struct rpmsg_device *rdev = &helper->rvdev.rdev;

    rdev->ns_unbind_cb = ignore_bind;
    helper->original_ns = rdev->ns_ept.cb;
    helper->original_ns_priv = rdev->ns_ept.priv;
    if (rdev->support_ns)
    {
        rdev->ns_ept.cb = receive_ns;
        rdev->ns_ept.priv = helper;
    }
    *out = helper;
    return 0;
}

struct rpmsg_device *mica_openamp_rpdev(struct mica_openamp *helper)
{
    return helper ? &helper->rvdev.rdev : NULL;
}

static int process_control(struct mica_openamp *helper)
{
    uint32_t command = metal_io_read32(&helper->table_io, 8);
    struct mica_context *ctx = helper->ctx;

    if (command != 0x84000009u)
    {
        atomic_store_explicit(&helper->resetting, false, memory_order_release);
        if (command == 0x84000002u)
        {
            void (*poweroff)(void) = ctx->config.sys_ops.system_poweroff;

            if (poweroff)
                poweroff();
            return 1;
        }
        return 0; /* Ordinary notifications never take the context mutex. */
    }
    /* Publish the RESET fence BEFORE inspecting users. A get checks this flag
     * within its gate; a get which won that gate first already registered its
     * borrower, which this scan must observe. No check-zero-then-admit gap. */
    atomic_store_explicit(&helper->resetting, true, memory_order_release);
    for (unsigned int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *slot = &ctx->services[i];
        int ret = mica_service_lock(slot);

        if (ret)
            return ret;
        bool busy = atomic_load_explicit(&slot->borrowers, memory_order_acquire) ||
            atomic_load_explicit(&slot->callbacks, memory_order_acquire) ||
            slot->state == MICA_SERVICE_STARTING;
        struct mica_service_data *data = slot->published_once ? slot->service_data : NULL;

        for (unsigned int j = 0; data && j < MICA_SERVICE_RX_QUEUE_CAPACITY + 1; ++j)
            busy |= data->items[j].owner != MICA_RX_FREE;
        ret = mica_service_unlock(slot);
        if (ret)
            return ret;
        if (busy)
            return -EAGAIN;
    }
    /* Legacy RESET semantics, now only in the sole receiver and after all
     * sends/held RX/business callbacks drain; no racing hard-IRQ vq reset. */
    struct rpmsg_virtio_device *rv = &helper->rvdev;

    metal_mutex_acquire(&rv->rdev.lock);
    if (rv->svq)
    {
        rv->svq->vq_free_cnt = rv->svq->vq_nentries;
        rv->svq->vq_queued_cnt = 0;
        rv->svq->vq_desc_head_idx = 0;
        rv->svq->vq_available_idx = 0;
        rv->svq->vq_used_cons_idx = 0;
    }
    if (rv->rvq)
    {
        rv->rvq->vq_available_idx = 0;
        rv->rvq->vq_used_cons_idx = 0;
        rv->rvq->vq_ring.used->idx = 0;
        rv->rvq->vq_ring.avail->idx = 0;
        metal_cache_flush(&rv->rvq->vq_ring.used->idx, sizeof(rv->rvq->vq_ring.used->idx));
        metal_cache_flush(&rv->rvq->vq_ring.avail->idx, sizeof(rv->rvq->vq_ring.avail->idx));
    }
    metal_mutex_release(&rv->rdev.lock);
    metal_io_write32(&helper->table_io, 8, 0);
    atomic_store_explicit(&helper->resetting, false, memory_order_release);
    return 1;
}

int mica_openamp_process_notification(struct mica_openamp *helper, uint32_t vqid)
{
    if (!helper)
        return -ENODEV;
    mica_service_drain_returns(helper->ctx);

    int control = process_control(helper);

    if (control > 0)
        return 0;
    if (control < 0 && control != -EAGAIN)
        return control;
    int ret = rproc_virtio_notified(helper->vdev, vqid);

    mica_service_drain_returns(helper->ctx);
    return ret;
}

int mica_openamp_publish_service(struct mica_openamp *helper, struct mica_service_slot *slot)
{
    if (!helper || !slot || slot->ctx != helper->ctx || !slot->name ||
        strlen(slot->name) >= RPMSG_NAME_SIZE || !slot->rx_callback)
        return -EINVAL;
    int ret = mica_service_lock(slot);

    if (ret)
        return ret;
    if (slot->published_once)
    {
        ret = slot->failed_retained ? -EOPNOTSUPP : -EALREADY;
        mica_service_unlock(slot);
        return ret;
    }

    struct rpmsg_device *rdev = &helper->rvdev.rdev;

    slot->endpoint.priv = slot;
    /* Empty-name registration does NOT perform NS/TX wait or call business
     * code. Keep this short local registration+publication bookkeeping atomic
     * under its gate so a later lock error cannot lose a live endpoint.
     * NS/data original paths release rdev lock before taking a service gate. */
    ret = rpmsg_create_ept(&slot->endpoint, rdev, "", RPMSG_ADDR_ANY, RPMSG_ADDR_ANY, receive_data,
        NULL);
    if (ret)
    {
        mica_service_unlock(slot);
        return ret < 0 ? ret : -ret;
    }
    slot->published_once = true;
    slot->endpoint_acquired = true;
    ret = mica_service_unlock(slot);
    if (ret)
        return ret;
    metal_mutex_acquire(&rdev->lock);
    strncpy(slot->endpoint.name, slot->name, sizeof(slot->endpoint.name) - 1);
    slot->endpoint.name[sizeof(slot->endpoint.name) - 1] = '\0';
    metal_mutex_release(&rdev->lock);
    ret = rdev->support_ns ? rpmsg_send_ns_message(&slot->endpoint, MICA_NS_CREATE) : 0;
    if (ret && !mica_service_lock(slot))
    {
        slot->failed_retained = true;
        slot->create_error = ret < 0 ? ret : -ret;
        slot->state = MICA_SERVICE_FAILED_RETAINED;
        slot->cancelled = true;
        mica_service_unlock(slot);
    }
    return ret < 0 ? ret : -ret;
}

int mica_openamp_destroy(struct mica_openamp *helper)
{
    if (!helper)
        return 0;
    struct mica_context *ctx = helper->ctx;
    int ret = mica_context_lock(ctx);

    if (ret)
        return ret;
    if (ctx->receiver_acquired)
        ret = -EBUSY;
    for (int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *s = &ctx->services[i];

        int locked = mica_service_lock(s);

        if (locked)
        {
            mica_context_unlock(ctx);
            return locked;
        }
        if (s->endpoint_acquired || s->borrowers || s->callbacks || s->worker_acquired)
            ret = -EBUSY;
        int unlocked = mica_service_unlock(s);

        if (!ret)
            ret = unlocked;
    }
    int released = mica_context_unlock(ctx);

    if (!ret)
        ret = released;
    if (ret)
        return ret;
    rpmsg_deinit_vdev(&helper->rvdev);
    rproc_virtio_remove_vdev(helper->vdev);
    free(helper);
    return 0;
}
