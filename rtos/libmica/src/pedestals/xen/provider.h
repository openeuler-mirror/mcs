/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_XEN_PROVIDER_H
#define MICA_XEN_PROVIDER_H
#include <stddef.h>
#include <stdint.h>
struct mica_xen_mapping;
struct mica_xen_binding;
typedef void (*mica_xen_event_fn)(void *arg); /* IRQ: pending+wake only */
struct mica_xen_provider_ops {
    /* OS startup only, once per domain; MICA attach/remove never own shutdown.
     * Native adapter attaches existing facilities, never initializes builtin.
     * First failure leaves domain NOT READY and unusable by MICA. This period
     * permits no domain_init retry; retain partial domain-owned handles for
     * diagnostics/domain teardown, not MICA remove. Other ops return -ENODEV
     * until READY. Repeated successful init is idempotent, never a second owner.
     * Concurrent initialization returns -EBUSY; repeated calls after a failed
     * first init return its recorded negative error without reacquiring state.
     */
    int (*domain_init)(unsigned event_irq);
    /* Serialized complete xenstore transactions. Reads NUL-terminate on
     * success; -ENOSPC if capacity insufficient, no silent truncation. */
    int (*xs_read)(const char *key, char *value, size_t capacity);
    int (*xs_write)(const char *key, const char *value);
    /* map returns contiguous LOCAL VA plus cookie retaining every page's
     * grant handle and slot. On failure *cookie is non-NULL if undo failed;
     * caller must retain it and retry unmap, never free a live mapping.
     * Initialize *cookie=NULL,*local=NULL,*bytes=0. Failure never publishes a
     * usable VA even if cookie remains. Pages are exactly 4KiB; local mapping
     * is contiguous/aligned. This period's only backend domid is Dom0=0;
     * reject any other domid with -EOPNOTSUPP, no generalized remote support. */
    int (*map)(uint16_t domid, const uint32_t *refs, size_t count,
               struct mica_xen_mapping **cookie, void **local, size_t *bytes);
    int (*unmap)(struct mica_xen_mapping *cookie); /* success consumes cookie */
    /* Remote domain is fixed Dom0=0 (implicit). Initialize *cookie=NULL;
     * same failure-cookie rule as map if a partially bound source is live. */
    int (*bind)(uint32_t remote_port, mica_xen_event_fn callback, void *arg,
                struct mica_xen_binding **cookie);
    /* Disable producer + wait entered callbacks. Success consumes binding;
     * failure retains binding and callback target. No domain-wide teardown. */
    int (*sync_unbind)(struct mica_xen_binding *cookie, uint64_t deadline_ns);
    int (*notify)(struct mica_xen_binding *cookie);
    /* Optional domain console; independent of domain_init and MICA lifecycle.
     * Missing pair means -EOPNOTSUPP, never builtin fallback for native. */
    int (*debug_init)(void);
    int (*debug_write)(const void *bytes, size_t len);
};
/* One build-selected whole provider. Missing native adapter is a build error,
 * never a fallback to builtin. No Xen ABI types in this contract. */
#ifdef __cplusplus
extern "C" {
#endif
const struct mica_xen_provider_ops *mica_xen_provider_get(void);
#ifdef __cplusplus
}
#endif
#endif
