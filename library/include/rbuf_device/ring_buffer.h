/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RING_BUFFER_H
#define RING_BUFFER_H

typedef struct ring_buffer {
	unsigned int	in;
	unsigned int	out;
	unsigned int	len;
	unsigned int	esize;
	char		    data[0];
} ring_buffer_t;

int ring_buffer_pair_init(void *rxaddr, void *txaddr, int len);
int ring_buffer_read(ring_buffer_t *ring_buffer, char *buf, int len);
int ring_buffer_write(ring_buffer_t *ring_buffer, char *buf, int len);

#endif
