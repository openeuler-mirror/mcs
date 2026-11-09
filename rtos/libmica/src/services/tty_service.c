/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 * SPDX-License-Identifier: MulanPSL-2.0
 */
#include <limits.h>
#include <stdarg.h>
#include <stdio.h>
#include "mica_service_internal.h"
#include "../mica_internal.h"

#define TTY_MAX_BUFFER_SIZE 512

static int receive_tty(struct rpmsg_endpoint *ept, void *data, size_t len, uint32_t src, void *priv)
{
    (void)ept;
    (void)src;
    return mica_service_enqueue(priv, data, len);
}

int mica_tty_init_service(struct mica_context *ctx, struct mica_service_slot *slot)
{
    (void)ctx; /* Stable slot identity is established before framework publish. */
    slot->name = "rpmsg-tty";
    slot->rx_callback = receive_tty;
    slot->worker_entry = tty_service_thread;
    return mica_service_prepare_data(slot, false);
}

void tty_service_thread(void *arg)
{
    struct mica_service_slot *slot = arg;

    for (;;)
    {
        unsigned int index;
        void *data;
        size_t len;
        int ret = mica_service_take(slot, true, &index, &data, &len);

        if (ret == -ESHUTDOWN)
            return;
        if (ret)
        {
            ret = mica_sem_wait_until(&slot->wake, mica_now_ns() + MICA_RECEIVER_POLL_NS);
            if (ret && ret != -ETIMEDOUT)
            {
                atomic_store_explicit(&slot->ctx->sync_failed, true, memory_order_release);
                return;
            }
            continue;
        }
        void(*handler)(char) = slot->ctx->config.sys_ops.shell_cmd_handler;
        if (handler)
            for (size_t i = 0; i < len; ++i)
            handler(((char *)data)[i]);
        mica_service_complete(slot, index, true);
    }
}

int mica_tty_is_ready(void)
{
    return mica_service_is_ready(MICA_SERVICE_TTY);
}

int mica_tty_send(unsigned char *data, size_t len)
{
    if (!data || !len || len > INT_MAX)
        return -EINVAL;
    struct mica_service_slot *slot;
    struct mica_peer_snapshot peer;
    int ret = mica_service_borrow(MICA_SERVICE_TTY, &slot, &peer);

    if (ret)
        return ret;
    struct mica_service_data *private = slot->service_data;

    metal_mutex_acquire(&private->tx_lock);
    ret = rpmsg_send_offchannel(&slot->endpoint, peer.local_addr, peer.remote_addr, data, (int)len);
    metal_mutex_release(&private->tx_lock);
    int returned = mica_service_unborrow(slot);

    return ret < 0 ? ret : returned ? returned : ret;
}

int mica_tty_printf(const char *format, ...)
{
    if (!format)
        return -EINVAL;
    unsigned char buffer[TTY_MAX_BUFFER_SIZE];

    va_list args;
    va_start(args, format);

    int len = vsnprintf((char *)buffer, sizeof(buffer), format, args);

    va_end(args);
    if (len < 0)
        return len;
    if ((size_t)len >= sizeof(buffer))
        return -EMSGSIZE;
    if (!len)
        return 0;
    return mica_tty_send(buffer, (size_t)len); /* no implicit NUL */
}
