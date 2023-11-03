#include <mqueue.h>
#include <stdio.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#include <string.h>
#include <sys/stat.h>
#include <stdlib.h>
#include <pthread.h>

#include <openamp/rpmsg_rpc_client_server.h>

// #include "openamp_module.h"

#include "ioctl_rpc.h"
#include "../rpc_server_internal.h"
#include "../rpc_internal_model.h"
#include "../../mica_demo/rpmsg_pty.h"

int ipc_handle_cmd_ioctl(cmd_base_req_t *req, unsigned int ep_id);
int ipc_handle_cmd_open(cmd_base_req_t *req, unsigned int ep_id);
int ipc_handle_cmd_close(cmd_base_req_t *req, unsigned int ep_id);

inline mqd_t mq_open_by_pid(pid_t pid)
{
    char respond_mq_name[64];
    sprintf(respond_mq_name, MQ_NAME_FORMATE, pid);
    lprintf("mq name: %s\n", respond_mq_name);
    return mq_open(respond_mq_name, O_WRONLY, 0, NULL);
}

static int process_cmd_request(cmd_base_req_t *req, unsigned int ep_id)
{
    int ret = 0;
    if (!req ) {
        return -1;
    }

    switch (req->func_id) {
        case IGH_IOCTL_ID:
            ret = ipc_handle_cmd_ioctl(req, ep_id);
            break;
        case IGH_OPEN_ID:
            ret = ipc_handle_cmd_open(req, ep_id);
            break;
        case IGH_CLOSE_ID:
            ret = ipc_handle_cmd_close(req, ep_id);
            break;
        default:
            lprintf("[ERROR] invalid func_id:%lu\n", req->func_id);
            ret = -1;
            break;
    }
    return ret;
}

static void *cmd_worker_thread(void *arg) {
    struct mq_attr attr = {0};
    struct pty_ep_data *pty_ep;
    pty_ep = (struct pty_ep_data *)arg;

    mqd_t mqd = mq_open(MQ_MASTER_NAME, O_CREAT | O_EXCL | O_RDONLY, S_IRUSR | S_IWUSR, NULL);

    if (mqd == -1 && errno == EEXIST) {
        mq_unlink(MQ_MASTER_NAME);
        lprintf("unlink and create\n");
        mqd = mq_open(MQ_MASTER_NAME, O_CREAT | O_EXCL | O_RDONLY, S_IRUSR | S_IWUSR, NULL);
    }
    if (mqd == -1) {
        lprintf("create fail %d, %s\n", errno, strerror(errno));
        return NULL;
    }

    mq_getattr(mqd, &attr);
    lprintf("max msg size: %ld, max msg num: %ld\n", attr.mq_msgsize, attr.mq_maxmsg);
    cmd_base_req_t *req = (cmd_base_req_t *)malloc(attr.mq_msgsize);
    if (req == NULL) {
        lprintf("alloc fail\n");
        goto exit_close_mq;
    }

    /* wait endpoint bound */
    while(!rpmsg_service_endpoint_is_bound(pty_ep->ep_id));

    while (1) {
        req->pid = 0;
        req->func_id = 0;
        lprintf("wait for msg...\n");
        mq_receive(mqd, (char *)req, attr.mq_msgsize, NULL);
        if (req->pid == 0 || !is_valid_cmd_func_id(req->func_id)) {
            lprintf("[ERROR] pid %d, func_id %lu", req->pid, req->func_id);
            // pid等于0, 或func_id不正确，未收到消息
            continue;
        }

        int ret = process_cmd_request(req, pty_ep->ep_id);
        if (ret >= 0) {
            // 处理成功，接收下一个消息
            continue;
        }

        // 处理失败，直接发送回复
        lprintf("[ERROR] process msg fail, ret:%d, %s\n", ret, strerror(errno));
        mqd_t mq_slave = mq_open_by_pid(req->pid);
        if (mq_slave == -1) {
            lprintf("[ERROR] open slave fail %d, %s\n", errno, strerror(errno));
            continue;
        }
        cmd_base_resp_t resp = {req->func_id, ret, errno};
        mq_send(mq_slave, (char *)&resp, sizeof(cmd_base_resp_t), 0);
        mq_close(mq_slave);
    }

    free(req);
exit_close_mq:
    mq_close(mqd);
    mq_unlink("/mq_0");
}

int cmd_workers_init(struct pty_ep_data *pty_ep)
{
    pthread_t thread;
    if (pthread_create(&thread, NULL, cmd_worker_thread, (void *)pty_ep) < 0) {
        printf("cmd thread create failed\n");
        return -1;
    }
    printf("cmd worker created\n");
    pthread_detach(thread);
}

