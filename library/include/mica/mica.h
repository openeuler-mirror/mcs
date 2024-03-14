/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_H
#define MICA_H

#include <openamp/open_amp.h>
#include <openamp/remoteproc.h>
#include <openamp/rpmsg_virtio.h>

#include "mica/mica_client.h"
#include "memory/shm_pool.h"
#include "rpmsg/rpmsg_service.h"

#if defined __cplusplus
extern "C" {
#endif

int mica_create(struct mica_client *client);
int mica_start(struct mica_client *client);
const char *mica_status(struct mica_client *client);

/* register a rpmsg/user-defined service */
int mica_register_service(struct mica_client *client, struct mica_service *svc);

/* unregister all the registered services */
void mica_unregister_all_services(struct mica_client *client);

void mica_print_service(struct mica_client *client, char *str, size_t size);
void print_device_of_service(struct mica_client *client, char *str, size_t size);

#if defined __cplusplus
}
#endif

#endif	/* MICA_H */
