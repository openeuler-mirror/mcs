/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "mcs/mica_debug_ring_buffer.h"
#include "mcs/mcs_common.h"

/* Deliver packets from server to RTOS */
int transfer_data_to_rtos(struct debug_ring_buffer_module_data *data)
{
	int ret;
	// create thread to send message to RTOS
	ret = pthread_create(&data->data_to_rtos_thread, NULL, data_to_rtos_thread, data);
	if (ret != 0) {
		perror("create thread failed");
		return ret;
	}
	ret = pthread_detach(data->data_to_rtos_thread);
	if (ret != 0) {
		perror("detach thread failed");
		pthread_cancel(data->data_to_rtos_thread);
	}
	return ret;
}

static void *data_to_rtos_thread(void *args)
{
	int ret;
	char recv_buf[MAX_BUFF_LENGTH];
	struct debug_ring_buffer_module_data *data = (struct debug_ring_buffer_module_data *)args;
	bool server_send_close = false;
	while (1) {
		if (server_send_close) {
			break;
		}
		// receive message from server
		int n_bytes = mq_receive(data->from_server, recv_buf, MAX_BUFF_LENGTH, NULL);
		if (n_bytes == -1) {
			perror("receive data from server failed");
			ret = -errno;
			break;
		}
		recv_buf[n_bytes] = '\0';
#ifdef MICA_DEBUG_LOG
		mica_debug_log_error("ring buffer", "from server %s\n", recv_buf);
#endif
		if (strcmp(recv_buf, EXIT_PACKET) == 0) {
			server_send_close = true;
		}
		// send message to RTOS
		while(writable(data->tx_buffer) == 0) {}
		ret = ring_buffer_write(data->tx_buffer, recv_buf, n_bytes);
		if (ret < 0) {
			perror("ring_buffer_write error");
			ret = -1;
			break;
		}
	}
	return INT_TO_PTR(ret);
}

/* Deliver packets from RTOS to server */
int transfer_data_to_server(struct debug_ring_buffer_module_data *data)
{
	int ret;
	// create thread to send message to server
	ret = pthread_create(&data->data_to_server_thread, NULL, data_to_server_thread, data);
	if (ret != 0) {
		perror("create thread failed");
		return ret;
	}
	ret = pthread_detach(data->data_to_server_thread);
	if (ret != 0) {
		perror("detach thread failed");
		pthread_cancel(data->data_to_server_thread);
	}
	return ret;
}

static void *data_to_server_thread(void *args)
{
	int ret;
	char recv_buf[MAX_BUFF_LENGTH];
	struct debug_ring_buffer_module_data *data = (struct debug_ring_buffer_module_data *)args;
	while (1) {
		// receive message from RTOS
		while(readable(data->rx_buffer) == 0) {
			pthread_testcancel();
		}
		int n_bytes = ring_buffer_read(data->rx_buffer, recv_buf, MAX_BUFF_LENGTH);
		if (n_bytes == -1) {
			perror("receive data from RTOS failed");
			ret = -errno;
			break;
		}
		recv_buf[n_bytes] = '\0';
#ifdef MICA_DEBUG_LOG
		mica_debug_log_error("ring buffer", "from rtos %s\n", recv_buf);
#endif
		// send message to server
		ret = mq_send(data->to_server, recv_buf, n_bytes, MSG_PRIO);
		if (ret == -1) {
			perror("send data to server failed");
			ret = -errno;
			break;
		}
	}
	return INT_TO_PTR(ret);
}

int start_ring_buffer_module(struct client_os_inst *client, mqd_t from_server, mqd_t to_server, struct debug_ring_buffer_module_data **data_out)
{
	int ret;
	struct debug_ring_buffer_module_data *data = (struct debug_ring_buffer_module_data *)calloc(sizeof(struct debug_ring_buffer_module_data), 1);
	*data_out = data;
	data->len = RING_BUFFER_LEN;
	data->from_server = from_server;
	data->to_server = to_server;
	// the ring buffer area should be mmaped first
	void *ring_buffer_va = mmap(NULL, RING_BUFFER_LEN * 2, PROT_READ | PROT_WRITE, MAP_SHARED, client->mcs_fd, RING_BUFFER_PA);
	if (ring_buffer_va == MAP_FAILED) {
		perror("mmap ring buffer failed");
		ret = -errno;
		goto err_exit;
	}
	data->rx_buffer = ring_buffer_va;
	data->tx_buffer = ring_buffer_va + data->len;
	if (ring_buffer_pair_init(data->rx_buffer, data->tx_buffer, data->len)) {
		printf("ring_buffer_pair_init failed\n");
		ret = -1;
		goto err_unmap_ring_buffer;
	}
	/* create thread to send message to server */
	ret = transfer_data_to_rtos(data);
	ret = transfer_data_to_server(data);

err_exit:
	return ret;

err_unmap_ring_buffer:
	munmap(ring_buffer_va, data->len * 2);
	return ret;
}

void free_resources_for_ring_buffer_module(struct debug_ring_buffer_module_data *data)
{
	if (data == NULL) {
		printf("ring buffer module: no resource to free\n");
		return;
	}
	printf("freeing resources for ring buffer module ...\n");
	if (data->data_to_rtos_thread > 0)
		pthread_cancel(data->data_to_rtos_thread);
	if (data->data_to_server_thread > 0)
		pthread_cancel(data->data_to_server_thread);
	printf("cancelled threads\n");
	memset(data->rx_buffer, 0, data->len);
	memset(data->tx_buffer, 0, data->len);
	void *ring_buffer_va = data->rx_buffer < data->tx_buffer ?
	data->rx_buffer : data->tx_buffer;
	munmap(ring_buffer_va, data->len * 2);
	printf("cleared ring buffer\n");
	free(data);
}
