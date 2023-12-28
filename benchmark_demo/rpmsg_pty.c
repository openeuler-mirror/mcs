/* SPDX-License-Identifier: MulanPSL-2.0 */

#define _XOPEN_SOURCE 600
#include <stdlib.h>
#include <stdio.h>
#include <fcntl.h>
#include <errno.h>
#include <unistd.h>
#include <string.h>
#include <pthread.h>

#include "openamp_module.h"
#include "benchmark.h"

/* define the keys according to your terminfo */
#define KEY_CTRL_D      4

static int stay_slave_fd;

static void pty_endpoint_exit(struct pty_ep_data *pty_ep)
{
	/* release the resources */
	close(pty_ep->fd_master);
	close(stay_slave_fd);
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
		return  ret;
	}

	printf("pty master fd is :%d\n", fdm);

	ret = grantpt(fdm);
	if (ret != 0) {
		printf("Error %d on grantpt()\n", errno);
		return ret;
	}

	ret = unlockpt(fdm);
	if (ret != 0) {
		printf("Error %d on unlockpt()\n", errno);
		return ret;
	}

	/* Open the slave side of the PTY */
	ret = ptsname_r(fdm, pts_name, sizeof(pts_name));
	if (ret != 0) {
		printf("Error %d on ptsname_r()\n", errno);
		return ret;
	}

	/* Prevent closing EIO error from device */
	stay_slave_fd = open(pts_name, O_RDWR);
	if (stay_slave_fd == -1) {
		perror("open");
		exit(EXIT_FAILURE);
	}

	printf("pls open %s to test pty\n", pts_name);

	*pfdm = fdm;

	return 0;
}

void *pty_thread(void *arg)
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
		ret = rpmsg_service_send(pty_ep->ep_id, cmd, ret);
		if (ret < 0) {
			printf("rpmsg_service_send error %d\n", ret);
			ret = -1;
			break;
		}
	}

	pty_endpoint_exit(pty_ep);

	return (void *)((unsigned long)(ret));
}

struct pty_ep_data *pty_ping_create(const char * ep_name)
{
	int ret;
	struct pty_ep_data * pty_ep;

	pty_ep = (struct pty_ep_data * )malloc(sizeof(struct pty_ep_data));

	if (pty_ep == NULL || ep_name == NULL) {
		return NULL;
	}

	ret = open_pty(&pty_ep->fd_master);

	if (ret != 0) {
		free(pty_ep);
		return NULL;
	}

	pty_ep->ep_id = rpmsg_service_register_endpoint(ep_name, pty_endpoint_cb,
											pty_endpoint_unbind_cb, pty_ep);

	if (pthread_create(&pty_ep->pty_thread, NULL, pty_thread, pty_ep) < 0) {
		printf("pty thread create failed\n");
		free(pty_ep);
		return NULL;
	}
	pthread_detach(pty_ep->pty_thread);

	return pty_ep;
}
