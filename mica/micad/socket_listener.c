/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */
#define _GNU_SOURCE
#include <ftw.h>
#include <stdio.h>
#include <stdlib.h>
#include <stdatomic.h>
#include <unistd.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <syslog.h>
#include <sys/un.h>
#include <sys/epoll.h>
#include <string.h>
#include <errno.h>

#include <mica/mica.h>
#include <services/pty/rpmsg_pty.h>
#include <services/debug/mica_debug.h>
#include <services/rpc/rpmsg_rpc.h>

#define MAX_EVENTS		64
#define MAX_NAME_LEN		32
#define MAX_PATH_LEN		64
#define CTRL_MSG_SIZE		32
#define RESPONSE_MSG_SIZE	256
#define MICA_SOCKET_DIRECTORY	"/run/mica"
#define MICA_GDB_SERVER_PORT 5678

#define MICA_MSG_SUCCESS	"MICA-SUCCESS"
#define MICA_MSG_FAILED		"MICA-FAILED"

typedef int (*listener_cb)(int epoll_fd, void *data);

static METAL_DECLARE_LIST(listener_list);
static atomic_bool listening;
static pthread_cond_t cond = PTHREAD_COND_INITIALIZER;
static pthread_mutex_t mutex = PTHREAD_MUTEX_INITIALIZER;

struct listen_unit {
	char name[MAX_NAME_LEN];
	int socket_fd;
	char socket_path[MAX_PATH_LEN];
	listener_cb cb;
	struct mica_client *client;
	struct metal_list node;
};

struct create_msg {
	uint32_t cpu;
	char name[MAX_NAME_LEN];
	char path[MAX_FIRMWARE_PATH_LEN];
	char ped[MAX_NAME_LEN];
	char ped_cfg[MAX_FIRMWARE_PATH_LEN];
	bool debug;
};

static void send_log(int msg_fd, const char *format, ...)
{
	int len;
	char *buffer;
	va_list args, args_copy;

	va_start(args, format);
	va_copy(args_copy, args);
	len = vsnprintf(NULL, 0, format, args_copy);
	va_end(args_copy);

	buffer = (char *)malloc(len + 1);
	if (!buffer) {
		va_end(args);
		return;
	}

	vsnprintf(buffer, len + 1, format, args);
	send(msg_fd, buffer, strlen(buffer), MSG_NOSIGNAL);
	free(buffer);
	va_end(args);
}

static void free_listener_by_name(const char *name)
{
	struct metal_list *node;
	struct listen_unit *unit;

	metal_list_for_each(&listener_list, node) {
		unit = metal_container_of(node, struct listen_unit, node);

		if (!strncmp(name, unit->name, MAX_NAME_LEN)) {
			metal_list_del(node);
			free(unit->client);
			close(unit->socket_fd);
			unlink(unit->socket_path);
			free(unit);
			return;
		}
	}
}

static void free_all_listeners(void)
{
	struct metal_list *node, *tmpnode;
	struct listen_unit *unit;

	/*
	 * To avoid RTOS being affected by micad's exit.
	 * We do not stop or destroy mica_client here.
	 */
	metal_list_for_each(&listener_list, node) {
		unit = metal_container_of(node, struct listen_unit, node);
		tmpnode = node;
		node = tmpnode->prev;
		metal_list_del(tmpnode);
		free(unit->client);
		close(unit->socket_fd);
		unlink(unit->socket_path);
		free(unit);
	}
}

