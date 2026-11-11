/* SPDX-License-Identifier: MIT
 * (C) 2006 - Cambridge University
 * (C) 2021-2024 - EPAM Systems
 * Modified 2026 from Zephyr v4.2.0 drivers/xen/gnttab.c: import-only,
 * per-page status/handle ledger and reverse retryable rollback; OS slots,
 * no fixed GPA, Zephyr/PRT allocator or grant-export table. See SOURCES.md
 * and LICENSES/THIRD_PARTY_LICENSES.txt for the full notice and terms.
 * A01 modification: provider removes RAM backing and restores every page
 * before releasing the original OS allocation; failed restores remain owned.
 */
#include "support.h"
#include <stdlib.h>
#include <limits.h>
/* OS reserve starts RAM-backed. removed stays true until populate succeeds;
 * mapped independently records an outstanding Xen grant handle. */
struct grant_page
{
    uint64_t gpa;
    uint32_t handle;
    bool removed;
    bool mapped;
};

struct mica_xen_mapping
{
    void *base;
    bool base_acquired;
    size_t count;
    struct grant_page pages[];
};

int xen_grant_unmap(struct mica_xen_mapping *map)
{
    int error = 0;

    if (!map)
        return -EINVAL;
    for (size_t n = map->count; n > 0; --n)
    {
        struct grant_page *page = &map->pages[n - 1];
        int rc = 0;

        if (page->mapped)
        {
            struct xen_grant_unmap op = {
                .host_addr = page->gpa,
                .handle = page->handle,
                .status = -1
            };
            rc = (int)HYPERVISOR_grant_table_op(GNT_UNMAP, &op, 1);
            if (!rc && op.status)
                rc = -EIO;
            if (!rc)
                page->mapped = false;
        }
        /* Never populate over a grant still owned by Xen. Other pages may be
         * restored independently, but the original whole allocation is kept. */
        if (!rc && page->removed)
        {
            rc = xen_restore_backing(page->gpa);
            if (!rc)
                page->removed = false;
        }
        if (rc)
        {
            if (!error)
                error = rc;
        }
    }
    if (error)
        return error;
    if (map->base_acquired)
    {
        int rc = mica_mem_free(map->base);

        if (rc)
            return rc;
        map->base_acquired = false;
    }
    free(map);
    return 0;
}

int xen_grant_map(uint16_t domid, const uint32_t *refs, size_t count,
    struct mica_xen_mapping **cookie, void **local, size_t *bytes)
{
    struct mica_xen_mapping *map;
    void *base = NULL;
    int rc;

    if (cookie)
        *cookie = NULL;
    if (local)
        *local = NULL;
    if (bytes)
        *bytes = 0;
    if (!cookie || !local || !bytes || !refs || !count)
        return -EINVAL;
    if (domid != 0)
        return -EOPNOTSUPP;
    if (count > SIZE_MAX / MICA_XEN_PAGE_SIZE ||
        count > (SIZE_MAX - sizeof(*map)) / sizeof(map->pages[0]))
        return -EOVERFLOW;
    map = calloc(1, sizeof(*map) + count * sizeof(map->pages[0]));
    if (!map)
        return -ENOMEM;
    map->count = count;
    rc = mica_mem_alloc(count * MICA_XEN_PAGE_SIZE, MICA_XEN_PAGE_SIZE, &map->base);
    if (rc)
        goto fail;
    map->base_acquired = true;
    base = map->base;
    /* Capture every GPA while the entire allocator window is still backed.
     * The ledger itself is outside the window Xen replaces. */
    for (size_t n = 0; n < count; ++n)
    {
        uint64_t gpa = 0;

        rc = mica_mem_address((char *)base + n * MICA_XEN_PAGE_SIZE, &gpa);
        if (rc)
            goto fail;
        if (gpa & (MICA_XEN_PAGE_SIZE - 1u))
        {
            rc = -EINVAL;
            goto fail;
        }
        map->pages[n].gpa = gpa;
    }
    for (size_t n = 0; n < count; ++n)
    {
        uint64_t gpa = map->pages[n].gpa;

        rc = xen_release_backing(gpa);
        if (rc)
            goto fail;
        map->pages[n].removed = true;
        struct xen_grant_map op = {
            .host_addr = gpa,
            .flags = GNT_HOST_MAP,
            .ref = refs[n],
            .dom = 0,
            .status = -1
        };
        rc = (int)HYPERVISOR_grant_table_op(GNT_MAP, &op, 1);
        if (!rc && op.status)
            rc = op.status == -12 ? -EAGAIN : -EIO;
        if (rc)
            goto fail;
        map->pages[n].handle = op.handle;
        map->pages[n].mapped = true;
    }
    *cookie = map;
    *local = base;
    *bytes = count * MICA_XEN_PAGE_SIZE;
    return 0;
fail:
    if (xen_grant_unmap(map))
        *cookie = map;
    return rc;
}
