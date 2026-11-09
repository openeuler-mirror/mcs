/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef MICA_SERVICE_INTERNAL_H
#define MICA_SERVICE_INTERNAL_H

#include <openamp/rpmsg.h>
#include <mica/mica.h>
#include "../mica_context.h"

#ifdef __cplusplus
extern "C"
{
#endif

/* Framework-owned device/configuration access. */

/**
 * Set rpmsg device (called after pedestal init).
 */
void mica_set_rpdev(struct rpmsg_device *rpdev);

/**
 * Get rpmsg device (set by pedestal, used by services).
 */
struct rpmsg_device *mica_get_rpdev(void);

/**
 * Get MICA config (saved by mica_init, used by services).
 */
struct mica_config *mica_get_config(void);

/* Service-common bounded RX storage and lifetime helpers. */

void mica_service_drain_returns(struct mica_context *ctx);
enum mica_rx_owner
{
    MICA_RX_FREE, MICA_RX_QUEUED, MICA_RX_PROCESSING, MICA_RX_RETURN_PENDING, MICA_RX_RETURNING
};

struct mica_rx_item
{
    void *data;
    size_t len;
    enum mica_rx_owner owner;
};

/* 8 queued + at most one processing/deferred item. Retained until stop once
 * published. Returns are executed ONLY at a safe receiver callback boundary
 * or after notify returns/receiver exits, never early from a worker. */
struct mica_service_data
{
    struct mica_rx_item items[MICA_SERVICE_RX_QUEUE_CAPACITY + 1];
    unsigned int queue[MICA_SERVICE_RX_QUEUE_CAPACITY];
    unsigned int head;
    unsigned int count;
    metal_mutex_t tx_lock;
    bool tx_lock_acquired;
    mica_sem_t passive;
    bool passive_acquired;
    atomic_bool passive_active; /* only the claiming receive call clears it */
    bool passive_signalled;
    bool send_initialized;
    uint64_t send_da;
    umt_rcv_cb_t callback;
    void *callback_arg;
};

int mica_service_prepare_data(struct mica_service_slot *slot, bool umt);

int mica_service_enqueue(struct mica_service_slot *slot, void *data, size_t len);

int mica_service_take(struct mica_service_slot *slot, bool callback, unsigned int *index, void **data,
    size_t *len);

void mica_service_complete(struct mica_service_slot *slot, unsigned int index, bool callback);

int mica_service_borrow(enum mica_service_type type, struct mica_service_slot **slot,
    struct mica_peer_snapshot *peer);

int mica_service_unborrow(struct mica_service_slot *slot);


/* LOCAL preparation only: initialize slot RX queue/data + wake semaphore,
 * immutable name/rx_callback/worker_entry; record each acquisition immediately.
 * Queue capacity is MICA_SERVICE_RX_QUEUE_CAPACITY, allocated once before
 * publication; never allocate per packet or on repeated failed create.
 * Never publish an endpoint, spawn a thread or wait for peer/NS/TX here.
 * start_one_service owns the only receiver/endpoint/worker creation sequence.
 * Caller holds the service gate. On failure keep obtained resources in slot
 * for that transaction's rollback. */
int mica_tty_init_service(struct mica_context *ctx, struct mica_service_slot *slot);

int mica_umt_init_service(struct mica_context *ctx, struct mica_service_slot *slot);

/* arg is the stable slot, whose ctx pointer is set before thread creation.
 * Business/shell/user callbacks run ONLY here, not in internal RPMsg RX cb. */
void tty_service_thread(void *arg);

void umt_service_thread(void *arg);

int mica_tty_is_ready(void);

int mica_umt_is_ready(void);

#ifdef __cplusplus
}

#endif

#endif /* MICA_SERVICE_INTERNAL_H */
