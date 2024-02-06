/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <syslog.h>

#include "mica/mica.h"
#include "remoteproc/remoteproc_module.h"
#include "rpmsg/rpmsg_vdev.h"

int mica_create(struct mica_client *client)
{
	int ret;

	ret = create_client(client);
	if (ret) {
		syslog(LOG_ERR, "create remoteproc failed, err: %d\n", ret);
		return ret;
	}

	ret = load_client_image(client);
	if (ret) {
		syslog(LOG_ERR, "load client image failed, err: %d\n", ret);
		return ret;
	}

	ret = create_rpmsg_device(client);
	if (ret)
		syslog(LOG_ERR, "create rpmsg device failed, err: %d\n", ret);

	return ret;
}

int mica_start(struct mica_client *client)
{
	int ret;

	ret = start_client(client);
	if (ret)
		syslog(LOG_ERR, "start client OS failed, err: %d\n", ret);
		/* TODO: free rpmsg device */

	return ret;
}

const char *mica_status(struct mica_client *client)
{
	return show_client_status(client);
}

void mica_print_service(struct mica_client *client, char *str, size_t size)
{
	print_device_of_service(client, str, size);
}
