/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_PEDESTAL_H
#define MICA_PEDESTAL_H

#include <openamp/open_amp.h>
#include <mica/mica.h>
#include <stdbool.h>
#include <mica/service.h>

#ifdef __cplusplus
extern "C" {
#endif

struct mica_context;
struct mica_service_slot;
struct mica_peer_snapshot;
struct metal_io_region;
struct mica_openamp;
/* Borrowed runtime view, NEVER implicitly replaced by rsc_table_get().
 * Frontend validates ranges after DRIVER_OK before publishing this layout. */
struct mica_rpmsg_layout {
    void *runtime_table;
    size_t table_bytes;
    void *vring_local[2];
    size_t vring_bytes[2];
    struct metal_io_region *buffer_io;
    int (*notify)(void *cookie, uint32_t vqid);
    void *notify_cookie;
};
/* On failure out retains a non-NULL partial helper if cleanup cannot finish;
 * callers record ownership rather than discarding live OpenAMP resources. */
int mica_openamp_create(const struct mica_rpmsg_layout *layout,
                        struct mica_openamp **out);
/* Requires all endpoints/dispatch/users quiescent. Failure retains handle. */
int mica_openamp_destroy(struct mica_openamp *helper);
struct rpmsg_device *mica_openamp_rpdev(struct mica_openamp *helper);
/* Sole receiver only; synchronously processes the existing OpenAMP notify
 * chain (not an IRQ entry and NOT promised bounded under continuous peer RX).
 * vqid is a runtime vring notify ID, not an endpoint/service ID. */
int mica_openamp_process_notification(struct mica_openamp *helper, uint32_t vqid);

/* Single publication path for both public create APIs. Everything callback
 * reachable (fixed slot/queue/wake/worker startup gate) exists first.
 * Call public rpmsg_create_ept with EMPTY name to register without implicit
 * NS. On success immediately record published_once/endpoint_acquired; set the
 * service name under rdev->lock, release it, then send exactly one original
 * NS CREATE through the private adapter below (never wait TX under a lock).
 * A failed NS must NOT unregister/clear rdev/free address: mark this service
 * FAILED_RETAINED and preserve the original error. No retry this domain.
 */
int mica_openamp_publish_service(struct mica_openamp *helper,
                                 struct mica_service_slot *slot);
/* Helper wraps the original NS endpoint callback BEFORE receiver starts and
 * installs safe device bind/unbind callbacks (no service creation/destruction).
 * Only that receiver runs original NS handling. After handling NS, publish
 * the affected stable slot's peer snapshot under its gate (DESTROY
 * clears bound even if OpenAMP retains old dest). The data callback similarly
 * publishes its peer/src snapshot before nonblocking RX enqueue/drop.
 * Neither path reopens business admission or activates failed slots. Data
 * wrapper checks failed admission FIRST under its gate; failed data returns
 * >=0 without holding RX or touching queue/wake/rdev or raw peer fields.
 * Public ready/send never inspect raw OpenAMP dest_addr. Service-common get
 * checks admission, registers a borrower and copies peer in one gate section.
 * Send uses existing offchannel send outside gates; there is no second snapshot
 * API or second admission pass through the context mutex.
 */
/* PRIVATE exact-version adapter declaration. Accurate guest OpenAMP exports
 * this symbol but declares it ONLY in rpmsg_internal.h, not installed API.
 * Helper owns this narrow declaration/compatibility check for the selected
 * dependency. Do not include the dependency private
 * source-tree header, expose this as public MICA API, or assume cross-version
 * ABI stability. No dependency/line-protocol change or alternate NS algorithm.
 */
int rpmsg_send_ns_message(struct rpmsg_endpoint *ept, unsigned long flags);

/* ========== Pedestal operations ========== */
struct mica_pedestal_ops {
    bool (*supports)(enum mica_service_type type);
    /* ctx.receiver_wake and state lock exist BEFORE prepare. Notifications
     * only mark pending and post that semaphore; no second receiver/wait API.
     * prepare publishes ctx.pedestal_cookie IMMEDIATELY on first acquisition
     * (including a failing return), and sets ctx.pedestal_acquired. Runtime
     * owns residual cleanup via quiesce/detach; layout usable only on success.
     * Never assembles OpenAMP; caller uses the single common helper. */
    int (*prepare)(struct mica_context *ctx, struct mica_rpmsg_layout *layout);
    /* Per-instance notification producer fence; entered callbacks finish.
     * Failure retains ctx/cookie, never tears down provider domain state. */
    int (*quiesce)(struct mica_context *ctx, uint64_t deadline_ns);
    /* Only after quiesce, workers/dispatch and helper destruction. */
    int (*detach)(struct mica_context *ctx);
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
