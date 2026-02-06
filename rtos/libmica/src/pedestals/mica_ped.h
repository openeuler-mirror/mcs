/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_PEDESTAL_H
#define MICA_PEDESTAL_H

#include <openamp/open_amp.h>
#include <mica/mica.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ========== Pedestal operations ========== */
struct mica_pedestal_ops {
    /**
     * Initialize IPC interrupt.
     * @param config: MICA configuration
     * @return: 0 on success, negative errno on failure
     */
    int (*init_irq)(struct mica_config *config);

    /**
     * Initialize RPMsg backend.
     * @param config: MICA configuration
     * @return: 0 on success, negative errno on failure
     */
    int (*init_rpmsg)(struct mica_config *config);

    /**
     * Process received RPMsg messages (poll/loop; called from receiver thread).
     */
    void (*rcv_message)(void);

    /**
     * Deinitialize RPMsg backend and release resources.
     */
    void (*deinit)(void);
};

/**
 * Get pedestal operations for the current platform.
 *
 * @return: pointer to ops, or NULL if pedestal type not supported
 */
const struct mica_pedestal_ops *mica_get_ped_ops(void);

#ifdef __cplusplus
}
#endif

#endif /* MICA_PEDESTAL_H */