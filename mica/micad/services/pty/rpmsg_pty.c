/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#define _XOPEN_SOURCE	600
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <poll.h>
#include <pthread.h>
#ifdef RPMSG_TTY_USE_CLIENT_NAME
#include <stdint.h>
#include <errno.h>
#endif

#include "mica/mica.h"
#include "rpmsg_pty.h"

#define RPMSG_TTY_NAME		"rpmsg-tty"
#ifdef RPMSG_TTY_USE_CLIENT_NAME
#define RPMSG_TTY_DEV		"/dev/ttyRPMSG_"
/* /dev/ttyRPMSG_ (14) + MAX_NAME_LEN (32) + \0 (1) = 47 */
#define RPMSG_TTY_DEV_LEN	47
#else
#define RPMSG_TTY_DEV		"/dev/ttyRPMSG"
#define RPMSG_TTY_DEV_LEN	20
#define RPMSG_TTY_MAX_DEV	10
#endif
#define BUF_SIZE		256

#ifndef RPMSG_TTY_USE_CLIENT_NAME
static int tty_id[RPMSG_TTY_MAX_DEV] = { [0 ... (RPMSG_TTY_MAX_DEV-1)] = -1 };
#endif

struct rpmsg_tty_service {
	atomic_int active;
	struct rpmsg_endpoint ept;
	int pty_master_fd;
	int pty_slave_fd;
	#ifdef RPMSG_TTY_USE_CLIENT_NAME
	char tty_suffix[MAX_NAME_LEN];
	#else
	int tty_index;
	#endif
	char tty_dev[RPMSG_TTY_DEV_LEN];
	struct metal_list node;
};

static void rpmsg_tty_unbind(struct rpmsg_endpoint *ept)
{
	struct rpmsg_tty_service *tty_svc = ept->priv;

	metal_list_del(&tty_svc->node);
	rpmsg_destroy_ept(&tty_svc->ept);
	#ifndef RPMSG_TTY_USE_CLIENT_NAME
	tty_id[tty_svc->tty_index] = -1;
	#endif
	unlink(tty_svc->tty_dev);

	close(tty_svc->pty_master_fd);
	close(tty_svc->pty_slave_fd);
	tty_svc->pty_master_fd = -1;
	tty_svc->pty_slave_fd = -1;

	/* stop rpmsg_tty_tx_task */
	tty_svc->active = 0;
}

#ifndef RPMSG_TTY_USE_CLIENT_NAME
static int rpmsg_tty_new_index(void)
{
	int i;

	for (i = 0; i < RPMSG_TTY_MAX_DEV; i++) {
		if (tty_id[i] == -1) {
			tty_id[i] = 1;
			return i;
		}
	}

	return -1;
}
#endif

#ifdef RPMSG_TTY_USE_CLIENT_NAME
static void sanitize_client_tty_name(char *dst, const char *src, size_t max_len)
{
	size_t i;

	if (!src || !dst || max_len == 0)
		return;

	for (i = 0; i < max_len - 1 && src[i] != '\0'; i++) {
		char c = src[i];
		if ((c >= 'a' && c <= 'z') ||
		    (c >= 'A' && c <= 'Z') ||
		    (c >= '0' && c <= '9') ||
		    c == '_' || c == '-') {
			dst[i] = c;
		} else {
			dst[i] = '_';
		}
	}
	dst[i] = '\0';
}
#endif

/**
 * Opens an unused pseudo terminal, and create a link to this
 * with the name RPMSG_TTY_DEV_<client_name>.
 * In legacy suffix style, client_name is ignored and we use index-based naming
 */
