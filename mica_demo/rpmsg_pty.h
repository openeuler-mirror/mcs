/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RPMSG_PTY_H
#define RPMSG_PTY_H

#include <stdio.h>
#include <stdarg.h>
#include <pthread.h>
#include "openamp_module.h"

#if defined __cplusplus
extern "C" {
#endif

#define RPMSG_CONSOLE_BUFFER_SIZE 2048

struct pty_ep_data {
	unsigned int ep_id; /* endpoint id */
    int fd_master;  /* pty master fd */
    FILE *f;
    pthread_t pty_thread; /* thread id */
};

struct rpmsg_app_resource {
    struct pty_ep_data *pty_ep_uart;
    struct pty_ep_data *pty_ep_console;
    pthread_t rpmsg_loop_thread;
};

/* entrance for starting rpmsg applications */
int rpmsg_app_start(struct client_os_inst *client);
/* free resources for rpmsg apps */
void rpmsg_app_stop(void);
/* thread for polling the interrupt from RTOS side */
static void *rpmsg_loop_thread(void *arg);
/* create pty service based on openAMP */
static struct pty_ep_data * pty_service_create(const char* ep_name);
int open_pty(int *pfdm);
static void *pty_thread(void *arg);
/* The callback functions needed for RPMsg endpoint of pty service */
static int pty_endpoint_cb(struct rpmsg_endpoint *ept, void *data,
		size_t len, uint32_t src, void *priv);
static void pty_endpoint_unbind_cb(struct rpmsg_endpoint *ept);
static void pty_endpoint_exit(struct pty_ep_data *pty_ep);

#if defined __cplusplus
}
#endif

#endif  /* RPMSG_PTY_H */
