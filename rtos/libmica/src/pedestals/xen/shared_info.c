/* SPDX-License-Identifier: Apache-2.0
 * Copyright (c) 2021 EPAM Systems
 * Modified 2026 from Zephyr v4.2.0 arch/arm64/core/xen/enlighten.c:
 * domain-owned OS slot and CPU alias; preserve every partial resource.
 * A01: remove RAM backing before replacement, restore before any OS release.
 */
#include "support.h"
struct xen_shared_info *mica_xen_shared;
static struct xen_special_page shared;

int xen_shared_init(void)
{
    int rc = mica_mem_alloc(MICA_XEN_PAGE_SIZE, MICA_XEN_PAGE_SIZE, &shared.base);

    if (rc)
        goto fail;
    shared.base_acquired = true;
    rc = mica_mem_address(shared.base, &shared.gpa);
    if (rc)
        goto fail;
    if (shared.gpa & (MICA_XEN_PAGE_SIZE - 1u))
    {
        rc = -EINVAL;
        goto fail;
    }
    rc = xen_release_backing(shared.gpa);
    if (rc)
        goto fail;
    shared.backing_removed = true;
    rc = xen_add_shared(shared.gpa);
    if (rc)
        goto fail;
    shared.xen_mapped = true;
    rc = mica_mem_map(shared.gpa, MICA_XEN_PAGE_SIZE, &shared.va);
    if (rc)
        goto fail;
    shared.view_acquired = true;
    mica_xen_shared = shared.va;
    return 0;
fail:
    /* Preserve the original init error. Cleanup may itself retain a domain
     * residual; sticky NOT_READY never retries init or forgets this ledger. */
    (void)xen_special_release(&shared);
    return rc;
}
