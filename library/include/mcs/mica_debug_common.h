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

/*
    This depends on the message length from RTOS
    content of message includes:
    headers and other information
    31 ordinary register with 64 bit each
    sp register with 64 bit
    pc register with 64 bit
    pstate register with 64 bit, but only send 32 bit
    34 floating point register with 128 bit each
*/
#define MAX_BUFF_LENGTH 1600
#define MSG_PRIO 0 // default priority for message queue
#define LOG_FILE_PATH "./logfile.txt"
#define EXIT_PACKET "$k#6b"

/*
    Considering the performance of the system,
    we do not want to open and close the log file each time we write to it.
    Therefore, we provide the following functions to open and close the log file.
    Users should call open_log_file() at the beginning of the program,
    and call close_log_file() when freeing resources.

*/
int open_log_file();
void close_log_file();
void mica_debug_log_error(const char *module, const char *fmt, ...);

#endif