/*
 * RISC-V: rcv-callback – umt_register_rcv_cb; internal thread calls callback on receive
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <pthread.h>
#include <user_msg/user_msg.h>

#define WAIT_SECONDS 30

/* priv 示例：传给回调的“应用上下文”，回调里通过 (struct app_ctx *)priv 使用 */
struct app_ctx {
    const char *name;
    int rcv_count;
    pthread_mutex_t mutex;
};

static void on_umt_rcv(const void *data, int data_len, void *priv)
{
    struct app_ctx *app = (struct app_ctx *)priv;

    if (app == NULL) {
        printf("[callback] received %d bytes (priv is NULL)\n", data_len);
        return;
    }

    pthread_mutex_lock(&app->mutex);
    app->rcv_count++;
    pthread_mutex_unlock(&app->mutex);

    printf("[callback] %s: received %d bytes (total callbacks: %d)",
        app->name, data_len, app->rcv_count);
    if (data_len > 0 && data != NULL) {
        int n = data_len > 20 ? 20 : data_len;
        printf(" first %d bytes: ", n);
        for (int i = 0; i < n; i++)
            printf("%02x ", ((const unsigned char *)data)[i]);
    }
    printf("\n");
}

int main(void)
{
    umt_context_t *ctx;
    struct app_ctx app_ctx = {
        .name = "rcv-callback",
        .rcv_count = 0,
        .mutex = PTHREAD_MUTEX_INITIALIZER,
    };
    int ret;

    printf("RISC-V rcv-callback: register callback and wait %d s for messages from RTOS...\n", WAIT_SECONDS);

    ctx = umt_context_create(0, MCS_KM_PED_RISCV);
    if (ctx == NULL) {
        printf("umt_context_create failed\n");
        return -1;
    }

    ret = umt_register_rcv_cb(ctx, on_umt_rcv, &app_ctx);
    if (ret != 0) {
        printf("umt_register_rcv_cb failed\n");
        umt_context_destroy(ctx);
        return -1;
    }

    sleep(WAIT_SECONDS);

    ret = umt_unregister_rcv_cb(ctx);
    if (ret != 0)
        printf("umt_unregister_rcv_cb returned %d (may be ok)\n", ret);

    umt_context_destroy(ctx);

    printf("\n========== Statistics (from priv app_ctx) ==========\n");
    printf("Total callbacks: %d\n", app_ctx.rcv_count);

    return 0;
}
