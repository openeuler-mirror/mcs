/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 * SPDX-License-Identifier: MulanPSL-2.0
 */
#include <mica/platform/log.h>
#include <stdlib.h>
#include "services/mica_service_internal.h"
#include "mica_internal.h"

static int mica_service_cancel_data(struct mica_service_slot *slot)
{
    struct mica_service_data *data = slot->service_data;

    if (data && data->passive_acquired)
    {
        int ret = mica_sem_post(&data->passive);

        if (ret && ret != -EOVERFLOW)
        {
            atomic_store_explicit(&slot->ctx->sync_failed, true, memory_order_release);
            return ret;
        }
    }
    return 0;
}

static int mica_service_release_data(struct mica_service_slot *slot)
{
    /* Unpublished, or receiver + all borrowers/workers have exited. */
    int ret = mica_service_lock(slot);

    if (ret)
        return ret;
    struct mica_service_data *data = slot->service_data;

    if (!data)
        return mica_service_unlock(slot);
    void *buffers[MICA_SERVICE_RX_QUEUE_CAPACITY + 1];
    unsigned int count = 0;

    for (unsigned int i = 0; i < MICA_SERVICE_RX_QUEUE_CAPACITY + 1; ++i)
    {
        if (data->items[i].data && data->items[i].owner != MICA_RX_RETURNING)
        {
            buffers[count++] = data->items[i].data;
            data->items[i].owner = MICA_RX_RETURNING;
        }
    }
    ret = mica_service_unlock(slot);
    if (ret)
        return ret;
    for (unsigned int i = 0; i < count; ++i)
        rpmsg_release_rx_buffer(&slot->endpoint, buffers[i]);
    ret = mica_service_lock(slot);
    if (ret)
        return ret;
    if (data->passive_acquired)
    {
        ret = mica_sem_destroy(&data->passive);

        if (ret)
            goto done;
        data->passive_acquired = false;
    }
    if (data->tx_lock_acquired)
    {
        metal_mutex_deinit(&data->tx_lock);
        data->tx_lock_acquired = false;
    }
    free(data);
    slot->service_data = NULL;
done:;
    int released = mica_service_unlock(slot);

    return ret ? ret : released;
}

static int normalized(int ret)
{
    return ret > 0 ? -ret : ret;
}

static int any_running(struct mica_context *ctx, bool *out)
{
    *out = false;
    for (int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *slot = &ctx->services[i];

        int ret = mica_service_lock(slot);

        if (ret)
            return ret;
        bool running = slot->state == MICA_SERVICE_RUNNING;

        ret = mica_service_unlock(slot);
        if (ret)
            return ret;
        if (running)
            *out = true;
    }
    return 0;
}

static bool service_resources(struct mica_context *ctx)
{
    if (ctx->receiver_acquired)
        return true;
    for (int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *s = &ctx->services[i];

        if (mica_service_lock(s))
            return true;
        bool resources = s->published_once || s->failed_retained || s->worker_acquired ||
            s->wake_acquired || s->service_data;

        if (mica_service_unlock(s) || resources)
            return true;
    }
    return false;
}

static int caller_is_worker(struct mica_context *ctx)
{
    /* Lifecycle admission has already rejected an active operation. No other
     * owner can join while these acquired/joined progress fields are read. */
    if (ctx->receiver_acquired && !ctx->receiver_joined &&
        mica_thread_is_self(ctx->receiver))
        return true;
    for (int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *slot = &ctx->services[i];
        int ret = mica_service_lock(slot);

        if (ret)
            return ret;
        bool self = slot->worker_acquired && !slot->worker_joined && mica_thread_is_self(slot->worker);

        ret = mica_service_unlock(slot);
        if (ret)
            return ret;
        if (self)
            return 1;
    }
    return false;
}

