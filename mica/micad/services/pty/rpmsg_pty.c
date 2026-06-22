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
#include <stdint.h>
#include <errno.h>
#include <limits.h>

#include "mica/mica.h"
#include "rpmsg_pty.h"

#define RPMSG_TTY_NAME		"rpmsg-tty"
#ifdef RPMSG_TTY_USE_CLIENT_NAME
#define RPMSG_TTY_DEV		"/dev/ttyRPMSG_"
#define RPMSG_TTY_DEV_LEN	(MAX_NAME_LEN + 32)
#else
#define RPMSG_TTY_DEV		"/dev/ttyRPMSG"
#define RPMSG_TTY_DEV_LEN	20
#define RPMSG_TTY_MAX_DEV	10
#endif
#define BUF_SIZE		256

#ifdef RPMSG_TTY_USE_CLIENT_NAME
#define CLIENT_MAX_TTY_INDICES 63 /* less concerns about uint64_t overflow */

#define BITMAP_MASK(idx)		(1ULL << (idx)) /* mark the bit as allocated */
#define BITMAP_SET(bitmap, idx)		((bitmap) |= BITMAP_MASK(idx))
#define BITMAP_CLEAR(bitmap, idx)	((bitmap) &= ~BITMAP_MASK(idx))
#define BITMAP_TEST(bitmap, idx)	(((bitmap) & BITMAP_MASK(idx)) == 0)
#define BITMAP_FULL(bitmap) (~(bitmap) == 0)

struct client_ttyidx {
	struct client_ttyidx *next;
	char name[MAX_NAME_LEN];
	uint64_t free_bitmap;	/* bit 0 = index 0 is allocated (1) or free (0) */
	unsigned int next_idx;	/* next index to allocate when free_bitmap is full */
	int rc;
};

static struct client_ttyidx *client_ttyidx_head;
static pthread_mutex_t idx_lock = PTHREAD_MUTEX_INITIALIZER;

static struct client_ttyidx *client_idx_get(const char *name)
{
	struct client_ttyidx *entry;

	pthread_mutex_lock(&idx_lock);
	for (entry = client_ttyidx_head; entry; entry = entry->next) {
		if (!strncmp(entry->name, name, MAX_NAME_LEN))
			goto out;
	}

	entry = calloc(1, sizeof(*entry));
	if (entry) {
		strncpy(entry->name, name, MAX_NAME_LEN - 1);
		entry->name[MAX_NAME_LEN - 1] = '\0';
		entry->free_bitmap = 0;
		entry->next_idx = CLIENT_MAX_TTY_INDICES;
		entry->next = client_ttyidx_head;
		client_ttyidx_head = entry;
	}

out:
	pthread_mutex_unlock(&idx_lock);
	return entry;
}

static int lowest_free(uint64_t bitmap) {
		uint64_t lfb;
    if (BITMAP_FULL(bitmap)) {
        return -ENOSPC;
    }
    lfb = (~bitmap & -~bitmap);
    return (int)__builtin_ctzll(lfb);
}

static int new_free_idx(struct client_ttyidx *state)
{
	int idx;

	if (!state)
		return -ENOMEM;

	pthread_mutex_lock(&idx_lock);

	idx = lowest_free(state->free_bitmap);
	if (idx >= 0) {
		BITMAP_SET(state->free_bitmap, idx);
	} else {
		idx = state->next_idx;
		state->next_idx++;
	}

	pthread_mutex_unlock(&idx_lock);
	return idx;
}

/**
 * idx_recycle
 * @idx: index to return
 *
 * Recycle a previously allocated TTY index so it can be reused later.
 * For indices < CLIENT_MAX_TTY_INDICES, clears the corresponding bit in bitmap.
 * For indices >= CLIENT_MAX_TTY_INDICES, no tracking (cannot be recycled).
 */
static void idx_recycle(struct client_ttyidx *state, int idx)
{
	if (!state || idx < 0)
		return;

	pthread_mutex_lock(&idx_lock);
	if (idx < CLIENT_MAX_TTY_INDICES) {
		BITMAP_CLEAR(state->free_bitmap, idx);
	}
	pthread_mutex_unlock(&idx_lock);
}

static void ref_inc(struct client_ttyidx *state)
{
	if (!state)
		return;
	pthread_mutex_lock(&idx_lock);
	state->rc++;
	pthread_mutex_unlock(&idx_lock);
}

static void ref_dec(struct client_ttyidx *state)
{
	int drop = 0;

	if (!state)
		return;

	pthread_mutex_lock(&idx_lock);
	state->rc--;
	if (state->rc == 0) {
		struct client_ttyidx **curr = &client_ttyidx_head;
		while (*curr && *curr != state)
			curr = &(*curr)->next;
		if (*curr)
			*curr = state->next;
		drop = 1;
	}
	pthread_mutex_unlock(&idx_lock);

	if (drop) {
		free(state);
	}
}
#else
static int tty_id[RPMSG_TTY_MAX_DEV] = { [0 ... (RPMSG_TTY_MAX_DEV-1)] = -1 };
#endif

