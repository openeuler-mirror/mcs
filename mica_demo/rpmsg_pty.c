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
	int active;
	struct rpmsg_endpoint ept;
	int pty_master_fd;
	int pty_slave_fd;
	int tty_index;
	char tty_dev[RPMSG_TTY_DEV_LEN];
};

static void rpmsg_tty_unbind(struct rpmsg_endpoint *ept)
{
	struct rpmsg_tty_service *svc = ept->priv;

	svc->active = 0;
	rpmsg_destroy_ept(ept);
	free(svc);
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
static int create_tty_device(struct rpmsg_tty_service *svc)
{
	int ret;
	int master_fd, slave_fd;
	char pts_name[RPROC_MAX_NAME_LEN] = {0};

	ret = rpmsg_tty_new_index();
	if (ret == -1)
		return ret;

	svc->tty_index = ret;

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

        snprintf(svc->tty_dev, RPMSG_TTY_DEV_LEN, "%s%d",
		 RPMSG_TTY_DEV, svc->tty_index);

	ret = symlink(pts_name, svc->tty_dev);
	if (ret != 0)
		goto err;

	/* keep open a handle to the slave to prevent EIO */
	slave_fd = open(pts_name, O_RDWR);
	if (slave_fd == -1) {
		unlink(pts_name);
		goto err;
	}

	svc->pty_master_fd = master_fd;
	svc->pty_slave_fd = slave_fd;
	return ret;
err:
	close(master_fd);
	svc->pty_master_fd = -1;
	svc->pty_slave_fd = -1;
	tty_id[svc->tty_index] = -1;
	svc->tty_index = -1;
	memset(svc->tty_dev, 0, sizeof(svc->tty_dev));
	return ret;
}

/**
 * RX callbacks for remote messages.
 */
static int rpmsg_rx_tty_callback(struct rpmsg_endpoint *ept, void *data,
				 size_t len, uint32_t src, void *priv)
{
	int ret;
	struct rpmsg_tty_service *svc = priv;

	if (svc->active != 1)
		return -EAGAIN;

	while (len) {
		ret = write(svc->pty_master_fd, data, len);
		if (ret < 0) {
			fprintf(stderr, "write %s error:%d\n", svc->tty_dev, ret);
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
	struct rpmsg_tty_service *svc = arg;
	char buf[BUF_SIZE];
	struct pollfd fds = {
		.fd = svc->pty_master_fd,
		.events = POLLIN
	};

	svc->active = 1;

	while (svc->active) {
		ret = poll(&fds, 1, -1);
		if (ret == -1) {
			fprintf(stderr, "%s failed: %s\n", __func__, strerror(errno));
			break;
		}

		if (fds.revents & POLLIN) {
			ret = read(svc->pty_master_fd, buf, BUF_SIZE);
			if (ret <= 0) {
				fprintf(stderr, "shell_user: get from ptmx failed: %d\n", ret);
				break;
			}

			ret = rpmsg_send(&svc->ept, buf, ret);
			if (ret < 0) {
				fprintf(stderr, "%s: rpmsg_send failed: %d\n", __func__, ret);
				break;
			}
		}
	}

	if (svc->active)
		rpmsg_tty_unbind(&svc->ept);
}

/**
 * Init function for rpmsg-tty.
 * Create a pty and an rpmsg tty endpoint.
 */
static void rpmsg_tty_init(struct rpmsg_device *rdev, const char *name,
			   uint32_t dest, void *priv)
{
	int ret;
	pthread_t tty_thread;
	struct rpmsg_tty_service *tty_svc;

	tty_svc = malloc(sizeof(struct rpmsg_tty_service));
	if (!tty_svc)
		return;
	tty_svc->ept.priv = tty_svc;

	ret = create_tty_device(tty_svc);
	if (ret)
		goto free_mem;

	ret = rpmsg_create_ept(&tty_svc->ept, rdev, name, RPMSG_ADDR_ANY, dest,
			       rpmsg_rx_tty_callback, rpmsg_tty_unbind);
	if (ret)
		goto free_mem;

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
			    uint32_t dest, void *priv)
{
	int len0, len1;

	len0 = strlen(name);
	len1 = strlen(RPMSG_TTY_NAME);
	len0 = len0 < len1 ? len0 : len1;

	return !strncmp(name, RPMSG_TTY_NAME, len0);
}

static struct mica_service rpmsg_tty_service = {
	.name = RPMSG_TTY_NAME,
	.rpmsg_ns_match = rpmsg_tty_match,
	.rpmsg_ns_bind_cb = rpmsg_tty_init,
};

int create_rpmsg_tty(struct mica_client *client)
{
	return mica_register_service(client, &rpmsg_tty_service);
}