static void *receiver_main(void *arg)
{
    struct mica_context *ctx = arg;

    while (!atomic_load_explicit(&ctx->cancel, memory_order_acquire))
    {
        if (atomic_load_explicit(&ctx->sync_failed, memory_order_acquire))
            return NULL;
        int ret = mica_sem_wait_until(&ctx->receiver_wake, mica_now_ns() + MICA_RECEIVER_POLL_NS);

        if (!ret)
            ret = mica_openamp_process_notification(ctx->openamp, UINT32_MAX);
        if (ret && ret != -ETIMEDOUT && ret != -EAGAIN)
        {
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
            return NULL;
        }
    }
    return NULL;
}

static void *worker_main(void *arg)
{
    struct mica_service_slot *slot = arg;

    slot->worker_entry(slot);
    return NULL;
}

static int reap(struct mica_context *ctx, mica_thread_t *thread, bool *acquired,
    bool *joined, mica_deadline_t deadline, struct mica_service_slot *slot)
{
    if (!*acquired)
        return 0;
    if (!*joined)
    {
        int ret = mica_thread_join(*thread, deadline);

        if (ret)
            return normalized(ret);
        *joined = true; /* operation owner only, before fallible state cleanup */
    }
    int locked = slot ? mica_service_lock(slot) : mica_context_lock(ctx);

    if (locked)
        return locked;
    *acquired = false;
    int released = slot ? mica_service_unlock(slot) : mica_context_unlock(ctx);

    return released;
}

int start_one_service(struct mica_context *ctx, enum mica_service_type type,
    struct mica_start_transaction *transaction)
{
    struct mica_service_slot *slot = &ctx->services[type];
    int ret;

    if ((ret = mica_service_lock(slot)))
        return ret;
    /* RESET publishes its fence before scanning this same gate. A late create
     * has acquired nothing: leave OFF and out of destructive rollback. */
    if (mica_openamp_resetting(ctx->openamp))
    {
        ret = mica_service_unlock(slot);
        return ret ? ret : -EAGAIN;
    }
    transaction->added[transaction->count++] = type;
    slot->state = MICA_SERVICE_STARTING;
    slot->cancelled = false;
    slot->local_start_released = false;
    ret = mica_service_unlock(slot);
    if (ret)
        return ret;
    if ((ret = mica_service_lock(slot)))
        goto failed;
    ret = type == MICA_SERVICE_TTY ? mica_tty_init_service(ctx, slot) : mica_umt_init_service(ctx, slot);
    int prepared_release = mica_service_unlock(slot);

    if (!ret)
        ret = prepared_release;
    if (ret)
        goto failed;
    if (!ctx->receiver_acquired)
    {
        if ((ret = mica_service_lock(slot)))
            goto failed;
        ctx->cancel = false;
        /* Publish the OS handle/flag before releasing the short lock. Entry
         * may be scheduled immediately, but cannot pass that lock first. */
        ret = mica_thread_create(&ctx->receiver, receiver_main, ctx);
        if (!ret)
        {
            ctx->receiver_acquired = true;
            ctx->receiver_joined = false;
        }
        int released = mica_service_unlock(slot);

        if (!ret)
            ret = released;
        if (ret)
            goto failed;
        transaction->receiver_added = true;
        ret = mica_sem_post(&ctx->receiver_wake);
        if (ret && ret != -EOVERFLOW)
        {
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
            goto failed;
        }
    }
    if ((ret = mica_service_lock(slot)))
        goto failed;
    ret = mica_thread_create(&slot->worker, worker_main, slot);
    if (!ret)
    {
        slot->worker_acquired = true;
        slot->worker_joined = false;
    }
    int released = mica_service_unlock(slot);

