/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_IO__H__
#error "Include mica/platform/io.h instead of mica/platform/system/demo/io.h"
#endif

#ifndef __MICA_DEMO_IO__H__
#define __MICA_DEMO_IO__H__

#ifdef __cplusplus
extern "C" {
#endif

static inline void mica_writeb(uint8_t val, unsigned long addr)
{
    demo_writeb(val, addr);
}

static inline void mica_writew(uint16_t val, unsigned long addr)
{
    demo_writew(val, addr);
}

static inline void mica_writel(uint32_t val, unsigned long addr)
{
    demo_writel(val, addr);
}

#ifdef __cplusplus
}
#endif

#endif /* __MICA_DEMO_IO__H__ */
