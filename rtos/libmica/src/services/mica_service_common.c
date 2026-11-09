/* SPDX-License-Identifier: MulanPSL-2.0 */
#include "../mica_internal.h"
#include <stdlib.h>

int mica_service_lock(struct mica_service_slot *slot)
{
    if (!slot->gate_acquired)
        return -ENODEV;
    if (atomic_load_explicit(&slot->ctx->sync_failed, memory_order_acquire))
        return -EIO;
    metal_mutex_acquire(&slot->gate);
    return 0;
}

int mica_service_unlock(struct mica_service_slot *slot)
{
    metal_mutex_release(&slot->gate);
    return 0;
}

int mica_service_prepare_data(struct mica_service_slot *slot, bool umt)
{
    if (slot->service_data || slot->wake_acquired)
        return -EBUSY;
    struct mica_service_data *data = calloc(1, sizeof(*data));

    if (!data)
        return -ENOMEM;
    atomic_init(&data->passive_active, false);
    slot->service_data = data; /* immediate partial-acquisition record */

    metal_mutex_init(&data->tx_lock);
    data->tx_lock_acquired = true;
    int ret = mica_sem_init(&slot->wake, 0);
    if (!ret)
        slot->wake_acquired = true;
    if (!ret && umt)
    {
        ret = mica_sem_init(&data->passive, 0);
        if (!ret)
            data->passive_acquired = true;
    }
    return ret;
}

int mica_service_enqueue(struct mica_service_slot *slot, void *buffer, size_t len)
{
    struct mica_context *ctx = slot->ctx;

    int locked = mica_service_lock(slot);

    if (locked)
    {
        atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
        return 0;
    }
    /* Check admission before dereferencing queue/wake/rdev on failed RX. */
    if (slot->cancelled || slot->failed_retained || ctx->cancel ||
        mica_openamp_resetting(ctx->openamp) ||
        slot->state != MICA_SERVICE_RUNNING || !buffer || !len)
    {
        mica_service_unlock(slot);
        return 0;
    }

    struct mica_service_data *data = slot->service_data;
    unsigned int index = MICA_SERVICE_RX_QUEUE_CAPACITY + 1;

    if (data && data->count < MICA_SERVICE_RX_QUEUE_CAPACITY)
    {
        for (unsigned int i = 0; i < MICA_SERVICE_RX_QUEUE_CAPACITY + 1; ++i)
        {
            if (data->items[i].owner == MICA_RX_FREE)
            {
                index = i;
                break;
            }
        }
    }
    if (index > MICA_SERVICE_RX_QUEUE_CAPACITY)
    {
        mica_service_unlock(slot);
        return 0;
    }
    /* The real OpenAMP dispatch releases its device lock before entering this
     * callback. hold is local bookkeeping, not send/wait/user callback. */
    rpmsg_hold_rx_buffer(&slot->endpoint, buffer);
    data->items[index] = (struct mica_rx_item) {
        buffer,
        len,
        MICA_RX_QUEUED
    };

    unsigned int tail = (data->head + data->count) % MICA_SERVICE_RX_QUEUE_CAPACITY;

    data->queue[tail] = index;
    ++data->count;
    int posted = mica_sem_post(&slot->wake);

    if (posted)
    {
        --data->count;
        data->items[index].owner = MICA_RX_RETURN_PENDING;
        if (posted != -EOVERFLOW)
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
    }
    else if (data->passive_acquired && !data->callback && !data->passive_signalled)
    {
        posted = mica_sem_post(&data->passive);
        if (!posted)
            data->passive_signalled = true;
        else if (posted != -EOVERFLOW)
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
    }
    mica_service_unlock(slot);
    return 0;
}

int mica_service_take(struct mica_service_slot *slot, bool callback, unsigned int *index,
    void **buffer, size_t *len)
{
    struct mica_context *ctx = slot->ctx;
    int ret = mica_service_lock(slot);

    if (ret)
        return ret;
    struct mica_service_data *data = slot->service_data;

    if (slot->cancelled || slot->failed_retained || ctx->cancel)
        ret = -ESHUTDOWN;
    else if (!slot->local_start_released || !data || !data->count)
        ret = -EAGAIN;
    else
    {
        *index = data->queue[data->head];
        data->head = (data->head + 1) % MICA_SERVICE_RX_QUEUE_CAPACITY;
        --data->count;

        struct mica_rx_item *item = &data->items[*index];

        item->owner = MICA_RX_PROCESSING;
        *buffer = item->data;
        *len = item->len;
        if (callback)
            ++slot->callbacks;
    }
    int released = mica_service_unlock(slot);

    if (!ret && released && callback)
        atomic_fetch_sub_explicit(&slot->callbacks, 1, memory_order_release);
    return ret ? ret : released;
}

