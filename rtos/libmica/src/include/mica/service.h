/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_SERVICE_H
#define MICA_SERVICE_H

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ========== Service types ========== */
enum mica_service_type {
    MICA_SERVICE_RPC = 0,
    MICA_SERVICE_TTY,
    MICA_SERVICE_UMT,
    MICA_SERVICE_MAX
};

/* ========== Service configuration ========== */

/**
 * Create all default services (RPC + TTY + UMT).
 * Note: spawns dedicated receiver and service threads.
 *
 * @return: 0 on success; negative errno or pthread error on failure
 */
int mica_create_all_services(void);

/**
 * Create a registered service endpoint by type.
 *
 * @param type: service type
 * @return: 0 on success; negative errno on failure (e.g. -EINVAL, -EOPNOTSUPP)
 */
int mica_create_service(enum mica_service_type type);

/**
 * Stop the receiver thread. Stops reception for all services.
 */
void mica_stop_receiver(void);

/**
 * Check whether a service is ready.
 *
 * @param type: service type
 * @return: 1 if ready, 0 if not ready
 */
int mica_service_is_ready(enum mica_service_type type);

/* ========== TTY API ========== */
/**
 * Send data on TTY endpoint.
 *
 * @param data: data buffer
 * @param len:  data length
 * @return: number of bytes sent on success; 0 if TTY not ready; negative on failure
 */
int mica_tty_send(unsigned char *data, size_t len);

/**
 * Print to TTY endpoint (printf-style).
 *
 * @param format: printf format string
 * @return: number of characters printed on success; negative errno on failure (e.g. printf/rpmsg error)
 */
int mica_tty_printf(const char *format, ...);


/* ========== UMT API ========== */
/**
 * UMT receive callback type: invoked from library thread when data arrives.
 *
 * @param data     pointer to received data; valid only during the callback, must not be used after return
 * @param data_len received length in bytes
 * @param priv     opaque pointer passed through from registration (e.g. application context)
 */
typedef void (*umt_rcv_cb_t)(const void *data, int data_len, void *priv);

/**
 * Register UMT receive callback (library thread waits for data and invokes callback).
 * Only one callback is supported. Mutually exclusive with mica_rcv_data(); do not use both.
 *
 * @param callback invoked on receive (data, data_len, priv); data valid only during the call
 * @param priv     opaque pointer passed to callback (e.g. application context)
 * @return: 0 on success; -EALREADY if already registered; -EAGAIN if UMT not ready; -EINVAL if callback is NULL
 */
int mica_umt_register_rcv_cb(umt_rcv_cb_t callback, void *priv);

/**
 * Unregister UMT receive callback. After this, use mica_rcv_data() for passive receive.
 *
 * @return: 0 on success
 */
int mica_umt_unregister_rcv_cb(void);

/**
 * Receive data from peer (passive pull; UMT copies into user buffer).
 * Mutually exclusive with callback mode: returns -EBUSY if mica_umt_register_rcv_cb was already registered.
 *
 * @param buffer: receive buffer (output)
 * @param len:    in: buffer size; out: actual received length
 * @return: 0 on success; -EBUSY if callback mode is active; -EAGAIN if UMT not ready or wait failed;
 *          -EINVAL if buffer or len is NULL; -EFAULT on internal receive error
 */
int mica_rcv_data(void *buffer, size_t *len);

/**
 * Send data to peer (UMT zero-copy transfer into shared memory).
 *
 * @param data:   data buffer
 * @param offset: offset in shared send buffer to copy from
 * @param len:    data length (must be > 0 and within buffer limits)
 * @return: 0 on success; -EAGAIN if UMT not ready; -EINVAL if data NULL, len 0 or too large;
 *          -EFAULT if send buffer not yet initialized; -EIO on send failure
 */
int mica_send_data(void *data, int offset, size_t len);


#ifdef __cplusplus
}
#endif

#endif /* MICA_SERVICE_H */