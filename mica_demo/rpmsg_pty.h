/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RPMSG_PTY_H
#define RPMSG_PTY_H

#if defined __cplusplus
extern "C" {
#endif

struct pty_ep_data {
	unsigned int ep_id; /* endpoint id */

    int fd_master;  /* pty master fd */

    pthread_t pty_thread; /* thread id */
};

struct pty_ep_data * pty_service_create(const char* ep_name);

#if defined __cplusplus
}
#endif

#endif  /* RPMSG_PTY_H */
