/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_DELAY__H__
#error "Include mica/platform/delay.h instead of mica/platform/system/demo/delay.h"
#endif

#ifndef __MICA_DEMO_DELAY__H__
#define __MICA_DEMO_DELAY__H__

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

static inline void mica_delay_tick(uint32_t tick)
{
    demo_task_delay(tick);
}

#ifdef __cplusplus
}
#endif

#endif /* __MICA_DEMO_DELAY__H__ */