void mica_service_complete(struct mica_service_slot *slot, unsigned int index, bool callback)
{
    struct mica_context *ctx = slot->ctx;

    if (mica_service_lock(slot))
    {
        atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
        /* Retain the item on failed fence, but completion cannot strand the
         * callback reference. The executing worker still pins slot lifetime. */
        if (callback)
            atomic_fetch_sub_explicit(&slot->callbacks, 1, memory_order_release);
        int posted = mica_sem_post(&ctx->references_changed);

        if (posted && posted != -EOVERFLOW)
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
        return;
    }
    struct mica_service_data *data = slot->service_data;

    data->items[index].owner = MICA_RX_RETURN_PENDING;
    if (callback)
        --slot->callbacks;
    int posted = mica_sem_post(&ctx->references_changed);
    int wake = mica_sem_post(&ctx->receiver_wake);

    if ((posted && posted != -EOVERFLOW) || (wake && wake != -EOVERFLOW))
        atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
    mica_service_unlock(slot);
}

void mica_service_drain_returns(struct mica_context *ctx)
{
    /* Sole receiver boundary only, OR receiver has exited. Previous callback
     * has completed OpenAMP's held-bit test before any buffer is reusable. */
    for (unsigned int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *slot = &ctx->services[i];

        void *buffers[MICA_SERVICE_RX_QUEUE_CAPACITY + 1];
        unsigned int indices[MICA_SERVICE_RX_QUEUE_CAPACITY + 1];
        unsigned int count = 0;

        if (mica_service_lock(slot))
        {
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
            return;
        }
        struct mica_service_data *data = slot->published_once ? slot->service_data : NULL;

        for (unsigned int j = 0; data && j < MICA_SERVICE_RX_QUEUE_CAPACITY + 1; ++j)
        {
            if (data->items[j].owner == MICA_RX_RETURN_PENDING)
            {
                buffers[count] = data->items[j].data;
                indices[count++] = j;
                /* Do not make the slot reusable before OpenAMP release has
                 * completed outside the gate at this receiver boundary. */
                data->items[j].owner = MICA_RX_RETURNING;
            }
        }
        if (mica_service_unlock(slot))
            return;
        for (unsigned int j = 0; j < count; ++j)
            rpmsg_release_rx_buffer(&slot->endpoint, buffers[j]);
        if (!count)
            continue;
        if (mica_service_lock(slot))
        {
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
            return;
        }
        for (unsigned int j = 0; j < count; ++j)
            data->items[indices[j]] = (struct mica_rx_item){0};
        if (mica_service_unlock(slot))
            return;
    }
}

int mica_service_borrow(enum mica_service_type type, struct mica_service_slot **out,
    struct mica_peer_snapshot *peer)
{
    struct mica_context *ctx = mica_context_get();
    int ret = mica_context_pin(ctx);

    if (ret)
        return ret;
    struct mica_service_slot *slot = &ctx->services[type];

    ret = mica_service_lock(slot);
    if (ret)
    {
        mica_context_unpin();
        return ret;
    }
    if (ctx->cancel || slot->cancelled || slot->failed_retained)
        ret = -ESHUTDOWN;
    else if (mica_openamp_resetting(ctx->openamp) || slot->state != MICA_SERVICE_RUNNING ||
        !slot->peer.bound)
        ret = -EAGAIN;
    bool borrowed = !ret;

    if (!ret)
    {
        ++slot->borrowers;
        *out = slot;
        *peer = slot->peer;
    }
    /* Keep the entry pin until unborrow's last notification. Closing/removing
     * cannot destroy the notification semaphores during reference return. */
    int released = mica_service_unlock(slot);

    if (!ret)
        ret = released;
    if (ret)
    {
        if (borrowed)
            atomic_fetch_sub_explicit(&slot->borrowers, 1, memory_order_release);
        mica_context_unpin();
        return ret;
    }
    return ret;
}

int mica_service_unborrow(struct mica_service_slot *slot)
{
    /* All data access is finished. The entry pin still protects wake objects;
     * reference return must not depend on a potentially failed mutex. */
    atomic_fetch_sub_explicit(&slot->borrowers, 1, memory_order_release);
    int posted = mica_sem_post(&slot->ctx->references_changed);
    int wake = mica_sem_post(&slot->ctx->receiver_wake);

    if ((posted && posted != -EOVERFLOW) || (wake && wake != -EOVERFLOW))
        atomic_store_explicit(&slot->ctx->sync_failed, true, memory_order_release);
    mica_context_unpin();
    return posted && posted != -EOVERFLOW ? posted : wake == -EOVERFLOW ? 0 : wake;
}