static int create_tty_device(struct rpmsg_tty_service *tty_svc, const char *client_name)
{
	int ret;
	int master_fd, slave_fd;
	char pts_name[RPROC_MAX_NAME_LEN] = {0};

	#ifdef RPMSG_TTY_USE_CLIENT_NAME
	if (!client_name || strlen(client_name) == 0) {
		fprintf(stderr, "Invalid client name\n");
		return -EINVAL;
	}

	sanitize_client_tty_name(tty_svc->tty_suffix, client_name, sizeof(tty_svc->tty_suffix));
	#else
	ret = rpmsg_tty_new_index();
	if (ret == -1)
		return ret;
	tty_svc->tty_index = ret;
	#endif

	ret = posix_openpt(O_RDWR | O_NOCTTY);
	if (ret == -1)
		return ret;

	master_fd = ret;
	ret = grantpt(master_fd);
	if (ret != 0)
		goto err;

	ret = unlockpt(master_fd);
	if (ret != 0)
		goto err;

	ret = ptsname_r(master_fd, pts_name, sizeof(pts_name));
	if (ret != 0)
		goto err;

	#ifdef RPMSG_TTY_USE_CLIENT_NAME
	snprintf(tty_svc->tty_dev, RPMSG_TTY_DEV_LEN, "%s%s",
		 RPMSG_TTY_DEV, tty_svc->tty_suffix);
	#else
	snprintf(tty_svc->tty_dev, RPMSG_TTY_DEV_LEN, "%s%d",
		 RPMSG_TTY_DEV, tty_svc->tty_index);
	#endif

	unlink(tty_svc->tty_dev);
	ret = symlink(pts_name, tty_svc->tty_dev);
	if (ret != 0)
		goto err;

	/* keep open a handle to the slave to prevent EIO */
	slave_fd = open(pts_name, O_RDWR);
	if (slave_fd == -1) {
		#ifdef RPMSG_TTY_USE_CLIENT_NAME
		unlink(tty_svc->tty_dev);
		#else
		unlink(pts_name);
		#endif
		goto err;
	}

	tty_svc->pty_master_fd = master_fd;
	tty_svc->pty_slave_fd = slave_fd;
	return 0;
err:
	close(master_fd);
	#ifndef RPMSG_TTY_USE_CLIENT_NAME
	tty_id[tty_svc->tty_index] = -1;
	#endif
	return ret;
}

/**
 * RX callbacks for remote messages.
 */
static int rpmsg_rx_tty_callback(struct rpmsg_endpoint *ept, void *data,
				 size_t len, uint32_t src, void *priv)
{
	int ret, i, j;
	char *msg, *msg_data, *p;
	struct rpmsg_tty_service *tty_svc = priv;

	if (tty_svc->active != 1)
		return -EAGAIN;

	msg = (char *)malloc(sizeof(char) * (len * 2));
	if (msg == NULL)
		return -ENOMEM;

	p = msg;
	msg_data = (char *)data;
	/* when using tty, translate '\n' to "\r\n" */
	for (i = 0, j = 0; i < len; ++i, ++msg_data) {
		if (*msg_data == '\n') {
			msg[i + j] = '\r';
			++j;
		}
		msg[i + j] = *msg_data;
	}
	len = i + j;

	while (len) {
		ret = write(tty_svc->pty_master_fd, msg, len);
		if (ret < 0) {
			fprintf(stderr, "write %s error:%d\n", tty_svc->tty_dev, ret);
			break;
		}
		len -= ret;
		msg = (char *)msg + ret;
	}

	free(p);
	return 0;
}

/*
 * TX thread. Listens for the tty device, and
 * send the messages to remote.
 */
void *rpmsg_tty_tx_task(void *arg)
{
	int ret;
	struct rpmsg_tty_service *tty_svc = arg;
	char buf[BUF_SIZE];
	struct pollfd fds = {
		.fd = tty_svc->pty_master_fd,
		.events = POLLIN
	};

	tty_svc->active = 1;

	while (tty_svc->active) {
		ret = poll(&fds, 1, -1);
		if (ret == -1) {
			fprintf(stderr, "%s failed: %s\n", __func__, strerror(errno));
			break;
		}

		if (fds.revents & POLLIN) {
			ret = read(tty_svc->pty_master_fd, buf, BUF_SIZE);
			if (ret <= 0) {
				fprintf(stderr, "shell_user: get from ptmx failed: %d\n", ret);
				break;
			}

			ret = rpmsg_send(&tty_svc->ept, buf, ret);
			if (ret < 0) {
				fprintf(stderr, "%s: rpmsg_send failed: %d\n", __func__, ret);
				break;
			}
		}
	}

	if (tty_svc->active)
		rpmsg_tty_unbind(&tty_svc->ept);

	free(tty_svc);
	pthread_exit(NULL);
}

/**
 * Init function for rpmsg-tty.
 * Create a pty and an rpmsg tty endpoint.
 */
