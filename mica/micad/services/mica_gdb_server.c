/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <netinet/in.h>
#include <string.h>
#include <stdlib.h>
#include <unistd.h>
#include <arpa/inet.h>
#include <sys/msg.h>
#include <errno.h>
#include <stdbool.h>
#include <openamp/rsc_table_parser.h>

#include <mica/mica.h>
#include <remoteproc/mica_rsc.h>

#include "mica_debug_common.h"
#include "mica_gdb_server.h"

static int send_to_gdb(int client_socket_fd, char *buffer, int n_bytes)
{
	int ret = 0;

	if (send(client_socket_fd, buffer, n_bytes, 0) == -1) {
		syslog(LOG_ERR, "send data to client failed");
		ret = -errno;
	}

	return ret;
}

static void *recv_from_shared_mem_thread(void *args)
{
	int ret;
	int n_bytes;
	char recv_buf[MAX_BUFF_LENGTH];
	struct proxy_server_recv_args *recv_args = (struct proxy_server_recv_args *)args;
	mqd_t to_server = recv_args->to_server;
	int client_socket_fd = recv_args->client_socket_fd;

	while (1) {
		// receive data from shared memory module through message queue
		n_bytes = mq_receive(to_server, recv_buf, MAX_BUFF_LENGTH, NULL);
		if (n_bytes == -1) {
			syslog(LOG_ERR, "receive data from shared memory module failed");
			ret = -errno;
			break;
		}
		recv_buf[n_bytes] = '\0';

#ifdef MICA_DEBUG_LOG
		mica_debug_log_error("proxy server", "from transfer module %s\n", recv_buf);
#endif

		ret = send_to_gdb(client_socket_fd, recv_buf, n_bytes);
		if (ret < 0) {
			break;
		}
	}
	return INT_TO_PTR(ret);
}

static int send_to_shared_mem(mqd_t from_server, char *buffer, int n_bytes)
{
	int ret = 0;

	if (mq_send(from_server, buffer, n_bytes, MSG_PRIO) == -1) {
		ret = -errno;
	}
	return ret;
}

static int recv_from_gdb(int client_socket_fd, char *buffer)
{
	int n_bytes;

	n_bytes = recv(client_socket_fd, buffer, MAX_BUFF_LENGTH, 0);
	if (n_bytes == -1) {
		syslog(LOG_ERR, "receive data from gdb failed");
		return -errno;
	}
	buffer[n_bytes] = '\0';

	return n_bytes;
}

void free_resources_for_proxy_server(struct proxy_server_resources *resources)
{
	if (resources == NULL) {
		syslog(LOG_ERR, "MICA gdb proxy server: no resources to free\n");
		return;
	}
	syslog(LOG_INFO, "MICA gdb proxy server: cleaning up...\n");

	/* release the resources */
	if (resources->recv_from_shared_mem_thread != 0) {
		pthread_cancel(resources->recv_from_shared_mem_thread);
	}
	syslog(LOG_INFO, "cancelled thread\n");

	if (resources->client_socket_fd != 0) {
		close(resources->client_socket_fd);
	}
	if (resources->server_socket_fd != 0) {
		close(resources->server_socket_fd);
	}
	syslog(LOG_INFO, "closed sockets\n");
	free(resources);
}

