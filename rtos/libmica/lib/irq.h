/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_IRQ__H__
#define __MICA_IRQ__H__

#include <mica/platform/system/@PROJECT_SYSTEM@/irq.h>

#ifdef __cplusplus
extern "C" {
#endif

static inline int mica_request_irq(unsigned int irq, mica_irq_handler_t handler);
static inline void mica_unmask_irq(unsigned int irq);
static inline void mica_trigger_irq(unsigned int irq);

#ifdef __cplusplus
}
#endif

#endif /* __MICA_IRQ__H__ */
