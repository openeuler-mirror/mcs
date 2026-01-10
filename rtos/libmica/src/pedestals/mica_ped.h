/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_PEDESTAL_H
#define MICA_PEDESTAL_H

#include <openamp/open_amp.h>
#include <mica/mica.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ========== 底座操作接口 ========== */
struct mica_pedestal_ops {
    /**
     * 初始化中断
     * @param config: MICA配置
     * @return: 0成功，负数失败
     */
    int (*init_irq)(struct mica_config *config);

    /**
     * 初始化RPMsg backend
     * @param config: MICA配置
     * @return: 0成功，负数失败
     */
    int (*init_rpmsg)(struct mica_config *config);

    /**
     * 初始化RPMsg backend
     * @param config: MICA配置
     * @return: rpmsg_device指针，NULL表示失败
     */
    void (*rcv_message)(void);

    /**
     * 反初始化RPMsg backend
     * @param rpdev: RPMsg设备指针
     */
    void (*deinit)(void);
};

/**
 * 获取底座操作接口
 * @param type: 底座类型
 * @return: 操作接口指针，NULL表示不支持
 */
const struct mica_pedestal_ops *mica_get_ped_ops(void);

#ifdef __cplusplus
}
#endif

#endif /* MICA_PEDESTAL_H */