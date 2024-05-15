/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_GDB_SERVER_H
#define MICA_GDB_SERVER_H

#include <mqueue.h>
#include <pthread.h>

#define GDB_PROXY_PORT 5678 // default port number
#define MAX_PARALLEL_CONNECTIONS 3

struct proxy_server_resources {
	int server_socket_fd;
	int client_socket_fd;
	pthread_t recv_from_shared_mem_thread;
};

struct proxy_server_recv_args {
	mqd_t to_server;
	int client_socket_fd;
};

/*
 * brief: start the proxy server waiting for connection from gdb client
 * param[in] from_server: the message queue descriptor to send message to transfer module
 * param[in] to_server: the message queue descriptor to receive message from transfer module
 * param[out] resources_out: the resources used by proxy server
 * return: 0 if success, <0 if failed to start the proxy server or failed to send message to transfer
 */
int start_proxy_server(mqd_t from_server, mqd_t to_server, struct proxy_server_resources **resources_out);

/*
 * brief: stop the proxy server and free resources
 * param[in] resources: the resources used by proxy server
 */
void free_resources_for_proxy_server(struct proxy_server_resources *resources);

#endif
