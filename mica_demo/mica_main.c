/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "mica/mica.h"
#include "mica_debug.h"
#include "rpmsg_pty.h"

static struct mica_client client_os = {
	/* physical address start of shared device mem */
	.static_mem_base = 0x70000000,
	/* size of shared device mem */
	.static_mem_size = 0x30000,
};

/* flag to show if the mica is in debug mode */
bool g_is_debugging = false;

static void cleanup(int sig)
{
	if (g_is_debugging)
		return;

	// openamp_deinit(&client_os);
	exit(EXIT_SUCCESS);
}

int main(int argc, char **argv)
{
	int ret;
	int opt;
	char *cpu_id = NULL;
	char *target_exe_file = NULL;

	/* ctrl+c signal, do cleanup before program exit */
	signal(SIGINT, cleanup);

	while ((opt = getopt(argc, argv, "c:b:t:d")) != -1) {
		switch (opt) {
		case 'c':
			cpu_id = optarg;
			break;
		case 't':
			target_exe_file = optarg;
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
	if (is_valid == false)
		return -1;

	client_os.cpu_id = strtol(cpu_id, NULL, STR_TO_DEC);
	client_os.path = target_exe_file;
	client_os.mode = RPROC_MODE_BARE_METAL;

	ret = mica_start(&client_os);
	if (ret) {
		printf("mica start failed:%d\n", ret);
		return ret;
	}

	ret = create_rpmsg_tty(&client_os);
	if (ret)
		printf("create_rpmsg_tty failed: %d\n", ret);

	while (1)
		sleep(1);

	return ret;
}
