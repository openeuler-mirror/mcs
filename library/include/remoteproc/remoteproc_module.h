/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef REMOTEPROC_MODULE_H
#define REMOTEPROC_MODULE_H

#include "mica/mica_client.h"

#if defined __cplusplus
extern "C" {
#endif

#define CPU_STATE_ON          0
#define CPU_STATE_OFF         1
#define CPU_STATE_ON_PENDING  2

struct img_store
{
	FILE *file;
	char *buf;
};

/* create remoteproc */
int create_client(struct client_os_inst *client);
int load_client_image(struct client_os_inst *client);
int start_client(struct client_os_inst *client);

/* destory remoteproc */
void destory_client(struct client_os_inst *client);

#if defined __cplusplus
}
#endif

#endif	/* REMOTEPROC_MODULE_H */