static int add_listener(const char *name, struct mica_client *client, listener_cb cb, int epoll_fd)
{
	int ret;
	struct sockaddr_un addr;
	struct listen_unit *unit;
	struct epoll_event ev = { 0 };

	unit = calloc(1, sizeof(*unit));
	if (!unit)
		return -ENOMEM;

	/* bind a mica client */
	if (client != NULL)
		unit->client = client;

	unit->cb = cb;
	strlcpy(unit->name, name, MAX_NAME_LEN);
		snprintf(unit->socket_path, MAX_PATH_LEN, "%s/%s.socket",
			 MICA_SOCKET_DIRECTORY, unit->name);

	unit->socket_fd = socket(AF_UNIX, SOCK_STREAM, 0);
	if (unit->socket_fd < 0) {
		syslog(LOG_ERR, "Failed to create socket: %s", strerror(errno));
		ret = -1;
		goto free_mem;
	}

	memset(&addr, 0, sizeof(addr));
	addr.sun_family = AF_UNIX;
	strlcpy(addr.sun_path, unit->socket_path, sizeof(addr.sun_path) - 1);

	ret = bind(unit->socket_fd, (struct sockaddr *)&addr, sizeof(addr));
	if (ret < 0) {
		syslog(LOG_ERR, "Failed to bind socket: %s", strerror(errno));
		goto free_socket;
	}

	/*
	 * From listen(2):
	 * If the backlog argument is greater than the value in
	 * /proc/sys/net/core/somaxconn, then it is silently capped to that value.
	 * Since Linux 5.4, the default in this file is 4096; in earlier kernels,
	 * the default value is 128.  Before Linux 2.4.25, this limit was a hard
	 * coded value, SOMAXCONN, with the value 128.
	 *
	 * Here we use 128 directly.
	 */
	ret = listen(unit->socket_fd, 128);
	if (ret < 0) {
		syslog(LOG_ERR, "Failed to listen socket: %s", strerror(errno));
		goto free_socket;
	}

	ev.events = EPOLLIN;
	ev.data.ptr = unit;
	ret = epoll_ctl(epoll_fd, EPOLL_CTL_ADD, unit->socket_fd, &ev);
	if (ret < 0) {
		syslog(LOG_ERR, "Failed to add epoll handler: %s", strerror(errno));
		goto free_socket;
	}

	metal_list_add_tail(&listener_list, &unit->node);
	return 0;

free_socket:
	close(unit->socket_fd);
	unlink(unit->socket_path);
free_mem:
	free(unit);
	return ret;
}

static int check_create_msg(struct create_msg msg, int msg_fd)
{
	int ret;
	struct listen_unit *unit;
	struct metal_list *node;

	ret = access(msg.path, F_OK);
	if (ret != 0) {
		syslog(LOG_ERR, "No such file: %s", msg.path);
		send_log(msg_fd, "No such file: %s", msg.path);
		return -EINVAL;
	}

	if (msg.cpu < 0 || msg.cpu >= sysconf(_SC_NPROCESSORS_CONF)) {
		syslog(LOG_ERR, "Invalid CPUID: %d, out of range(0-%ld)",
			msg.cpu, sysconf(_SC_NPROCESSORS_CONF) - 1);
		send_log(msg_fd, "Invalid CPUID: %d, out of range(0-%ld)",
			msg.cpu, sysconf(_SC_NPROCESSORS_CONF) - 1);
		return -EINVAL;
	}

	metal_list_for_each(&listener_list, node) {
		unit = metal_container_of(node, struct listen_unit, node);

		if (!strncmp(msg.name, unit->name, MAX_NAME_LEN)) {
			syslog(LOG_ERR, "%s is already created", msg.name);
			send_log(msg_fd, "%s is already created", msg.name);
			return -EINVAL;
		}
	}

	return 0;
}

static void show_status(int msg_fd, struct listen_unit *unit)
{
	const char *status;
	char response[RESPONSE_MSG_SIZE * 2] = { 0 };
	char buffer[RESPONSE_MSG_SIZE] = { 0 };

	status = mica_status(unit->client);
	mica_print_service(unit->client, buffer, RESPONSE_MSG_SIZE);
	snprintf(response, RESPONSE_MSG_SIZE * 2, "%-30s%-20d%-20s%s",
		 unit->name, unit->client->cpu_id, status, buffer);

	send_log(msg_fd, "%s", response);
}

static inline int start_gdb_client(int msg_fd, struct listen_unit *unit)
{
	syslog(LOG_INFO, "receive starting gdb server request\n");
	// check if the elf support debugging
	if (unit->client->debug == false) {
		syslog(LOG_ERR, "The elf file does not support debugging\n");
		send_log(msg_fd, "The elf file does not support debugging\n");
		return -1;
	}
	char gdb_cmd[RESPONSE_MSG_SIZE];

	snprintf(gdb_cmd, RESPONSE_MSG_SIZE, "gdb %s -ex \'set remotetimeout unlimited\' "
	"-ex \'target extended-remote :%d\' -ex \'set remote run-packet off\'",
	unit->client->path, MICA_GDB_SERVER_PORT);

	DEBUG_PRINT("gdb_cmd: %s\n", gdb_cmd);
	send_log(msg_fd, "%s", gdb_cmd);

	return 0;
}

