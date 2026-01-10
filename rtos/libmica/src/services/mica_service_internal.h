/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_SERVICE_INTERNAL_H
#define MICA_SERVICE_INTERNAL_H

#include <openamp/rpmsg.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ========== 模块间共享接口 ========== */

/**
 * 设置rpmsg device（pedestal初始化完成后调用）
 */
void mica_set_rpdev(struct rpmsg_device *rpdev);

/**
 * 获取rpmsg device（由pedestal始化，供service使用）
 */
struct rpmsg_device *mica_get_rpdev(void);

/**
 * 获取MICA配置（由mica_init保存，供service使用）
 */
struct mica_config *mica_get_config(void);

/* ========== Service初始化函数 ========== */
int mica_rpc_init_service(void);
int mica_tty_init_service(pthread_attr_t *attr);
int mica_umt_init_service(pthread_attr_t *attr);

void *tty_service_thread(void *arg);
void *umt_service_thread(void *arg);

int mica_rpc_is_ready(void);
int mica_tty_is_ready(void);
int mica_umt_is_ready(void);

#ifdef __cplusplus
}
#endif

#endif /* MICA_SERVICE_INTERNAL_H */