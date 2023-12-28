/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_DEBUG_RING_BUFFER_H
#define MICA_DEBUG_RING_BUFFER_H

#include <unistd.h>
#include <fcntl.h>
#include <sys/mman.h>
#include <pthread.h>
#include <stdio.h>
#include <mqueue.h>
#include <errno.h>
#include <string.h>
#include <stdlib.h>

#include "mcs_common.h"
#include "mica_debug_common.h"
#include "openamp_module.h"
#include "ring_buffer.h"

/* the shared memory space used for communication */
#define RING_BUFFER_PA 0x70040000
#define RING_BUFFER_LEN 0x1000

struct debug_ring_buffer_module_data{
	void *rx_buffer;
	void *tx_buffer;
	int len;
	mqd_t to_server;
	mqd_t from_server;
	pthread_t data_to_rtos_thread;
	pthread_t data_to_server_thread;
};

/* Deliver packets from server to RTOS */
int transfer_data_to_rtos(struct debug_ring_buffer_module_data *ring_buffer_module_data);
static void *data_to_rtos_thread(void *args);
/* Deliver packets from RTOS to server */
int transfer_data_to_server(struct debug_ring_buffer_module_data *ring_buffer_module_data);
static void *data_to_server_thread(void *args);

int start_ring_buffer_module(struct client_os_inst *client, mqd_t from_server, mqd_t to_server, struct debug_ring_buffer_module_data **ring_buffer_module_data_out);
void free_resources_for_ring_buffer_module(struct debug_ring_buffer_module_data *ring_buffer_module_data);

#endif
