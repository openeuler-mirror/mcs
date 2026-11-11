/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_INTERNAL_H
#define MICA_INTERNAL_H
#include <errno.h>
#include "mica_context.h"
#include "services/mica_service_internal.h"
#define MICA_STOP_BUDGET_NS 1000000000ULL
#define MICA_RECEIVER_POLL_NS 10000000ULL
/* Implementation-only utilities, not a second lifecycle/platform contract. */
int mica_context_pin(struct mica_context *ctx);
int mica_context_close(struct mica_context *ctx);
int mica_context_open(struct mica_context *ctx);
int mica_context_drain_entries(struct mica_context *ctx, mica_deadline_t deadline);
int mica_service_lock(struct mica_service_slot *slot);
int mica_service_unlock(struct mica_service_slot *slot);
bool mica_openamp_resetting(struct mica_openamp *helper);
int mica_context_lock(struct mica_context *ctx);

int mica_context_entry_lock(struct mica_context *ctx);

int mica_context_entry_unlock(struct mica_context *ctx);

void mica_context_unpin(void);

unsigned int mica_context_entry_users(void);

int mica_context_destroy_lock(struct mica_context *ctx);

int mica_context_unlock(struct mica_context *ctx);

int mica_context_cancel(struct mica_context *ctx);

int mica_rsc_layout(void *table, size_t table_bytes, struct metal_io_region *buffers,
    int(*notify)(void *, uint32_t), void *cookie, struct mica_rpmsg_layout *layout);
#endif
