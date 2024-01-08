/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "mcs/mica_gdb_server.h"
#include "mcs/mcs_common.h"

void free_resources_for_proxy_server(struct proxy_server_resources *resources)
{
	if (resources == NULL) {
		printf("MICA gdb proxy server: no resources to free\n");
		return;
	}
	printf("MICA gdb proxy server: cleaning up...\n");
	/* release the resources */
	if (resources->recv_from_shared_mem_thread != 0) {
		pthread_cancel(resources->recv_from_shared_mem_thread);
	}
	printf("cancelled thread\n");
	if (resources->client_socket_fd != 0) {
		close(resources->client_socket_fd);
	}
	if (resources->server_socket_fd != 0) {
		close(resources->server_socket_fd);
	}
	printf("closed sockets\n");
	free(resources);
}

int start_proxy_server(mqd_t from_server, mqd_t to_server, struct proxy_server_resources **resources_out)
{
	struct proxy_server_resources *resources = (struct proxy_server_resources *)calloc(sizeof(struct proxy_server_resources), 1);
	*resources_out = resources;
	struct sockaddr_in server_addr;
	struct sockaddr_in client_addr;
	int sin_size;
	char recv_buf[MAX_BUFF_LENGTH], send_buf[MAX_BUFF_LENGTH];
	int n_bytes, opt = 1;

	printf("MICA gdb proxy server: starting...\n");

	// create socket file descriptor
	if ((resources->server_socket_fd = socket(AF_INET, SOCK_STREAM, 0)) == -1) {
		perror("socket creation failed");
		exit(EXIT_FAILURE);
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
		perror("setsockopt");
		goto err_close_server_sock;
	}

	// bind socket to server address
	if (bind(resources->server_socket_fd, (struct sockaddr *)&server_addr, sizeof(struct sockaddr)) == -1) {
		perror("bind socket to address failed");
		goto err_close_server_sock;
	}

	// listen on socket, for now we only accept one connection
	if (listen(resources->server_socket_fd, MAX_PARALLEL_CONNECTIONS) == -1){
		perror("listen on socket failed");
		goto err_close_server_sock;
	}

	// accept connection
	sin_size = sizeof(server_addr);
	if ((resources->client_socket_fd = accept(resources->server_socket_fd, (struct sockaddr *)&client_addr, &sin_size)) == -1) {
		perror("accept connection failed");
		goto err_close_server_sock;
	}

	printf("server: got connection from %s\n", inet_ntoa(client_addr.sin_addr));

	// create a new thread for receiving data from shared memory Transfer Module
	struct proxy_server_recv_args args = {to_server, resources->client_socket_fd};
	if (pthread_create(&resources->recv_from_shared_mem_thread, NULL, recv_from_shared_mem_thread, &args) != 0) {
		perror("create thread failed");
		goto err_close_client_sock;
	}
	if(pthread_detach(resources->recv_from_shared_mem_thread) != 0) {
		perror("detach thread failed");
		goto err_cancel_recv_thread;
	}

	printf("MICA gdb proxy server: read for messages forwarding ...\n");

	bool gdb_send_close = false;
	while (1) {
		if (gdb_send_close == true) {
			break;
		}
		// receive data from client
		if ((n_bytes = recv_from_gdb(resources->client_socket_fd, recv_buf)) < 0) {
			goto err_cancel_recv_thread;
		}
		if (n_bytes == 0) {
			printf("client closed connection\n");
			break;
		}
		if (strcmp(recv_buf, EXIT_PACKET) == 0) {
			gdb_send_close = true;
		}

		// transfer data to shared memory transfer module through message queue
		if (send_to_shared_mem(from_server, recv_buf, n_bytes) < 0) {
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
	exit(EXIT_FAILURE);
}

int send_to_shared_mem(mqd_t from_server, char *buffer, int n_bytes)
{
	int ret = 0;
	if(mq_send(from_server, buffer, n_bytes, MSG_PRIO) == -1) {
#ifdef MICA_DEBUG_LOG
		mica_debug_log_error("proxy server", "send data to shared memory transfer module failed");
#endif
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
			perror("receive data from shared memory module failed");
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

int send_to_gdb(int client_socket_fd, char *buffer, int n_bytes)
{
	int ret = 0;
	if (send(client_socket_fd, buffer, n_bytes, 0) == -1) {
		perror("send data to client failed");
		ret = -errno;
	}
	return ret;
}

int recv_from_gdb(int client_socket_fd, char *buffer)
{
	int n_bytes;
	if ((n_bytes = recv(client_socket_fd, buffer, MAX_BUFF_LENGTH, 0)) == -1) {
		perror("receive data from gdb failed");
		return -errno;
	}
	buffer[n_bytes] = '\0';
#ifdef MICA_DEBUG_LOG
	mica_debug_log_error("proxy server", "from gdb %s\n", buffer);
#endif
	return n_bytes;
}
