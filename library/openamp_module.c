/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include "openamp_module.h"
#include "mica_elf_loader.h"

static int reserved_mem_init(struct client_os_inst *client)
{
    int bin_fd;
    struct stat buf;
    void *file_addr, *sh_bin_addr;
    int bin_size;
    void *sh_mem_addr;
    int err;

    /* shared memory for virtio */
    sh_mem_addr = mmap(NULL, client->shared_mem_size,
            PROT_READ | PROT_WRITE, MAP_SHARED, client->mcs_fd,
            client->phy_shared_mem);
    if (sh_mem_addr == MAP_FAILED) {
        printf("mmap failed: sh_mem_addr:%p\n", sh_mem_addr);
        return -EPERM;
    }
    /* must be initialized to zero */
    memset(sh_mem_addr, 0, client->shared_mem_size);

    /* open clientos bin file from Linux file system */
    bin_fd = open(client->path, O_RDONLY);
    if (bin_fd < 0) {
        printf("open %s failed, bin_fd:%d\n", client->path, bin_fd);
        err = bin_fd;
        goto err_ummap_share_mem;
    }
    /* memory for loading clientos bin file */
    fstat(bin_fd, &buf);
    bin_size = PAGE_ALIGN(buf.st_size);

    /* the address of bin file in Linux */
    file_addr = mmap(NULL, bin_size, PROT_READ, MAP_PRIVATE, bin_fd, 0);
    if (file_addr == MAP_FAILED) {
        printf("mmap failed: file_addr: %p\n", file_addr);
        err = -errno;
        goto err_close_bin_fd;
    }

    void *e_entry = elf_image_load(file_addr, client->mcs_fd, (char *)client->load_address);
    if (e_entry == NULL) {
        if (errno) {
            printf("load elf failed\n");
            goto err_ummap_bin_file;
        }
        printf("input executable is not in ELF format\n");
        /* the address in the shared memory to put bin file */
        sh_bin_addr = mmap(NULL, bin_size, PROT_READ | PROT_WRITE, MAP_SHARED,
                        client->mcs_fd, client->load_address);
        if (sh_bin_addr == MAP_FAILED) {
            printf("mmap reserved mem failed: sh_bin_addr:%p\n",
                    sh_bin_addr);
            err = -errno;
            goto err_ummap_bin_file;
        }

        /* load clientos */
        memcpy(sh_bin_addr, file_addr, bin_size);
        munmap(sh_bin_addr, bin_size);
    } else
        client->load_address = (intptr_t)e_entry;
    
    /* unmap bin file, both from the Linux and shared memory */
    close(bin_fd);
    munmap(file_addr, bin_size);

    client->virt_shared_mem = sh_mem_addr;
    client->vdev_status_reg = sh_mem_addr;
    client->virt_tx_addr = sh_mem_addr + client->shared_mem_size - client->vdev_status_size;
    client->virt_rx_addr = client->virt_tx_addr - client->vdev_status_size;

    return 0;

err_ummap_bin_file:
    munmap(file_addr, bin_size);
err_close_bin_fd:
    close(bin_fd);
err_ummap_share_mem:
    munmap(sh_mem_addr, client->phy_shared_mem);
    return err;
}

static void reserved_mem_release(struct client_os_inst *client)
{
    if (client->virt_shared_mem) {
        munmap(client->virt_shared_mem, client->shared_mem_size);
    }

    if (client->mcs_fd >= 0) {
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
    if (ret < 0) {
        printf("create remoteproc failed\n");
        goto err_close_fd;
    }

    ret = reserved_mem_init(client);
    if (ret < 0) {
        printf("failed to init reserved mem\n");
        goto err_close_fd;
    }

    virtio_init(client);
    rpmsg_sys_service_init();

    printf("start client os\n");
    ret = remoteproc_start(&client->rproc);
    if (ret < 0) {
        printf("start processor failed\n");
        goto err_close_fd;
    }

    return 0;

err_close_fd:
    close(client->mcs_fd);
    return ret;
}

void openamp_deinit(struct client_os_inst *client)
{
    printf("\nOpenAMP demo ended.\n");

    destory_remoteproc(client); /* shutdown clientos first */
    virtio_deinit(client);
    reserved_mem_release(client);
}
