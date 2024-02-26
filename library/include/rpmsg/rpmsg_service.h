/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RPMSG_MODULE_H
#define RPMSG_MODULE_H

#include <openamp/rpmsg.h>

#if defined __cplusplus
extern "C" {
#endif

/* name service callback */
void mica_ns_bind_cb(struct rpmsg_device *rdev, const char *name, uint32_t dest);

/* register a remote endpoint */
void register_remote_ept(const char *name, uint32_t addr, uint32_t dest_addr);

#if defined __cplusplus
}
#endif

#endif /* RPMSG_MODULE_H */
