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

/* ========== Inter-module shared API ========== */

/**
 * Set rpmsg device (called after pedestal init).
 */
void mica_set_rpdev(struct rpmsg_device *rpdev);

/**
 * Get rpmsg device (set by pedestal, used by services).
 */
struct rpmsg_device *mica_get_rpdev(void);

/**
 * Get MICA config (saved by mica_init, used by services).
 */
struct mica_config *mica_get_config(void);

/* ========== Service init entry points ========== */
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