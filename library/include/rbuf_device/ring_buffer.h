/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RING_BUFFER_H
#define RING_BUFFER_H

typedef struct ring_buffer {
	int len;
	volatile int busy; // lock
	volatile int tail; // writer pos
	volatile int head; // reader pos
	char redzone[8];
} ring_buffer_t;

int ring_buffer_pair_init(void *rxaddr, void *txaddr, int len);
int readable(ring_buffer_t *o);
int writable(ring_buffer_t *o);
int ring_buffer_write(ring_buffer_t *ring_buffer, char *buf, int len);
int ring_buffer_read(ring_buffer_t *ring_buffer, char *buf, int len);

#endif