static int client_ctrl_handler(int epoll_fd, void *data)
{
	int msg_fd, ret;
	struct sockaddr_un addr;
	struct listen_unit *unit = data;
	socklen_t addrlen = sizeof(addr);
	char msg[CTRL_MSG_SIZE] = { 0 };

	msg_fd = accept(unit->socket_fd, (struct sockaddr *)&addr, &addrlen);
	if (msg_fd == -1) {
		syslog(LOG_ERR, "Failed to accept %s: %s", unit->socket_path, strerror(errno));
		return -1;
	}

	ret = recv(msg_fd, msg, CTRL_MSG_SIZE, 0);
	if (ret < 0) {
		syslog(LOG_ERR, "Failed to receive %s: %s", unit->socket_path, strerror(errno));
		goto err;
	}

	if (strncmp(msg, "start", CTRL_MSG_SIZE) == 0) {
		syslog(LOG_INFO, "Starting %s(%s) on CPU%d", unit->name, unit->client->path, unit->client->cpu_id);
		ret = mica_start(unit->client);
		if (ret) {
			syslog(LOG_ERR, "Start failed, ret(%d)", ret);
			goto err;
		}

		if (unit->client->debug) {
			ret = create_debug_service(unit->client);
			if (ret) {
				syslog(LOG_ERR, "Create rpmsg_tty failed, ret(%d)", ret);
				goto err;
			}
		}

		ret = create_rpmsg_tty(unit->client);
		if (ret) {
			syslog(LOG_ERR, "Create rpmsg_tty failed, ret(%d)", ret);
			goto err;
		}

		ret = create_rpmsg_rpc_service(unit->client);
		if (ret) {
			syslog(LOG_ERR, "enable rpmsg_rpc_service failed, ret(%d)", ret);
			goto err;
		}
	} else if (strncmp(msg, "stop", CTRL_MSG_SIZE) == 0) {
		syslog(LOG_INFO, "Stopping %s", unit->name);
		mica_stop(unit->client);
	} else if (strncmp(msg, "rm", CTRL_MSG_SIZE) == 0) {
		syslog(LOG_INFO, "Removing %s", unit->name);
		ret = epoll_ctl(epoll_fd, EPOLL_CTL_DEL, unit->socket_fd, NULL);
		if (ret < 0) {
			syslog(LOG_ERR, "Failed to remove fd from epoll, ret(%d)", ret);
			goto err;
		}
		mica_remove(unit->client);
	} else if (strncmp(msg, "status", CTRL_MSG_SIZE) == 0) {
		show_status(msg_fd, unit);
	} else if (strncmp(msg, "gdb", CTRL_MSG_SIZE) == 0) {
		ret = start_gdb_client(msg_fd, unit);
		if (ret == -1)
			goto err;
	} else {
		send_log(msg_fd, "Invalid command: %s", msg);
		syslog(LOG_ERR, "Invalid command: %s", msg);
		goto err;
	}

	send_log(msg_fd, "%s", MICA_MSG_SUCCESS);
	close(msg_fd);

	if (strncmp(msg, "rm", CTRL_MSG_SIZE) == 0)
		free_listener_by_name(unit->name);

	syslog(LOG_INFO, "%s done", msg);
	return 0;

err:
	send_log(msg_fd, "%s", MICA_MSG_FAILED);
	close(msg_fd);
	return ret;
}

