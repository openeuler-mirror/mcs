/* SPDX-License-Identifier: Apache-2.0
 * Modified 2026: private libmica support organization; see SOURCES.md.
 */
#ifndef MICA_XEN_SUPPORT_H
#define MICA_XEN_SUPPORT_H
#include "abi.h"
#include "provider.h"
#include <mica/platform/memory.h>
#include <mica/platform/irq.h>
#include <mica/platform/lock.h>
#include <mica/platform/sem.h>
#include <stdatomic.h>
#include <stdbool.h>
#include <errno.h>
#define XEN_WAIT_NS UINT64_C(1000000000)
#define MICA_XEN_PAGE_SIZE 4096u
struct xen_special_page
{
    void *base;
    void *va;
    uint64_t gpa;
    bool base_acquired;
    bool view_acquired;
    bool backing_removed;
    bool xen_mapped;
};

extern struct xen_shared_info *mica_xen_shared;

uint64_t xen_deadline(void);

int xen_hvm_get(uint32_t index, uint64_t *value);

int xen_add_shared(uint64_t gpa);

int xen_remove_backing(uint64_t gpa);

int xen_release_backing(uint64_t gpa);

int xen_restore_backing(uint64_t gpa);

int xen_special_release(struct xen_special_page *page);

int xen_shared_init(void);

int xen_events_init(unsigned event_irq);

int xen_events_bind(uint32_t remote, mica_xen_event_fn fn, void *arg, struct mica_xen_binding **out);

int xen_events_unbind(struct mica_xen_binding *binding, uint64_t deadline);

int xen_events_notify(struct mica_xen_binding *binding);

int xen_event_send(uint32_t port);

int xen_xs_init(void);

int xen_xs_read(const char *key, char *value, size_t capacity);

int xen_xs_write(const char *key, const char *value);

int xen_grant_map(uint16_t domid, const uint32_t *refs, size_t count,
    struct mica_xen_mapping **cookie, void **local, size_t *bytes);

int xen_grant_unmap(struct mica_xen_mapping *cookie);
#endif
