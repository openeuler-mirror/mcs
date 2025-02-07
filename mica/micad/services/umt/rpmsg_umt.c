#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <poll.h>
#include "../rpc/rpc_server_internal.h"
#include "rpmsg_umt.h"

#define RPMSG_UMT_NAME "rpmsg-umt"

METAL_DECLARE_LIST(g_umt_list);

static void rpmsg_umt_unbind(struct rpmsg_endpoint *ept)
{
	char tmp_name[64] = {0};
	struct rpmsg_umt_service *umt_svc = ept->priv;

	metal_list_del(&umt_svc->node);
	rpmsg_destroy_ept(&umt_svc->ept);
	sem_destroy(&umt_svc->sem);
	pthread_mutex_destroy(&umt_svc->lock);

	munmap(umt_svc->process_shared_memory, sizeof(process_shared_data_t));
	sprintf(tmp_name, SHM_NAME, umt_svc->instance_id);
	shm_unlink(tmp_name);

	sem_close(umt_svc->sem_user_to_micad);
	sprintf(tmp_name, SEM_USER_TO_MICAD, umt_svc->instance_id);
	sem_unlink(tmp_name);

	sem_close(umt_svc->sem_micad_to_user);
	sprintf(tmp_name, SEM_MICAD_TO_USER, umt_svc->instance_id);
	sem_unlink(tmp_name);

	umt_svc->active  = 0;
}

int rpmsg_rx_umt_callback(struct rpmsg_endpoint *ept, void *data, size_t len, uint32_t src, void *priv)
{
	struct rpmsg_umt_service *umt_svc = priv;

	memcpy(umt_svc->process_shared_memory->rcv_buffer, data, len);
	umt_svc->process_shared_memory->rcv_data_len = len;
    sem_post(umt_svc->sem_micad_to_user);
	return RPMSG_SUCCESS;

}

void *rpmsg_umt_tx_task(void *arg)
{
	struct rpmsg_umt_service *umt_svc = arg;
	process_shared_data_t *shared_data = umt_svc->process_shared_memory;
	umt_svc->active = 1;
	umt_send_msg_t msg = {0};

	while(umt_svc->active) {
		/* 等待用户发送消息 */
		sem_wait(umt_svc->sem_user_to_micad);
		msg.phy_addr = shared_data->phy_addr;
		msg.data_len = shared_data->data_len;
		rpmsg_send(&umt_svc->ept, &msg, sizeof(msg));
	}

	free(umt_svc);
	pthread_exit(NULL);
}

static void umt_service_init(struct rpmsg_device *rdev, const char *name, uint32_t remote_addr, uint32_t remote_dest, void *priv)
{
	int ret;
	pthread_t umt_thread;
	struct rpmsg_virtio_device *rvdev;
	struct remoteproc_virtio *rpvdev;
	struct remoteproc *rproc;
	struct mica_client *client;
	struct rpmsg_umt_service *umt_svc;
	char message[] = "first message from umt_service!";
	umt_send_msg_t msg = {0};

	umt_svc = malloc(sizeof(struct rpmsg_umt_service));
	if (!umt_svc)
		return;
	umt_svc->ept.priv = umt_svc;
	/**
	 * Create the corresponding rpmsg endpoint
	 *
	 * endpoint callback: rpmsg_rx_umt_callback
	 * endpoint unbind function: rpmsg_umt_unbind
	 */
	ret = rpmsg_create_ept(&umt_svc->ept, rdev, name, remote_dest, remote_addr,
			rpmsg_rx_umt_callback, rpmsg_umt_unbind);
	if (ret)
		goto free_mem;

	rvdev = metal_container_of(rdev, struct rpmsg_virtio_device, rdev);
	rpvdev = metal_container_of(rvdev->vdev, struct remoteproc_virtio, vdev);
	rproc = rpvdev->priv;
	client = metal_container_of(rproc, struct mica_client, rproc);
	umt_svc->instance_id = 0; /* 当前不支持多实例，这里先赋值为0，等支持以后修改成具体实例号 */
	sem_init(&umt_svc->sem, 0, 0);
    pthread_mutex_init(&umt_svc->lock, NULL);
	metal_list_add_tail(&g_umt_list, &umt_svc->node);
    msg.data_len = sizeof(message);
	msg.phy_addr = 0x0;
	rpmsg_send(&umt_svc->ept, &msg, sizeof(msg));

	/* 初始化共享内存 */
	umt_svc->process_shared_memory = init_process_shared_memory(0);
	if (umt_svc->process_shared_memory == NULL)
		goto free_ept;
	umt_svc->process_shared_memory->lock = 0;
	umt_svc->process_shared_memory->instance_id = 0; /* 当前不支持多实例，这里先赋值为0，等支持以后修改成具体实例号 */
	/* 创建于用户进程通信的信号量 */
	ret = create_sem(0, &umt_svc->sem_user_to_micad, &umt_svc->sem_micad_to_user);
	if (ret)
		goto free_ept;

	ret = pthread_create(&umt_thread, NULL, rpmsg_umt_tx_task, umt_svc);
	if (ret)
		goto free_ept;

	ret = pthread_detach(umt_thread);
	if (ret)
		goto free_pthread;

	return;

free_pthread:
	pthread_cancel(umt_thread);
free_ept:
	rpmsg_destroy_ept(&umt_svc->ept);
	metal_list_del(&umt_svc->node);
	if(umt_svc && umt_svc->process_shared_memory)
		munmap(umt_svc->process_shared_memory, sizeof(process_shared_data_t));
	if(umt_svc->sem_micad_to_user != SEM_FAILED)
		sem_close(umt_svc->sem_micad_to_user);
	if(umt_svc->sem_user_to_micad != SEM_FAILED)
		sem_close(umt_svc->sem_user_to_micad);
free_mem:
	free(umt_svc);
}

static bool umt_name_match(struct rpmsg_device *rdev, const char *name,
	uint32_t remote_addr, uint32_t remote_dest, void *priv)
{
	return !strcmp(name, RPMSG_UMT_NAME);
}

static void remove_umt_service(struct mica_client *client, struct mica_service *svc)
{
	struct rpmsg_umt_service *umt_svc;
	struct metal_list *node, *tmp_node;

	metal_list_for_each(&g_umt_list, node) {
		umt_svc = metal_container_of(node, struct rpmsg_umt_service, node);
		/* 当前不支持多实例，这里实例号应该等于0，支持以后等于实际实例号 */
		if (umt_svc->instance_id == 0) {
			rpmsg_umt_unbind(&umt_svc->ept);
			return;
		}
		tmp_node = node;
		node = tmp_node->prev;
	}

	return;

}

static struct mica_service rpmsg_umt_service = {
	.name = RPMSG_UMT_NAME,
	.rpmsg_ns_match = umt_name_match,
	.rpmsg_ns_bind_cb = umt_service_init,
	.remove = remove_umt_service,
};

int create_rpmsg_umt_service(struct mica_client *client)
{
	return mica_register_service(client, &rpmsg_umt_service);
}