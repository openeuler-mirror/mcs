/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <mica/service.h>
#include <mica/platform/log.h>
#include <mica/platform/macro.h>
#include <mica/platform/delay.h>
#include "pedestals/mica_ped.h"
#include "services/mica_service_internal.h"

/* 接收线程相关 */
static pthread_t g_receiver_thread;
static int g_receiver_running = 0;

/* 接收线程函数 */
static void *receiver_thread_func(void *arg)
{
    const struct mica_pedestal_ops *ped_ops = mica_get_ped_ops();
    while (g_receiver_running) {
        ped_ops->rcv_message();
    }
    
    pthread_exit(NULL);
}

/**
 * 启动接收线程
 */
static int mica_start_receiver(pthread_attr_t *attr)
{
    int ret;
    
    if (g_receiver_running) {
        return -EBUSY;
    }

    g_receiver_running = 1;
    ret = pthread_create(&g_receiver_thread, attr, 
                            receiver_thread_func, NULL);
    
    if (ret != 0) {
        g_receiver_running = 0;
    }
    
    return ret;
}

/**
 * 停止接收线程
 */
void mica_stop_receiver(void)
{
    g_receiver_running = 0;
}

/**
 * 检查service是否就绪
 * @param type: service类型
 * @return: 1就绪，0未就绪
 */
int mica_service_is_ready(enum mica_service_type type)
{
    switch (type)
    {
#ifdef SUPPORT_RPC
    case MICA_SERVICE_RPC:
        return mica_rpc_is_ready();
#endif
    case MICA_SERVICE_TTY:
        return mica_tty_is_ready();

    case MICA_SERVICE_UMT:
        return mica_umt_is_ready();

    default:
        mica_log("Invalid mica service type.");
        return 0;
    }
}

#ifdef EXPERIMENTAL
// TODO: support creating individual service. Remember to take care of the pthread attr
int mica_create_service(enum mica_service_type type)
{
    int ret;

    switch (type)
    {
    case MICA_SERVICE_RPC:
#ifdef SUPPORT_RPC
        if (mica_service_is_ready(MICA_SERVICE_RPC))
            return MICA_SUCCESS;

        ret = mica_rpc_init_service();
        if (ret)
            return ret;
        break;
#else
        mica_log("RPC service not supported.");
        return -EOPNOTSUPP;
#endif

    case MICA_SERVICE_TTY:
        if (mica_service_is_ready(MICA_SERVICE_TTY))
            return MICA_SUCCESS;

        ret = mica_tty_init_service();
        if (ret)
            return ret;
        break;

    case MICA_SERVICE_UMT:
        if (mica_service_is_ready(MICA_SERVICE_UMT))
            return MICA_SUCCESS;

        ret = mica_umt_init_service();
        if (ret)
            return ret;
        break;
    
    default:
        mica_log("Invalid mica service type.");
        return -EINVAL;
    }

    /* Only need to start receiver thread once, it will listen to all epts */
    if (g_receiver_running) {
        return MICA_SUCCESS;
    }

    ret = mica_start_receiver(&attr);
    if (ret)
        return ret;

    return MICA_SUCCESS;
}
#endif

int mica_create_all_services(void)
{
    pthread_attr_t attr;
    pthread_t tty_thread, umt_thread, receiver_thread;
    int ret_tty, ret_umt, ret_rcv;
    int ret;

    /* 初始化线程属性（共用） */
    ret = pthread_attr_init(&attr);
    if (ret != 0) {
        mica_log("ERROR: pthread_attr_init failed: %d\n", ret);
        return ret;
    }

    ret = pthread_attr_setdetachstate(&attr, PTHREAD_CREATE_DETACHED);
    if (ret != 0) {
        mica_log("ERROR: setdetachstate failed: %d\n", ret);
        pthread_attr_destroy(&attr);
        return ret;
    }

    /* 一次性创建所有线程 */

    mica_log("Creating TTY thread...\n");
    ret_tty = mica_tty_init_service(&attr);

    mica_log("Creating receiver thread...\n");
    ret_rcv = mica_start_receiver(&attr);

    mica_log("Creating UMT thread...\n");
    ret_umt = mica_umt_init_service(&attr);

    pthread_attr_destroy(&attr);

    /* 检查是否都成功 */
    if (ret_tty != 0 || ret_umt != 0 || ret_rcv != 0) {
        mica_log("ERROR: Thread creation failed: tty=%d, umt=%d, rcv=%d\n",
                 ret_tty, ret_umt, ret_rcv);
        g_receiver_running = 0;
        return -1;
    }

    /* 等待 UMT 就绪 */
    while (!mica_service_is_ready(MICA_SERVICE_UMT)) {
        mica_delay_tick(100);
    }

    mica_log("All services ready!\n");
    return MICA_SUCCESS;
}