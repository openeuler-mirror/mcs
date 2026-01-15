/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <metal/device.h>
#include <metal/cache.h>
#include <mica/platform/sem.h>
#include <mica/platform/barrier.h>
#include "services/mica_service_internal.h"
#include "pedestals/ped_rsc_table.h"
#include "pedestals/mica_ped.h"

/* ========== 全局上下文（保存配置和rpdev） ========== */
static struct {
    struct mica_config *config;      /* 用户配置指针 */
    struct rpmsg_device *rpdev;      /* rpmsg设备指针 */
    int initialized;                  /* 初始化标志 */
} g_mica_ctx = {0};

struct mica_config g_mica_config;
struct mica_sys_ops g_mica_sys_ops;

/* ========== 内部接口实现 ========== */

/**
 * 设置rpmsg device（由底座层调用）
 * 这是内部接口，底座初始化完成后调用
 */
void mica_set_rpdev(struct rpmsg_device *rpdev)
{
    g_mica_ctx.rpdev = rpdev;
}

/**
 * 获取rpmsg device
 * 供service层使用
 */
struct rpmsg_device *mica_get_rpdev(void)
{
    return g_mica_ctx.rpdev;
}

/**
 * 获取MICA配置
 * 供service层使用
 */
struct mica_config *mica_get_config(void)
{
    return g_mica_ctx.config;
}

/* ========== 对外API ========== */
int mica_init(struct mica_config *mica_config)
{
    int ret;
    const struct mica_pedestal_ops *ped_ops;

    if (!mica_config) {
        return -EINVAL;
    }

    if (g_mica_ctx.initialized) {
        return -EBUSY;  /* 已经初始化过了 */
    }

    /* 保存配置指针 */
    memcpy(&g_mica_config, mica_config, sizeof(struct mica_config));
    memcpy(&g_mica_sys_ops, &mica_config->sys_ops, sizeof(struct mica_sys_ops));
    g_mica_config.sys_ops = g_mica_sys_ops;
    g_mica_ctx.config = &g_mica_config;

    /* 获取底座操作接口 */
    ped_ops = mica_get_ped_ops();
    if (!ped_ops) {
        ret = -ENODEV;
        goto err;
    }

    /* 初始化中断 */
    if (!ped_ops->init_irq) {
        ret = -ENODEV;
        goto err;
    }
    ret = ped_ops->init_irq(&g_mica_config);
    if (ret) {
        goto err;
    }

    /* 初始化rpmsg backend */
    if (!ped_ops->init_rpmsg) {
        ret = -ENODEV;
        goto err;
    }
    ret = ped_ops->init_rpmsg(&g_mica_config);
    if (ret) {
        goto err;
    }

    /* 标记已初始化 */
    g_mica_ctx.initialized = 1;
    return 0;

err:
    g_mica_ctx.config = NULL;
    return ret;
}

/**
 * 反初始化MICA
 */
void mica_sys_deinit(void)
{
    const struct mica_pedestal_ops *ped_ops;

    if (!g_mica_ctx.initialized) {
        return;
    }

    ped_ops = mica_get_ped_ops();
    if (ped_ops && ped_ops->deinit) {
        ped_ops->deinit();
    }

    g_mica_ctx.config = NULL;
    g_mica_ctx.rpdev = NULL;
    g_mica_ctx.initialized = 0;
}