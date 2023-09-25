/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include "mica_debug.h"

/* create message queue */
mqd_t g_from_server, g_to_server;

/* resources of gdb proxy server */
struct proxy_server_resources *g_proxy_server_resources;
/* resources of ring buffer module */
struct debug_ring_buffer_module_data *g_ring_buffer_module_data;

int debug_start(struct client_os_inst *client_os, char *elf_name)
{
    int ret;
    ret = alloc_message_queue();
    if (ret < 0) {
        printf("alloc message queue failed\n");
        return ret;
    }
#ifdef MICA_DEBUG_LOG
    ret = open_log_file();
    if (ret < 0) {
        printf("open log file failed\n");
        goto err_free_message_queue;
    }
#endif
#ifdef CONFIG_RING_BUFFER
    ret = start_ring_buffer_module(client_os, g_from_server, g_to_server, &g_ring_buffer_module_data);
    if (ret < 0) {
        printf("start ring buffer module failed\n");
        goto err_close_log_file;
    }
#endif

    pthread_t server_loop;
    if ((ret = pthread_create(&server_loop, NULL, server_loop_thread, NULL)) != 0) {
        perror("create server loop thread failed\n");
        goto err_free_ring_buffer_module;
    }

    // create a process to execute gdb
    pid_t pid = fork();
    if (pid < 0) {
        perror("fork");
        goto err_create_process;
    } else if (pid == 0) {
        // child process
        // the gdb needs to be executed through a shell
        // otherwise it will quit automatically
        char exec_param[MAX_PARAM_LENGTH];
        memset(exec_param, 0, sizeof(exec_param));
        int port = GDB_PROXY_PORT;
        sprintf(exec_param, "target remote :%d", port);
        char *argv[] = {"gdb", elf_name, "-ex", exec_param, NULL};
        execvp(argv[0], argv);
        perror("execvp");
        goto err_create_process;
    } else {
        waitpid(pid, NULL, 0);
    }

    pthread_join(server_loop, NULL);

    goto normal_exit;

err_create_process:
    pthread_cancel(server_loop);

normal_exit:
    free_resources_for_proxy_server(g_proxy_server_resources);

err_free_ring_buffer_module:
#ifdef CONFIG_RING_BUFFER
    free_resources_for_ring_buffer_module(g_ring_buffer_module_data);
#endif

err_close_log_file:
#ifdef MICA_DEBUG_LOG
    close_log_file();
#endif

err_free_message_queue:
    free_message_queue();
    return ret;
}

static void *server_loop_thread(void *args)
{
    int ret = start_proxy_server(g_from_server, g_to_server, &g_proxy_server_resources);
    return INT_TO_PTR(ret);
}

static int alloc_message_queue()
{
    /* attributes of message queues */
    struct mq_attr attr;
    attr.mq_maxmsg = MAX_QUEUE_SIZE;
    attr.mq_msgsize = MAX_BUFF_LENGTH;

    if ((g_from_server = mq_open(TO_SHARED_MEM_QUEUE_NAME, O_RDWR | O_CREAT, 0600, &attr)) == -1) {
        perror("open to shared memory message queue failed\n");
        return -errno;
    }

    if ((g_to_server = mq_open(FROM_SHARED_MEM_QUEUE_NAME, O_RDWR | O_CREAT, 0600, &attr)) == -1) {
        perror("open from shared memory message queue failed\n");
        return -errno;
    }

    return 0;
}

static void free_message_queue()
{
    if (g_from_server != 0)
        mq_close(g_from_server);
    if (g_to_server != 0)
        mq_close(g_to_server);
    printf("closed message queue\n");
}