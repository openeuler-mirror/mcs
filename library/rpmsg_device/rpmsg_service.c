/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <pthread.h>
#include <stdio.h>
#include <syslog.h>
#include <openamp/remoteproc_virtio.h>

#include <mica/mica.h>
#include <rpmsg/rpmsg_service.h>
#include <remoteproc/mica_rsc.h>

static METAL_DECLARE_LIST(remote_ept_list);
static pthread_mutex_t remote_ept_lock = PTHREAD_MUTEX_INITIALIZER;

struct remote_ept {
	char              name[RPMSG_NAME_SIZE];
	uint32_t          addr;
	uint32_t          dest_addr;
	struct mica_client *client;
	struct metal_list node;
};

static void take_remote_epts(struct mica_client *client, struct metal_list *pending)
{
	struct metal_list *node, *next;

	pthread_mutex_lock(&remote_ept_lock);
	for (node = remote_ept_list.next; node != &remote_ept_list; node = next) {
		struct remote_ept *ept = metal_container_of(node, struct remote_ept, node);

		next = node->next;
		if (ept->client == client) {
			metal_list_del(node);
			metal_list_add_tail(pending, node);
		}
	}
	pthread_mutex_unlock(&remote_ept_lock);
}

void clear_remote_epts(struct mica_client *client)
{
	METAL_DECLARE_LIST(pending);
	struct metal_list *node;

	take_remote_epts(client, &pending);
	while ((node = metal_list_first(&pending)) != NULL) {
		metal_list_del(node);
		metal_free_memory(metal_container_of(node, struct remote_ept, node));
	}
}

void print_device_of_service(struct mica_client *client, char *str, size_t size)
{
	struct metal_list *node;
	struct mica_service *svc;
	size_t len;

	metal_list_for_each(&client->services, node) {
		svc = metal_container_of(node, struct mica_service, node);
		len = strlen(str);
		if (len >= size)
			break;

		if (svc->get_match_device != NULL) {
			svc->get_match_device(str + len, size - len, svc->priv);
		} else {
			snprintf(str + len, size - len, "%s ", svc->name);
		}
	}
}

int mica_register_service(struct mica_client *client, struct mica_service *svc)
{
	METAL_DECLARE_LIST(pending);
	struct metal_list *node, *next;
	struct remote_ept *r_ept;
	struct mica_service *new_svc;
	int ret;

	if (client->rproc.state != RPROC_RUNNING)
		return -EPERM;

	new_svc = malloc(sizeof(*new_svc));
	if (!new_svc)
		return -ENOMEM;

	memcpy(new_svc, svc, sizeof(*new_svc));

	if (new_svc->init)
		new_svc->init(client, new_svc);

	if (new_svc->rpmsg_ns_match == NULL)
		goto out;

	if (new_svc->rpmsg_ns_bind_cb == NULL) {
		if (new_svc->remove)
			new_svc->remove(client, new_svc);
		free(new_svc);
		syslog(LOG_ERR, "%s failed: require rpmsg_ns_bind_cb() operation\n", __func__);
		return -EINVAL;
	}

	/* Take only this client's announcements; call service code unlocked. */
	take_remote_epts(client, &pending);
	for (node = pending.next; node != &pending; node = next) {
		next = node->next;
		r_ept = metal_container_of(node, struct remote_ept, node);

		if (new_svc->rpmsg_ns_match(client->rdev, r_ept->name, r_ept->addr, r_ept->dest_addr, new_svc->priv)) {
			new_svc->rpmsg_ns_bind_cb(client->rdev, r_ept->name, r_ept->addr, r_ept->dest_addr, new_svc->priv);
			DEBUG_PRINT("binding an already existing service. local: %s, remote: %s\n", new_svc->name, r_ept->name);
			ret = rsc_update_ept_table(&client->rproc, client->rdev);
			if (ret != 0)
				syslog(LOG_ERR, "Failed to update ept rsc table, ret %d", ret);
			metal_list_del(node);
			metal_free_memory(r_ept);
		}
	}
	/* Requeue unmatched entries ahead of announcements that arrived meanwhile. */
	pthread_mutex_lock(&remote_ept_lock);
	while (!metal_list_is_empty(&pending)) {
		node = pending.prev;
		metal_list_del(node);
		metal_list_add_head(&remote_ept_list, node);
	}
	pthread_mutex_unlock(&remote_ept_lock);

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
			svc->remove(client, svc);
		tmp_node = node;
		node = tmp_node->prev;
		metal_list_del(tmp_node);
		free(svc);
	}
}

void mica_ns_bind_cb(struct rpmsg_device *rdev, const char *name, uint32_t dest)
{
	struct mica_service *svc;
	struct rpmsg_virtio_device *rvdev;
	struct remoteproc_virtio *rpvdev;
	struct remoteproc *rproc;
	struct mica_client *client;
	struct metal_list *node;
	int ret;

	rvdev = metal_container_of(rdev, struct rpmsg_virtio_device, rdev);
	rpvdev = metal_container_of(rvdev->vdev, struct remoteproc_virtio, vdev);
	rproc = rpvdev->priv;
	client = metal_container_of(rproc, struct mica_client, rproc);

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
	register_remote_ept(client, name, dest, RPMSG_ADDR_ANY);
}

void register_remote_ept(struct mica_client *client, const char *name,
			 uint32_t addr, uint32_t dest_addr)
{
	struct remote_ept *r_ept;

	r_ept = metal_allocate_memory(sizeof(*r_ept));
	if (!r_ept) {
		syslog(LOG_ERR, "%s failed: remote_ept node creation failed, no memory\n", __func__);
		return;
	}

	DEBUG_PRINT("queue endpoint: %s, addr:%u, dest_addr: %u", name, addr, dest_addr);
	r_ept->client = client;
	r_ept->addr = addr;
	r_ept->dest_addr = dest_addr;
	strlcpy(r_ept->name, name, RPMSG_NAME_SIZE);
	pthread_mutex_lock(&remote_ept_lock);
	metal_list_add_tail(&remote_ept_list, &r_ept->node);
	pthread_mutex_unlock(&remote_ept_lock);
}
