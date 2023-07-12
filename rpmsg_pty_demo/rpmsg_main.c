/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <stdarg.h>
#include <pthread.h>
#include "rpmsg_pty.h"

#include "openamp_module.h"

static struct client_os_inst client_os = {
    /* physical address start of shared device mem */
    .phy_shared_mem = 0x3fde00000,
    /* size of shared device mem */
    .shared_mem_size = 0x30000,
    .vring_size = VRING_SIZE,
    .vdev_status_size = VDEV_STATUS_SIZE,
};

static void cleanup(int sig)
{
    openamp_deinit(&client_os);
    exit(0);
}

int rpmsg_app_master(struct client_os_inst *client)
{
    struct pty_ep_data *pty_shell1;

    pty_shell1 = pty_service_create("uart");

    if (pty_shell1 == NULL) {
        return -1;
    }

    struct pty_ep_data *pty_shell2;
    pty_shell2 = pty_service_create("console");

    if (pty_shell2 == NULL) {
        return -1;
    }

    rpmsg_service_receive_loop(client);

    return 0;
}

int main(int argc, char **argv)
{
    int ret;
    int opt;
    char *cpu_id;
    char *target_binfile;
    char *target_binaddr;
    char *target_entry = NULL;

    /* ctrl+c signal, do cleanup before program exit */
    signal(SIGINT, cleanup);

    /* \todo: parameter check */
    while ((opt = getopt(argc, argv, "c:b:t:a:e::")) != -1) {
        switch (opt) {
        case 'c':
            cpu_id = optarg;
            break;
        case 'b':
            if (strlen(optarg) > sizeof(client_os.boot_bin_path) - 1) {
                printf("Error: boot_bin path string is too long\n");
                return -1;
            }
            strcpy(client_os.boot_bin_path, optarg);
            break;
        case 't':
            target_binfile = optarg;
            break;
        case 'a':
            target_binaddr = optarg;
            break;
        case 'e':
            target_entry = optarg;
            break;
        case '?':
            printf("Unknown option: %c ",(char)optopt);
        default:
            break;
        }
    }

    client_os.cpu_id = strtol(cpu_id, NULL, STR_TO_DEC);
    client_os.load_address = strtol(target_binaddr, NULL, STR_TO_HEX);
    client_os.entry = target_entry ? strtol(target_entry, NULL, STR_TO_HEX) :
                        client_os.load_address;
    client_os.path = target_binfile;

    printf("cpu:%d, ld:%lx, entry:%lx, path:%s\n",
        client_os.cpu_id,client_os.load_address, client_os.entry, client_os.path);

    ret = openamp_init(&client_os);
    if (ret) {
        printf("openamp init failed: %d\n", ret);
        return ret;
    }

    ret = rpmsg_app_master(&client_os);
    if (ret) {
        printf("rpmsg app master failed: %d\n", ret);
        openamp_deinit(&client_os);
        return ret;
    }

    openamp_deinit(&client_os);

    return 0;
}
