/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>

#include "openamp_module.h"


static int reserved_mem_init(struct client_os_inst *client)
{
    int binfd;
    struct stat buf;
    void *file_addr, *binaddr;
    int binsize;
    void *shmaddr = NULL;

    /* open clientos bin file */
    binfd = open(client->path, O_RDONLY);
    if (binfd < 0) {
        printf("open %s failed, binfd:%d\n", client->path, binfd);
        return -1;
    }

    printf("mcs fd:%d\n", client->mcs_fd);
    /* shared memory for virtio */
    shmaddr = mmap(NULL, client->shared_mem_size,
            PROT_READ | PROT_WRITE, MAP_SHARED, client->mcs_fd,
            client->phy_shared_mem);
    /* must be initialized to zero */
    memset(shmaddr, 0, client->shared_mem_size);

    /* memory for loading clientos bin file */
    fstat(binfd, &buf);
    binsize = PAGE_ALIGN(buf.st_size);

    /* check clientos must be in the range of shared mem */


    binaddr = mmap(NULL, binsize, PROT_READ | PROT_WRITE, MAP_SHARED,
                    client->mcs_fd, client->load_address);

    if (shmaddr < 0 || binaddr < 0) {
        printf("mmap reserved mem failed: shmaddr:%p, binaddr:%p\n",
                shmaddr, binaddr);
        return -1;
    }

    /* load clientos */
    file_addr = mmap(NULL, binsize, PROT_READ, MAP_PRIVATE, binfd, 0);
    memcpy(binaddr, file_addr, binsize);

    munmap(file_addr, binsize);
    munmap(binaddr, binsize);
    close(binfd);

    client->virt_shared_mem = shmaddr;
    client->vdev_status_reg = shmaddr;
    client->virt_tx_addr = shmaddr + client->shared_mem_size - client->vdev_status_size;
    client->virt_rx_addr = client->virt_tx_addr - client->vdev_status_size;

    return 0;
}

static void reserved_mem_release(struct client_os_inst *client)
{
    if (client->virt_shared_mem) {
        munmap(client->virt_shared_mem, client->shared_mem_size);
    }

    if (client->mcs_fd) {
        close(client->mcs_fd);
    }
}

int openamp_init(struct client_os_inst *client)
{
    int ret;
    struct remoteproc *rproc;
    int cpu_state;

    ret = open(MCS_DEVICE_NAME, O_RDWR | O_SYNC);
    if (ret < 0) {
        printf("open %s device failed\n", MCS_DEVICE_NAME);
        return ret;
    }
    client->mcs_fd = ret;

    ret = create_remoteproc(client);
    if (ret) {
        printf("create remoteproc failed\n");
        return -1;
    }

    ret = reserved_mem_init(client);
    if (ret) {
        printf("failed to init reserved mem\n");
        return ret;
    }

    virtio_init(client);
    rpmsg_sys_service_init();

    printf("start client os\n");
    ret = remoteproc_start(&client->rproc);
    if (ret) {
        printf("start processor failed\n");
        return ret;
    }

    return 0;
}

void openamp_deinit(struct client_os_inst *client)
{
    printf("\nOpenAMP demo ended.\n");

    destory_remoteproc(client); /* shutdown clientos first */
    virtio_deinit(client);
    reserved_mem_release(client);
}
