/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "openamp_module.h"
#include "rpmsg_rpc_service.h"

static const struct rpmsg_rpc_service *find_rpc_service(struct rpmsg_rpc_instance *inst,
					unsigned int id)
{
	const struct rpmsg_rpc_service *service;

	for (unsigned int i = 0; i < inst->n_services; i++) {
		service = &inst->services[i];

		if (service->id == id) {
			return service;
		}
	}
	return NULL;
}

static int endpoint_cb_rpmsg_rpc(struct rpmsg_endpoint *ept, void *data,
					size_t len, uint32_t src, void * priv)
{
	uint32_t id;

	struct rpmsg_rpc_instance *inst;
	const struct rpmsg_rpc_service *service;

	if (len < RPC_ID_LEN) {
		return 0;
	}

	inst = (struct rpmsg_rpc_instance *)priv;

	/* skip rpc id */
	id = *(uint32_t *)data;
	data = (char *)data + RPC_ID_LEN;
	len -= RPC_ID_LEN;

	service = find_rpc_service(inst, id);

	if (service) {
		if (service->cb_function(data, len) < 0) {
			printf("call back %d error\n", id);
		}
	} else {
		printf("no service found\n");
	}
}

int rpmsg_rpc_service_init(struct rpmsg_rpc_instance *inst, const struct rpmsg_rpc_service *services,
							unsigned int n_services)
{
	int ret;

	/* parameter check */
	if (inst == NULL || services == NULL || n_services == 0) {
		return -1;
	}

	inst->services = services;
	inst->n_services = n_services;

	ret = rpmsg_service_register_endpoint(RPMSG_RPC_SERVICE_NAME, endpoint_cb_rpmsg_rpc, NULL, inst);

	if (ret >= 0) {
		inst->ep_id = ret;
		return 0;
	} else {
		return ret;
	}
}

int rpmsg_rpc_send(struct rpmsg_rpc_instance *inst, uint32_t rpc_id, void *params, size_t len)
{
	int ret;
	struct rpmsg_rpc_data data;

	if (inst == NULL || params == NULL || len == 0) {
		return -1;
	}

	data.id = rpc_id;
	memcpy(data.params, params, len);

	ret = rpmsg_service_send(inst->ep_id, &data, RPC_ID_LEN + len);

	if (ret > 0) {
		ret -= RPC_ID_LEN;
	}

	return ret;
}
