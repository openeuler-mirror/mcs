/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */
#ifndef _SOCKET_H
#define _SOCKET_H

#include <stdint.h>


#ifdef __cplusplus
extern "C" {
#endif

int register_socket_listener(void);
void unregister_socket_listener(void);

#ifdef __cplusplus
}
#endif

#endif