struct rpmsg_tty_service {
	atomic_int active;
	struct rpmsg_endpoint ept;
	int pty_master_fd;
	int pty_slave_fd;
	int tty_index;
#ifdef RPMSG_TTY_USE_CLIENT_NAME
	char tty_suffix[MAX_NAME_LEN];
	struct client_ttyidx *client_idx_state;
#endif
	char tty_dev[RPMSG_TTY_DEV_LEN];
	struct metal_list node;
};

static void rpmsg_tty_unbind(struct rpmsg_endpoint *ept)
{
	struct rpmsg_tty_service *tty_svc = ept->priv;

	metal_list_del(&tty_svc->node);
	rpmsg_destroy_ept(&tty_svc->ept);
#ifdef RPMSG_TTY_USE_CLIENT_NAME
	if (tty_svc->client_idx_state && tty_svc->tty_index >= 0) {
		idx_recycle(tty_svc->client_idx_state, tty_svc->tty_index);
		ref_dec(tty_svc->client_idx_state);
		tty_svc->client_idx_state = NULL;
		tty_svc->tty_index = -1;
	}
#else
	if (tty_svc->tty_index >= 0 && tty_svc->tty_index < RPMSG_TTY_MAX_DEV) {
		tty_id[tty_svc->tty_index] = -1;
		tty_svc->tty_index = -1;
	}
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
 * with the name RPMSG_TTY_DEV_<client_name>_<idx> when client-name mode is enabled.
 * In legacy idx mode we continue to append the numeric idx alone.
 */
static int create_tty_device(struct rpmsg_tty_service *tty_svc, const char *client_name)
{
	int ret;
	int master_fd, slave_fd;
	char pts_name[RPROC_MAX_NAME_LEN] = {0};
#ifdef RPMSG_TTY_USE_CLIENT_NAME
	struct client_ttyidx *client_state = NULL;
	int client_state_refed = 0;
#endif

	tty_svc->tty_index = -1;
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
	client_state = client_idx_get(tty_svc->tty_suffix);
	if (!client_state) {
		ret = -ENOMEM;
		goto err;
	}

	ref_inc(client_state);
	client_state_refed = 1;

	ret = new_free_idx(client_state);
	if (ret < 0)
		goto err_state;

	tty_svc->tty_index = ret;
	tty_svc->client_idx_state = client_state;
	snprintf(tty_svc->tty_dev, RPMSG_TTY_DEV_LEN, "%s%s_%d",
		 RPMSG_TTY_DEV, tty_svc->tty_suffix, tty_svc->tty_index);
#else
	snprintf(tty_svc->tty_dev, RPMSG_TTY_DEV_LEN, "%s%d",
		 RPMSG_TTY_DEV, tty_svc->tty_index);
#endif

	unlink(tty_svc->tty_dev);
	ret = symlink(pts_name, tty_svc->tty_dev);
	if (ret != 0)
#ifdef RPMSG_TTY_USE_CLIENT_NAME
		goto err_state;
#else
		goto err;
#endif

	/* keep open a handle to the slave to prevent EIO */
	slave_fd = open(pts_name, O_RDWR);
	if (slave_fd == -1) {
#ifdef RPMSG_TTY_USE_CLIENT_NAME
		unlink(tty_svc->tty_dev);
		goto err_state;
#else
		unlink(pts_name);
		goto err;
#endif
	}

	tty_svc->pty_master_fd = master_fd;
	tty_svc->pty_slave_fd = slave_fd;
	return 0;
#ifdef RPMSG_TTY_USE_CLIENT_NAME
err_state:
	if (client_state && tty_svc->tty_index >= 0) {
		idx_recycle(client_state, tty_svc->tty_index);
		tty_svc->tty_index = -1;
		tty_svc->client_idx_state = NULL;
	}
	if (client_state_refed) {
		ref_dec(client_state);
		client_state_refed = 0;
		client_state = NULL;
	}
#endif
err:
#ifndef RPMSG_TTY_USE_CLIENT_NAME
	if (tty_svc->tty_index >= 0 && tty_svc->tty_index < RPMSG_TTY_MAX_DEV)
		tty_id[tty_svc->tty_index] = -1;
	tty_svc->tty_index = -1;
#endif
	close(master_fd);
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
	client_name = client->ped_setup.name;
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
#ifdef RPMSG_TTY_USE_CLIENT_NAME
	if (tty_svc->client_idx_state && tty_svc->tty_index >= 0) {
		idx_recycle(tty_svc->client_idx_state, tty_svc->tty_index);
		ref_dec(tty_svc->client_idx_state);
		tty_svc->client_idx_state = NULL;
		tty_svc->tty_index = -1;
	}
#endif
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
	size_t len;

	metal_list_for_each(tty_dev_list, node) {
		tty_svc = metal_container_of(node, struct rpmsg_tty_service, node);
		len = strlen(str);
		if (len >= size)
			break;

		snprintf(str + len, size - len, "%s(%s) ",
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
