/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "openamp/virtio.h"
#include "ped_rsc_table.h"
#include <metal/io.h>
#include "../mica_internal.h"

#ifndef OS_SEC_RSC_TABLE
#define OS_SEC_RSC_TABLE __attribute__((section(".resource_table")))
#endif

OS_SEC_RSC_TABLE static struct fw_resource_table resource_table = {
    .ver = 1,
    .num = RSC_TABLE_NUM_ENTRY,
    .offset = {
        offsetof(struct fw_resource_table, ept_table),
#ifdef OS_GDB_STUB
        offsetof(struct fw_resource_table, rbufs),
#endif
        offsetof(struct fw_resource_table, vdev),
#ifdef OS_OPTION_XEN
        offsetof(struct fw_resource_table, vring_offset),
#endif
    },

    .ept_table = {
        .type = RSC_VENDOR_EPT_TABLE,
    .num_of_epts = 0,
    },
#ifdef OS_GDB_STUB
    .rbufs = {RSC_VENDOR_RINGBUFFER, 0, 0, 0, RINGBUFFER_TOTAL_SIZE, 0, {0}},
#endif
    /* Virtio device entry */
    .vdev = {
        RSC_VDEV, VIRTIO_ID_RPMSG, 2, RPMSG_IPU_C0_FEATURES, 0, 0, 0,
        VRING_COUNT, {0, 0},
    },

    /* Vring rsc entry - part of vdev rsc entry */
    .vring0 = {VRING_TX_ADDRESS, VRING_ALIGNMENT,
                   NUM_RPMSG_BUFF, VRING0_ID, 0},
    .vring1 = {VRING_RX_ADDRESS, VRING_ALIGNMENT,
           NUM_RPMSG_BUFF, VRING1_ID, 0},
#ifdef OS_OPTION_XEN
    .vring_offset = {
        .type = RSC_VENDOR_VRING_OFFSET,
        .offset = {0, 0},
    },
#endif
};

void rsc_table_get(void **table_ptr, int *length)
{
    *table_ptr = (void *)&resource_table;
    *length = sizeof(resource_table);
}

int mica_rsc_layout(void *table, size_t bytes, struct metal_io_region *buffers, int(*notify)(void *,
    uint32_t), void *cookie, struct mica_rpmsg_layout *layout)
{
    if (!table || bytes < sizeof(struct fw_resource_table) || !buffers || !layout)
        return -EINVAL;
    struct fw_resource_table *runtime = table;

    struct fw_rsc_vdev_vring *rings[2] = {
        &runtime->vring0,
        &runtime->vring1
    };
    struct mica_rpmsg_layout result = {
        .runtime_table = table,
        .table_bytes = bytes,
        .buffer_io = buffers,
        .notify = notify,
        .notify_cookie = cookie
    };
    for (unsigned int i = 0; i < 2; ++i)
    {
        struct fw_rsc_vdev_vring *r = rings[i];

        if (!r->num || r->num > RPROC_MAX_VRING_DESC || (r->num & (r->num - 1)) || !r->align ||
            (r->align & (r->align - 1)) || r->align > 65536)
            return -EINVAL;
        size_t length = (size_t)vring_size(r->num, r->align);
        void *local = metal_io_phys_to_virt(buffers, r->da);

        if (!local)
            return -EINVAL;
        unsigned long offset = metal_io_virt_to_offset(buffers, local);

        if (offset == METAL_BAD_OFFSET || offset > buffers->size || length > buffers->size - offset)
            return -EINVAL;
        result.vring_local[i] = local;
        result.vring_bytes[i] = length;
    }
    *layout = result;
    return 0;
}

void rsc_table_set_offline_flag(void)
{
    struct mica_context *ctx = mica_context_get();

    if (mica_context_lock(ctx))
        return;
    struct fw_resource_table *table = ctx->layout.runtime_table;

    if (table)
    {
        table->reserved[0] = 0x84000002u;
        /* Stronger post-store ordering, not a cache/device-completion proof. */
        atomic_thread_fence(memory_order_seq_cst);
    }
    mica_context_unlock(ctx);
}
