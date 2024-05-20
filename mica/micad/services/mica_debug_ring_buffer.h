/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_DEBUG_RING_BUFFER_H
#define MICA_DEBUG_RING_BUFFER_H

#include <mqueue.h>
#include <mica/mica.h>

struct debug_ring_buffer_module_data {
	void *rx_buffer;
	void *tx_buffer;
	int len;
	mqd_t to_server;
	mqd_t from_server;
	pthread_t data_to_rtos_thread;
	pthread_t data_to_server_thread;
};
/*
 * brief: start the ring buffer module
 * param[in] client: the mica client to start ring buffer module
 * param[in] from_server: the message queue descriptor to receive message from server
 * param[in] to_server: the message queue descriptor to send message to server
 * param[out] ring_buffer_module_data_out: the data structure to store the ring buffer module data
 * return: 0 if success, <0 if failed
 */
int start_ring_buffer_module(struct mica_client *client, mqd_t from_server, mqd_t to_server, struct debug_ring_buffer_module_data **ring_buffer_module_data_out);

/*
 * brief: stop the ring buffer module
 * param[in] ring_buffer_module_data: the data structure to store the ring buffer module data
 */
void free_resources_for_ring_buffer_module(struct debug_ring_buffer_module_data *ring_buffer_module_data);

#endif
