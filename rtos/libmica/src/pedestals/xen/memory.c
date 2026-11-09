/* SPDX-License-Identifier: Apache-2.0
 * Copyright (c) 2023 EPAM Systems
 * Modified 2026 from Zephyr v4.2.0 drivers/xen/memory.c: checked single-page
 * add/remove/populate, provider-owned backing ledger. See SOURCES.md/A01.
 */
#include "support.h"
/* Release ordinary RAM reservation as well as its P2M entry. Special Xen
 * mappings instead use xen_remove_backing(), which only removes the entry. */
int xen_release_backing(uint64_t gpa)
{
    uint64_t pfn = gpa >> 12;
    struct xen_memory_reservation op = {
        .extent_start = (uintptr_t)&pfn,
        .nr_extents = 1,
        .domid = XEN_DOMID_SELF
    };

    if (gpa & (MICA_XEN_PAGE_SIZE - 1u))
        return -EINVAL;
    long rc = HYPERVISOR_memory_op(XENMEM_DECREASE, &op);

    return rc == 1 ? 0 : rc < 0 ? (int)rc : -EIO;
}

int xen_remove_backing(uint64_t gpa)
{
    struct xen_remove_physmap op = {
        .domid = XEN_DOMID_SELF,
        .gpfn = gpa >> 12
    };
    if (gpa & 4095u)
        return -EINVAL;
    return(int)HYPERVISOR_memory_op(XENMEM_REMOVE, &op);
}

int xen_restore_backing(uint64_t gpa)
{
    uint64_t pfn = gpa >> 12;

    struct xen_memory_reservation op = {
        .extent_start = (uintptr_t) & pfn,
        .nr_extents = 1,
        .domid = XEN_DOMID_SELF
    };
    if (gpa & 4095u)
        return -EINVAL;
    long rc = HYPERVISOR_memory_op(XENMEM_POPULATE, &op);
    return rc == 1 ? 0 : rc < 0 ? (int)rc : -ENOMEM;
}

int xen_special_release(struct xen_special_page *page)
{
    int rc;

    if (page->view_acquired)
    {
        rc = mica_mem_unmap(page->va, MICA_XEN_PAGE_SIZE);
        if (rc)
            return rc;
        page->view_acquired = false;
        page->va = NULL;
    }
    if (page->xen_mapped)
    {
        rc = xen_remove_backing(page->gpa);
        if (rc)
            return rc;
        page->xen_mapped = false;
    }
    if (page->backing_removed)
    {
        rc = xen_restore_backing(page->gpa);
        if (rc)
            return rc;
        page->backing_removed = false;
    }
    if (page->base_acquired)
    {
        rc = mica_mem_free(page->base);
        if (rc)
            return rc;
        page->base_acquired = false;
        page->base = NULL;
    }
    return 0;
}

int xen_add_shared(uint64_t gpa)
{
    struct xen_add_physmap op = {
        .domid = XEN_DOMID_SELF,
        .space = XENMAP_SHARED_INFO,
        .gpfn = gpa >> 12
    };
    if (gpa & (MICA_XEN_PAGE_SIZE - 1u))
        return -EINVAL;
    return(int)HYPERVISOR_memory_op(XENMEM_ADD, &op);
}

uint64_t xen_deadline(void)
{
    uint64_t now = mica_now_ns();

    return now > UINT64_MAX - XEN_WAIT_NS ? UINT64_MAX : now + XEN_WAIT_NS;
}
