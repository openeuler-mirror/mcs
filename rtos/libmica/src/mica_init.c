/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 * SPDX-License-Identifier: MulanPSL-2.0
 */
#include <string.h>
#include <mica/platform/log.h>
#include "services/mica_service_internal.h"
#include "mica_internal.h"

void mica_set_rpdev(struct rpmsg_device *rpdev)
{
    mica_context_get()->rpdev = rpdev;
}

struct rpmsg_device *mica_get_rpdev(void)
{
    return mica_context_get()->rpdev;
}

struct mica_config *mica_get_config(void)
{
    return &mica_context_get()->config;
}

static int cleanup_backend(struct mica_context *ctx, mica_deadline_t deadline)
{
    const struct mica_pedestal_ops *ped = mica_get_ped_ops();
    int ret;

    ret = mica_context_close(ctx);
    if (ret)
        return ret;
    ret = mica_context_drain_entries(ctx, deadline);
    if (ret)
        return ret;
    if (atomic_load_explicit(&ctx->sync_failed, memory_order_acquire))
        return -EIO;
    if (ctx->pedestal_acquired)
    {
        if (!ped || !ped->quiesce || !ped->detach)
            return -ENODEV;
        ret = ped->quiesce(ctx, deadline);
        if (ret)
            return ret < 0 ? ret : -ret;
    }
    if (ctx->openamp_acquired)
    {
        ret = mica_openamp_destroy(ctx->openamp);
        if (ret)
            return ret < 0 ? ret : -ret;
        ctx->openamp = NULL;
        ctx->openamp_acquired = false;
        ctx->rpdev = NULL;
    }
    if (ctx->pedestal_acquired)
    {
        ret = ped->detach(ctx);
        if (ret)
            return ret < 0 ? ret : -ret;
        ctx->pedestal_cookie = NULL;
        ctx->pedestal_acquired = false;
    }
    /* No producers, helper, receivers or service/API borrowers remain. New
     * public entry attempts only observe STOPPING+operation and return. */
    if (ctx->receiver_wake_acquired)
    {
        ret = mica_sem_destroy(&ctx->receiver_wake);
        if (ret)
            return ret;
        ctx->receiver_wake_acquired = false;
    }
    if (ctx->references_changed_acquired)
    {
        ret = mica_sem_destroy(&ctx->references_changed);
        if (ret)
            return ret;
        ctx->references_changed_acquired = false;
    }
    for (unsigned int i = MICA_SERVICE_MAX; i > 0; --i)
    {
        struct mica_service_slot *slot = &ctx->services[i - 1];

        if (slot->gate_acquired)
        {
            metal_mutex_deinit(&slot->gate);
            slot->gate_acquired = false;
        }
    }
    memset(&ctx->layout, 0, sizeof(ctx->layout));
    return mica_context_destroy_lock(ctx);
}