static int create_mica_client(int epoll_fd, void *data)
{
	int msg_fd, ret;
	struct create_msg msg;
	struct sockaddr_un addr;
	struct mica_client *client;
	struct listen_unit *unit = data;
	socklen_t addrlen = sizeof(addr);

	msg_fd = accept(unit->socket_fd, (struct sockaddr *)&addr, &addrlen);
	if (msg_fd == -1) {
		syslog(LOG_ERR, "Failed to accept %s: %s", unit->socket_path, strerror(errno));
		return -1;
	}

	ret = recv(msg_fd, &msg, sizeof(msg), 0);
	if (ret < 0) {
		syslog(LOG_ERR, "Failed to receive %s: %s", unit->socket_path, strerror(errno));
		goto out;
	}

	ret = check_create_msg(msg, msg_fd);
	if (ret < 0)
		goto out;

	syslog(LOG_INFO, "receive create msg. cpu: %d, name:%s, path:%s", msg.cpu, msg.name, msg.path);

	client = calloc(1, sizeof(*client));
	if (!client) {
		ret = -ENOMEM;
		goto out;
	}

	client->cpu_id = msg.cpu;
	strlcpy(client->path, msg.path, MAX_FIRMWARE_PATH_LEN);

	if (strcmp(msg.ped, "jailhouse") == 0)
		client->ped = JAILHOUSE;
	else
		client->ped = BARE_METAL;

	strlcpy(client->ped_cfg, msg.ped_cfg, MAX_FIRMWARE_PATH_LEN);

	client->debug = msg.debug;
	syslog(LOG_INFO, "value of debug: %d", msg.debug);

	ret = mica_create(client);
	if (ret < 0) {
		syslog(LOG_ERR, "Failed to create mica client, ret: %d", ret);
		free(client);
		goto out;
	}

	ret = add_listener(msg.name, client, client_ctrl_handler, epoll_fd);
	if (ret < 0) {
		syslog(LOG_ERR, "Failed to add listener for %s, ret: %d", msg.name, ret);
		free(client);
	}
out:
	if (ret != 0)
		send_log(msg_fd, "%s", MICA_MSG_FAILED);
	else
		send_log(msg_fd, "%s", MICA_MSG_SUCCESS);
	close(msg_fd);
	return ret;
}

static void *wait_create_msg(void *arg)
{
	int i, ret, fds, epoll_fd;
	struct listen_unit *unit;
	struct epoll_event events[MAX_EVENTS];

	epoll_fd = epoll_create1(0);
	if (epoll_fd == -1) {
		syslog(LOG_ERR, "Failed to create epoll: %s", strerror(errno));
		goto out;
	}

	ret = add_listener("mica-create", NULL, create_mica_client, epoll_fd);
	if (ret < 0) {
		close(epoll_fd);
		goto out;
	}

	listening = true;
	pthread_mutex_lock(&mutex);
	pthread_cond_broadcast(&cond);
	pthread_mutex_unlock(&mutex);

	while (listening) {
		fds = epoll_wait(epoll_fd, events, MAX_EVENTS, -1);
		if (fds < 0) {
			perror("epoll_wait");
			exit(EXIT_FAILURE);
		}

		for (i = 0; i < fds; i++) {
			unit = (struct listen_unit *)events[i].data.ptr;
			unit->cb(epoll_fd, unit);
		}
	}
	/*
	 * We do the listener cleanup in unregister_socket_listener(),
	 * so all we need to do here is close epoll.
	 */
	close(epoll_fd);
	return NULL;

out:
	pthread_mutex_lock(&mutex);
	pthread_cond_broadcast(&cond);
	pthread_mutex_unlock(&mutex);
	return NULL;
}

static int unlink_cb(const char *fpath, const struct stat *sb, int typeflag, struct FTW *ftwbuf)
{
	int rv = remove(fpath);

	if (rv)
		syslog(LOG_ERR, "Cannot remove %s: %s", fpath, strerror(errno));

	return rv;
}

static void rmrf(const char *path)
{
	(void)!nftw(path, unlink_cb, 64, FTW_DEPTH | FTW_PHYS);
}

int register_socket_listener(void)
{
	int ret;
	pthread_t thread;

	rmrf(MICA_SOCKET_DIRECTORY);
	ret = mkdir(MICA_SOCKET_DIRECTORY, 0600);
	if (ret == -1 && errno != EEXIST) {
		syslog(LOG_ERR, "Failed to create %s: %s", MICA_SOCKET_DIRECTORY, strerror(errno));
		return ret;
	}

	ret = pthread_create(&thread, NULL, wait_create_msg, NULL);
	if (ret)
		return ret;

	ret = pthread_detach(thread);
	if (ret) {
		pthread_cancel(thread);
		return ret;
	}

	pthread_mutex_lock(&mutex);
	pthread_cond_wait(&cond, &mutex);
	pthread_mutex_unlock(&mutex);

	ret = listening ? 0 : -1;
	return ret;
}

void unregister_socket_listener(void)
{
	listening = false;
	free_all_listeners();
	rmrf(MICA_SOCKET_DIRECTORY);
}

