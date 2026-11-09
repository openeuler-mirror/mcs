/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_CONTEXT_H
#define MICA_CONTEXT_H
#include <stdbool.h>
#include <stdatomic.h>
#include <stddef.h>
#include <openamp/rpmsg.h>
#include <mica/mica.h>
#include <mica/service.h>
#include <mica/platform/thread.h>
#include <metal/mutex.h>
#include <mica/platform/sem.h>
#include "pedestals/mica_ped.h"

struct rpmsg_device;
struct rpmsg_endpoint;
struct mica_openamp;
enum mica_framework_state
{
    MICA_UNINITIALIZED, MICA_INITIALIZING, MICA_INITIALIZED, MICA_STARTING, MICA_RUNNING, MICA_STOPPING
};

enum mica_service_state
{
    MICA_SERVICE_OFF, MICA_SERVICE_STARTING, MICA_SERVICE_RUNNING, MICA_SERVICE_STOPPING,
        MICA_SERVICE_FAILED_RETAINED
};

#define MICA_SERVICE_RX_QUEUE_CAPACITY 8u
struct mica_peer_snapshot
{
    uint32_t local_addr;
    uint32_t remote_addr;
    bool bound;
};

struct mica_service_slot
{
    /* All mutable service state, peer, queue and callback configuration use
     * this gate. Lifecycle nesting is context -> one service gate only. */
    struct mica_context *ctx;
    metal_mutex_t gate;
    bool gate_acquired;
    enum mica_service_type type;
    enum mica_service_state state;
    /* Stable ept + minimum callback context/rdev/address live from publication
     * until overall stop proves the sole receiver exited. No unregister/reuse
     * on post-publication failure; not even an empty queue/callback count is
     * an endpoint termination proof. Never use generation as wire isolation. */
    struct rpmsg_endpoint endpoint;
    const char *name;
    rpmsg_ept_cb rx_callback; /* internal, nonblocking enqueue/drop, returns >=0 */
    void (*worker_entry)(void *);
    mica_thread_t worker;
    mica_sem_t wake;
    /* Admission under gate; return never depends on a fallible mutex. */
    atomic_uint borrowers;
    atomic_uint callbacks;
    bool wake_acquired;
    bool endpoint_acquired;
    bool worker_acquired;
    bool worker_joined;
    bool published_once;
    bool failed_retained; /* sticky through this domain */
    int create_error; /* original failure, retained for diagnostics */
    struct mica_peer_snapshot peer; /* gate; no raw API dest reads */
    bool cancelled;
    bool local_start_released;
    /* One preallocated fixed queue, capacity above (+ one in worker). Queue,
     * wake and endpoint callback context may ALL be retained after failure.
     * No repeated-create or per-packet allocation growth; full RX is dropped
     * with nonnegative return, without acquiring another held buffer. */
    void *service_data; /* type-private RX/queue; no second lifecycle truth */
};

struct mica_context
{
    struct mica_config config;
    enum mica_framework_state state;
    /* The unique operation owner can relinquish admission after a failed state
     * lock reacquisition without racing lifecycle entrants under that lock. */
    atomic_bool operation;
    bool stopped;
    atomic_bool cancel;
    bool entry_closed; /* protected only by the static entry gate */
    /* Native release left ownership unknown, or an internal communication
     * step could not finish after a synchronization failure. Ordinary API
     * admission timeouts still return errors without setting this flag. */
    atomic_bool sync_failed;
    /* B FAILED_RETAINED never forces a RUNNING A/framework into STOPPING.
     * If no service runs but published remnants exist, remain INITIALIZED;
     * per-slot flags still require real overall stop (not no-worker success). */
    metal_mutex_t state_lock;
    mica_sem_t receiver_wake;
    mica_sem_t references_changed;
    mica_thread_t receiver;
    struct rpmsg_device *rpdev;
    struct mica_openamp *openamp;
    struct mica_rpmsg_layout layout; /* borrowed runtime-table/window references */
    void *pedestal_cookie;
    bool state_lock_acquired;
    bool receiver_wake_acquired;
    bool references_changed_acquired;
    bool pedestal_acquired;
    bool openamp_acquired;
    bool receiver_acquired;
    bool receiver_joined;
    struct mica_service_slot services[MICA_SERVICE_MAX];
};

