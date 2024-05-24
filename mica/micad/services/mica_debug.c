/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <stdarg.h>
#include <pthread.h>
#include <mqueue.h>
#include <errno.h>
#include <string.h>
#include <stdbool.h>
#include <stdlib.h>
#include <sys/wait.h>
#include <fcntl.h>
#include <sys/ioctl.h>
#include <unistd.h>

#include <memory/shm_pool.h>

#include "mica_gdb_server.h"
#include "mica_debug.h"
#include "mica_debug_common.h"

/* create message queue */
mqd_t g_from_server, g_to_server;

/* resources of gdb proxy server */
struct proxy_server_resources *g_proxy_server_resources;
/* resources of ring buffer module */
struct debug_ring_buffer_module_data *g_ring_buffer_module_data;

static void *server_loop_thread(void *args)
{
	struct mica_client *client = args;
	int ret = start_proxy_server(client, g_from_server, g_to_server, &g_proxy_server_resources);

	return INT_TO_PTR(ret);
}

static int alloc_message_queue(void)
{
	/* attributes of message queues */
	struct mq_attr attr;

	attr.mq_maxmsg = MAX_QUEUE_SIZE;
	attr.mq_msgsize = MAX_BUFF_LENGTH;

	g_from_server = mq_open(TO_SHARED_MEM_QUEUE_NAME, O_RDWR | O_CREAT, 0600, &attr);
	if (g_from_server == -1) {
		syslog(LOG_ERR, "open to shared memory message queue failed\n");
		return -errno;
	}

	g_to_server = mq_open(FROM_SHARED_MEM_QUEUE_NAME, O_RDWR | O_CREAT, 0600, &attr);
	if (g_to_server == -1) {
		syslog(LOG_ERR, "open from shared memory message queue failed\n");
		return -errno;
	}

	return 0;
}

static void free_message_queue(void)
{
	if (g_from_server != 0)
		mq_close(g_from_server);

	if (g_to_server != 0)
		mq_close(g_to_server);

	syslog(LOG_INFO, "closed message queue\n");
}

static int debug_start(struct mica_client *client_os, struct mica_service *svc)
{
	int ret;

	ret = alloc_message_queue();
	if (ret < 0) {
		syslog(LOG_ERR, "alloc message queue failed\n");
		return ret;
	}
	syslog(LOG_INFO, "alloc message queue success\n");

	ret = start_ring_buffer_module(client_os, g_from_server, g_to_server, &g_ring_buffer_module_data);
	if (ret < 0) {
		syslog(LOG_ERR, "start ring buffer module failed\n");
		goto err_free_message_queue;
	}

	syslog(LOG_INFO, "start ring buffer module success\n");

	pthread_t server_loop;

	ret = pthread_create(&server_loop, NULL, server_loop_thread, client_os);
	if (ret != 0) {
		ret = -ret;
		syslog(LOG_ERR, "%s: create server loop thread failed\n", __func__);
		goto err_free_ring_buffer_module;
	}
	syslog(LOG_INFO, "create server loop thread success\n");

	ret = pthread_detach(server_loop);
	if (ret != 0) {
		ret = -ret;
		syslog(LOG_ERR, "%s: detach server_loop_thread failed", __func__);
		goto err_cancel_server;
	}

	return 0;

err_cancel_server:
	free_resources_for_proxy_server(g_proxy_server_resources);
	pthread_cancel(server_loop);

err_free_ring_buffer_module:
	free_resources_for_ring_buffer_module(g_ring_buffer_module_data);

err_free_message_queue:
	free_message_queue();
	return ret;
}

static void debug_stop(struct mica_client *client_os, struct mica_service *svc)
{
	free_resources_for_proxy_server(g_proxy_server_resources);
	free_resources_for_ring_buffer_module(g_ring_buffer_module_data);
	free_message_queue();
}

static struct mica_service debug_service = {
	.name = "debug-rtos-kernel",
	.init = debug_start,
	.remove = debug_stop,
};

int create_debug_service(struct mica_client *client)
{
	return mica_register_service(client, &debug_service);
}
