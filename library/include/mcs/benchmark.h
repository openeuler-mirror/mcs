#ifndef BENCHMARK_H
#define BENCHMARK_H
#include "openamp_module.h"
#include <sys/time.h>
#include <sys/timeb.h>
#include <time.h>
#include "rpmsg_rpc_service.h"

struct _payload{
    unsigned long num;
    unsigned long size;
    unsigned long data[];
};

struct _large_msg{
    unsigned long flag;
    unsigned long data[];
};

struct pty_ep_data {
	unsigned int ep_id; /* endpoint id */

    int fd_master;  /* pty master fd */

    pthread_t pty_thread; /* thread id */
};

void ping(int loop);
void long_ping(unsigned long total_size,int loop);
void pty_ping(struct pty_ep_data *pty_shell);
int benchmark_service_init();
struct pty_ep_data * pty_ping_create(const char* ep_name);
static unsigned long get_system_time_microsecond()
{
    struct timespec timestamp = {};
    if (0 == clock_gettime(CLOCK_MONOTONIC, &timestamp))
        return timestamp.tv_sec * 1000000 + timestamp.tv_nsec / 1000;
    else
        return 0;
}

static unsigned long get_system_time_milliseconds()
{
    struct timespec timestamp = {};
    if (0 == clock_gettime(CLOCK_MONOTONIC, &timestamp))
        return timestamp.tv_sec * 1000 + timestamp.tv_nsec / 1000000;
    else
        return 0;
}

static void get_current_time(struct timespec *ts) {
    clock_gettime(CLOCK_MONOTONIC, ts);
}

static inline int64_t calcdiff_us(struct timespec t1, struct timespec t2) {
    int64_t diff;
    diff = (int64_t)((int)t1.tv_sec - (int)t2.tv_sec) * 1000000LL; // 秒级别的差值转换为微秒
    diff += ((int)t1.tv_nsec - (int)t2.tv_nsec) / 1000LL;          // 纳秒级别的差值转换为微秒
    return diff;
}

#define PAYLOAD_MIN_SIZE 1
#define BENCHMARK_RPC_PING "rpc-ping"
#define BENCHMARK_RPC_LONG_PING "rpc-long-ping"
#endif // BENCHMARK_H