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
#include "rbuf_device/rbuf_dev.h"

int mica_create(struct mica_client *client)
{
	int ret;

	ret = create_client(client);
	if (ret)
		syslog(LOG_ERR, "create remoteproc failed, err: %d\n", ret);

	return ret;
}

int mica_start(struct mica_client *client)
{
	int ret;

	ret = load_client_image(client);
	if (ret) {
		syslog(LOG_ERR, "load client image failed, err: %d\n", ret);
		return ret;
	}

	ret = start_client(client);
	if (ret) {
		syslog(LOG_ERR, "start client OS failed, err: %d\n", ret);
		return ret;
	}

	ret = create_rpmsg_device(client);
	if (ret)
		syslog(LOG_ERR, "create rpmsg device failed, err: %d\n", ret);

	if (client->debug) {
		ret = create_rbuf_device(client);
		if (ret)
			syslog(LOG_ERR, "create rbuf device failed, err: %d\n", ret);
	}

	return ret;
}

void mica_stop(struct mica_client *client)
{
	/*
	 * step1: remove all the registered services
	 * step2: remove rpmsg device
	 * step3: shutdown remoteproc
	 */
	struct remoteproc *rproc = &client->rproc;

	remoteproc_stop(rproc);
	mica_unregister_all_services(client);
	release_rpmsg_device(client);
	if (client->debug)
		destroy_rbuf_device(client);
	stop_client(client);
}

void mica_remove(struct mica_client *client)
{
	struct remoteproc *rproc = &client->rproc;

	if (rproc->state != RPROC_OFFLINE)
		mica_stop(client);

	if (client->gdb_server_thread) {
		pthread_cancel(client->gdb_server_thread);
		pthread_join(client->gdb_server_thread, NULL);
	}
		

	destory_client(client);
}

const char *mica_status(struct mica_client *client)
{
	return show_client_status(client);
}

void mica_print_service(struct mica_client *client, char *str, size_t size)
{
	print_device_of_service(client, str, size);
}