int mica_init(struct mica_config *config)
{
    if (!config)
        return -EINVAL;
    struct mica_config copied_config = *config;
    struct mica_context *ctx = mica_context_get();

    if (atomic_flag_test_and_set_explicit(&mica_init_gate, memory_order_acquire))
        return -EBUSY;
    if (ctx->state_lock_acquired || ctx->state != MICA_UNINITIALIZED || mica_context_entry_users())
    {
        atomic_flag_clear_explicit(&mica_init_gate, memory_order_release);
        return -EBUSY;
    }
    if (ctx->stopped)
    {
        atomic_flag_clear_explicit(&mica_init_gate, memory_order_release);
        return -EOPNOTSUPP;
    }
    /* Zero only the plain context, never its distinct static atomic gate. */
    memset(ctx, 0, sizeof(*ctx));

    atomic_init(&ctx->sync_failed, false);
    atomic_init(&ctx->operation, false);
    atomic_init(&ctx->cancel, false);
    ctx->entry_closed = true;
    for (unsigned int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        atomic_init(&ctx->services[i].borrowers, 0);
        atomic_init(&ctx->services[i].callbacks, 0);
        ctx->services[i].ctx = ctx;
        ctx->services[i].type = (enum mica_service_type)i;
    }
    metal_mutex_init(&ctx->state_lock);
    ctx->state_lock_acquired = true;
    ctx->config = copied_config;
    ctx->state = MICA_INITIALIZING;
    ctx->operation = true;
    atomic_flag_clear_explicit(&mica_init_gate, memory_order_release);
    int ret = mica_sem_init(&ctx->receiver_wake, 0);
    if (!ret)
        ctx->receiver_wake_acquired = true;
    if (!ret)
    {
        ret = mica_sem_init(&ctx->references_changed, 0);
        if (!ret)
            ctx->references_changed_acquired = true;
    }
    for (unsigned int i = 0; !ret && i < MICA_SERVICE_MAX; ++i)
    {
        metal_mutex_init(&ctx->services[i].gate);
        ctx->services[i].gate_acquired = true;
    }

    const struct mica_pedestal_ops *ped = mica_get_ped_ops();

    if (!ret && (!ped || !ped->supports || !ped->prepare || !ped->quiesce || !ped->detach))
        ret = -ENODEV;
    if (!ret)
        ret = ped->prepare(ctx, &ctx->layout);
    /* prepare itself records its FIRST partial cookie, including on error. */
    if (!ret)
    {
        ret = mica_openamp_create(&ctx->layout, &ctx->openamp);
        if (ctx->openamp)
            ctx->openamp_acquired = true;
    }
    if (!ret)
    {
        ctx->rpdev = mica_openamp_rpdev(ctx->openamp);
        if (!ctx->rpdev)
            ret = -ENODEV;
    }
    if (!ret)
    {
        ret = mica_context_lock(ctx);
        if (!ret)
        {
            ctx->state = MICA_INITIALIZED;
            ret = mica_context_unlock(ctx);
            if (!ret)
                ret = mica_context_open(ctx);
            if (!ret)
            {
                /* Keep lifecycle ownership until entry admission is published;
                 * remove cannot race gate teardown against this final open. */
                atomic_store_explicit(&ctx->operation, false, memory_order_release);
                return 0;
            }
        }
    }
    ret = ret < 0 ? ret : -ret;
    if (!mica_context_lock(ctx))
    {
        ctx->state = MICA_STOPPING;
        mica_context_unlock(ctx);
    }

    int cleanup = cleanup_backend(ctx, mica_now_ns() + MICA_STOP_BUDGET_NS);

    if (cleanup && !mica_context_lock(ctx))
    {
        ctx->state = MICA_STOPPING;
        mica_context_unlock(ctx);
    }
    mica_log("MICA init failed=%d cleanup=%d retained=%d\n", ret, cleanup, cleanup != 0);
    if (cleanup)
        atomic_store_explicit(&ctx->operation, false, memory_order_release);
    return ret;
}

void mica_remove(void)
{
    struct mica_context *ctx = mica_context_get();
    int ret = mica_context_entry_lock(ctx);

    if (ret)
        return;
    if (ctx->operation)
    {
        mica_context_entry_unlock(ctx);
        mica_log("MICA remove rejected: lifecycle operation\n");
        return;
    }

    bool busy = ctx->state == MICA_RUNNING || ctx->state == MICA_STARTING || ctx->receiver_acquired;

    for (unsigned int i = 0; i < MICA_SERVICE_MAX; ++i)
    {
        struct mica_service_slot *s = &ctx->services[i];

        if (s->gate_acquired)
        {
            ret = mica_service_lock(s);
            if (ret)
            {
                mica_context_entry_unlock(ctx);
                return;
            }
        }
        busy |= s->worker_acquired || s->endpoint_acquired || s->borrowers || s->callbacks ||
            s->service_data != NULL || s->wake_acquired;
        if (s->gate_acquired && mica_service_unlock(s))
        {
            mica_context_entry_unlock(ctx);
            return;
        }
    }
    if (busy)
    {
        mica_context_entry_unlock(ctx);
        mica_log("MICA remove rejected: live/unstopped backend\n");
        return;
    }
    ctx->operation = true;
    ctx->state = MICA_STOPPING;
    ctx->cancel = true;
    mica_context_entry_unlock(ctx);

    mica_deadline_t deadline = mica_now_ns() + MICA_STOP_BUDGET_NS;

    ret = cleanup_backend(ctx, deadline);
    if (ret)
    {
        if (!mica_context_lock(ctx))
        {
            ctx->state = MICA_STOPPING;
            mica_context_unlock(ctx);
        }
        mica_log("MICA remove retained backend: %d\n", ret);
        atomic_store_explicit(&ctx->operation, false, memory_order_release);
    }
}
