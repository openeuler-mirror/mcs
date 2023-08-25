/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <metal/alloc.h>
#include <metal/io.h>
#include <sys/ioctl.h>

#include "openamp_module.h"


static struct remoteproc *rproc_init(struct remoteproc *rproc,
                                     const struct remoteproc_ops *ops, void *args)
{
    rproc->ops = ops;
    rproc->priv = args;
    rproc->state = RPROC_READY;

    return rproc;
}

static void rproc_remove(struct remoteproc *rproc)
{

}

static int rproc_start(struct remoteproc *rproc)
{
    int ret;
    struct client_os_inst *client = (struct client_os_inst *)rproc->priv;
    struct cpu_info info = {
        .cpu = client->cpu_id,
        .boot_addr = client->entry
    };

    ret = ioctl(client->mcs_fd, IOC_CPUON, &info);
    if (ret < 0) {
        printf("boot clientos failed\n");
        return ret;
    }

    return 0;
}

static int rproc_stop(struct remoteproc *rproc)
{
    /* TODO: send order to clientos by RPC service, clientos shut itself down by PSCI */
    printf("stop rproc\n");

    sys_service_power_off(0);

    return 0;
}

const struct remoteproc_ops rproc_ops = {
    .init = rproc_init,
    .remove = rproc_remove,
    .start = rproc_start,
    .stop = rproc_stop,
};

int create_remoteproc(struct client_os_inst *client)
{
    int ret;
    struct remoteproc *rproc;
    struct cpu_info info = {
        .cpu = client->cpu_id
    };

    ret = ioctl(client->mcs_fd, IOC_AFFINITY_INFO, &info);
    if (ret < 0) {
        printf("acquire cpu state failed\n");
        return -1;
    }

    rproc = remoteproc_init(&client->rproc, &rproc_ops, client);
    if (rproc == NULL) {
        printf("remoteproc init failed\n");
        return -1;
    }

    return 0;
}

void destory_remoteproc(struct client_os_inst *client)
{
    if (client == NULL) {
        return;
    }

    remoteproc_stop(&client->rproc);
    client->rproc.state = RPROC_OFFLINE;

    remoteproc_remove(&client->rproc);
}
