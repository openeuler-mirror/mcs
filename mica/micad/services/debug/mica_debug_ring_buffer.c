/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <unistd.h>
#include <fcntl.h>
#include <sys/mman.h>
#include <pthread.h>
#include <stdio.h>
#include <errno.h>
#include <string.h>
#include <stdlib.h>

#include <rbuf_device/rbuf_dev.h>
#include <rbuf_device/ring_buffer.h>

#include "mica_debug_common.h"
#include "mica_debug_ring_buffer.h"

static void *data_to_rtos_thread(void *args)
{
	int ret;
	char recv_buf[MAX_BUFF_LENGTH];
	struct debug_ring_buffer_module_data *data = (struct debug_ring_buffer_module_data *)args;

	while (1) {
		// receive message from server
		int n_bytes = mq_receive(data->from_server, recv_buf, MAX_BUFF_LENGTH, NULL);

		if (n_bytes == -1) {
			syslog(LOG_ERR, "receive data from server failed");
			ret = -errno;
			break;
		}

		recv_buf[n_bytes] = '\0';

		// send message to RTOS
		ret = ring_buffer_write(data->tx_buffer, recv_buf, n_bytes);
		if (ret < 0) {
			syslog(LOG_ERR, "ring_buffer_write error");
			ret = -1;
			break;
		}
	}
	return INT_TO_PTR(ret);
}

static void *data_to_server_thread(void *args)
{
	int ret;
	char recv_buf[MAX_BUFF_LENGTH];
	struct debug_ring_buffer_module_data *data = (struct debug_ring_buffer_module_data *)args;

	while (1) {
		// receive message from RTOS
		pthread_testcancel();

		int n_bytes = ring_buffer_read(data->rx_buffer, recv_buf, MAX_BUFF_LENGTH);

		if (n_bytes == -1) {
			syslog(LOG_ERR, "receive data from RTOS failed");
			ret = -errno;
			break;
		} else if (n_bytes == 0)
			continue;

		recv_buf[n_bytes] = '\0';

		// send message to server
		ret = mq_send(data->to_server, recv_buf, n_bytes, MSG_PRIO);
		if (ret == -1) {
			syslog(LOG_ERR, "send data to server failed");
			ret = -errno;
			break;
		}
	}
	return INT_TO_PTR(ret);
}

/* Deliver packets from server to RTOS */
static int transfer_data_to_rtos(struct debug_ring_buffer_module_data *data)
{
	int ret;
	// create thread to send message to RTOS
	ret = pthread_create(&data->data_to_rtos_thread, NULL, data_to_rtos_thread, data);
	if (ret != 0) {
		syslog(LOG_ERR, "create thread failed");
		return ret;
	}

	return ret;
}

/* Deliver packets from RTOS to server */
static int transfer_data_to_server(struct debug_ring_buffer_module_data *data)
{
	int ret;
	// create thread to send message to server
	ret = pthread_create(&data->data_to_server_thread, NULL, data_to_server_thread, data);
	if (ret != 0) {
		syslog(LOG_ERR, "create thread failed");
		return ret;
	}

	return ret;
}

int start_ring_buffer_module(struct mica_client *client, mqd_t from_server, mqd_t to_server, struct debug_ring_buffer_module_data **data_out)
{
	int ret;
	struct debug_ring_buffer_module_data *data = (struct debug_ring_buffer_module_data *)calloc(1, sizeof(struct debug_ring_buffer_module_data));
	struct rbuf_device *rbuf_dev = client->rbuf_dev;
	*data_out = data;
	data->len = rbuf_dev->rbuf_len;
	data->from_server = from_server;
	data->to_server = to_server;
	data->rx_buffer = rbuf_dev->rx_va;
	data->tx_buffer = rbuf_dev->tx_va;
	/* create thread to send message to server */
	ret = transfer_data_to_rtos(data);
	ret = transfer_data_to_server(data);

	return ret;
}

void free_resources_for_ring_buffer_module(struct debug_ring_buffer_module_data *data)
{
	if (data == NULL) {
		syslog(LOG_ERR, "%s: ring buffer module: no resource to free\n", __func__);
		return;
	}
	syslog(LOG_INFO, "freeing resources for ring buffer module ...\n");

	if (data->data_to_rtos_thread > 0) {
		pthread_cancel(data->data_to_rtos_thread);
		pthread_join(data->data_to_rtos_thread, NULL);
	}
		
	if (data->data_to_server_thread > 0) {
		pthread_cancel(data->data_to_server_thread);
		pthread_join(data->data_to_server_thread, NULL);
	}
		
	syslog(LOG_INFO, "cancelled threads\n");

	memset(data->rx_buffer, 0, data->len);
	memset(data->tx_buffer, 0, data->len);
	// since now we depend on "metal" library for accessing memory
	// when shutting down the remoteproc instance,
	// the whole shared memory will be released at that time
	// we do not need to release it here
	syslog(LOG_INFO, "cleared ring buffer\n");
	free(data);
}
