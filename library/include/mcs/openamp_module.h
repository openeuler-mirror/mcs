/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef OPENAMP_MODULE_H
#define OPENAMP_MODULE_H

#include <openamp/open_amp.h>

#include "mcs_common.h"
#include "remoteproc_module.h"
#include "virtio_module.h"
#include "rpmsg_endpoint.h"
#include "rpmsg_rpc_service.h"
#include "rpmsg_sys_service.h"

#if defined __cplusplus
extern "C" {
#endif

/* initialize openamp module, including remoteproc, virtio, rpmsg */
int openamp_init(struct client_os_inst *client);

/* release openamp resource */
void openamp_deinit(struct client_os_inst *client);

#if defined __cplusplus
}
#endif

#endif  /* OPENAMP_MODULE_H */
