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

/* ========== Platform operation callbacks ========== */
struct mica_sys_ops {
    /* Shell command handler (optional).
     * @param c: input character
     */
    void (*shell_cmd_handler)(char c);

    /* System control callback (optional) */
    void (*system_poweroff)(void);

    /* Exception notification (optional).
     * @param signal: signal value
     * @param frame: exception frame (may be NULL)
     */
    // void (*notify_panic)s(int signal, void *frame);
};

/* ========== MICA main configuration ========== */
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

/* ========== Backend core API ========== */
/**
 * Initialize MICA backend (IRQ, RPMsg, shared memory).
 *
 * @param config: backend configuration (must not be NULL)
 * @return: 0 on success; negative errno on failure:
 *   -EINVAL config is NULL
 *   -EBUSY  already initialized
 *   -ENODEV pedestal ops unavailable or init_irq/init_rpmsg failed
 */
int mica_init(struct mica_config *config);

/**
 * Tear down MICA backend (RPMsg and related resources).
 * Safe to call if not initialized.
 */
void mica_remove(void);


#ifdef __cplusplus
}
#endif

#endif /* __MICA_H__ */