    if (!ret)
        ret = released;
    if (ret)
        goto failed;
    ret = mica_openamp_publish_service(ctx->openamp, slot);
    if (ret)
        goto failed;
    if ((ret = mica_service_lock(slot)))
        goto failed;
    slot->state = MICA_SERVICE_RUNNING;
    slot->local_start_released = true;
    ret = mica_service_unlock(slot);
    if (ret)
        goto failed;
    ret = mica_sem_post(&slot->wake);
    if (ret == -EOVERFLOW)
        ret = 0;
    if (ret)
    {
        atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
        goto failed;
    }
    return ret;
failed:
    ret = normalized(ret);
    if (!mica_service_lock(slot))
    {
        slot->create_error = ret;
        mica_service_unlock(slot);
    }
    return ret;
}

int mica_service_quiesce(struct mica_context *ctx, enum mica_service_type type, mica_deadline_t deadline)
{
    struct mica_service_slot *slot = &ctx->services[type];
    int ret = mica_service_lock(slot);

    if (ret)
        return ret;
    slot->cancelled = true;
    if (slot->published_once)
    {
        slot->failed_retained = true;
        slot->state = MICA_SERVICE_FAILED_RETAINED;
    }
    else
    {
        slot->state = MICA_SERVICE_STOPPING;
    }
    ret = mica_service_unlock(slot);
    if (ret)
        return ret;
    if (slot->wake_acquired)
    {
        ret = mica_sem_post(&slot->wake);
        if (ret && ret != -EOVERFLOW)
        {
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
            return ret;
        }
    }
    ret = mica_service_cancel_data(slot);
    if (ret)
        return ret;
    for (;;)
    {
        if ((ret = mica_service_lock(slot)))
            return ret;
        bool busy = slot->borrowers || slot->callbacks;

        ret = mica_service_unlock(slot);
        if (ret)
            return ret;
        if (!busy)
            break;
        if (mica_now_ns() >= deadline)
            return -ETIMEDOUT;
        ret = mica_sem_wait_until(&ctx->references_changed, deadline);
        if (ret)
            return normalized(ret);
    }
    return reap(ctx, &slot->worker, &slot->worker_acquired, &slot->worker_joined, deadline, slot);
}

int mica_rollback_start(struct mica_context *ctx, struct mica_start_transaction *transaction,
    mica_deadline_t deadline)
{
    int first = 0;

    while (transaction->count)
    {
        enum mica_service_type type = transaction->added[--transaction->count];
        struct mica_service_slot *slot = &ctx->services[type];
        int ret = mica_service_quiesce(ctx, type, deadline);

        if (ret)
        {
            if (!first)
                first = ret;
            continue; /* reachable data must survive the failed fence */
        }
        if (!slot->published_once)
        {
            ret = mica_service_release_data(slot);
            if (ret)
            {
                if (!first)
                    first = normalized(ret);
                continue;
            }
            if (slot->wake_acquired)
            {
                ret = mica_sem_destroy(&slot->wake);
                if (ret)
                {
                    if (!first)
                        first = normalized(ret);
                    continue;
                }
                slot->wake_acquired = false;
            }
            if (!mica_service_lock(slot))
            {
                slot->state = MICA_SERVICE_OFF;
                mica_service_unlock(slot);
            }
        }
    }

    bool retained = false;

    for (int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *slot = &ctx->services[i];

        if (mica_service_lock(slot))
            return first ? first : -EIO;
        retained |= slot->published_once || slot->worker_acquired;
        int released = mica_service_unlock(slot);

        if (released)
            return first ? first : released;
    }
    if (transaction->receiver_added && !retained)
    {
        if (!mica_context_lock(ctx))
        {
            ctx->cancel = true;
            mica_context_unlock(ctx);
        }
        int posted = mica_sem_post(&ctx->receiver_wake);

        if (posted && posted != -EOVERFLOW)
        {
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
            if (!first)
                first = posted;
            return first;
        }

        int ret = reap(ctx, &ctx->receiver, &ctx->receiver_acquired, &ctx->receiver_joined, deadline, NULL);

        if (ret && !first)
            first = ret;
    }
    return first;
}