int rpmsg_handle_cmd_ioctl(void *data, struct rpc_instance *inst, void *priv)
{
    rpc_cmd_ioctl_req_t *req = (rpc_cmd_ioctl_req_t *)data;
    int ret = 0;
    (void)inst;
    (void)priv;
    if (!req || !inst)
        return -EINVAL;
    lprintf("receive ioctl reply: pid:%d, func_id:%lu, resp_func_id:%lu, size:%d\n", req->cmd_pid, req->func_id,
        req->ioctl_respond.func_id, req->ioctl_respond.arg_size);

    mqd_t mq_slave = mq_open_by_pid(req->cmd_pid);
    if (mq_slave == -1) {
        lprintf("[ERROR] open slave fail %d, %s\n", errno, strerror(errno));
        return -1;
    }
    ret = mq_send(mq_slave, (char *)&req->ioctl_respond, sizeof(cmd_ioctl_resp_t) + req->ioctl_respond.arg_size, 0);
    mq_close(mq_slave);

    return ret;
}

int rpmsg_handle_cmd_base(void *data, struct rpc_instance *inst, void *priv)
{
    rpc_cmd_base_req_t *req = (rpc_cmd_base_req_t *)data;
    int ret = 0;
    (void)inst;
    (void)priv;
    if (!req || !inst)
        return -EINVAL;
    lprintf("receive base reply: pid:%d, func_id:%lu, resp_func_id:%lu\n", req->cmd_pid, req->func_id,
        req->base_respond.func_id);

    mqd_t mq_slave = mq_open_by_pid(req->cmd_pid);
    if (mq_slave == -1) {
        lprintf("[ERROR] open slave fail %d, %s\n", errno, strerror(errno));
        return -1;
    }
    ret = mq_send(mq_slave, (char *)&req->base_respond, sizeof(cmd_base_resp_t), 0);
    mq_close(mq_slave);

    return ret;
}

int ipc_handle_cmd_ioctl(cmd_base_req_t *req, unsigned int ep_id)
{
    int ret = 0;
    if (!req) {
        return -EINVAL;
    }
    cmd_ioctl_req_t *ioctl_req = (cmd_ioctl_req_t *)req;

    lprintf("receive ioctl request: pid:%d, func_id:%lu, request:%u, arg_size:%d\n",
        ioctl_req->pid, ioctl_req->func_id, ioctl_req->request, ioctl_req->arg_size);

    size_t payload_size = sizeof(cmd_ioctl_req_t) + ioctl_req->arg_size;
    if (ioctl_req->arg_size == 0) {
        payload_size += sizeof(void *);
    }

    cmd_ioctl_req_t *rpc_ioctl_req = (cmd_ioctl_req_t *)malloc(payload_size);
    if (rpc_ioctl_req == NULL) {
        return -1;
    }
    memcpy(rpc_ioctl_req, ioctl_req, payload_size);
    ret = rpc_server_send(ep_id, IGH_IOCTL_ID, RPMSG_RPC_OK, rpc_ioctl_req, payload_size);
    free(rpc_ioctl_req);
    return ret < 0 ? ret : 0;
}

int ipc_handle_cmd_open(cmd_base_req_t *req, unsigned int ep_id)
{
    int ret = 0;
    if (!req) {
        return -EINVAL;
    }
    lprintf("receive open request: pid:%d, func_id:%lu\n", req->pid, req->func_id);

    size_t payload_size = sizeof(cmd_base_req_t);

    cmd_base_req_t *rpc_base_req = (cmd_base_req_t *)malloc(payload_size);
    if (rpc_base_req == NULL) {
        return -1;
    }
    memcpy(rpc_base_req, req, sizeof(cmd_base_req_t));

    /* Transmit rpc response */
    lprintf("send open request: pid:%d, func_id:%lu, size:%lu\n", rpc_base_req->pid,
        rpc_base_req->func_id, payload_size);
    ret = rpc_server_send(ep_id, req->func_id, RPMSG_RPC_OK, rpc_base_req, payload_size);
    free(rpc_base_req);
    return ret < 0 ? ret : 0;
}

int ipc_handle_cmd_close(cmd_base_req_t *req, unsigned int ep_id)
{
    int ret = 0;
    if (!req) {
        return -EINVAL;
    }
    lprintf("receive close request: pid:%d, func_id:%lu\n", req->pid, req->func_id);

    size_t payload_size = sizeof(cmd_close_req_t);
    
    cmd_close_req_t *rpc_close_req = (cmd_close_req_t *)malloc(payload_size);
    if (rpc_close_req == NULL) {
        return -1;
    }
    memcpy(rpc_close_req, req, sizeof(cmd_close_req_t));

    /* Transmit rpc response */
    lprintf("send close request: pid:%d, func_id:%lu, fd:%d, size:%lu\n", rpc_close_req->pid,
        rpc_close_req->func_id, rpc_close_req->fd, payload_size);
    ret = rpc_server_send(ep_id, req->func_id, RPMSG_RPC_OK, rpc_close_req, payload_size);
    free(rpc_close_req);
    return ret < 0 ? ret : 0;
}
