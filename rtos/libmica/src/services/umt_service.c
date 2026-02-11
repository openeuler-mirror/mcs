/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <errno.h>
#include <stdbool.h>
#include <string.h>
#include <mica/service.h>
#include <mica/platform/macro.h>
#include <mica/platform/sem.h>
#include <mica/platform/delay.h>
#include <mica/platform/log.h>
#include "mica_service_internal.h"

#define UMT_SEND_BUFFER_OFFSET  0x100000  /* 1MB */

/* UMT data descriptor */
typedef struct umt_data {
    uint64_t phy_addr;            /* physical address */
    uint32_t data_len;            /* data length */
} umt_data_t;

/* UMT receive message */
struct umt_rcv_msg {
    void *data;
    size_t len;
};

/* User-registered receive callback (single; payload pointer invalid after callback returns) */
struct umt_rcv_cb {
    umt_rcv_cb_t fn;
    void *priv;
    bool registered;
};

/* UMT service private data */
struct umt_service_priv {
    mica_sem_t rx_sem;             /* posted when rpmsg receives; consumed by umt_service_thread */
    mica_sem_t passive_rcv_sem;    /* passive pull: posted by umt_service_thread, consumed by mica_rcv_data */
    struct umt_rcv_msg rx_msg;
    struct umt_rcv_cb rcv_cb;
    bool umt_send_addr_init;       /* whether send buffer addr is initialized */
    uintptr_t send_buffer_addr;   /* send buffer address */
};

#define RPMSG_UMT_EPT_NAME "rpmsg-umt"
static struct rpmsg_endpoint g_umt_ept;
static struct umt_service_priv g_umt_priv;
static bool g_umt_service_running = false;

/**
 * Check if UMT is ready.
 */
int mica_umt_is_ready(void)
{
    return is_rpmsg_ept_ready(&g_umt_ept);
}

/* UMT receive callback */
static int umt_rx_callback(struct rpmsg_endpoint *ept, void *data,
                          size_t len, uint32_t src, void *priv)
{
    struct umt_service_priv *umt_priv = (struct umt_service_priv *)priv;
    umt_data_t *umt_data;

    /* hold rx buffer */
    rpmsg_hold_rx_buffer(ept, data);

    umt_priv->rx_msg.data = data;
    umt_priv->rx_msg.len = len;
    umt_data = (umt_data_t *)data;

    /* init send buffer address on first receive */
    if (!g_umt_priv.umt_send_addr_init) {
        g_umt_priv.send_buffer_addr = umt_data->phy_addr + UMT_SEND_BUFFER_OFFSET;
        g_umt_priv.umt_send_addr_init = true;

    }

    /* if valid physical address, notify receive thread */
    if (umt_data->phy_addr) {
        mica_sem_post(umt_priv->rx_sem);
    } else {
        /* invalid message, release immediately */
        rpmsg_release_rx_buffer(ept, data);
    }

    return MICA_SUCCESS;
}

/* UMT service thread */
void *umt_service_thread(void *arg)
{
    int ret;
    struct rpmsg_device *rpdev = mica_get_rpdev();
    umt_data_t *umt_data;

    (void)arg;
    if (!rpdev) {
        mica_log("UMT: rpmsg device not initialized\n");
        goto err_init;
    }

    /* create semaphores */
    ret = mica_sem_init(&g_umt_priv.rx_sem, 0);
    if (ret != MICA_SUCCESS) {
        mica_log("UMT: failed to create rx_sem: %d\n", ret);
        goto err_init;
    }
    ret = mica_sem_init(&g_umt_priv.passive_rcv_sem, 0);
    if (ret != MICA_SUCCESS) {
        mica_log("UMT: failed to create passive_rcv_sem: %d\n", ret);
        goto err_rx_sem;
    }

    g_umt_priv.umt_send_addr_init = false;
    g_umt_priv.rcv_cb.registered = false;

    g_umt_ept.priv = &g_umt_priv;
    /* register and create UMT endpoint */
    ret = rpmsg_create_ept(&g_umt_ept, rpdev, RPMSG_UMT_EPT_NAME,
                   RPMSG_ADDR_ANY, RPMSG_ADDR_ANY,
                   umt_rx_callback, NULL);
    if (ret != 0) {
        mica_log("UMT: failed to create endpoint: %d\n", ret);
        goto err_passive_sem;
    }

    g_umt_service_running = true;

    /* main loop: consume rx_sem; callback mode invokes user callback with payload ptr, else post passive_rcv_sem for mica_rcv_data */
    while (g_umt_ept.addr != RPMSG_ADDR_ANY && g_umt_service_running) {
        if (mica_sem_wait(g_umt_priv.rx_sem) != MICA_SUCCESS)
            continue;
        if (g_umt_priv.rx_msg.len == 0 || g_umt_priv.rx_msg.data == NULL)
            continue;

        umt_data = (umt_data_t *)g_umt_priv.rx_msg.data;

        if (g_umt_priv.rcv_cb.registered && g_umt_priv.rcv_cb.fn != NULL) {
            /* callback mode: pass payload pointer, valid only during callback */
            g_umt_priv.rcv_cb.fn((const void *)(uintptr_t)umt_data->phy_addr,
                                (int)umt_data->data_len,
                                g_umt_priv.rcv_cb.priv);
        } else {
            /* passive pull: notify mica_rcv_data */
            mica_sem_post(g_umt_priv.passive_rcv_sem);
            continue;   /* do not release here; mica_rcv_data releases after copy */
        }
        rpmsg_release_rx_buffer(&g_umt_ept, g_umt_priv.rx_msg.data);
        g_umt_priv.rx_msg.data = NULL;
        g_umt_priv.rx_msg.len = 0;
    }

err_passive_sem:
    mica_sem_destroy(g_umt_priv.passive_rcv_sem);
err_rx_sem:
    mica_sem_destroy(g_umt_priv.rx_sem);
err_init:
    mica_log("UMT: service thread exiting with error\n");
    pthread_exit(NULL);
}

