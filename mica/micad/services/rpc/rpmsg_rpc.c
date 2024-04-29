/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <poll.h>

#include "rpmsg_rpc.h"

#define RPMSG_RPC_EPT_NAME "rpmsg-rpc"

static void rpmsg_tty_unbind(struct rpmsg_endpoint *ept)
{
	rpmsg_destroy_ept(ept);
}

/**
 * Init function for rpmsg_rpc_service.
 */
static void rpc_service_init(struct rpmsg_device *rdev, const char *name,
	uint32_t remote_addr, uint32_t remote_dest, void *priv)
{
	int ret;
	struct rpmsg_endpoint *rpc_service_ept = NULL;
	char message[] = "first message from rpc_service!";

	rpc_service_ept = malloc(sizeof(struct rpmsg_endpoint));
	if (!rpc_service_ept)
		return;

	/**
	 * Create the corresponding rpmsg endpoint
	 *
	 * endpoint callback: rpmsg_rx_callback
	 * endpoint unbind function: rpmsg_ept_unbind
	 */
	ret = rpmsg_create_ept(rpc_service_ept, rdev, name, remote_dest, remote_addr,
			rpmsg_rpc_server_cb, rpmsg_tty_unbind);
	if (ret)
		goto free_mem;

	/* create tx thread  */
	rpmsg_send(rpc_service_ept, message, sizeof(message));
	return;

free_mem:
	free(rpc_service_ept);
	return;
}

/**
 * Just support "rpmsg-rpc"
 */
static bool rpc_name_match(struct rpmsg_device *rdev, const char *name,
	uint32_t remote_addr, uint32_t remote_dest, void *priv)
{
	return !strcmp(name, RPMSG_RPC_EPT_NAME);
}

static void remove_rpc_service(struct mica_service *svc)
{
	rpmsg_rpc_service_terminate();
}

static struct mica_service rpmsg_rpc_service = {
	.name = RPMSG_RPC_EPT_NAME,
	.rpmsg_ns_match = rpc_name_match,
	.rpmsg_ns_bind_cb = rpc_service_init,
	.remove = remove_rpc_service,
};

int create_rpmsg_rpc_service(struct mica_client *client)
{
	rpmsg_rpc_service_init();
	return mica_register_service(client, &rpmsg_rpc_service);
}
