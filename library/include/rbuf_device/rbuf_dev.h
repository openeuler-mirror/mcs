/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RBUF_DEV_H
#define RBUF_DEV_H

#include <metal/io.h>

#include "mica/mica_client.h"

#if defined __cplusplus
extern "C" {
#endif

/*
 * Ring buffer device
 * @rbuf_pa: physical address of the ring buffer
 * @tx_addr: virtual address of the ring buffer for tx
 * @rx_addr: virtual address of the ring buffer for rx
 * @rbuf_len: length of each ring buffer
 */
struct rbuf_device {
	void *tx_va;
	void *rx_va;
	int rbuf_len;
};

int create_rbuf_device(struct mica_client *client);
void destroy_rbuf_device(struct mica_client *client);

#if defined __cplusplus
}
#endif

#endif	/* RBUF_DEV_H */
