/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "openamp/virtio.h"
#include "ped_rsc_table.h"

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
