/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#define _XOPEN_SOURCE 600
#include <stdlib.h>
#include <stdio.h>
#include <fcntl.h>
#include <errno.h>
#include <unistd.h>
#include <string.h>
#include <pthread.h>

#include "mica/mica.h"
#include "mcs/mcs_common.h"
#include "rpmsg_pty.h"

/* define the keys according to your terminfo */
#define KEY_CTRL_D      4

struct rpmsg_app_resource g_rpmsg_app_resource;

static void pty_endpoint_exit(struct pty_ep_data *pty_ep)
{
	/* release the resources */
	close(pty_ep->fd_master);
	pthread_cancel(pty_ep->pty_thread);
	rpmsg_service_unregister_endpoint(pty_ep->ep_id);
	free(pty_ep);
}

static void pty_endpoint_unbind_cb(struct rpmsg_endpoint *ept)
{
	printf("%s: get unbind request from client side\n", ept->name);

	struct pty_ep_data *pty_ep = (struct pty_ep_data *)ept->priv;

	pty_endpoint_exit(pty_ep);
}

static int pty_endpoint_cb(struct rpmsg_endpoint *ept, void *data,
		size_t len, uint32_t src, void *priv)
{
	int ret;
	struct pty_ep_data *pty_ep = (struct pty_ep_data *)priv;

	while (len) {
		ret = write(pty_ep->fd_master, data, len);
		if (ret < 0) {
			printf("write pty master error:%d\n", ret);
			break;
		}
		len -= ret;
		data = (char *)data + ret;
	}

	return 0;
}

int open_pty(int *pfdm)
{
	int ret = -1;
	int fdm;
	char pts_name[20] = {0};

	/* Open the master side of the PTY */
	fdm = posix_openpt(O_RDWR | O_NOCTTY);
	if (fdm < 0) {
		printf("Error %d on posix_openpt()\n", errno);
		return ret;
	}

	printf("pty master fd is :%d\n", fdm);

	ret = grantpt(fdm);
	if (ret != 0) {
		printf("Error %d on grantpt()\n", errno);
		goto err_close_pty;
	}

	ret = unlockpt(fdm);
	if (ret != 0) {
		printf("Error %d on unlockpt()\n", errno);
		goto err_close_pty;
	}

	/* Open the slave side of the PTY */
	ret = ptsname_r(fdm, pts_name, sizeof(pts_name));
	if (ret != 0) {
		printf("Error %d on ptsname_r()\n", errno);
		goto err_close_pty;
	}

	printf("pls open %s to talk with client OS\n", pts_name);

	*pfdm = fdm;

	return 0;

err_close_pty:
	close(fdm);
}

static void *pty_thread(void *arg)
{
	int ret;
	int fdm;
	unsigned char cmd[128];
	struct pty_ep_data * pty_ep;

	pty_ep = (struct pty_ep_data *)arg;

	printf("pty_thread for %s is runnning\n", rpmsg_service_endpoint_name(pty_ep->ep_id));
	fdm = pty_ep->fd_master;

	/* wait endpoint bound */
	while(!rpmsg_service_endpoint_is_bound(pty_ep->ep_id));

	while (1) {
		ret = read(fdm, cmd, 128);   /* get command from ptmx */
		if (ret <= 0) {
			printf("shell_user: get from ptmx failed: %d\n", ret);
			ret = -1;
			break;
		}

		if (cmd[ret - 1] == KEY_CTRL_D) {  /* special key: ctrl+d */
			ret = 0;  /* exit this thread, the same as pthread_exit */
			break;
		}

		printf("hzc debug, get command from pty, send to remote\n");
		ret = rpmsg_service_send(pty_ep->ep_id, cmd, ret);
		if (ret < 0) {
			printf("rpmsg_service_send error %d\n", ret);
			ret = -1;
			break;
		}
	}

	pty_endpoint_exit(pty_ep);

	return INT_TO_PTR(ret);
}

static struct pty_ep_data *pty_service_create(const char * ep_name)
{
	if (ep_name == NULL) {
		return NULL;
	}

	int ret;
	struct pty_ep_data * pty_ep;

	pty_ep = (struct pty_ep_data * )malloc(sizeof(struct pty_ep_data));
	if (pty_ep == NULL) {
		return NULL;
	}

	ret = open_pty(&pty_ep->fd_master);
	if (ret != 0) {
		goto err_free_resource_struct;
	}

	printf("hzc debug, register endpoint %s\n", ep_name);
	pty_ep->ep_id = rpmsg_service_register_endpoint(ep_name, pty_endpoint_cb,
											pty_endpoint_unbind_cb, pty_ep);
	if (pty_ep->ep_id < 0) {
		printf("register endpoint %s failed\n", ep_name);
		goto err_close_pty;
	}

	if (pthread_create(&pty_ep->pty_thread, NULL, pty_thread, pty_ep) != 0) {
		printf("pty thread create failed\n");
		goto err_unregister_endpoint;
	}
	if (pthread_detach(pty_ep->pty_thread) != 0) {
		printf("pty thread detach failed\n");
		goto err_cancel_thread;
	}

	return pty_ep;

err_cancel_thread:
	pthread_cancel(pty_ep->pty_thread);
err_unregister_endpoint:
	rpmsg_service_unregister_endpoint(pty_ep->ep_id);
err_close_pty:
	close(pty_ep->fd_master);
err_free_resource_struct:
	free(pty_ep);
	return NULL;
}

int rpmsg_app_start(struct client_os_inst *client)
{
	int ret;
	g_rpmsg_app_resource.pty_ep_uart = pty_service_create("rpmsg-tty");
	if (g_rpmsg_app_resource.pty_ep_uart == NULL) {
		return -1;
	}

	g_rpmsg_app_resource.pty_ep_console = pty_service_create("console");
	if (g_rpmsg_app_resource.pty_ep_console == NULL) {
		ret = -1;
		goto err_free_uart;
	}

	if (pthread_create(&g_rpmsg_app_resource.rpmsg_loop_thread, NULL, rpmsg_loop_thread, client) != 0) {
		perror("create rpmsg loop thread failed\n");
		ret = -errno;
		goto err_free_console;
	}
	if (pthread_detach(g_rpmsg_app_resource.rpmsg_loop_thread) != 0) {
		perror("detach rpmsg loop thread failed\n");
		ret = -errno;
		goto err_cancel_thread;
	}

	return 0;

err_cancel_thread:
	pthread_cancel(g_rpmsg_app_resource.rpmsg_loop_thread);
err_free_console:
	pty_endpoint_exit(g_rpmsg_app_resource.pty_ep_console);
err_free_uart:
	pty_endpoint_exit(g_rpmsg_app_resource.pty_ep_uart);
	return ret;
}

static void *rpmsg_loop_thread(void *args)
{
	struct client_os_inst *client = (struct client_os_inst *)args;
	printf("start polling for remote interrupts\n");
	rpmsg_service_receive_loop(client);
	return NULL;
}

void rpmsg_app_stop(void)
{
	pthread_cancel(g_rpmsg_app_resource.rpmsg_loop_thread);
	pty_endpoint_exit(g_rpmsg_app_resource.pty_ep_uart);
	pty_endpoint_exit(g_rpmsg_app_resource.pty_ep_console);
	printf("rpmsg apps have stopped\n");
}
