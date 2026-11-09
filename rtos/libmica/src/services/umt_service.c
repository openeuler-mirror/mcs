/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 * SPDX-License-Identifier: MulanPSL-2.0
 */
#include <limits.h>
#include <metal/io.h>
#include <string.h>
#include "mica_service_internal.h"
#include "../mica_internal.h"

#define UMT_SEND_BUFFER_OFFSET 0x100000u
/* Preserve the existing native ABI (including its trailing alignment), not a
 * new packed wire format or Xen UMT extension. */
struct umt_descriptor
{
    uint64_t phy_addr;
    uint32_t data_len;
};

static void *mica_service_payload(struct mica_context *ctx, uint64_t da, size_t len)
{
    struct metal_io_region *io = ctx->layout.buffer_io;

    if (!io || !len)
        return NULL;
    void *va = metal_io_phys_to_virt(io, (metal_phys_addr_t)da);

    if (!va)
        return NULL;
    unsigned long offset = metal_io_virt_to_offset(io, va);

    if (offset == METAL_BAD_OFFSET || offset > io->size || len > io->size - offset)
        return NULL;
    return va;
}

static int receive_umt(struct rpmsg_endpoint *ept, void *buffer, size_t len, uint32_t src, void *priv)
{
    (void)ept;
    (void)src;

    struct mica_service_slot *slot = priv;

    if (mica_service_lock(slot))
        return 0;
    bool active = slot->state == MICA_SERVICE_RUNNING && !slot->cancelled && !slot->failed_retained &&
        !slot->ctx->cancel;

    if (mica_service_unlock(slot))
        return 0;
    if (!active || !buffer || len < sizeof(struct umt_descriptor))
        return 0;
    struct umt_descriptor descriptor;

    memcpy(&descriptor, buffer, sizeof(descriptor));
    if (!descriptor.phy_addr || descriptor.data_len > INT_MAX || !mica_service_payload(slot->ctx,
        descriptor.phy_addr, descriptor.data_len))
        return 0;
    if (!mica_service_lock(slot))
    {
        struct mica_service_data *data = slot->service_data;

        if (slot->state == MICA_SERVICE_RUNNING && !slot->cancelled && !slot->failed_retained &&
            !data->send_initialized && descriptor.phy_addr <= UINT64_MAX - UMT_SEND_BUFFER_OFFSET)
        {
            data->send_da = descriptor.phy_addr + UMT_SEND_BUFFER_OFFSET;
            data->send_initialized = true;
        }
        if (mica_service_unlock(slot))
            return 0;
    }
    return mica_service_enqueue(slot, buffer, len);
}

int mica_umt_init_service(struct mica_context *ctx, struct mica_service_slot *slot)
{
    (void)ctx; /* Stable slot identity is established before framework publish. */
    slot->name = "rpmsg-umt";
    slot->rx_callback = receive_umt;
    slot->worker_entry = umt_service_thread;
    return mica_service_prepare_data(slot, true);
}

int mica_umt_is_ready(void)
{
    return mica_service_is_ready(MICA_SERVICE_UMT);
}

void umt_service_thread(void *arg)
{
    struct mica_service_slot *slot = arg;
    struct mica_context *ctx = slot->ctx;

    for (;;)
    {
        if (mica_service_lock(slot))
        {
            /* The worker cannot complete communication processing. This is
             * sticky failure, not proof that the native mutex is damaged. */
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
            return;
        }
        struct mica_service_data *data = slot->service_data;
        bool stop = slot->cancelled || slot->failed_retained || ctx->cancel;
        umt_rcv_cb_t callback = data->callback;
        void *callback_arg = data->callback_arg;
        bool available = !stop && slot->local_start_released && callback && data->count;

        if (available)
            ++slot->callbacks; /* pins callback snapshot vs unregister */
        if (mica_service_unlock(slot))
        {
            if (available)
                atomic_fetch_sub_explicit(&slot->callbacks, 1, memory_order_release);
            return;
        }
        if (stop)
            return;
        if (!available)
        {
            int waited = mica_sem_wait_until(&slot->wake, mica_now_ns() + MICA_RECEIVER_POLL_NS);

            if (waited && waited != -ETIMEDOUT)
            {
                atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
                return;
            }
            continue;
        }

        unsigned int index;
        void *buffer;
        size_t len;
        int ret = mica_service_take(slot, false, &index, &buffer, &len);

        if (!ret)
        {
            struct umt_descriptor descriptor;

            memcpy(&descriptor, buffer, sizeof(descriptor));

            void *payload = mica_service_payload(ctx, descriptor.phy_addr, descriptor.data_len);

            if (payload)
                callback(payload, (int)descriptor.data_len, callback_arg);
            mica_service_complete(slot, index, false);
        }
        atomic_fetch_sub_explicit(&slot->callbacks, 1, memory_order_release);
        int posted = mica_sem_post(&ctx->references_changed);
        /* Completion may have woken RESET before the callback pin was returned.
         * The worker still owns slot/context lifetime through both posts. */
        int wake = mica_sem_post(&ctx->receiver_wake);

        if ((posted && posted != -EOVERFLOW) || (wake && wake != -EOVERFLOW))
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
    }
}

