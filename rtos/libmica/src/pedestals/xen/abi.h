/* SPDX-License-Identifier: MIT
 * ARM64-only excerpts of Xen RELEASE-4.18.2 public ABI (see SOURCES.md).
 * Copyright (c) 2004, K A Fraser (xen.h, grant_table.h)
 * Copyright 2011 (C) Citrix Systems (arch-arm.h)
 * Copyright (c) 2005, Keir Fraser <keir@xensource.com> (memory.h)
 * Copyright (c) 2003-2004, K A Fraser. (event_channel.h)
 * Copyright (c) 2007, Keir Fraser (hvm/hvm_op.h, hvm/params.h)
 * Copyright (C) 2005 Rusty Russell IBM Corporation (io/xs_wire.h)
 * Original per-header copyrights and MIT terms: LICENSES/THIRD_PARTY_LICENSES.txt.
 * Modified 2026: reduced names/types to this support's used ABI; no OS headers.
 */
#ifndef MICA_XEN_ABI_H
#define MICA_XEN_ABI_H
#include <stdint.h>
#include <stddef.h>
#define XEN_DOMID_SELF 0x7ff0u
#define XEN_PORTS 4096u
#define XENMEM_ADD 7u
#define XENMEM_REMOVE 15u
#define XENMEM_DECREASE 1u
#define XENMEM_POPULATE 6u
#define XENMAP_SHARED_INFO 0u
#define EVT_BIND 0u
#define EVT_CLOSE 3u
#define EVT_SEND 4u
#define EVT_UNMASK 9u
#define GNT_MAP 0u
#define GNT_UNMAP 1u
#define GNT_HOST_MAP 2u
#define HVM_GET_PARAM 1u
#define HVM_STORE_PFN 1u
#define HVM_STORE_EVTCHN 2u
struct xen_add_physmap {
    uint16_t domid, size;
    uint32_t space;
    uint64_t idx, gpfn;
};
struct xen_remove_physmap { uint16_t domid; uint64_t gpfn; };
struct xen_memory_reservation {
    uint64_t extent_start, nr_extents;
    uint32_t extent_order, mem_flags;
    uint16_t domid;
};
struct xen_hvm_param { uint16_t domid; uint32_t index; uint64_t value; };
struct xen_evt_bind { uint16_t remote_dom; uint32_t remote_port, local_port; };
struct xen_evt_port { uint32_t port; };
/* Xen RELEASE-4.18.2 public io/console.h, MIT, (c)2005 Keir Fraser.
 * Local ABI only; see LICENSES/THIRD_PARTY_LICENSES.txt. */
struct xencons_interface {
    char in[1024], out[2048];
    uint32_t in_cons, in_prod, out_cons, out_prod;
};
#define HVM_CONSOLE_PFN 17u
#define HVM_CONSOLE_EVTCHN 18u
_Static_assert(sizeof(struct xencons_interface)==3088,"console ABI");
_Static_assert(offsetof(struct xencons_interface,out_cons)==3080,"console cursor ABI");
struct xen_grant_map {
    uint64_t host_addr;
    uint32_t flags, ref;
    uint16_t dom;
    int16_t status;
    uint32_t handle;
    uint64_t dev_bus_addr;
};
struct xen_grant_unmap {
    uint64_t host_addr, dev_bus_addr;
    uint32_t handle;
    int16_t status;
};
struct xen_vcpu_time {
    uint32_t version, pad0;
    uint64_t tsc_timestamp, system_time;
    uint32_t tsc_to_system_mul;
    int8_t tsc_shift;
    uint8_t flags, pad1[2];
};
struct xen_vcpu_info {
    uint8_t evtchn_upcall_pending, pad0;
    uint64_t evtchn_pending_sel;
    struct xen_vcpu_time time;
};
struct xen_shared_info {
    struct xen_vcpu_info vcpu_info[1];
    uint64_t evtchn_pending[64], evtchn_mask[64];
    uint32_t wc_version, wc_sec, wc_nsec, wc_sec_hi;
};
#define XS_READ 2u
#define XS_WRITE 11u
#define XS_ERROR 16u
#define XS_RING_SIZE 1024u
#define XS_PAYLOAD_MAX 4096u
struct xen_xs_msg { uint32_t type, req_id, tx_id, len; };
struct xen_xs_ring {
    char req[XS_RING_SIZE], rsp[XS_RING_SIZE];
    uint32_t req_cons, req_prod, rsp_cons, rsp_prod;
    uint32_t server_features, connection, error;
};
_Static_assert(sizeof(struct xen_grant_map) == 32, "grant map ABI");
_Static_assert(sizeof(struct xen_grant_unmap) == 24, "grant unmap ABI");
_Static_assert(sizeof(struct xen_add_physmap) == 24, "physmap ABI");
_Static_assert(sizeof(struct xen_remove_physmap) == 16, "remove ABI");
_Static_assert(sizeof(struct xen_memory_reservation) == 32, "reservation ABI");
_Static_assert(sizeof(struct xen_hvm_param) == 16, "hvm ABI");
_Static_assert(sizeof(struct xen_vcpu_info) == 48, "ARM vcpu ABI");
_Static_assert(offsetof(struct xen_shared_info, evtchn_pending) == 48, "shared ABI");
_Static_assert(offsetof(struct xen_shared_info, evtchn_mask) == 560, "shared mask ABI");
_Static_assert(offsetof(struct xen_xs_ring, req_cons) == 2048, "xs ABI");
long HYPERVISOR_memory_op(unsigned int op, void *arg);
long HYPERVISOR_hvm_op(unsigned int op, void *arg);
long HYPERVISOR_event_channel_op(unsigned int op, void *arg);
long HYPERVISOR_grant_table_op(unsigned int op, void *arg, unsigned int count);
long HYPERVISOR_console_io(unsigned int op, unsigned int count, const char *str);
long HYPERVISOR_sched_op(unsigned int op, void *arg);
long HYPERVISOR_xen_version(unsigned int op, void *arg);
#endif
