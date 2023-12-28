/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "mica_debug_common.h"

FILE *log_file;

int open_log_file()
{
	log_file = fopen(LOG_FILE_PATH, "a");
	if (log_file == NULL) {
		perror("cannot open log file");
		return -errno;
	}
	return 0;
}

void close_log_file()
{
	fclose(log_file);
}

void mica_debug_log_error(const char *module, const char *fmt, ...)
{
	// in case log file is not opened
	if (log_file == NULL) {
		perror("log file is not opened");
		return;
	}

	time_t current_time;
	struct tm* time_info;
	char time_str[20];
	char error_header[strlen(module) + 30];

	// get local time
	time(&current_time);
	time_info = localtime(&current_time);

	// formatting time string
	strftime(time_str, sizeof(time_str), "%Y-%m-%d %H:%M:%S", time_info);

	// formatting error message
	int len = snprintf(error_header, sizeof(error_header), "[%s] [%s] ", time_str, module);

	// print log
	fprintf(log_file, "%s", error_header);

	// write variable arguments into log file
	va_list args;
	va_start(args, fmt);
	vfprintf(log_file, fmt, args);
	va_end(args);
}