/**
 * Initialize UMT service (spawn dedicated thread).
 */
int mica_umt_init_service(pthread_attr_t *attr)
{
    pthread_t thread;
    int ret;

    ret = pthread_create(&thread, attr, umt_service_thread, NULL);

    return ret;
}

/**
 * Send data to peer (zero-copy into shared buffer).
 */
int mica_send_data(void *data, int offset, size_t len)
{
    umt_data_t msg = {0};
    int ret;

    if (!mica_umt_is_ready()) {
        return -EAGAIN;
    }

    if (!data || len == 0 || len > UMT_SEND_BUFFER_OFFSET) {
        return -EINVAL;
    }

    /* check send buffer initialized */
    if (!g_umt_priv.umt_send_addr_init) {
        mica_log("UMT: send_buffer_addr not initialized\n");
        return -EFAULT;
    }

    /* copy data to shared memory */
    memcpy((void *)(uintptr_t)g_umt_priv.send_buffer_addr + offset, data, len);
    /* set message physical address and length */
    msg.phy_addr = g_umt_priv.send_buffer_addr + offset;
    msg.data_len = (uint32_t)len;

    ret = rpmsg_send(&g_umt_ept, &msg, sizeof(msg));
    if (ret < 0) {
        mica_log("UMT: send failed, ret=%d, len=%zu\n", ret, len);
        return -EIO;
    }

    return MICA_SUCCESS;
}

/**
 * Register UMT receive callback. Mutually exclusive with mica_rcv_data; returns -EALREADY if already registered.
 */
int mica_umt_register_rcv_cb(umt_rcv_cb_t callback, void *priv)
{
    if (!mica_umt_is_ready())
        return -EAGAIN;
    if (!callback)
        return -EINVAL;
    if (g_umt_priv.rcv_cb.registered)
        return -EALREADY;

    g_umt_priv.rcv_cb.fn = callback;
    g_umt_priv.rcv_cb.priv = priv;
    g_umt_priv.rcv_cb.registered = true;
    return 0;
}

/**
 * Unregister UMT receive callback. After this, use mica_rcv_data() for passive receive.
 */
int mica_umt_unregister_rcv_cb(void)
{
    g_umt_priv.rcv_cb.registered = false;
    g_umt_priv.rcv_cb.fn = NULL;
    g_umt_priv.rcv_cb.priv = NULL;
    return 0;
}

/**
 * Receive data from peer (passive pull; copy into user buffer).
 * Mutually exclusive with callback mode: returns -EBUSY if mica_umt_register_rcv_cb was registered.
 */
int mica_rcv_data(void *buffer, size_t *len)
{
    umt_data_t *umt_data;
    int ret;

    if (!mica_umt_is_ready()) {
        return -EAGAIN;
    }

    if (!buffer || !len) {
        return -EINVAL;
    }

    if (g_umt_priv.rcv_cb.registered) {
        return -EBUSY;
    }

    /* passive wait: umt_service_thread posts when message received */
    /* TODO: do timeout */
    // if (timeout_ms < 0) {
    //     ret = mica_sem_wait(g_umt_priv.passive_rcv_sem, MICA_WAIT_FOREVER);
    // } else if (timeout_ms == 0) {
    //     ret = mica_sem_wait(g_umt_priv.passive_rcv_sem, MICA_NO_WAIT);
    // } else {
    //     ret = mica_sem_wait(g_umt_priv.passive_rcv_sem, timeout_ms);
    // }
    ret = mica_sem_wait(g_umt_priv.passive_rcv_sem);
    if (ret != MICA_SUCCESS) {
        return -EAGAIN;
    }

    /* check received message */
    if (g_umt_priv.rx_msg.len == 0 || g_umt_priv.rx_msg.data == NULL) {
        return -EFAULT;
    }

    umt_data = (umt_data_t *)g_umt_priv.rx_msg.data;

    /* TODO: check buffer size */

    /* copy from shared memory */
    memcpy(buffer, (void *)(uintptr_t)umt_data->phy_addr, umt_data->data_len);
    *len = umt_data->data_len;

    /* release rx buffer and clear */
    rpmsg_release_rx_buffer(&g_umt_ept, g_umt_priv.rx_msg.data);
    g_umt_priv.rx_msg.len = 0;
    g_umt_priv.rx_msg.data = NULL;

    return 0;
}