static void rpmsg_tty_init(struct rpmsg_device *rdev, const char *name,
			   uint32_t remote_addr, uint32_t remote_dest, void *priv)
{
	int ret;
	pthread_t tty_thread;
	struct rpmsg_tty_service *tty_svc;
	struct metal_list *tty_dev_list = priv;
	const char *client_name = NULL;
	#ifdef RPMSG_TTY_USE_CLIENT_NAME
	struct rpmsg_virtio_device *rvdev;
	struct remoteproc_virtio *rpvdev;
	struct remoteproc *rproc;
	struct mica_client *client;
	#endif

	tty_svc = malloc(sizeof(struct rpmsg_tty_service));
	if (!tty_svc)
		return;
	tty_svc->ept.priv = tty_svc;

	#ifdef RPMSG_TTY_USE_CLIENT_NAME
	/* extract client name from rpmsg device hierarchy */
	rvdev = metal_container_of(rdev, struct rpmsg_virtio_device, rdev);
	rpvdev = metal_container_of(rvdev->vdev, struct remoteproc_virtio, vdev);
	rproc = rpvdev->priv;
	client = metal_container_of(rproc, struct mica_client, rproc);
	client_name = client->name;
	#endif

	ret = create_tty_device(tty_svc, client_name);
	if (ret)
		goto free_mem;

	ret = rpmsg_create_ept(&tty_svc->ept, rdev, name, remote_dest, remote_addr,
			       rpmsg_rx_tty_callback, rpmsg_tty_unbind);
	if (ret)
		goto free_mem;

	/*
	 * If the ept is successfully created, append the device to tty_dev_list
	 * to make it easier to get the associated device.
	 */
	metal_list_add_tail(tty_dev_list, &tty_svc->node);

	/* Create a tx task to listen for a pty and send pty messages to the remote */
	ret = pthread_create(&tty_thread, NULL, rpmsg_tty_tx_task, tty_svc);
	if (ret)
		goto free_ept;

	ret = pthread_detach(tty_thread);
	if (ret)
		goto free_pthread;

	fprintf(stdout, "Please open %s to talk with client OS\n", tty_svc->tty_dev);
	return;

free_pthread:
	pthread_cancel(tty_thread);
free_ept:
	rpmsg_destroy_ept(&tty_svc->ept);
	metal_list_del(&tty_svc->node);
free_mem:
	free(tty_svc);
}

/**
 * Allow for wildcard matches.
 * It is possible to support "rpmsg-tty*", i.e:
 *    rpmsg-tty0
 *    rpmsg-tty1
 */
static bool rpmsg_tty_match(struct rpmsg_device *rdev, const char *name,
			    uint32_t remote_addr, uint32_t remote_dest, void *priv)
{
	int len0, len1;

	len0 = strlen(name);
	len1 = strlen(RPMSG_TTY_NAME);
	len0 = len0 < len1 ? len0 : len1;

	return !strncmp(name, RPMSG_TTY_NAME, len0);
}

static void get_rpmsg_tty_dev(char *str, size_t size, void *priv)
{
	struct rpmsg_tty_service *tty_svc;
	struct metal_list *node;
	struct metal_list *tty_dev_list = priv;

	metal_list_for_each(tty_dev_list, node) {
		tty_svc = metal_container_of(node, struct rpmsg_tty_service, node);
		snprintf(str + strlen(str), size - strlen(str), "%s(%s) ",
			 tty_svc->ept.name, tty_svc->tty_dev);
	}
}

static int create_tty_dev_lists(struct mica_client *client, struct mica_service *svc)
{
	struct metal_list *tty_dev_list;

	tty_dev_list = malloc(sizeof(*tty_dev_list));
	if (!tty_dev_list)
		return -ENOMEM;

	metal_list_init(tty_dev_list);
	svc->priv = tty_dev_list;
	return 0;
}

static void remove_tty_dev_lists(struct mica_client *client, struct mica_service *svc)
{
	struct rpmsg_tty_service *tty_svc;
	struct metal_list *node, *tmp_node;
	struct metal_list *tty_dev_list = svc->priv;

	/* unbind all services */
	metal_list_for_each(tty_dev_list, node) {
		tty_svc = metal_container_of(node, struct rpmsg_tty_service, node);
		tmp_node = node;
		node = tmp_node->prev;
		rpmsg_tty_unbind(&tty_svc->ept);
	}

	free(svc->priv);
	svc->priv = NULL;
}

static struct mica_service rpmsg_tty_service = {
	.name = RPMSG_TTY_NAME,
	.init = create_tty_dev_lists,
	.remove = remove_tty_dev_lists,
	.rpmsg_ns_match = rpmsg_tty_match,
	.rpmsg_ns_bind_cb = rpmsg_tty_init,
	.get_match_device = get_rpmsg_tty_dev,
};

int create_rpmsg_tty(struct mica_client *client)
{
	return mica_register_service(client, &rpmsg_tty_service);
}
