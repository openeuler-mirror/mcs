/*
 * RISC-V: send-data, umt_context_create + send_data_with_umt_context
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <limits.h>
#include <errno.h>
#include <user_msg/user_msg.h>

#define STR_SIZE 1024
#define LOOP_COUNT 8

int main()
{
    struct timespec start, end, total_start, total_end;
    char *sendbuf = NULL;
    int ret;
    long total_time = 0;
    long min_time = LONG_MAX;
    long max_time = 0;

    sendbuf = (char *)malloc(STR_SIZE + 1);
    if (sendbuf == NULL) {
        printf("malloc failed\n");
        return -ENOMEM;
    }

    printf("RISC-V send-data (umt_context_create + send_data_with_umt_context)...\n");
    clock_gettime(CLOCK_MONOTONIC, &total_start);

    umt_context_t *ctx = umt_context_create(0, MCS_KM_PED_RISCV);
    if (ctx == NULL) {
        printf("umt_context_create failed\n");
        free(sendbuf);
        return -ENOMEM;
    }

    srand((unsigned int)time(NULL));
    for (int i = 0; i < STR_SIZE; i++) {
        sendbuf[i] = 'A' + rand() % 26;
    }
    sendbuf[STR_SIZE] = '\0';

    printf("Starting %d sends...\n", LOOP_COUNT);
    for (int i = 0; i < LOOP_COUNT; i++) {
        clock_gettime(CLOCK_MONOTONIC, &start);
        ret = send_data_with_umt_context(ctx, i * STR_SIZE, sendbuf, STR_SIZE);
        clock_gettime(CLOCK_MONOTONIC, &end);

        if (ret != 0) {
            printf("send_data_with_umt_context failed at iteration %d\n", i);
            break;
        }

        long exec_time = (end.tv_sec - start.tv_sec) * 1000000000L + (end.tv_nsec - start.tv_nsec);
        total_time += exec_time;
        if (exec_time < min_time) min_time = exec_time;
        if (exec_time > max_time) max_time = exec_time;
        printf("Data sent (last 10 char) %s\n", sendbuf + STR_SIZE - 10);
    }

    clock_gettime(CLOCK_MONOTONIC, &total_end);
    long wall_time = (total_end.tv_sec - total_start.tv_sec) * 1000000000L + (total_end.tv_nsec - total_start.tv_nsec);

    umt_context_destroy(ctx);

    printf("\n========== Statistics (RISC-V send-data) ==========\n");
    printf("Total sends:     %d\n", LOOP_COUNT);
    printf("Total time:      %ld ns (%.2f ms)\n", wall_time, wall_time / 1000000.0);
    printf("Average time:    %ld ns\n", total_time / LOOP_COUNT);
    printf("Min time:        %ld ns\n", min_time);
    printf("Max time:        %ld ns\n", max_time);
    printf("Throughput:      %.2f sends/sec\n", LOOP_COUNT / (wall_time / 1000000000.0));

    free(sendbuf);
    return 0;
}
