/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef _PED_OPENAMP_INTERNAL_H__
#define _PED_OPENAMP_INTERNAL_H__

#define SHM_DEVICE_NAME		"mica_device"

#define VRING_COUNT		2

#define VRING0_ID 0 /* (master to remote) fixed to 0 for Linux compatibility */
#define VRING1_ID 1 /* (remote to master) fixed to 1 for Linux compatibility */

#define VRING_RX_ADDRESS        -1  /* allocated by Master processor */
#define VRING_TX_ADDRESS        -1  /* allocated by Master processor */

#define VRING_ALIGNMENT		4

#define VDEV_ID                 0xFF

#define RPMSG_IPU_C0_FEATURES   1
#define NUM_RPMSG_BUFF          8

#define RPMSG_VIRTIO_CONSOLE_CONFIG        \
    (&(const struct rpmsg_virtio_config) { \
        .h2r_buf_size = 512,  \
        .r2h_buf_size = 512,  \
        .split_shpool = false,\
})

#endif /* _PED_OPENAMP_INTERNAL_H__ */