int mica_send_data(void *buffer, int offset, size_t len)
{
    if (!buffer || offset < 0 || !len || len > UMT_SEND_BUFFER_OFFSET ||
        (size_t)offset > UMT_SEND_BUFFER_OFFSET - len)
        return -EINVAL;
    struct mica_service_slot *slot;
    struct mica_peer_snapshot peer;
    int ret = mica_service_borrow(MICA_SERVICE_UMT, &slot, &peer);

    if (ret)
        return ret;
    struct mica_service_data *data = slot->service_data;
    uint64_t da = 0;

    ret = mica_service_lock(slot);
    if (!ret)
    {
        if (!data->send_initialized)
            ret = -EFAULT;
        else
            da = data->send_da;
        int released = mica_service_unlock(slot);

        if (!ret)
            ret = released;
    }
    if (!ret)
    {
        if (da > UINT64_MAX - (unsigned int)offset)
            ret = -EFAULT;
        else
        {
            da += (unsigned int)offset;

            void *target = mica_service_payload(slot->ctx, da, len);

            if (!target)
                ret = -EFAULT;
            else
            {
                metal_mutex_acquire(&data->tx_lock);
                memcpy(target, buffer, len);
                struct umt_descriptor descriptor = {0};
                descriptor.phy_addr = da;
                descriptor.data_len = (uint32_t)len;

                int sent = rpmsg_send_offchannel(&slot->endpoint, peer.local_addr,
                    peer.remote_addr, &descriptor, sizeof(descriptor));

                ret = sent == (int)sizeof(descriptor) ? 0 : -EIO;
                metal_mutex_release(&data->tx_lock);
            }
        }
    }
    int returned = mica_service_unborrow(slot);

    return ret ? ret : returned;
}

int mica_umt_register_rcv_cb(umt_rcv_cb_t callback, void *arg)
{
    if (!callback)
        return -EINVAL;
    struct mica_service_slot *slot;
    struct mica_peer_snapshot peer;
    int ret = mica_service_borrow(MICA_SERVICE_UMT, &slot, &peer);

    if (ret)
        return ret;
    if (!(ret = mica_service_lock(slot)))
    {
        struct mica_service_data *data = slot->service_data;

        if (data->callback)
            ret = -EALREADY;
        else if (atomic_load_explicit(&data->passive_active, memory_order_acquire) || slot->callbacks)
            ret = -EBUSY;
        else
        {
            data->callback = callback;
            data->callback_arg = arg;
            ret = mica_sem_post(&slot->wake);
            if (ret == -EOVERFLOW)
                ret = 0;
            if (ret)
                atomic_store_explicit(&slot->ctx->sync_failed, true, memory_order_release);
        }
        int released = mica_service_unlock(slot);

        if (!ret)
            ret = released;
    }
    int returned = mica_service_unborrow(slot);

    return ret ? ret : returned;
}

