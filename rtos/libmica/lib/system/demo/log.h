/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_LOG__H__
#error "Include mica/platform/log.h instead of mica/platform/system/demo/log.h"
#endif

#ifndef __MICA_DEMO_LOG__H__
#define __MICA_DEMO_LOG__H__

#ifdef __cplusplus
extern "C" {
#endif

#include <mica/service.h>

#define mica_log(fmt, ...) mica_tty_printf(fmt, ##__VA_ARGS__)

#ifdef __cplusplus
}
#endif

#endif /* __MICA_DEMO_LOG__H__ */
