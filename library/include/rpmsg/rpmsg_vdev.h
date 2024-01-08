/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RPMSG_VDEV_H
#define RPMSG_VDEV_H

#include "mica/mica_client.h"

#if defined __cplusplus
extern "C" {
#endif

int create_rpmsg_device(struct client_os_inst *client);

#if defined __cplusplus
}
#endif

#endif	/* RPMSG_VDEV_H */