static int create_list(const enum mica_service_type *types, size_t count)
{
    struct mica_context *ctx = mica_context_get();
    const struct mica_pedestal_ops *ped = mica_get_ped_ops();

    struct mica_start_transaction transaction = {0};

    bool cleanup_incomplete = false;
    int ret = mica_context_entry_lock(ctx);

    if (ret)
        return ret;
    if (ctx->operation || ctx->state == MICA_INITIALIZING || ctx->state == MICA_STOPPING)
        ret = -EBUSY;
    else if (ctx->stopped)
        ret = -EOPNOTSUPP;
    else if (ctx->state != MICA_INITIALIZED && ctx->state != MICA_RUNNING)
        ret = -ENODEV;
    for (size_t i = 0; !ret && i < count; ++i)
    {
        struct mica_service_slot *slot = &ctx->services[types[i]];

        ret = mica_service_lock(slot);
        if (ret)
            break;
        if (!ped || !ped->supports || !ped->supports(types[i]))
            ret = -EOPNOTSUPP;
        else if (slot->failed_retained)
            ret = -EOPNOTSUPP;
        else if (slot->state == MICA_SERVICE_RUNNING)
            ret = -EALREADY;
        else if (slot->state != MICA_SERVICE_OFF)
            ret = -EBUSY;
        else if (ctx->cancel && ctx->receiver_acquired)
            ret = -EBUSY; /* async stop request is not permission to restart */
        int unlocked = mica_service_unlock(slot);

        if (!ret)
            ret = unlocked;
    }
    if (!ret)
        ctx->operation = true;
    int released = mica_context_entry_unlock(ctx);

    if (!ret)
        ret = released;
    if (ret)
        return ret;
    for (size_t i = 0; i < count; ++i)
    {
        ret = start_one_service(ctx, types[i], &transaction);
        if (ret)
        {
            if (!mica_context_lock(ctx))
            {
                /* Earlier members of all succeeded locally but are now
                 * logically revoked by this same original transaction error. */
                for (size_t j = 0; j < transaction.count; ++j)
                {
                    struct mica_service_slot *s = &ctx->services[transaction.added[j]];

                    if (!mica_service_lock(s))
                    {
                        if (!s->create_error)
                            s->create_error = ret;
                        mica_service_unlock(s);
                    }
                }
                mica_context_unlock(ctx);
            }

            int cleanup = mica_rollback_start(ctx, &transaction, mica_now_ns() + MICA_STOP_BUDGET_NS);

            cleanup_incomplete = cleanup != 0;
            /* Retain original create error; diagnostics never use MICA TTY. */
            mica_log("MICA create type=%d error=%d cleanup=%d\n", types[i], ret, cleanup);
            break;
        }
    }

    int lock_ret = mica_context_lock(ctx);

    if (lock_ret)
    {
        atomic_store_explicit(&ctx->operation, false, memory_order_release);
        return ret ? ret : lock_ret;
    }
    bool running;
    int inspected = any_running(ctx, &running);

    if (!ret)
        ret = inspected;
    if (!inspected)
        ctx->state = running ? MICA_RUNNING : cleanup_incomplete ? MICA_STOPPING : MICA_INITIALIZED;
    /* An unpublished cleanup that left a live worker/receiver cannot allow
     * another create into its storage. Per-slot STOPPING rejects that retry. */
    ctx->operation = false;
    released = mica_context_unlock(ctx);
    return ret ? ret : released;
}

int mica_create_service(enum mica_service_type type)
{
    if (type < MICA_SERVICE_RPC || type >= MICA_SERVICE_MAX)
        return -EINVAL;
    return create_list(&type, 1);
}

int mica_create_all_services(void)
{
    static const enum mica_service_type defaults[] = {
        MICA_SERVICE_TTY,
        MICA_SERVICE_UMT
    };
    return create_list(defaults, sizeof(defaults) / sizeof(defaults[0]));
}

