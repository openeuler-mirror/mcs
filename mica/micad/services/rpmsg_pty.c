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

#include "mica/mica.h"
#include "rpmsg_pty.h"

#define RPMSG_TTY_NAME		"rpmsg-tty"
#define RPMSG_TTY_DEV		"/dev/ttyRPMSG"
#define RPMSG_TTY_DEV_LEN	20
#define RPMSG_TTY_MAX_DEV	10
#define BUF_SIZE		256

static int tty_id[RPMSG_TTY_MAX_DEV] = { [ 0 ... (RPMSG_TTY_MAX_DEV-1) ] = -1 };

struct rpmsg_tty_service
{
	atomic_int active;
	struct rpmsg_endpoint ept;
	int pty_master_fd;
	int pty_slave_fd;
	int tty_index;
	char tty_dev[RPMSG_TTY_DEV_LEN];
	struct metal_list node;
};

static void rpmsg_tty_unbind(struct rpmsg_endpoint *ept)
{
	struct rpmsg_tty_service *tty_svc = ept->priv;

	metal_list_del(&tty_svc->node);
	rpmsg_destroy_ept(&tty_svc->ept);
	tty_id[tty_svc->tty_index] = -1;
	unlink(tty_svc->tty_dev);

	close(tty_svc->pty_master_fd);
	close(tty_svc->pty_slave_fd);
	tty_svc->pty_master_fd = -1;
	tty_svc->pty_slave_fd = -1;

	/* stop rpmsg_tty_tx_task */
	tty_svc->active = 0;
}

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

/**
 * opens an unused pseudo terminal, and create a link to this
 * with the name RPMSG_TTY_DEV.
 */
static int create_tty_device(struct rpmsg_tty_service *tty_svc)
{
	int ret;
	int master_fd, slave_fd;
	char pts_name[RPROC_MAX_NAME_LEN] = {0};

	ret = rpmsg_tty_new_index();
	if (ret == -1)
		return ret;
	tty_svc->tty_index = ret;

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

	snprintf(tty_svc->tty_dev, RPMSG_TTY_DEV_LEN, "%s%d",
		 RPMSG_TTY_DEV, tty_svc->tty_index);

	unlink(tty_svc->tty_dev);
	ret = symlink(pts_name, tty_svc->tty_dev);
	if (ret != 0)
		goto err;

	/* keep open a handle to the slave to prevent EIO */
	slave_fd = open(pts_name, O_RDWR);
	if (slave_fd == -1) {
		unlink(pts_name);
		goto err;
	}

	tty_svc->pty_master_fd = master_fd;
	tty_svc->pty_slave_fd = slave_fd;
	return ret;
err:
	close(master_fd);
	tty_id[tty_svc->tty_index] = -1;
	return ret;
}

/**
 * RX callbacks for remote messages.
 */
static int rpmsg_rx_tty_callback(struct rpmsg_endpoint *ept, void *data,
				 size_t len, uint32_t src, void *priv)
{
	int ret;
	struct rpmsg_tty_service *tty_svc = priv;

	if (tty_svc->active != 1)
		return -EAGAIN;

	while (len) {
		ret = write(tty_svc->pty_master_fd, data, len);
		if (ret < 0) {
			fprintf(stderr, "write %s error:%d\n", tty_svc->tty_dev, ret);
			break;
		}
		len -= ret;
		data = (char *)data + ret;
	}
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

	tty_svc = malloc(sizeof(struct rpmsg_tty_service));
	if (!tty_svc)
		return;
	tty_svc->ept.priv = tty_svc;

	ret = create_tty_device(tty_svc);
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
	return;
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

static int create_tty_dev_lists(struct mica_service *svc)
{
	struct metal_list *tty_dev_list;

	tty_dev_list = malloc(sizeof(*tty_dev_list));
	if (!tty_dev_list)
		return -ENOMEM;

	metal_list_init(tty_dev_list);
	svc->priv = tty_dev_list;
	return 0;
}

static void remove_tty_dev_lists(struct mica_service *svc)
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
