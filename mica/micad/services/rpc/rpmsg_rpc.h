/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RPMSG_RPC_H
#define RPMSG_RPC_H

#include <mica/mica.h>

int create_rpmsg_rpc_service(struct mica_client *client);

void rpmsg_rpc_service_terminate(void);

int rpmsg_rpc_server_cb(struct rpmsg_endpoint *ept, void *data,
	size_t len, uint32_t src, void *priv);

int rpmsg_rpc_service_init(void);

#endif  /* RPMSG_RPC_H */
