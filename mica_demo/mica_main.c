/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "mica_debug.h"
#include "rpmsg_pty.h"

static struct client_os_inst client_os = {
	/* physical address start of shared device mem */
	.phy_shared_mem = 0x70000000,
	/* size of shared device mem */
	.shared_mem_size = 0x30000,
	.vring_size = VRING_SIZE,
	.vdev_status_size = VDEV_STATUS_SIZE,
};

/* flag to show if the mica is in debug mode */
bool g_is_debugging = false;

static void cleanup(int sig)
{
	if (g_is_debugging)
		return;

	rpmsg_app_stop();
	openamp_deinit(&client_os);
	exit(EXIT_SUCCESS);
}

int main(int argc, char **argv)
{
	int ret;
	int opt;
	char *cpu_id = NULL;
	char *target_exe_file = NULL;
	char *target_exe_addr = NULL;
	char *target_entry = NULL;

	/* ctrl+c signal, do cleanup before program exit */
	signal(SIGINT, cleanup);

	while ((opt = getopt(argc, argv, "c:b:t:a:e::d")) != -1) {
		switch (opt) {
		case 'c':
			cpu_id = optarg;
			break;
		case 't':
			target_exe_file = optarg;
			break;
		case 'a':
			target_exe_addr = optarg;
			break;
		case 'e':
			target_entry = optarg;
			break;
		case 'd':
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
	if (target_exe_file == NULL) {
		printf("Usage: -t <path to the target executable>\n");
		is_valid = false;
	}
	if (target_exe_addr == NULL) {
		printf("Usage: -a <physical address for the executable to be put on>\n");
		is_valid = false;
	}
	if (is_valid == false)
		return -1;

	client_os.cpu_id = strtol(cpu_id, NULL, STR_TO_DEC);
	client_os.load_address = strtol(target_exe_addr, NULL, STR_TO_HEX);
	client_os.entry = target_entry ? strtol(target_entry, NULL, STR_TO_HEX) :
				client_os.load_address;
	client_os.path = target_exe_file;

	/* init openamp for RPMsg communication */
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
		ret = debug_start(&client_os, target_exe_file);
		if (ret < 0)
			printf("debug start failed\n");

		g_is_debugging = false;
	}
	printf("wait for rpmsg app exit\n");
	// blocked here in case automatically exit
	while (1)
		sleep(1);

	rpmsg_app_stop();
err_openamp_deinit:
	openamp_deinit(&client_os);
	return ret;
}
