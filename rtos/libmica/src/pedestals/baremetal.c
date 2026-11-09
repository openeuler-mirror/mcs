/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 * SPDX-License-Identifier: MulanPSL-2.0
 */
#include <stdlib.h>
#include <metal/io.h>
#include <mica/platform/irq.h>
#include "ped_rsc_table.h"
#include "../mica_internal.h"

struct baremetal_cookie
{
    struct metal_io_region buffers;
    metal_phys_addr_t phys;
    struct mica_context *ctx;
    bool metal_acquired;
    bool irq_acquired;
};

static void handle_irq(void *arg)
{
    struct mica_context *ctx = arg;

    /* IRQ producer only: even reset/poweroff control is handled by receiver. */
    int ret = mica_sem_post(&ctx->receiver_wake);

    if (ret && ret != -EOVERFLOW)
        atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
}

static int notify(void *arg, uint32_t id)
{
    (void)id;

    struct baremetal_cookie *cookie = arg;

    return mica_trigger_irq(cookie->ctx->config.ipc_irq_num);
}

static bool supports(enum mica_service_type t)
{
    return t == MICA_SERVICE_TTY || t == MICA_SERVICE_UMT;
}

static int prepare(struct mica_context *ctx, struct mica_rpmsg_layout *layout)
{
    if (!ctx->config.shm_base_addr || !ctx->config.shm_size)
        return -EINVAL;
    if (ctx->pedestal_cookie)
        return -EBUSY;
    struct baremetal_cookie *cookie = calloc(1, sizeof(*cookie));

    if (!cookie)
        return -ENOMEM;
    cookie->ctx = ctx;
    ctx->pedestal_cookie = cookie;
    ctx->pedestal_acquired = true;

    struct metal_init_params params = METAL_INIT_DEFAULTS;
    int ret = metal_init(&params);

    if (ret)
        return ret < 0 ? ret : -ret;
    cookie->metal_acquired = true;
    cookie->phys = ctx->config.shm_base_addr;
    metal_io_init(&cookie->buffers, (void *)ctx->config.shm_base_addr, &cookie->phys,
        ctx->config.shm_size, -1, 0, NULL);

    void *table;
    int bytes;

    rsc_table_get(&table, &bytes);
    if (bytes < (int)sizeof(struct fw_resource_table))
        return -EINVAL;
    ret = mica_request_irq(ctx->config.ipc_irq_num, handle_irq, ctx);
    if (ret)
    {
        return ret < 0 ? ret : -ret;
    }
    cookie->irq_acquired = true;
    ret = mica_unmask_irq(ctx->config.ipc_irq_num);
    if (ret)
        return ret < 0 ? ret : -ret;

    struct fw_resource_table *runtime = table;
    volatile uint8_t *status = &runtime->vdev.status;
    mica_deadline_t deadline = mica_now_ns() + MICA_STOP_BUDGET_NS;

    while (!(*status & VIRTIO_CONFIG_STATUS_DRIVER_OK))
    {
        ret = mica_context_lock(ctx);
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
    return mica_rsc_layout(table, (size_t)bytes, &cookie->buffers, notify, cookie, layout);
}

static int quiesce(struct mica_context *ctx, uint64_t deadline)
{
    struct baremetal_cookie *cookie = ctx->pedestal_cookie;

    if (!cookie || !cookie->irq_acquired)
        return 0;
    int ret = mica_sync_release_irq(ctx->config.ipc_irq_num, deadline);

    if (!ret)
    {
        cookie->irq_acquired = false;
    }
    return ret;
}

static int detach(struct mica_context *ctx)
{
    struct baremetal_cookie *cookie = ctx->pedestal_cookie;

    if (!cookie)
        return 0;
    if (cookie->irq_acquired || ctx->openamp_acquired || ctx->receiver_acquired)
        return -EBUSY;
    if (cookie->metal_acquired)
        metal_finish();
    free(cookie);
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
