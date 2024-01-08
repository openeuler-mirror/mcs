/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_DEBUG_H
#define MICA_DEBUG_H

#include <stdio.h>
#include <stdarg.h>
#include <pthread.h>
#include <mqueue.h>
#include <errno.h>
#include <string.h>
#include <stdbool.h>
#include <stdlib.h>
#include <sys/wait.h>

#include "mica/mica.h"
#include "mcs/mica_gdb_server.h"
#include "mcs/mcs_common.h"

#ifdef CONFIG_RING_BUFFER
#include "mcs/mica_debug_ring_buffer.h"
#endif

#define TO_SHARED_MEM_QUEUE_NAME "/to_shared_mem_queue"
#define FROM_SHARED_MEM_QUEUE_NAME "/from_shared_mem_queue"
#define MAX_QUEUE_SIZE 10
#define MAX_PARAM_LENGTH 100

/*
	the message queues between proxy server and shared memory
	transferred module.
*/
static int alloc_message_queue();
static void free_message_queue();

int debug_start(struct client_os_inst *client_os, char *exe_name);
static void *server_loop_thread(void *arg);

#endif
