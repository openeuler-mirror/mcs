/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_IO__H__
#define __MICA_IO__H__

#include <mica/platform/system/@PROJECT_SYSTEM@/io.h>

#ifdef __cplusplus
extern "C" {
#endif

/** \defgroup IO Interfaces
 *  @{
 */

/**
 * @brief      Generic IO write byte
 */
static inline void mica_writeb(uint8_t val, unsigned long addr);

/**
 * @brief      Generic IO write word
 */
static inline void mica_writew(uint16_t val, unsigned long addr);

/**
 * @brief      Generic IO write long
 */
static inline void mica_writel(uint32_t val, unsigned long addr);

/** @} */

#ifdef __cplusplus
}
#endif

#endif /* __MICA_IO__H__ */