int start_proxy_server(struct mica_client *client, mqd_t from_server, mqd_t to_server, struct proxy_server_resources **resources_out)
{
	struct proxy_server_resources *resources = (struct proxy_server_resources *)calloc(sizeof(struct proxy_server_resources), 1);
	*resources_out = resources;
	struct sockaddr_in server_addr;
	struct sockaddr_in client_addr;
	socklen_t sin_size;
	char recv_buf[MAX_BUFF_LENGTH];
	int n_bytes, opt = 1, ret;

	syslog(LOG_INFO, "MICA gdb proxy server: starting...\n");

	// create socket file descriptor
	resources->server_socket_fd = socket(AF_INET, SOCK_STREAM, 0);
	if (resources->server_socket_fd == -1) {
		syslog(LOG_ERR, "socket creation failed");
		return -1;
	}

	// set server address
	server_addr.sin_family = AF_INET;
	server_addr.sin_port = htons(GDB_PROXY_PORT);
	server_addr.sin_addr.s_addr = INADDR_ANY;
	memset(server_addr.sin_zero, 0, sizeof(server_addr.sin_zero));

	// set socket options
	if (setsockopt(resources->server_socket_fd, SOL_SOCKET,
				   SO_REUSEADDR, &opt,
				   sizeof(opt))) {
		syslog(LOG_ERR, "setsockopt");
		ret = -1;
		goto err_close_server_sock;
	}

	// bind socket to server address
	ret = bind(resources->server_socket_fd, (struct sockaddr *)&server_addr, sizeof(struct sockaddr));
	if (ret == -1) {
		ret = -errno;
		syslog(LOG_ERR, "bind socket to address failed");
		goto err_close_server_sock;
	}

	// listen on socket, for now we only accept one connection
	ret = listen(resources->server_socket_fd, MAX_PARALLEL_CONNECTIONS);
	if (ret == -1) {
		ret = -errno;
		syslog(LOG_ERR, "listen on socket failed");
		goto err_close_server_sock;
	}

	// accept connection
	sin_size = sizeof(server_addr);
	resources->client_socket_fd = accept(resources->server_socket_fd, (struct sockaddr *)&client_addr, &sin_size);
	if (resources->client_socket_fd == -1) {
		ret = -errno;
		syslog(LOG_ERR, "accept connection failed");
		goto err_close_server_sock;
	}

	syslog(LOG_INFO, "server: got connection from %s\n", inet_ntoa(client_addr.sin_addr));

	// create a new thread for receiving data from shared memory Transfer Module
	struct proxy_server_recv_args args = {to_server, resources->client_socket_fd};

	ret = pthread_create(&resources->recv_from_shared_mem_thread, NULL, recv_from_shared_mem_thread, &args);
	if (ret != 0) {
		ret = -ret;
		syslog(LOG_ERR, "create thread failed");
		goto err_close_client_sock;
	}
	ret = pthread_detach(resources->recv_from_shared_mem_thread);
	if (ret != 0) {
		ret = -ret;
		syslog(LOG_ERR, "detach thread failed");
		goto err_cancel_recv_thread;
	}

	syslog(LOG_INFO, "MICA gdb proxy server: read for messages forwarding ...\n");

	bool restart = false;

	while (1) {
		// receive data from client
		n_bytes = recv_from_gdb(resources->client_socket_fd, recv_buf);
		if (n_bytes < 0) {
			ret = n_bytes;
			goto err_cancel_recv_thread;
		}
		if (n_bytes == 0) {
			syslog(LOG_INFO, "client closed connection\n");
			break;
		}

		if (strcmp(recv_buf, CTRLC_PACKET) == 0) {
			// received CTRLC
			syslog(LOG_INFO, "received CTRLC\n");
			// get resource table and write to the state
			struct remoteproc *rproc;
			void *rsc_table;
			struct fw_rsc_rbuf_pair *rbuf_rsc;

			rproc = &client->rproc;
			rsc_table = rproc->rsc_table;
			DEBUG_PRINT("rsctable: %p\n", rsc_table);

			size_t rbuf_rsc_offset = find_rsc(rsc_table, RSC_VENDOR_RBUF_PAIR, 0);
			if (!rbuf_rsc_offset) {
				ret = -ENODEV;
				goto err_cancel_recv_thread;
			}
			DEBUG_PRINT("found rbuf resource at offset: 0x%lx\n", rbuf_rsc_offset);

			rbuf_rsc = (struct fw_rsc_rbuf_pair *)(rsc_table + rbuf_rsc_offset);
			DEBUG_PRINT("rbuf resource length: %lx\n", rbuf_rsc->len);

			// remote receives the IPI, then check the state to see the exact info
			rbuf_rsc->state = RBUF_STATE_CTRL_C;
			rproc->ops->notify(rproc, 0);
		}

		// transfer data to shared memory transfer module through message queue
		ret = send_to_shared_mem(from_server, recv_buf, n_bytes);
		if (ret < 0) {
			goto err_cancel_recv_thread;
		}
	}

	return 0;

err_cancel_recv_thread:
	pthread_cancel(resources->recv_from_shared_mem_thread);
err_close_client_sock:
	close(resources->client_socket_fd);
err_close_server_sock:
	close(resources->server_socket_fd);
	free(resources);
	return ret;
}