int mica_umt_unregister_rcv_cb(void)
{
    struct mica_service_slot *slot;
    struct mica_peer_snapshot peer;
    int ret = mica_service_borrow(MICA_SERVICE_UMT, &slot, &peer);

    if (ret)
        return ret;
    mica_deadline_t deadline = mica_now_ns() + MICA_STOP_BUDGET_NS;

    for (;;)
    {
        ret = mica_service_lock(slot);
        if (ret)
            break;
        struct mica_service_data *data = slot->service_data;

        if (slot->callbacks && slot->worker_acquired && mica_thread_is_self(slot->worker))
            ret = -EDEADLK;
        if (!ret)
        {
            data->callback = NULL;
            data->callback_arg = NULL;
        }

        bool busy = slot->callbacks != 0;

        if (!ret && !busy && data->count && !data->passive_signalled)
        {
            ret = mica_sem_post(&data->passive);
            if (ret == -EOVERFLOW)
                ret = 0;
            if (!ret)
                data->passive_signalled = true;
            else
                atomic_store_explicit(&slot->ctx->sync_failed, true, memory_order_release);
        }
        int released = mica_service_unlock(slot);

        if (!ret)
            ret = released;
        if (ret || !busy)
            break;
        if (mica_now_ns() >= deadline)
        {
            ret = -ETIMEDOUT;
            break;
        }
        ret = mica_sem_wait_until(&slot->ctx->references_changed, deadline);
        if (ret)
            break;
    }
    int returned = mica_service_unborrow(slot);

    return ret ? ret : returned;
}

int mica_rcv_data(void *buffer, size_t *length)
{
    if (!buffer || !length)
        return -EINVAL;
    struct mica_service_slot *slot;
    struct mica_peer_snapshot peer;
    int ret = mica_service_borrow(MICA_SERVICE_UMT, &slot, &peer);

    if (ret)
        return ret;
    struct mica_service_data *data = slot->service_data;

    bool claimed_passive = false;

    ret = mica_service_lock(slot);
    if (ret)
        goto exit;
    if (data->callback || atomic_load_explicit(&data->passive_active, memory_order_acquire) ||
        slot->callbacks)
        ret = -EBUSY;
    else
    {
        atomic_store_explicit(&data->passive_active, true, memory_order_release);
        claimed_passive = true;
    }
    int released = mica_service_unlock(slot);

    if (!ret)
        ret = released;
    if (ret)
        goto exit;
    for (;;)
    {
        ret = mica_service_lock(slot);
        if (ret)
            break;
        if (slot->cancelled || slot->failed_retained || slot->ctx->cancel)
            ret = -ESHUTDOWN;
        bool available = data->count != 0;

        if (!ret && available)
        {
            struct umt_descriptor descriptor;
            unsigned int index = data->queue[data->head];

            memcpy(&descriptor, data->items[index].data, sizeof(descriptor));
            if (*length < descriptor.data_len)
            {
                *length = descriptor.data_len;
                ret = -EMSGSIZE;
            }
        }
        released = mica_service_unlock(slot);
        if (!ret)
            ret = released;
        if (ret)
            break;
        if (available)
        {
            unsigned int index;
            void *packet;
            size_t len;

            ret = mica_service_take(slot, false, &index, &packet, &len);
            if (!ret)
            {
                struct umt_descriptor descriptor;

                memcpy(&descriptor, packet, sizeof(descriptor));

                void *payload = mica_service_payload(slot->ctx, descriptor.phy_addr,
                    descriptor.data_len);

                /* Validate the same final snapshot used by the actual copy. */
                if (*length < descriptor.data_len)
                {
                    *length = descriptor.data_len;
                    ret = -EMSGSIZE;
                }
                else if (!payload)
                    ret = -EFAULT;
                else
                {
                    memcpy(buffer, payload, descriptor.data_len);
                    *length = descriptor.data_len;
                }
                mica_service_complete(slot, index, false);
            }
            break;
        }
        ret = mica_sem_wait_until(&data->passive, mica_now_ns() + MICA_RECEIVER_POLL_NS);
        if (ret && ret != -ETIMEDOUT)
            break;
        if (!ret && !mica_service_lock(slot))
        {
            data->passive_signalled = false;
            released = mica_service_unlock(slot);
            if (!ret)
                ret = released;
        }
    }
exit:
    /* All payload/state accesses are finished; only this call's claim may be
     * cleared. Reference return still owns the entry pin protecting data. */
    if (claimed_passive)
        atomic_store_explicit(&data->passive_active, false, memory_order_release);
    int returned = mica_service_unborrow(slot);

    return ret ? ret : returned;
}
