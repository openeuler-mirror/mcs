/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_DEBUG_COMMON_H
#define MICA_DEBUG_COMMON_H

#include <time.h>
#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <stdarg.h>
#include <syslog.h>

/*
 * This depends on the message length from RTOS
 * content of message includes:
 * headers and other information
 * 31 ordinary register with 64 bit each
 * sp register with 64 bit
 * pc register with 64 bit
 * pstate register with 64 bit, but only send 32 bit
 * 34 floating point register with 128 bit each
 */
#define MAX_BUFF_LENGTH 1600
#define MSG_PRIO 0 // default priority for message queue
#define CTRLC_PACKET "\x03"

/*
 * Some functions have pointer return value type (void *),
 * but we want to return error codes, which are integer type (int).
 * The direct conversion between pointer type (void *) and the integer type (int)
 * is undefined behavior,
 * so we need to convert to intptr_t type as an intermediate state.
 */
#define INT_TO_PTR(x) ((void *)(intptr_t)(x))
#define PTR_TO_INT(x) ((int)(intptr_t)(x))

#endif
