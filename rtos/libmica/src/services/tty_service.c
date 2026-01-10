/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdarg.h>
#include <mica/mica.h>
#include <mica/service.h>
#include <mica/platform/macro.h>
#include <mica/platform/sem.h>
#include <mica/platform/log.h>
#include <mica/platform/delay.h>
#include <mica/platform/securec.h>
#include "mica_service_internal.h"

#define TTY_MAX_BUFFER_SIZE  512

/* TTY接收消息 */
struct tty_rcv_msg {
    void *data;
    size_t len;
};

/* TTY服务私有数据 */
struct tty_service_priv {
    mica_sem_t rx_sem;
    struct tty_rcv_msg rx_msg;
#ifdef MICA_SMP
    mica_spinlock_t tx_lock;
#endif
};

#define RPMSG_TTY_EPT_NAME "rpmsg-tty"
static struct rpmsg_endpoint g_tty_ept;
static struct tty_service_priv g_tty_priv;
static bool g_tty_service_running = false;

/**
 * 检查TTY是否就绪
 */
int mica_tty_is_ready(void)
{
    return is_rpmsg_ept_ready(&g_tty_ept);
}

/* TTY接收回调 */
static int rx_tty_callback(struct rpmsg_endpoint *ept, void *data,
                          size_t len, uint32_t src, void *priv)
{
    struct tty_service_priv *tty_priv = (struct tty_service_priv *)priv;
    int ret;

    rpmsg_hold_rx_buffer(ept, data);

    tty_priv->rx_msg.data = data;
    tty_priv->rx_msg.len = len;
    
    /* 通知处理线程 */
    ret = mica_sem_post(tty_priv->rx_sem);
    
    if (ret != 0) {
        mica_log("ERROR: sem_post failed in callback!\n");
    }

    return 0;
}

void *tty_service_thread(void *arg)
{
    int ret;
    char *tty_data;
    struct mica_config *mica_config = mica_get_config();
    struct rpmsg_device *rpdev = mica_get_rpdev();

    if (!rpdev) {
        mica_log("TTY: rpmsg device not initialized\n");
        goto err_init;
    }

    ret = mica_sem_init(&g_tty_priv.rx_sem, 0);
    if (ret != MICA_SUCCESS) {
        mica_log("TTY: failed to create semaphore: %d\n", ret);
        goto err_init;
    }
    
    g_tty_ept.priv = &g_tty_priv;
    ret = rpmsg_create_ept(&g_tty_ept, rpdev, RPMSG_TTY_EPT_NAME,
                   RPMSG_ADDR_ANY, RPMSG_ADDR_ANY,
                   rx_tty_callback, NULL);
    if (ret != 0) {
        mica_log("TTY: failed to create endpoint: %d\n", ret);
        goto err_sem;
    }

    g_tty_service_running = true;

    while (g_tty_ept.addr != RPMSG_ADDR_ANY && g_tty_service_running) {
        /* 等待接收数据 */
        ret = mica_sem_wait(g_tty_priv.rx_sem);

        if (ret != MICA_SUCCESS) {
            mica_log("TTY: sem_wait failed: %d\n", ret);
            continue;
        }

        if (g_tty_priv.rx_msg.len == 0 || g_tty_priv.rx_msg.data == NULL) {
            mica_log("TTY: invalid rx message, len=%zu, data=%p\n",
                     g_tty_priv.rx_msg.len, g_tty_priv.rx_msg.data);
            continue;
        }

        tty_data = (char *)g_tty_priv.rx_msg.data;
        tty_data[g_tty_priv.rx_msg.len] = '\0';

        /* 调用平台shell处理回调 */
        if (mica_config && mica_config->sys_ops.shell_cmd_handler) {
            for (int i = 0; i < g_tty_priv.rx_msg.len; i++) {
                mica_config->sys_ops.shell_cmd_handler(tty_data[i]);
            }
        }

        /* 释放rx buffer */
        rpmsg_release_rx_buffer(&g_tty_ept, g_tty_priv.rx_msg.data);

        /* 清空消息 */
        g_tty_priv.rx_msg.len = 0;
        g_tty_priv.rx_msg.data = NULL;
    }
    mica_log("TTY: service exiting\n");
    rpmsg_destroy_ept(&g_tty_ept);

err_sem:
    mica_sem_destroy(g_tty_priv.rx_sem);
err_init:
    g_tty_service_running = false;

    pthread_exit(NULL);
}

/**
 * 初始化TTY service（创建独立线程）
 */
int mica_tty_init_service(pthread_attr_t *attr)
{
    pthread_t thread;
    int ret;

    ret = pthread_create(&thread, attr, tty_service_thread, NULL);

    return ret;
}

int mica_tty_stop_service(void)
{

}

/**
 * TTY发送数据
 */
int mica_tty_send(unsigned char *data, size_t len)
{
    int ret;
    uintptr_t intSave;

    if (!mica_tty_is_ready()) {
        return 0;
    }
#if defined(MICA_SMP)
    intSave = LOS_SplIrqLock(&g_ttyLock);
#endif
    ret = rpmsg_send(&g_tty_ept, data, len);
#if defined(MICA_SMP)
    LOS_SplIrqUnlock(&g_ttyLock, intSave);
#endif
    return ret;
}

/**
 * TTY格式化打印
 */
int mica_tty_printf(const char *format, ...)
{
    int len;
    va_list vaList;
    unsigned char tty_buff[TTY_MAX_BUFFER_SIZE];

    mica_memset_s(tty_buff, TTY_MAX_BUFFER_SIZE, 0, TTY_MAX_BUFFER_SIZE);
    va_start(vaList, format);
    len = vsnprintf(tty_buff, TTY_MAX_BUFFER_SIZE, format, vaList);
    if (len == -1) {
        return len;
    }
    va_end(vaList);

    return mica_tty_send(tty_buff, len + 1);
}