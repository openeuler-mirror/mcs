/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <metal/device.h>
#include <metal/cache.h>
#include <mica/mica.h>
#include <mica/platform/macro.h>
#include <mica/platform/sem.h>
#include <mica/platform/barrier.h>
#include <mica/platform/io.h>
#include <mica/platform/irq.h>
#include <mica/platform/log.h>
#include "ped_openamp.h"
#include "ped_rsc_table.h"
#include "mica_ped.h"
#include "../services/mica_service_internal.h"

/*
 * Use resource tables's  reserved[0] to carry some extra information.
 * The following IDs come from PSCI definition
 */
#define CPU_ON_FUNCID    0xC4000003
#define CPU_OFF_FUNCID   0x84000002
#define SYSTEM_RESET     0x84000009


/* riscv irq register macros */
#define IPC_INT_A55MP_NUM      0x0
#define IPC_INT_RISCV_NUM      0x6

#define IPC_INT_SET          (0x00)
#define IPC_INT_CLEAR        (0x04)
#define IPC_INT_MSTS         (0x08)
#define IPC_INT_MASK         (0x0C)
#define IPC_INT_RSTS         (0x10)

static mica_sem_t g_ipi_sem;
void (*g_ipi_handler)(void);
static bool g_irq_initialized = false;
static struct virtio_device *g_vdev;
static struct rpmsg_virtio_device g_rvdev;
static struct mica_config *g_ped_mica_config = NULL;

/* shared memory device */
static metal_phys_addr_t shm_physmap[1];

static struct metal_device shm_device = {
    .name = SHM_DEVICE_NAME,
    .num_regions = 2,
    .regions = {
        /*
         * shared memory io, only the addr in [share mem start + share mem size]
         * can be accessed and guaranteed by metal_io_read/write
         */
        {.virt = NULL},
        /*
         * resource table io, only the addr in [resource table start + table size]
         * can be accessed and guaranteed by metal_io_read/write
         */
        {.virt = NULL},
    },
    .node = { NULL },
    .irq_num = 0,
    .irq_info = NULL
};

/* rsc table methods */

static inline struct fw_rsc_vdev *rsc_table_to_vdev(void *rsc_table)
{
    return &((struct fw_resource_table *)rsc_table)->vdev;
}

static inline struct fw_rsc_vdev_vring *rsc_table_get_vring0(void *rsc_table)
{
    return &((struct fw_resource_table *)rsc_table)->vring0;
}

static inline struct fw_rsc_vdev_vring *rsc_table_get_vring1(void *rsc_table)
{
    return &((struct fw_resource_table *)rsc_table)->vring1;
}

/* operations on riscv irq register */
static inline void clear_riscv_irq(void)
{
    mica_writel(IPC_INT_RISCV_NUM, g_ped_mica_config->ipc_irq_base + IPC_INT_CLEAR);
}

static inline void mask_riscv_irq(void)
{
    mica_writel(IPC_INT_RISCV_NUM, g_ped_mica_config->ipc_irq_base + IPC_INT_MASK);
}

static inline void unmask_riscv_irq(void)
{
    mica_writel(0x80000000 + IPC_INT_RISCV_NUM, g_ped_mica_config->ipc_irq_base + IPC_INT_MASK);
}

/* rpmsg operations */
static int virtio_notify(void *priv, uint32_t id)
{
    mica_writel(IPC_INT_A55MP_NUM, g_ped_mica_config->ipc_irq_base + IPC_INT_SET);
    return MICA_SUCCESS;
}