struct mica_start_transaction
{
    /* Record each attempted type BEFORE its first acquisition, including the
     * failing member, so rollback closes it and earlier additions exactly once. */
    enum mica_service_type added[2];
    size_t count;
    bool receiver_added;
};

/* Producer: mica_context.c defines exactly one static-lifetime atomic_flag
 * initialized by ATOMIC_FLAG_INIT (never memset/reinitialize a live flag).
 * Acquire test_and_set before admission/pinning or creating/destroying native
 * state storage; contended calls return -EBUSY (no spin/wait). Data pins check
 * entry_closed and do not take the context mutex. Lifecycle pins may enter a
 * closed framework to retry retained cleanup, then reject operation conflicts.
 * Lock teardown requires operation ownership + closed admission + zero pins.
 * Never hold gate over prepare, peer/worker waits or callback execution. */
extern atomic_flag mica_init_gate;
/* Sole instance belongs to runtime, never to provider/pedestal/service files. */
struct mica_context *mica_context_get(void);
/* Caller holds logical operation ownership, NOT state_lock across waits.
 * Both public create paths invoke exactly this function; no public recursion.
 * Fixed local order: type init(ctx,slot) prepares RX data/queue/wake first;
 * start shared receiver if absent, BEFORE any NS/TX wait; create joinable
 * worker behind slot.local_start_released=false; then publish via the unique
 * helper empty-name registration + explicit NS path. All allocations/local
 * failure-prone preparation precede publication. Release worker start only on
 * local success; create returns original local/NS failure synchronously.
 * Before publication, complete rollback permits retry. After registration,
 * published_once is sticky: any later failure logically closes this slot,
 * records create_error/FAILED_RETAINED, and subsequent create is -EOPNOTSUPP.
 * Successful duplicate remains -EALREADY. Sends/NS hold lifetime borrows, no
 * dispatch/state/rdev lock across TX wait. No second receiver/consumer. */
int start_one_service(struct mica_context *ctx, enum mica_service_type type,
    struct mica_start_transaction *transaction);
/* Logically revoke ALL additions of this transaction, not preexisting A.
 * Unpublished resources can be reclaimed; any published addition (even an
 * earlier locally successful member of all) becomes FAILED_RETAINED and is
 * not retryable. This is NOT physical complete rollback. Preserve original
 * create error; keep the bounded published resources for overall stop. */
int mica_rollback_start(struct mica_context *ctx, struct mica_start_transaction *transaction,
    mica_deadline_t deadline);
/* Internal business quiesce ONLY, not a public single-stop and NOT permission
 * to unregister/destroy endpoint. Close admission, cancel/wake worker, wait
 * borrowers/business callbacks outside all mutexes. Failed RX returns >=0,
 * does not hold RX or touch queue/wake/heap/rdev; stable slot is still valid.
 * NS bind/unbind may update peer but never change FAILED_RETAINED to RUNNING.
 * Whole stop must inspect published_once/failed_retained/acquired resources,
 * not just worker existence. Only after sole receiver actually exits and all
 * remaining users/held RX are handled may physical endpoint destroy occur;
 * then remove can destroy helper/vdev/unmap. Continuous peer may prevent
 * receiver exit: return -ETIMEDOUT and retain resources, not fake success.
 * No dispatch_gate or bounded receiver-boundary cleanup promise is required.
 */
int mica_service_quiesce(struct mica_context *ctx, enum mica_service_type type,
    mica_deadline_t deadline);
#endif
