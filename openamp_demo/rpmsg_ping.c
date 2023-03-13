/*
 * SPDX-License-Identifier: BSD-3-Clause
 */

/* This is a sample demonstration application that showcases usage of rpmsg 
This application is meant to run on the remote CPU running baremetal code. 
This application echoes back data that was sent to it by the master core. */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include "openamp_module.h"

#define LPRINTF(format, ...) printf(format, ##__VA_ARGS__)
#define LPERROR(format, ...) LPRINTF("ERROR: " format, ##__VA_ARGS__)

struct _payload {
	unsigned long num;
	unsigned long size;
	unsigned char data[];
};

#define PAYLOAD_MIN_SIZE	1
#define NUMS_PACKAGES 2000
#define RPMSG_ERR_NO_BUFF -2002
/* Globals */
int err_cnt = 0;
static struct _payload *i_payload;
static int rnum = 0;
static int ept_deleted = 0;

/* External functions */
extern int init_system();
extern void cleanup_system();

/*-----------------------------------------------------------------------------*
 *  RPMSG endpoint callbacks
 *-----------------------------------------------------------------------------*/
int ping_cb(struct rpmsg_endpoint *ept, void *data, size_t len,
			     uint32_t src, void *priv)
{
	int i;
	struct _payload *r_payload = (struct _payload *)data;

	(void)ept;
	(void)src;
	(void)priv;
	LPRINTF(" received payload number %lu of size %lu \r\n",
		r_payload->num, (unsigned long)len);

	if (r_payload->size == 0) {
		LPERROR(" Invalid size of package is received.\r\n");
		err_cnt++;
		return 0;
	}
	/* Validate data buffer integrity. */
	for (i = 0; i < (int)r_payload->size; i++) {
		if (r_payload->data[i] != 0xA5) {
			LPRINTF("Data corruption at index %d\r\n", i);
			err_cnt++;
			break;
		}
	}
	rnum = r_payload->num + 1;
	return 0;
}


void ping(unsigned int ep_id)
{
    int ret;
	int i;
	int size, max_size, num_payloads;
	int expect_rnum = 0;
    err_cnt = 0;
	LPRINTF(" 1 - Send data to remote core, retrieve the echo");
	LPRINTF(" and validate its integrity ..\r\n");

	max_size = 2 * sizeof(unsigned long) + 5;
	if (max_size < 0) {
		LPERROR("No available buffer size.\r\n");
		return -1;
	}
	max_size -= sizeof(struct _payload);
	num_payloads = max_size - PAYLOAD_MIN_SIZE + 1;
	i_payload =
	    (struct _payload *)metal_allocate_memory(2 * sizeof(unsigned long) +
				      max_size);

	if (!i_payload) {
		LPERROR("memory allocation failed.\r\n");
		return -1;
	}

	for (i = 0, size = PAYLOAD_MIN_SIZE; i < num_payloads; i++, size++) {
		i_payload->num = i;
		i_payload->size = size;

		/* Mark the data buffer. */
		memset(&(i_payload->data[0]), 0xA5, size);

		LPRINTF("sending payload number %lu of size %lu\r\n",
			i_payload->num,
			(unsigned long)(2 * sizeof(unsigned long)) + size);
        ret =  rpmsg_service_send(ep_id, i_payload,
				 (2 * sizeof(unsigned long)) + size);
		if (ret < 0) {
			LPERROR("Failed to send data...\r\n");
			break;
		}
		LPRINTF("echo test: sent : %lu\r\n",
			(unsigned long)(2 * sizeof(unsigned long)) + size);

		expect_rnum++;
	}
    LPRINTF("**********************************\r\n");
	LPRINTF(" Test Results: Error count = %d \r\n", err_cnt);
	LPRINTF("**********************************\r\n");
}

int flood_ping_cb(struct rpmsg_endpoint *ept, void *data, size_t len,
			     uint32_t src, void *priv)
{
	struct _payload *r_payload = (struct _payload *)data;
	unsigned char *r_buf, *i_buf;
	unsigned int i;

	(void)ept;
	(void)src;
	(void)priv;
	(void)len;

	if (r_payload->size == 0 || r_payload->size > 496) {
		LPERROR(" Invalid size of package is received 0x%x.\r\n",
			(unsigned int)r_payload->size);
		err_cnt++;
		return 0;
	}
	/* Validate data buffer integrity. */
	r_buf = (unsigned char*)r_payload->data;
	i_buf = (unsigned char*)i_payload->data;
	for (i = 0; i < (unsigned int)r_payload->size; i++) {
		if (*r_buf != *i_buf) {
			LPERROR("Data corruption %lu, size %lu\r\n",
				r_payload->num, r_payload->size);
			err_cnt++;
			break;
		}
		r_buf++;
		i_buf++;
	}
	rnum = r_payload->num + 1;
	return 0;
}

int flood_ping(unsigned int ep_id)
{
	int ret;
	int i, s, max_size;
	int num_pkgs;
    err_cnt = 0;
	LPRINTF(" 1 - Send data to remote core, retrieve the echo");
	LPRINTF(" and validate its integrity ..\r\n");

	num_pkgs = NUMS_PACKAGES;
    max_size = 2 * sizeof(unsigned long) + 5;
	if (max_size < 0) {
		LPERROR("No available buffer size.\r\n");
		return -1;
	}
	i_payload = (struct _payload *)metal_allocate_memory(max_size);

	if (!i_payload) {
		LPERROR("memory allocation failed.\r\n");
		return -1;
	}
	max_size -= sizeof(struct _payload);

	memset(&(i_payload->data[0]), 0xA5, max_size);
	for (s = PAYLOAD_MIN_SIZE; s <= max_size; s++) {
		int size;

		i_payload->size = s;
		size = sizeof(struct _payload) + s;
		LPRINTF("echo test: package size %d, num of packages: %d\r\n",
			size, num_pkgs);
		rnum = 0;
		for (i = 0; i < num_pkgs; i++) {
			i_payload->num = i;
			while (!err_cnt && !ept_deleted) {
                rpmsg_service_send(ep_id, i_payload,
				 (2 * sizeof(unsigned long)) + size);
				if (ret == RPMSG_ERR_NO_BUFF) {
					LPERROR("RPMSG_ERR_NO_BUFF...\r\n");
				} else if (ret < 0) {
					LPERROR("Failed to send data...\r\n");
					break;
				} else {
					break;
				}
			}
			if (ret < 0 || err_cnt || ept_deleted)
				break;
		}
		if (ret < 0)
			break;
		if (err_cnt || ept_deleted)
			break;
	}
    LPRINTF("**********************************\r\n");
	LPRINTF(" Test Results: Error count = %d \r\n", err_cnt);
	LPRINTF("**********************************\r\n");
	return 0;
}
