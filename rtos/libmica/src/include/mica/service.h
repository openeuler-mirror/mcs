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
 * Create the fixed default set TTY + UMT, using the same internal creation
 * path as mica_create_service. Preflight capability/duplicates before changes.
 * Failure logically revokes only this call's additions; existing services stay
 * running. Published additions retain bounded objects until overall stop and
 * cannot be retried this domain; this is not complete physical rollback.
 *
 * @return: 0 when locally ready (not peer-bound); negative errno on failure
 */
int mica_create_all_services(void);

/**
 * Create one service, reusing the framework's single receiver. Local success
 * does not wait for remote readiness; query mica_service_is_ready separately.
 * A failed addition must not interrupt another running service.
 * Before endpoint publication, complete cleanup permits retry. After successful
 * registration, any later failure returns its original error but retains that
 * service's bounded objects, closes business admission and forbids same-domain
 * retry (-EOPNOTSUPP). Late RX/NS cannot reactivate it.
 *
 * @param type: service type
 * @return: 0 on local success; -EINVAL invalid type, -EOPNOTSUPP unsupported
 * capability, retry of a published failed service, or create after completed
 * stop; -EALREADY successful duplicate, -ENODEV
 * uninitialized, -EBUSY lifecycle operation/initialization/stop in progress.
 */
int mica_create_service(enum mica_service_type type);

/**
 * Synchronously stop ALL services and the shared receiver. No public timeout
 * or individual stop/recreate API. Only truly uninitialized/resource-empty
 * stop is a no-op; retained published endpoints require stop even with no
 * worker. Empty stop does not prevent a first create; completed stop is
 * idempotent. Endpoint destruction waits for the sole receiver to exit;
 * continuous peer RX may prevent exit and cause the internal timeout.
 * @return 0 on completion, -EBUSY competing lifecycle operation, -EDEADLK
 * from a library worker/callback, -ETIMEDOUT internal completion deadline.
 * Failure/timeout retains all resources still reachable by live users.
 */
int mica_stop_services(void);

/**
 * Asynchronously request receiver stop and wake it; does not join or reclaim.
 * This is not a substitute for mica_stop_services().
 */
void mica_stop_receiver(void);

/**
 * Check local RUNNING admission and a locked peer snapshot; never read raw
 * OpenAMP dest_addr from the caller thread. Retained failed service is not ready.
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
 * @return: bytes sent; -EINVAL invalid args, -EAGAIN not ready,
 * -ESHUTDOWN stopping, or another negative errno
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
 * Unregister UMT receive callback and wait for in-flight callbacks to drain.
 * Only a successful return permits freeing priv or other callback-owned state
 * and switching to mica_rcv_data(). A timeout or other error does not prove
 * callbacks have exited, even if the registered callback was already cleared.
 *
 * @return: 0 after callbacks drain; -EDEADLK from the active callback itself;
 *          -ETIMEDOUT if draining exceeds the deadline; -EAGAIN if UMT is not
 *          ready; other negative errors on synchronization or lifecycle failure
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
