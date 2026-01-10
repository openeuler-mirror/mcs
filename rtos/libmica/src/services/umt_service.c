/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <mica/service.h>
#include <mica/platform/macro.h>
#include <mica/platform/sem.h>
#include <mica/platform/delay.h>
#include <mica/platform/log.h>
#include "mica_service_internal.h"

#define UMT_SEND_BUFFER_OFFSET  0x100000  /* 1MB */

/* UMT数据结构 */
typedef struct umt_data {
    uint64_t phy_addr;            /* 物理地址 */
    uint32_t data_len;            /* 数据长度 */
} umt_data_t;

/* UMT接收消息 */
struct umt_rcv_msg {
    void *data;
    size_t len;
};

/* UMT服务私有数据 */
struct umt_service_priv {
    mica_sem_t rx_sem;
    struct umt_rcv_msg rx_msg;
    bool umt_send_addr_init;      /* 是否初始化发送地址 */
    uintptr_t send_buffer_addr;  /* 发送缓冲区地址 */
};

#define RPMSG_UMT_EPT_NAME "rpmsg-umt"
static struct rpmsg_endpoint g_umt_ept;
static struct umt_service_priv g_umt_priv;

/**
 * 检查UMT是否就绪
 */
int mica_umt_is_ready(void)
{
    return is_rpmsg_ept_ready(&g_umt_ept);
}

/* UMT接收回调 */
static int umt_rx_callback(struct rpmsg_endpoint *ept, void *data,
                          size_t len, uint32_t src, void *priv)
{
    struct umt_service_priv *umt_priv = (struct umt_service_priv *)priv;
    umt_data_t *umt_data;

    /* 保持rx buffer */
    rpmsg_hold_rx_buffer(ept, data);

    umt_priv->rx_msg.data = data;
    umt_priv->rx_msg.len = len;
    umt_data = (umt_data_t *)data;

    /* 首次接收时初始化发送缓冲区地址 */
    if (!g_umt_priv.umt_send_addr_init) {
        g_umt_priv.send_buffer_addr = umt_data->phy_addr + UMT_SEND_BUFFER_OFFSET;
        g_umt_priv.umt_send_addr_init = true;

    }

    /* 如果包含有效的物理地址，则通知接收线程 */
    if (umt_data->phy_addr) {
        mica_sem_post(umt_priv->rx_sem);
    } else {
        /* 无效消息，直接释放 */
        rpmsg_release_rx_buffer(ept, data);
    }

    return MICA_SUCCESS;
}

/* UMT service线程 */
void *umt_service_thread(void *arg)
{
    int ret;
    struct rpmsg_device *rpdev = mica_get_rpdev();

    if (!rpdev) {
        mica_log("UMT: rpmsg device not initialized\n");
        goto err_init;
    }

    /* 创建信号量 */
    ret = mica_sem_init(&g_umt_priv.rx_sem, 0);
    if (ret != MICA_SUCCESS) {
        mica_log("UMT: failed to create semaphore: %d\n", ret);
        goto err_init;
    }

    g_umt_priv.umt_send_addr_init = false;

    g_umt_ept.priv = &g_umt_priv;
    /* 注册并创建UMT endpoint */
    ret = rpmsg_create_ept(&g_umt_ept, rpdev, RPMSG_UMT_EPT_NAME,
                   RPMSG_ADDR_ANY, RPMSG_ADDR_ANY,
                   umt_rx_callback, NULL);
    if (ret != 0) {
        mica_log("UMT: failed to create endpoint: %d\n", ret);
        goto err_sem;
    }

    /* UMT service不需要主循环，只提供send/receive API */
    pthread_exit(NULL);

err_sem:
    mica_sem_destroy(g_umt_priv.rx_sem);
err_init:
    mica_log("UMT: service thread exiting with error\n");
    pthread_exit(NULL);
}

/**
 * 初始化UMT service（创建独立线程）
 */
int mica_umt_init_service(pthread_attr_t *attr)
{
    pthread_t thread;
    int ret;

    ret = pthread_create(&thread, attr, umt_service_thread, NULL);

    return ret;
}

/**
 * 发送数据到对端（零拷贝）
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

    /* 检查发送缓冲区是否已初始化 */
    if (!g_umt_priv.umt_send_addr_init) {
        mica_log("UMT: send_buffer_addr not initialized\n");
        return -EFAULT;
    }

    /* 拷贝数据到共享内存 */
    memcpy((void *)(uintptr_t)g_umt_priv.send_buffer_addr + offset, data, len);
    /* 设置消息物理地址和数据长度 */
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
 * 接收数据从对端（零拷贝）
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

    /* 等待接收数据 */
    ret = mica_sem_wait(g_umt_priv.rx_sem);

    /* TODO: do timeout */
    // if (timeout_ms < 0) {
    //     ret = mica_sem_wait(g_umt_priv.rx_sem, MICA_WAIT_FOREVER);
    // } else if (timeout_ms == 0) {
    //     ret = mica_sem_wait(g_umt_priv.rx_sem, MICA_NO_WAIT);
    // } else {
    //     ret = mica_sem_wait(g_umt_priv.rx_sem, timeout_ms);
    // }

    if (ret != MICA_SUCCESS) {
        return -EAGAIN;
    }

    /* 检查接收消息 */
    if (g_umt_priv.rx_msg.len == 0 || g_umt_priv.rx_msg.data == NULL) {
        return -EFAULT;
    }

    umt_data = (umt_data_t *)g_umt_priv.rx_msg.data;

    /* todo: 检查缓冲区大小 */

    /* 从共享内存拷贝数据 */
    memcpy(buffer, (void *)(uintptr_t)umt_data->phy_addr, umt_data->data_len);
    *len = umt_data->data_len;

    /* 释放rx buffer */
    rpmsg_release_rx_buffer(&g_umt_ept, g_umt_priv.rx_msg.data);

    /* 清空接收消息 */
    g_umt_priv.rx_msg.len = 0;
    g_umt_priv.rx_msg.data = NULL;

    return MICA_SUCCESS;
}
