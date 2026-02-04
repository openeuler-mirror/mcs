#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <limits.h>
#include <errno.h>
#include <user_msg/user_msg.h>

#define STR_SIZE 1024 * 1024
#define TIMEOUT_SECONDS 30

int main()
{
    char *rcvbuf = NULL;
    int rcv_data_len = 0;
    int ret;
    int rcv_cnt = 0;

    rcvbuf = (char*)malloc(STR_SIZE + 1);
    if (rcvbuf == NULL) {
        printf("malloc failed\n");
        return -1;
    }

    // 创建上下文（只初始化一次）
    printf("Creating context...\n");
    umt_context_t *ctx = umt_context_create(0, MCS_KM_PED_RISCV);
    if (ctx == NULL) {
        printf("umt_context_create failed\n");
        free(rcvbuf);
        return -1;
    }

    printf("Waiting for MICA message from LiteOS (timeout: %d seconds)...\n", TIMEOUT_SECONDS);

    // 多次接收
    while (1) {
        memset(rcvbuf, 0, STR_SIZE + 1);

        ret = receive_data_with_umt_context(ctx, rcvbuf, &rcv_data_len, TIMEOUT_SECONDS * 1000);

        if (ret == -ETIMEDOUT) {
            printf("receive_data_with_umt_context timeout at iteration %d\n", rcv_cnt);
            break;
        } else if (ret != 0) {
            printf("receive_data_with_umt_context failed at iteration %d\n", rcv_cnt);
            break;
        }

        rcv_cnt++;

        printf("Received message %d  (last 10 chars): %s (len: %d)\n",
               rcv_cnt, rcvbuf + (rcv_data_len > 10 ? rcv_data_len - 10 : 0), rcv_data_len);
    }

    // 销毁上下文（只清理一次）
    umt_context_destroy(ctx);

    printf("\n========== Statistics ==========\n");
    printf("Total receives:  %d\n", rcv_cnt);

    free(rcvbuf);
    return 0;
}
