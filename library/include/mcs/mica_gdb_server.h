/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_GDB_SERVER_H
#define MICA_GDB_SERVER_H

#include <stdio.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <netinet/in.h>
#include <string.h>
#include <stdlib.h>
#include <unistd.h>
#include <arpa/inet.h>
#include <sys/msg.h>
#include <mqueue.h>
#include <errno.h>
#include <pthread.h>
#include <stdbool.h>

#include "mcs_common.h"
#include "mica_debug_common.h"

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

int start_proxy_server(mqd_t from_server, mqd_t to_server, struct proxy_server_resources **resources_out);
int send_to_shared_mem(mqd_t from_server, char *buffer, int n_bytes);
static void *recv_from_shared_mem_thread(void *args);
int send_to_gdb(int client_socket, char *buffer, int n_bytes);
int recv_from_gdb(int client_socket, char *buffer);
void free_resources_for_proxy_server(struct proxy_server_resources *resources);

#endif