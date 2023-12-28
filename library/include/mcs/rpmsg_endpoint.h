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

/* register a rpmsg service endpoint */
int rpmsg_service_register_endpoint(const char *name, rpmsg_ept_cb cb,
									rpmsg_ns_unbind_cb unbind_cb, void *priv);

int rpmsg_service_unregister_endpoint(unsigned int endpoint_id);

/* send data using given rpmsg service endpoint */
int rpmsg_service_send(unsigned int endpoint_id, const void *data, size_t len);

/* check a rpmsg service endpoint is bound */
bool rpmsg_service_endpoint_is_bound(unsigned int endpoint_id);

/* bound a rpmsg service endpoint */
void rpmsg_service_endpoint_bound(unsigned int endpoint_id);

/* return endpoint's name */
const char * rpmsg_service_endpoint_name(unsigned int endpoint_id);

/* name service callback: create matching endpoint */
void ns_bind_cb(struct rpmsg_device *rdev, const char *name, uint32_t dest);

void *rpmsg_service_receive_loop(void *arg);

#if defined __cplusplus
}
#endif

#endif /* RPMSG_MODULE_H */
