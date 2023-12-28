/* SPDX-License-Identifier: MulanPSL-2.0 */

#include "benchmark.h"
#include "openamp_module.h"
#include <limits.h>
#include <stdio.h>
#include <time.h>
#include <unistd.h>

/* Globals */
static struct _payload *i_payload;
static struct _large_msg *i_large_msg;
static int64_t count_ping_msg, min_ping, total_ping, max_ping;
static int ping_flag = 1;

#define MSG_BUFFER_SIZE 480
#define MSG_DATA 2023
#define MSG_PING_SIZE 32

/* Used to record the sending time of ping. */
struct timespec prev, now;

static int benchmark_rpc_ping_cb(struct rpmsg_endpoint *ept, void *data, size_t len, uint32_t src, void *priv)
{
	int64_t ping_delay;
	int64_t avg_ping;

	get_current_time(&now);
	ping_delay = calcdiff_us(now, prev);

	count_ping_msg++;
	min_ping = ping_delay < min_ping ? ping_delay : min_ping;
	max_ping = ping_delay > max_ping ? ping_delay : max_ping;
	total_ping += ping_delay;
	avg_ping = total_ping / count_ping_msg;

	printf("Policy: ping_msg.  Reply from remote os:\n");
	printf("Count:%6ld | Realtime:%6ldμs | Min:%6ldμs | Avg:%6ldμs | Max:%6ldμs\n\e[2A", count_ping_msg, ping_delay, min_ping, avg_ping, max_ping);
	ping_flag++;
	return 0;
}

static int benchmark_rpc_long_ping_cb(struct rpmsg_endpoint *ept, void *data, size_t len, uint32_t src, void *priv)
{
	int64_t ping_delay;
	int64_t avg_ping;
	struct _large_msg *r_payload = (struct _large_msg *)data;

	if (r_payload->flag == -1) {
		get_current_time(&now);
		ping_delay = calcdiff_us(now, prev);

		count_ping_msg++;
		min_ping = ping_delay < min_ping ? ping_delay : min_ping;
		max_ping = ping_delay > max_ping ? ping_delay : max_ping;
		total_ping += ping_delay;
		avg_ping = total_ping / count_ping_msg;

		printf("Policy: long_ping_msg.  Reply from remote os:\n");
		printf("Count:%6ld | Realtime:%6ldμs | Min:%6ldμs | Avg:%6ldμs | Max:%6ldμs\n\e[2A", count_ping_msg, ping_delay, min_ping, avg_ping, max_ping);
		ping_flag++;
	}

	return 0;
}

static int rpc_ping_id, rpc_long_ping_id;

int benchmark_service_init()
{
	int ret;

	/* Initialize global variables */
	count_ping_msg = 0, min_ping = INT_MAX, total_ping = 0, max_ping = 0;

	ret = rpmsg_service_register_endpoint(BENCHMARK_RPC_PING, benchmark_rpc_ping_cb, NULL, &rpc_ping_id);
	if (ret >= 0) {
		rpc_ping_id = ret;
		printf("BENCHMARK_RPC_PING registered successfully!\n");
	} else {
		return ret;
	}

	ret = rpmsg_service_register_endpoint(BENCHMARK_RPC_LONG_PING, benchmark_rpc_long_ping_cb, NULL, &rpc_long_ping_id);
	if (ret >= 0) {
		rpc_long_ping_id = ret;
		printf("BENCHMARK_RPC_LONG_PING registered successfully!\n");
		return 0;
	} else {
		return ret;
	}
}

static void send_ping_msg()
{
	int ret;

	if (ping_flag == 0)
		return;

	ping_flag--;
	get_current_time(&prev);
	ret = rpmsg_service_send(rpc_ping_id, i_payload, (2 * sizeof(unsigned long) + MSG_PING_SIZE));
	if (ret < 0)
		printf("ping msg failed to send data...\n");
	usleep(200000);
}

void ping(int loop)
{
	int ret;
	i_payload = (struct _payload *)metal_allocate_memory(2 * sizeof(unsigned long) + MSG_PING_SIZE);

	if (!i_payload) {
		printf("memory allocation failed.\r\n");
		return;
	}

	i_payload->num = 0;
	i_payload->size = MSG_PING_SIZE;
	for (int i = 0; i < MSG_PING_SIZE / sizeof(unsigned long); i++)
		i_payload->data[i] = MSG_DATA;

	if (!loop) {
		while (1) {
			if (ping_flag)
				send_ping_msg();
		}
	} else {
		for (int i = 0; i < loop; i++)
			if (ping_flag)
				send_ping_msg();
	}
	metal_free_memory(i_payload);
}

static void send_long_msg(int num_segments)
{
	int ret;

	for (int i = 0, flag = 0; i < num_segments; i++) {
		i_large_msg->flag = i;
		/* Fill 480 bytes of data 59*8+8 Bytes. */
		for (int j = 0; j < (MSG_BUFFER_SIZE - sizeof(unsigned long)) / sizeof(unsigned long); j++)
			i_large_msg->data[j] = MSG_DATA;

		if (i == num_segments - 1)
			i_large_msg->flag = -1;

		if (i_large_msg->flag == 0)
			get_current_time(&prev);
		ret = rpmsg_service_send(rpc_long_ping_id, i_large_msg, MSG_BUFFER_SIZE);
		if (ret < 0) {
			printf("long ping msg failed to send data...\n");
			break;
		}
	}
}

void long_ping(unsigned long total_size, int loop)
{
	int ret;
	int num_segments = total_size / MSG_BUFFER_SIZE;

	if (total_size < MSG_BUFFER_SIZE) {
		printf("long ping msg total_size < %d\n", MSG_BUFFER_SIZE);
		return;
	}

	i_large_msg = (struct _large_msg *)metal_allocate_memory(MSG_BUFFER_SIZE);

	if (!loop) {
		while (1) {
			if (ping_flag--) {
				send_long_msg(num_segments);
				usleep(200000);
			}
		}
	} else {
		if (ping_flag--)
			for (int i = 0; i < loop; i++) {
				send_long_msg(num_segments);
				usleep(200000);
			}
	}

	metal_free_memory(i_large_msg);
}
