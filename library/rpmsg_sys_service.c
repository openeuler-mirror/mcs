/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>

#include "openamp_module.h"

/* this call back is called when the response of
 * sys_service_power_off arrives (in fact maybe never arrives)
 */
static int sys_service_power_off_cb(void *params, size_t len)
{
	(void)params;

	printf("get response of power_off service:%ld\n", len);

	return 0;
}

static struct rpmsg_rpc_instance sys_service_inst;
static struct rpmsg_rpc_service sys_service_table[] = {
	{RPMSG_SYS_SERVICE_POWER_OFF, sys_service_power_off_cb}
};

int rpmsg_sys_service_init(void)
{
	unsigned int n_services = sizeof(sys_service_table)/ sizeof(struct rpmsg_rpc_service);

	printf("number of services: %d\n", n_services);

	rpmsg_rpc_service_init(&sys_service_inst, sys_service_table, n_services);

	return 0;
}

int sys_service_power_off(int client)
{
	int ret;
	char *cmd_str = "power off";

	printf("power off the client os: %d\n", client);

	ret = rpmsg_rpc_send(&sys_service_inst, RPMSG_SYS_SERVICE_POWER_OFF, cmd_str, strlen(cmd_str));
	if (ret < 0) {
		printf("sys_service_power_off :rpmsg_rpc_send failed:%d", ret);
	}

	return ret;
}