static void reset_vq(void)
{
    if (g_rvdev.svq != NULL) {
        /*
         * For svq:
         * vq_free_cnt: Set to vq_nentries, all descriptors in the svq are available.
         * vq_queued_cnt: Set to 0, no descriptors waiting to be processed in the svq.
         * vq_desc_head_idx: Set to 0, the next available descriptor is at the beginning
         *                   of the descriptor table.
         * vq_available_idx: Set to 0, No descriptors have been added to the available ring.
         * vq_used_cons_idx: No descriptors have been added to the used ring.
         * vq_ring.avail->idx and vq_ring.used->idx will be set at host.
         */
        g_rvdev.svq->vq_free_cnt = g_rvdev.svq->vq_nentries;
        g_rvdev.svq->vq_queued_cnt = 0;
        g_rvdev.svq->vq_desc_head_idx = 0;
        g_rvdev.svq->vq_available_idx = 0;
        g_rvdev.svq->vq_used_cons_idx = 0;
    }

    if (g_rvdev.rvq != NULL) {
        /*
         * For rvq:
         * Because host resets its tx vq, on the remote side,
         * it also needs to reset the rx rq.
         */
        g_rvdev.rvq->vq_available_idx = 0;
        g_rvdev.rvq->vq_used_cons_idx = 0;
        g_rvdev.rvq->vq_ring.used->idx = 0;
        g_rvdev.rvq->vq_ring.avail->idx = 0;
        metal_cache_flush(&(g_rvdev.rvq->vq_ring.used->idx),
                          sizeof(g_rvdev.rvq->vq_ring.used->idx));
        metal_cache_flush(&(g_rvdev.rvq->vq_ring.avail->idx),
                          sizeof(g_rvdev.rvq->vq_ring.avail->idx));
    }
}
static void ped_hetero_receive_message(void)
{
    if (mica_sem_wait(g_ipi_sem) == MICA_SUCCESS) {
        rproc_virtio_notified(g_vdev, VRING1_ID);
    }
}

static void handle_ipi(void)
{
    struct fw_resource_table *rsc_table;
    uint32_t status;

    /* 读取resource table状态 */
    rsc_table = (struct fw_resource_table *)g_ped_mica_config->shm_base_addr;
    mica_mb();

    status = rsc_table->reserved[0];

    /* 根据状态处理 */
    if (status == 0 || status == CPU_ON_FUNCID) {
        /* 正常消息 */
        mica_sem_post(g_ipi_sem);
    } else if (status == SYSTEM_RESET) {
        /* 重置virtqueue */
        reset_vq();
        rsc_table->reserved[0] = 0;
        mica_mb();
    } else if (status == CPU_OFF_FUNCID) {
        /* 下电请求 */
        if (g_ped_mica_config->sys_ops.system_poweroff) {
            g_ped_mica_config->sys_ops.system_poweroff();
        }
    }
}

static void hetero_irq_handler(void)
{
    /* mask irq */
    mask_riscv_irq();
    /* clear irq flags */
    clear_riscv_irq();

    if (g_ipi_handler)
        g_ipi_handler();

    /* unmask irq */
    unmask_riscv_irq();
}
static void ipi_handler_init_done(void)
{
    g_ipi_handler = handle_ipi;
}

/* initializing mica ipi */
static int ped_hetero_init_irq(struct mica_config *config)
{
    int ret;

    g_ped_mica_config = config;

    clear_riscv_irq();
    unmask_riscv_irq();

    /* 注册中断 */
    ret = mica_request_irq(config->ipc_irq_num, hetero_irq_handler);
    if (ret) {
        return ret;
    }

    mica_unmask_irq(config->ipc_irq_num);

    ipi_handler_init_done();

    return MICA_SUCCESS;
}

/* placeholder for setting offline */
void rsc_table_set_offline_flag(void)
{
    void *rsc;
    int rsc_size;
    uint32_t status;
    struct fw_resource_table *rsc_table;

    rsc_table = (struct fw_resource_table *)g_ped_mica_config->shm_base_addr;

    rsc_table->reserved[0] = CPU_OFF_FUNCID;
    mica_mb();
}

/* create virtio device */
static struct virtio_device *
platform_create_vdev(void *rsc_table, struct metal_io_region *rsc_io)
{
    struct fw_rsc_vdev_vring *vring_rsc;
    struct virtio_device *vdev;
    int ret;

    vdev = rproc_virtio_create_vdev(VIRTIO_DEV_DEVICE, VDEV_ID,
                    rsc_table_to_vdev(rsc_table),
                    rsc_io, NULL, virtio_notify, NULL);
    if (!vdev)
        return NULL;

    /* wait master rpmsg init completion */
    rproc_virtio_wait_remote_ready(vdev);

    vring_rsc = rsc_table_get_vring0(rsc_table);
    mica_log("[openamp]: get vring0: da %lx\n", vring_rsc->da);

    ret = rproc_virtio_init_vring(vdev, 0, vring_rsc->notifyid,
                      (void *)(uintptr_t)vring_rsc->da, rsc_io,
                      vring_rsc->num, vring_rsc->align);
    if (ret)
        goto failed;

    vring_rsc = rsc_table_get_vring1(rsc_table);
    mica_log("[openamp]: get vring1: da %lx\n", vring_rsc->da);

    ret = rproc_virtio_init_vring(vdev, 1, vring_rsc->notifyid,
                      (void *)(uintptr_t)vring_rsc->da, rsc_io,
                      vring_rsc->num, vring_rsc->align);
    if (ret)
        goto failed;

    return vdev;

failed:
    rproc_virtio_remove_vdev(vdev);
    return NULL;
}

