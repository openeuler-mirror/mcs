/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "mica_debug.h"
#include "rpmsg_pty.h"

#define UNIPROTON_SHARED_MEM_LENGTH 0x2000000
#define UNIPROTON_LOG_LENGTH 0x200000
#define UNIPROTON_SHARED_MEM_SHIFT (UNIPROTON_SHARED_MEM_LENGTH + UNIPROTON_LOG_LENGTH)

static struct client_os_inst client_os = {
    /* size of shared device mem */
    .shared_mem_size = 0x30000,
    .vring_size = VRING_SIZE,
    .vdev_status_size = VDEV_STATUS_SIZE,
};

/* flag to show if the mica is in debug mode */
bool g_is_debugging = false;

static void cleanup(int sig)
{
    if (g_is_debugging) {
        return;
    }
    rpmsg_app_stop();
    openamp_deinit(&client_os);
    exit(EXIT_SUCCESS);
}

int main(int argc, char **argv)
{
    int ret;
    int opt;
    char *cpu_id = NULL;
    char *target_binfile = NULL;
    char *target_binaddr = NULL;
    char *target_entry = NULL;
    char *target_elf = NULL;

    /* ctrl+c signal, do cleanup before program exit */
    signal(SIGINT, cleanup);

    while ((opt = getopt(argc, argv, "c:b:t:a:e::d:")) != -1) {
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
        case 'd':
            target_elf = optarg;
            g_is_debugging = true;
            break;
        case '?':
            printf("Unknown option: %c ",(char)optopt);
        default:
            break;
        }
    }

    // check for input validity
    bool is_valid = true;
    if (cpu_id == NULL) {
        printf("Usage: -c <id of the CPU running client OS>\n");
        is_valid = false;
    }
    if (target_binfile == NULL) {
        printf("Usage: -t <path to the target executable>\n");
        is_valid = false;
    }
    if (target_binaddr == NULL) {
        printf("Usage: -a <physical address for the executable to be put on>\n");
        is_valid = false;
    }
    if (g_is_debugging && target_elf == NULL) {
        printf("Usage: -d <path to the ELF file needed to be debugged>\n");
        is_valid = false;
    }
    if (is_valid == false) {
        return -1;
    }

    client_os.cpu_id = strtol(cpu_id, NULL, STR_TO_DEC);
    client_os.load_address = strtol(target_binaddr, NULL, STR_TO_HEX);
    client_os.entry = target_entry ? strtol(target_entry, NULL, STR_TO_HEX) :
                        client_os.load_address;
    client_os.path = target_binfile;

    /* clientos_map_info[LOG_TABLE].size + [SHAREMEM_TABLE].size */
    if (client_os.entry < UNIPROTON_SHARED_MEM_SHIFT) {
        printf("Error: target_binaddr is too small\n");
        return -1;
    }
    client_os.phy_shared_mem = client_os.entry - UNIPROTON_SHARED_MEM_SHIFT;

    printf("cpu:%d, ld:%lx, entry:%lx, path:%s share_mem:%lx\n",
        client_os.cpu_id,client_os.load_address, client_os.entry, client_os.path,
        client_os.phy_shared_mem);

    ret = openamp_init(&client_os);
    if (ret) {
        printf("openamp init failed:%d\n", ret);
        return ret;
    }
    ret = rpmsg_app_start(&client_os);
    if (ret) {
        printf("rpmsg app start failed: %d\n", ret);
        goto err_openamp_deinit;
    }

    if (g_is_debugging) {
        ret = debug_start(&client_os, target_elf);
        if (ret < 0) {
            printf("debug start failed\n");
        }
        // exit rpmsg app
        goto debug_exit;
    }
    printf("wait for rpmsg app exit\n");
    // blocked here in case automatically exit
    while (1) {
        sleep(1);
    }
debug_exit:
    rpmsg_app_stop();
err_openamp_deinit:
    openamp_deinit(&client_os);
    return ret;
}