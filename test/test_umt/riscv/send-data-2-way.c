/*
 * RISC-V: send-data-2-way, send_data_to_rtos_and_wait_rcv_ped(..., MCS_KM_PED_RISCV)
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <user_msg/user_msg.h>

#define STR_SIZE 1024
#define INSTANCE_NUM 1

int main()
{
    struct timespec start, end;
    char *rcvbuf = NULL;
    int rcv_data_len = 0;
    int ret = 0, i = 0, j = 0;
    char *sendbuf = NULL;

    srand((unsigned int)time(NULL));
    sendbuf = (char *)malloc(STR_SIZE + 1);
    if (sendbuf == NULL) {
        printf("sendbuf malloc failed\n");
        return -1;
    }

    rcvbuf = (char *)malloc(STR_SIZE + 1);
    if (rcvbuf == NULL) {
        printf("rcvbuf malloc failed\n");
        free(sendbuf);
        return -1;
    }

    for (i = 0; i < INSTANCE_NUM; i++) {
        memset(sendbuf, 0, STR_SIZE + 1);
        memset(rcvbuf, 0, STR_SIZE + 1);
        for (j = 0; j < STR_SIZE; j++) {
            sendbuf[j] = 'A' + rand() % 26;
        }

        printf("sendbuf %s total_len %ld\n", sendbuf + STR_SIZE - 10, (long)strlen(sendbuf));

        if (clock_gettime(CLOCK_MONOTONIC, &start) == -1) {
            printf("clock_gettime failed\n");
            goto out;
        }
        ret = send_data_to_rtos_and_wait_rcv_ped(sendbuf, (int)strlen(sendbuf), i, rcvbuf, &rcv_data_len, MCS_KM_PED_RISCV);
        if (clock_gettime(CLOCK_MONOTONIC, &end) == -1) {
            printf("clock_gettime failed\n");
            goto out;
        }

        if (ret != 0) {
            printf("send_data_to_rtos_and_wait_rcv_ped failed\n");
            goto out;
        }
        printf("rcvbuf %s total_len %d\n", rcvbuf + (rcv_data_len > 10 ? rcv_data_len - 10 : 0), rcv_data_len);

        long seconds = end.tv_sec - start.tv_sec;
        long nanoseconds = end.tv_nsec - start.tv_nsec;
        long exec_time = seconds * 1000000000L + nanoseconds;
        printf("Function execution time: %ld nanoseconds\n", exec_time);
    }

out:
    free(sendbuf);
    free(rcvbuf);
    return ret != 0 ? -1 : 0;
}
