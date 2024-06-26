/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/mman.h>
#include <errno.h>

#include <rbuf_device/ring_buffer.h>

#include "debug/mica_debug_common.h"
#include "test_ring_buffer.h"
#include "debug/mica_debug_ring_buffer.h"

int main(void)
{
	// open mcs device
	int ret = open(MCS_DEVICE_NAME, O_RDWR | O_SYNC);

	if (ret < 0) {
		printf("open %s device failed\n", MCS_DEVICE_NAME);
		return ret;
	}
	int mcs_fd = ret;
	// mmap shared memory
	void *virt_addr = mmap(NULL, RING_BUFFER_LEN * 2, PROT_READ | PROT_WRITE, MAP_SHARED, mcs_fd, RING_BUFFER_PA);

	if (virt_addr == MAP_FAILED) {
		printf("mmap failed: sh_mem_addr:%p\n", virt_addr);
		return -EPERM;
	}
	void *rx_buffer = virt_addr + RING_BUFFER_LEN, *tx_buffer = virt_addr;
	// read and write message from ring buffer
	char recv_buf[MAX_BUFF_LENGTH];

	while (1) {
		int n_bytes = ring_buffer_read(rx_buffer, recv_buf, MAX_BUFF_LENGTH);

		if (n_bytes == -1) {
			perror("ring_buffer_read error");
			ret = -1;
			break;
		} else if (n_bytes == 0)
			continue;
		recv_buf[n_bytes] = '\0';
		printf("read from ring buffer: %s\n", recv_buf);
		char *send_buf = "hello world";

		n_bytes = ring_buffer_write(tx_buffer, send_buf, strlen(send_buf));
		if (n_bytes == -1) {
			perror("ring_buffer_write error");
			ret = -1;
			break;
		}
		printf("write to ring buffer: %s\n", send_buf);
	}

	munmap(virt_addr, RING_BUFFER_LEN * 2);
	close(mcs_fd);
	return 0;
}
