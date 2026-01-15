/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_SECUREC__H__
#define __MICA_SECUREC__H__

#include <mica/platform/system/@PROJECT_SYSTEM@/securec.h>

#ifdef __cplusplus
extern "C" {
#endif

static inline int mica_memset_s(void *dest, size_t destMax, int c, size_t len);
static inline int mica_memcpy_s(void *dest, size_t destMax, const void *src, size_t len);

#ifdef __cplusplus
}
#endif

#endif /* __MICA_SECUREC__H__ */