int mica_service_is_ready(enum mica_service_type type)
{
    if (type < 0 || type >= MICA_SERVICE_MAX)
        return 0;
    struct mica_service_slot *slot;
    struct mica_peer_snapshot peer;
    int ret = mica_service_borrow(type, &slot, &peer);

    if (ret)
        return 0;
    return mica_service_unborrow(slot) == 0;
}

void mica_stop_receiver(void)
{
    struct mica_context *ctx = mica_context_get();

    if (mica_context_entry_lock(ctx))
        return;
    if (ctx->operation)
    {
        mica_context_entry_unlock(ctx);
        return;
    }
    ctx->cancel = true;
    if (ctx->receiver_wake_acquired)
    {
        int ret = mica_sem_post(&ctx->receiver_wake);

        if (ret && ret != -EOVERFLOW)
            atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
    }
    mica_context_entry_unlock(ctx);
}

int mica_stop_services(void)
{
    struct mica_context *ctx = mica_context_get();
    const struct mica_pedestal_ops *ped = mica_get_ped_ops();
    mica_deadline_t deadline = mica_now_ns() + MICA_STOP_BUDGET_NS;
    int ret = mica_context_entry_lock(ctx);

    if (ret == -ENODEV)
        return 0;
    if (ret)
        return ret;
    if (ctx->operation || ctx->state == MICA_INITIALIZING)
        ret = -EBUSY;
    else
    {
        int self = caller_is_worker(ctx);

        if (self)
            ret = self < 0 ? self : -EDEADLK;
    }
    if (!ret && !service_resources(ctx))
    {
        mica_context_entry_unlock(ctx);
        return 0;
    }
    if (!ret)
    {
        ctx->operation = true;
        ctx->state = MICA_STOPPING;
    }
    int released = mica_context_entry_unlock(ctx);

    if (!ret)
        ret = released;
    if (ret)
        return ret;
    /* Close every service before fencing the IRQ producer. No late borrower
     * can pass its own gate once this phase has visited that service. */
    for (unsigned int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *slot = &ctx->services[i];

        ret = mica_service_lock(slot);
        if (ret)
            goto finish;
        slot->cancelled = true;
        ret = mica_service_unlock(slot);
        if (ret)
            goto finish;
    }
    if (!ped || !ped->quiesce)
    {
        ret = -ENODEV;
        goto finish;
    }
    ret = normalized(ped->quiesce(ctx, deadline));
    if (ret)
        goto finish;
    if ((ret = mica_context_cancel(ctx)))
        goto finish;
    for (int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        ret = mica_service_quiesce(ctx, (enum mica_service_type)i, deadline);
        if (ret)
            goto finish;
    }
    ret = reap(ctx, &ctx->receiver, &ctx->receiver_acquired, &ctx->receiver_joined, deadline, NULL);
    if (ret)
        goto finish;
    /* No receiver or business borrower can still reference these endpoints. */
    for (int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *s = &ctx->services[i];

        ret = normalized(mica_service_release_data(s));
        if (ret)
            goto finish;
        if (s->endpoint_acquired)
        {
            rpmsg_destroy_ept(&s->endpoint);
            ret = mica_service_lock(s);
            if (ret)
                goto finish;
            s->endpoint_acquired = false;
            ret = mica_service_unlock(s);
            if (ret)
                goto finish;
        }
        if (s->wake_acquired)
        {
            ret = normalized(mica_sem_destroy(&s->wake));
            if (ret)
                goto finish;
            s->wake_acquired = false;
        }
    }
finish:
    {
        int locked = mica_context_lock(ctx);

        if (locked)
        {
            atomic_store_explicit(&ctx->operation, false, memory_order_release);
            return ret ? ret : locked;
        }
    }
    {
        if (!ret)
        {
            ctx->stopped = true;
            ctx->state = MICA_INITIALIZED;
        }
        ctx->operation = false;
        int released = mica_context_unlock(ctx);

        if (!ret)
            ret = released;
    }
    return ret;
}
