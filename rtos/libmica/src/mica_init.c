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

/* ========== Global context (config and rpdev) ========== */
static struct {
    struct mica_config *config;      /* user config pointer */
    struct rpmsg_device *rpdev;      /* rpmsg device pointer */
    int initialized;                  /* init flag */
} g_mica_ctx = {0};

struct mica_config g_mica_config;
struct mica_sys_ops g_mica_sys_ops;

/* ========== Internal API ========== */

/**
 * Set rpmsg device (called by pedestal layer after init).
 */
void mica_set_rpdev(struct rpmsg_device *rpdev)
{
    g_mica_ctx.rpdev = rpdev;
}

/**
 * Get rpmsg device (used by service layer).
 */
struct rpmsg_device *mica_get_rpdev(void)
{
    return g_mica_ctx.rpdev;
}

/**
 * Get MICA config (used by service layer).
 */
struct mica_config *mica_get_config(void)
{
    return g_mica_ctx.config;
}

/* ========== Public API ========== */
int mica_init(struct mica_config *mica_config)
{
    int ret;
    const struct mica_pedestal_ops *ped_ops;

    if (!mica_config) {
        return -EINVAL;
    }

    if (g_mica_ctx.initialized) {
        return -EBUSY;  /* already initialized */
    }

    /* save config pointer */
    memcpy(&g_mica_config, mica_config, sizeof(struct mica_config));
    memcpy(&g_mica_sys_ops, &mica_config->sys_ops, sizeof(struct mica_sys_ops));
    g_mica_config.sys_ops = g_mica_sys_ops;
    g_mica_ctx.config = &g_mica_config;

    /* get pedestal ops */
    ped_ops = mica_get_ped_ops();
    if (!ped_ops) {
        ret = -ENODEV;
        goto err;
    }

    /* init IRQ */
    if (!ped_ops->init_irq) {
        ret = -ENODEV;
        goto err;
    }
    ret = ped_ops->init_irq(&g_mica_config);
    if (ret) {
        goto err;
    }

    /* init rpmsg backend */
    if (!ped_ops->init_rpmsg) {
        ret = -ENODEV;
        goto err;
    }
    ret = ped_ops->init_rpmsg(&g_mica_config);
    if (ret) {
        goto err;
    }

    /* mark initialized */
    g_mica_ctx.initialized = 1;
    return 0;

err:
    g_mica_ctx.config = NULL;
    return ret;
}

/**
 * Deinitialize MICA (tear down backend).
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