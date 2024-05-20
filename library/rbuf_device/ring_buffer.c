/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <rbuf_device/ring_buffer.h>

static void ring_buffer_lock(ring_buffer_t *ring_buffer)
{
	while (ring_buffer->busy) {
		;
	}
	ring_buffer->busy = 1;
}

static void ring_buffer_unlock(ring_buffer_t *ring_buffer)
{
	ring_buffer->busy = 0;
}

static inline int is_empty(ring_buffer_t *o)
{
	return o->head == o->tail;
}

static inline int is_full(ring_buffer_t *o)
{
	return (o->tail + 1) % o->len == o->head;
}

ring_buffer_t *ring_buffer_init(void *addr, int len)
{
	ring_buffer_t *ring_buffer = (ring_buffer_t *)addr;

	ring_buffer->busy = 0;
	ring_buffer->len = len;
	ring_buffer->tail = ring_buffer->head = 0;
	for (int i = 0; i < sizeof(ring_buffer->redzone); i++) {
		ring_buffer->redzone[i] = 0x7a;
	}
	return ring_buffer;
}

int ring_buffer_pair_init(void *rxaddr, void *txaddr, int len)
{
	if (!rxaddr || !txaddr || len <= sizeof(ring_buffer_t)) {
		return -1;
	}
	ring_buffer_init(rxaddr, len - sizeof(ring_buffer_t));
	ring_buffer_init(txaddr, len - sizeof(ring_buffer_t));
	return 0;
}

int readable(ring_buffer_t *o)
{
	int ret;

	ring_buffer_lock(o);
	ret = !is_empty(o);
	ring_buffer_unlock(o);
	return ret;
}

int writable(ring_buffer_t *o)
{
	int ret;

	ring_buffer_lock(o);
	ret = !is_full(o);
	ring_buffer_unlock(o);
	return ret;
}

int ring_buffer_write(ring_buffer_t *ring_buffer, char *buf, int len)
{
	int olen = ring_buffer->len;
	int cnt = 0;
	char *obuf = (char *)ring_buffer + sizeof(ring_buffer_t);

	ring_buffer_lock(ring_buffer);
	while (cnt < len) {
		if (is_full(ring_buffer)) {
			break;
		}
		obuf[(ring_buffer->tail++) % olen] = buf[cnt++];
	}
	ring_buffer_unlock(ring_buffer);

	return cnt;
}

int ring_buffer_read(ring_buffer_t *ring_buffer, char *buf, int len)
{
	int olen = ring_buffer->len;
	char *obuf = (char *)ring_buffer + sizeof(ring_buffer_t);
	int cnt = 0;

	ring_buffer_lock(ring_buffer);
	while (cnt < len) {
		if (is_empty(ring_buffer)) {
			break;
		}
		buf[cnt++] = obuf[(ring_buffer->head++) % olen];
	}
	ring_buffer_unlock(ring_buffer);

	return cnt;
}
