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

/* register a rpmsg/user-defined service */
int mica_register_service(struct mica_client *client, struct mica_service *svc);

/* name service callback */
void mica_ns_bind_cb(struct rpmsg_device *rdev, const char *name, uint32_t dest);

void *rpmsg_service_receive_loop(void *arg);

#if defined __cplusplus
}
#endif

#endif /* RPMSG_MODULE_H */
