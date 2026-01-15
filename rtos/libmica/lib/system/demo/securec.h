/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_SECUREC__H__
#error "Include mica/platform/securec.h instead of mica/platform/system/demo/securec.h"
#endif

#ifndef __MICA_DEMO_SECUREC__H__
#define __MICA_DEMO_SECUREC__H__

#include <securec.h>

#ifdef __cplusplus
extern "C" {
#endif

static inline int mica_memset_s(void *dest, size_t destMax, int c, size_t len)
{
	return memset_s(dest, destMax, c, len);
}

static inline int mica_memcpy_s(void *dest, size_t destMax, const void *src, size_t len)
{
	return memcpy_s(dest, destMax, src, len);
}

#ifdef __cplusplus
}
#endif

#endif /* __MICA_DEMO_SECUREC__H__ */
