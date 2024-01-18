/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_H
#define MICA_H

#include <openamp/remoteproc.h>
#include <openamp/rpmsg_virtio.h>

#include "mica/mica_client.h"
#include "memory/shm_pool.h"
#include "rpmsg/rpmsg_service.h"

#if defined __cplusplus
extern "C" {
#endif

int mica_start(struct mica_client *client);

#if defined __cplusplus
}
#endif

#endif	/* MICA_H */
