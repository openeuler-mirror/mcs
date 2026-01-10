/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef __MICA_H__
#define __MICA_H__

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ========== 平台操作回调 ========== */
struct mica_sys_ops {
    /* Shell处理函数（可选）
     * @param c: 输入字符
     */
    void (*shell_cmd_handler)(char c);

    /* 系统控制回调（可选） */
    void (*system_poweroff)(void);

    /* 异常通知（可选）
     * @param signal: 信号值
     * @param frame: 异常帧（可为NULL）
     */
    // void (*notify_panic)s(int signal, void *frame);
};

/* ========== MICA主配置结构 ========== */
struct mica_config {
    /* shared memory configuration */
    uintptr_t shm_base_addr;
    size_t shm_size;

    /* interrupt configuration */
    uint32_t ipc_irq_num;
    uintptr_t ipc_irq_base;

    /* client OS system operations */
    struct mica_sys_ops sys_ops;
};

/* ========== Backend核心接口 ========== */
/**
 * 初始化MICA backend
 * @param config: backend配置
 * @return: 0成功，负数失败
 */
int mica_init(struct mica_config *config);

/**
 * 移除RPMsg backend
 */
void mica_remove(void);


#ifdef __cplusplus
}
#endif

#endif /* __MICA_H__ */
