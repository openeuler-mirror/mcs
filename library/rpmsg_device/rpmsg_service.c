/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <syslog.h>
#include <openamp/remoteproc_virtio.h>

#include <mica/mica.h>
#include <rpmsg/rpmsg_service.h>
#include <remoteproc/mica_rsc.h>

static METAL_DECLARE_LIST(remote_ept_list);

struct remote_ept
{
	char              name[RPMSG_NAME_SIZE];
	uint32_t          addr;
	uint32_t          dest_addr;
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
	struct mica_service *new_svc;

	if (client->rproc.state != RPROC_RUNNING)
		return -EPERM;

	new_svc = malloc(sizeof(*new_svc));
	if (!new_svc)
		return -ENOMEM;

	memcpy(new_svc, svc, sizeof(*new_svc));

	if (new_svc->init)
		new_svc->init(new_svc);

	if (new_svc->rpmsg_ns_match == NULL)
		goto out;

	if (new_svc->rpmsg_ns_bind_cb == NULL) {
		if (new_svc->remove)
			new_svc->remove(new_svc);
		free(new_svc);
		syslog(LOG_ERR, "%s failed: require rpmsg_ns_bind_cb() operation\n", __func__);
		return -EINVAL;
	}

	/* check if the service is registered by rpmsg name service */
	metal_list_for_each(&remote_ept_list, node) {
		r_ept = metal_container_of(node, struct remote_ept, node);

		if (new_svc->rpmsg_ns_match(client->rdev, r_ept->name, r_ept->addr, r_ept->dest_addr, new_svc->priv)) {
			new_svc->rpmsg_ns_bind_cb(client->rdev, r_ept->name, r_ept->addr, r_ept->dest_addr, new_svc->priv);
			DEBUG_PRINT("binding an already existing service. local: %s, remote: %s\n", new_svc->name, r_ept->name);
			tmp_node = node;
			node = tmp_node->prev;
			metal_list_del(tmp_node);
			metal_free_memory(r_ept);
		}
	}

out:
	DEBUG_PRINT("register service: %s\n", new_svc->name);
	metal_list_add_tail(&client->services, &new_svc->node);
	return 0;
}

void mica_unregister_all_services(struct mica_client *client)
{
	struct metal_list *node, *tmp_node;
	struct mica_service *svc;

	metal_list_for_each(&client->services, node) {
		svc = metal_container_of(node, struct mica_service, node);
		if (svc->remove)
			svc->remove(svc);
		tmp_node = node;
		node = tmp_node->prev;
		metal_list_del(tmp_node);
		free(svc);
	}
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
	int ret;

	rvdev = metal_container_of(rdev, struct rpmsg_virtio_device, rdev);
	rpvdev = metal_container_of(rvdev->vdev, struct remoteproc_virtio, vdev);
	rproc = rpvdev->priv;
	client = rproc->priv;

	DEBUG_PRINT("remote ept: name %s, dest: %d\n", name, dest);
	metal_list_for_each(&client->services, node) {
		svc = metal_container_of(node, struct mica_service, node);

		DEBUG_PRINT("local service: %s\n", svc->name);
		if (svc->rpmsg_ns_match == NULL)
			continue;
		if (svc->rpmsg_ns_bind_cb == NULL) {
			syslog(LOG_ERR, "%s failed: require rpmsg_ns_bind_cb() operation\n", __func__);
			return;
		}
		if (svc->rpmsg_ns_match(rdev, name, dest, RPMSG_ADDR_ANY, svc->priv)) {
			DEBUG_PRINT("binding service. local: %s, remote: %s\n", svc->name, name);
			svc->rpmsg_ns_bind_cb(rdev, name, dest, RPMSG_ADDR_ANY, svc->priv);
			/* Store endpoint information in RSC_VENDOR_EPT_TABLE */
			ret = rsc_update_ept_table(rproc, rdev);
			if (ret != 0)
				syslog(LOG_ERR, "Failed to update ept rsc table, ret %d", ret);
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

	r_ept->addr = dest;
	r_ept->dest_addr = RPMSG_ADDR_ANY;
	strlcpy(r_ept->name, name, RPMSG_NAME_SIZE);
	metal_list_add_tail(&remote_ept_list, &r_ept->node);
}

void register_remote_ept(const char *name, uint32_t addr, uint32_t dest_addr)
{
	struct remote_ept *r_ept;

	r_ept = metal_allocate_memory(sizeof(*r_ept));
	if (!r_ept) {
		syslog(LOG_ERR, "%s failed: remote_ept node creation failed, no memory\n", __func__);
		return;
	}

	DEBUG_PRINT("restore endpoint: %s, addr:%d, dest_addr: %d", name, addr, dest_addr);
	r_ept->addr = addr;
	r_ept->dest_addr = dest_addr;
	strlcpy(r_ept->name, name, RPMSG_NAME_SIZE);
	metal_list_add_tail(&remote_ept_list, &r_ept->node);
}
