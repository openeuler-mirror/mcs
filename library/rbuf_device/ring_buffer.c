/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <rbuf_device/ring_buffer.h>
#include <string.h>

static ring_buffer_t * ring_buffer_init(void *addr, int len)
{
    ring_buffer_t *ring_buffer = (ring_buffer_t *)addr;
    ring_buffer->in = ring_buffer->out = 0;
    ring_buffer->len = len;
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

#ifdef __x86_64__
#define mb()    asm volatile("mfence":::"memory")
#define rmb()   asm volatile("lfence":::"memory")
#define wmb()   asm volatile("sfence" ::: "memory")
#elif __aarch64__
#define dmb(opt)    __asm__ volatile("dmb " #opt : : : "memory")
#define dsb(opt)    __asm__ volatile("dsb " #opt : : : "memory")

#define mb()        dsb(sy)
#define rmb()       dsb(ld)
#define wmb()       dsb(st)
#else
#error  "unsupported arch"
#endif

#define min(a, b) (((a) < (b)) ? (a) : (b))
/*
 * internal helper to calculate the unused elements in a fifo
 */
static inline unsigned int kfifo_unused(struct ring_buffer *fifo)
{
	return fifo->len - (fifo->in - fifo->out);
}

static void kfifo_copy_in(struct ring_buffer *fifo, const void *src,
		unsigned int len, unsigned int off)
{
	unsigned int size = fifo->len;
	unsigned int l;

	off = off % size;
	l = min(len, size - off);

	memcpy(fifo->data + off, src, l);
	memcpy(fifo->data, src + l, len - l);
	/*
	 * make sure that the data in the fifo is up to date before
	 * incrementing the fifo->in index counter
	 */
	wmb();
}
/*

*/
static unsigned int __kfifo_in(struct ring_buffer *fifo,
		const void *buf, unsigned int len)
{
	unsigned int l;

	l = kfifo_unused(fifo);
	if (len > l)
		len = l;

	kfifo_copy_in(fifo, buf, len, fifo->in);
	fifo->in += len;
	return len;
}


static void kfifo_copy_out(struct ring_buffer *fifo, void *dst,
		unsigned int len, unsigned int off)
{
	unsigned int size = fifo->len;
	unsigned int l;

	off %= size;
	l = min(len, size - off);

	memcpy(dst, fifo->data + off, l);
	memcpy(dst + l, fifo->data, len - l);
	/*
	 * make sure that the data is copied before
	 * incrementing the fifo->out index counter
	 */
	wmb();
}

static unsigned int __kfifo_out_peek(struct ring_buffer *fifo,
		void *buf, unsigned int len)
{
	unsigned int l;

	l = fifo->in - fifo->out;
	if (len > l)
		len = l;

	kfifo_copy_out(fifo, buf, len, fifo->out);
	return len;
}


static unsigned int __kfifo_out(struct ring_buffer *fifo,
		void *buf, unsigned int len)
{
	len = __kfifo_out_peek(fifo, buf, len);
	fifo->out += len;
	return len;
}

int ring_buffer_write(ring_buffer_t *ring_buffer, char *buf, int len)
{
    int cnt = 0;
        while (cnt < len) {
            int o = __kfifo_in(ring_buffer, &buf[cnt], len - cnt);
            cnt += o;
        }
    return cnt;
}

int ring_buffer_read(ring_buffer_t *ring_buffer, char *buf, int len)
{
    return __kfifo_out(ring_buffer, buf, len);
}
