/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_IRQ__H__
#error "Include mica/platform/irq.h instead of mica/platform/system/demo/irq.h"
#endif

#ifndef __MICA_DEMO_IRQ__H__
#define __MICA_DEMO_IRQ__H__

#ifdef __cplusplus
extern "C" {
#endif

typedef DEMO_HWI_FUNC mica_irq_handler_t;

static inline int mica_request_irq(unsigned int irq, mica_irq_handler_t handler)
{
    return demo_request_irq(irq, handler);
}

static inline void mica_unmask_irq(unsigned int irq)
{
    demo_irq_unmask(irq);
}

#ifdef __cplusplus
}
#endif

#endif /* __MICA_DEMO_IRQ__H__ */
