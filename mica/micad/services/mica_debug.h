/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_DEBUG_H
#define MICA_DEBUG_H

#include "mica/mica.h"

#include "mica_debug_ring_buffer.h"

#define TO_SHARED_MEM_QUEUE_NAME "/to_shared_mem_queue"
#define FROM_SHARED_MEM_QUEUE_NAME "/from_shared_mem_queue"
#define MAX_QUEUE_SIZE 10
#define MAX_PARAM_LENGTH 100

int create_debug_service(struct mica_client *client);

#endif