/* initializing rpmsg backend */
static int ped_hetero_init_rpmsg(struct mica_config *config)
{
    void *rsc_table;
    struct metal_io_region *rsc_io, *shm_io;
    int rsc_size;
    int32_t err;
    struct metal_init_params metal_params = METAL_INIT_DEFAULTS;
    struct metal_device *device;
    static struct rpmsg_device *rpdev;

    g_ped_mica_config = config;

    shm_physmap[0] = config->shm_base_addr;

    err = mica_sem_init(&g_ipi_sem, 0);
    if (err) {
        mica_log("[openamp] mica_sem_init failed %d\n", err);
        goto cleanup_ipi;
    }

    /* Libmetal setup */
    err = metal_init(&metal_params);
    if (err) {
        mica_log("[openamp] metal_init failed %d\n", err);
        goto cleanup_ipi;
    }

    err = metal_register_generic_device(&shm_device);
    if (err) {
        mica_log("[openamp] Couldn't register shared memory device %d\n", err);
        goto cleanup_metal;
    }

    err = metal_device_open("generic", SHM_DEVICE_NAME, &device);
    if (err) {
        mica_log("[openamp] metal_device_open failed %d\n", err);
        goto cleanup_metal;
    }

    metal_io_init(&device->regions[0], (void *)g_ped_mica_config->shm_base_addr, shm_physmap,
              g_ped_mica_config->shm_size, -1, 0, NULL);

    shm_io = metal_device_io_region(device, 0);
    if (!shm_io) {
        err = -EFAULT;
        mica_log("[openamp] get shared memory io region failed %d\n", err);
        goto cleanup_metal;
    }

    rsc_table_get(&rsc_table, &rsc_size);
    rsc_table = (struct fw_resource_table *)g_ped_mica_config->shm_base_addr;

    metal_io_init(&device->regions[1], rsc_table, (metal_phys_addr_t *)rsc_table,
              rsc_size, -1, 0, NULL);

    rsc_io = metal_device_io_region(device, 1);
    if (!rsc_io) {
        err = -EFAULT;
        mica_log("[openamp] get rsctable io region failed %d\n", err);
        goto cleanup_metal;
    }

    /* virtio device setup */
    g_vdev = platform_create_vdev(rsc_table, rsc_io);
    if (!g_vdev) {
        err = -ENODEV;
        mica_log("[openamp] create virtio device failed %d\n", err);
        goto cleanup_metal;
    }

    /* setup g_rvdev */
    err = rpmsg_init_vdev_with_config(&g_rvdev, g_vdev, NULL, shm_io, NULL, RPMSG_VIRTIO_CONSOLE_CONFIG);
    if (err) {
        mica_log("[openamp] rpmsg_init_vdev_with_config failed %d\n", err);
        goto cleanup_vdev;
    }

    rpdev = rpmsg_virtio_get_rpmsg_device(&g_rvdev);

    mica_set_rpdev(rpdev);

    return 0;

cleanup_vdev:
    rproc_virtio_remove_vdev(g_vdev);
cleanup_metal:
    metal_finish();
cleanup_ipi:
    mica_sem_destroy(g_ipi_sem);
    return err;
}

/* 反初始化 */
static void ped_hetero_deinit(void)
{
    rpmsg_deinit_vdev(&g_rvdev);
    rproc_virtio_remove_vdev(g_vdev);
    metal_finish();
    mica_sem_destroy(g_ipi_sem);
    /* TODO: disable openamp ipi */
}

/* 底座操作接口 */
static const struct mica_pedestal_ops hetero_ops = {
    .init_irq = ped_hetero_init_irq,
    .init_rpmsg = ped_hetero_init_rpmsg,
    .rcv_message = ped_hetero_receive_message,
    .deinit = ped_hetero_deinit,
};

/* 获取底座操作接口 */
const struct mica_pedestal_ops *mica_get_ped_ops(void)
{
    return &hetero_ops;
}