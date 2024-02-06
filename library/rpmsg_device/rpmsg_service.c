/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <syslog.h>
#include <openamp/remoteproc_virtio.h>

#include "mica/mica.h"
#include "rpmsg/rpmsg_service.h"

static METAL_DECLARE_LIST(remote_ept_list);

struct remote_ept
{
	char              name[RPMSG_NAME_SIZE];
	uint32_t          dest;
	struct metal_list node;
};

void print_device_of_service(struct mica_client *client, char *str, size_t size)
{
	struct metal_list *node;
	struct mica_service *svc;

	metal_list_for_each(&client->services, node) {
		svc = metal_container_of(node, struct mica_service, node);

		if (svc->get_match_device != NULL)
			svc->get_match_device(str + strlen(str), size - strlen(str), svc->priv);
		else
			snprintf(str + strlen(str), size - strlen(str), "%s", svc->name);
	}
}

int mica_register_service(struct mica_client *client, struct mica_service *svc)
{
	struct metal_list *node, *tmp_node;
	struct remote_ept *r_ept;
	void *priv = svc->priv;

	if (client->rproc.state != RPROC_RUNNING)
		return -EPERM;

	if (svc->init)
		svc->init(priv);

	if (svc->rpmsg_ns_match == NULL)
		return 0;

	if (svc->rpmsg_ns_bind_cb == NULL) {
		syslog(LOG_ERR, "%s failed: require rpmsg_ns_bind_cb() operation\n", __func__);
		return -EINVAL;
	}

	/* check if the service is registered by rpmsg name service */
	metal_list_for_each(&remote_ept_list, node) {
		r_ept = metal_container_of(node, struct remote_ept, node);

		if (svc->rpmsg_ns_match(client->rdev, r_ept->name, r_ept->dest, priv)) {
			svc->rpmsg_ns_bind_cb(client->rdev, r_ept->name, r_ept->dest, priv);
			tmp_node = node;
			node = tmp_node->prev;
			metal_list_del(tmp_node);
			metal_free_memory(r_ept);
		}
	}

	metal_list_add_tail(&client->services, &svc->node);
	return 0;
}

void mica_ns_bind_cb(struct rpmsg_device *rdev, const char *name, uint32_t dest)
{
	struct mica_service *svc;
	struct remote_ept *r_ept;
	struct rpmsg_virtio_device *rvdev;
	struct remoteproc_virtio *rpvdev;
	struct remoteproc *rproc;
	struct mica_client *client;
	struct metal_list *node;

	rvdev = metal_container_of(rdev, struct rpmsg_virtio_device, rdev);
	rpvdev = metal_container_of(rvdev->vdev, struct remoteproc_virtio, vdev);
	rproc = rpvdev->priv;
	client = rproc->priv;

	DEBUG_PRINT("remote ept: name %s, dest: %d\n", name, dest);
	metal_list_for_each(&client->services, node) {
		svc = metal_container_of(node, struct mica_service, node);

		DEBUG_PRINT("binding service. local: %s, remote: %s\n", svc->name, name);
		if (svc->rpmsg_ns_match == NULL)
			continue;
		if (svc->rpmsg_ns_bind_cb == NULL) {
			syslog(LOG_ERR, "%s failed: require rpmsg_ns_bind_cb() operation\n", __func__);
			return;
		}
		if (svc->rpmsg_ns_match(rdev, name, dest, svc->priv)) {
			svc->rpmsg_ns_bind_cb(rdev, name, dest, svc->priv);
			return;
		}
	}

	/*
	 * If no service is matched, append this endpoint to
	 * remote_ept_list and wait for a register.
	 */
	r_ept = metal_allocate_memory(sizeof(*r_ept));
	if (!r_ept) {
		syslog(LOG_ERR, "%s failed: remote_ept node creation failed, no memory\n", __func__);
		return;
	}

	r_ept->dest = dest;
	strlcpy(r_ept->name, name, RPMSG_NAME_SIZE);
	metal_list_add_tail(&remote_ept_list, &r_ept->node);
}
