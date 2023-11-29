/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#define _XOPEN_SOURCE 600
#include <stdlib.h>
#include <stdio.h>
#include <fcntl.h>
#include <errno.h>
#include <unistd.h>
#include <string.h>
#include <pthread.h>

#include <openamp/rpmsg_rpc_client_server.h>

#include "openamp_module.h"
#include "rpmsg_pty.h"
#include  "../rpmsg_rpc_demo/rpc_server_internal.h"

/* define the keys according to your terminfo */
#define KEY_CTRL_D      4
#define CMD_KEY_ESC_VALUE 0x1b
#define CMD_KEY_COMBINATION_VALUE 0x5b

enum {
    STAT_NOMAL_KEY,
    STAT_ESC_KEY,
    STAT_CSI_KEY
};

static int g_escape_state = 0;

struct rpmsg_app_resource g_rpmsg_app_resource;

static void pty_endpoint_exit(struct pty_ep_data *pty_ep)
{
    /* release the resources */
    close(pty_ep->fd_master);
    pthread_cancel(pty_ep->pty_thread);
    rpmsg_service_unregister_endpoint(pty_ep->ep_id);
    free(pty_ep);
}

static void pty_endpoint_unbind_cb(struct rpmsg_endpoint *ept)
{
    printf("%s: get unbind request from client side\n", ept->name);

    struct pty_ep_data *pty_ep = (struct pty_ep_data *)ept->priv;

    pty_endpoint_exit(pty_ep);
}

static int pty_endpoint_cb(struct rpmsg_endpoint *ept, void *data,
		size_t len, uint32_t src, void *priv)
{
    rpmsg_endpoint_server_cb(ept, data, len, src, priv);
}

int pty_write(void *data, size_t len, void *priv)
{
    int ret, i, j;
    struct pty_ep_data *pty_ep = (struct pty_ep_data *)priv;
    char msg[len * 2 + 1];
    char *msg_data = (char *)data;

    /* when using pty, translate '\n' to "\r\n" */
    for (i = 0, j = 0; i < len; ++i, ++msg_data) {
        if (*msg_data == '\n') {
            msg[i + j] = '\r';
            ++j;
        }
        msg[i + j] = *msg_data;
    }

    len = i + j;
    msg[len] = '\0';
    ret = write(pty_ep->fd_master, msg, len);
    if (ret < 0) {
        printf("write pty master error:%d\n", ret);
        return ret;
    }

    return 0;
}

int open_pty(int *pfdm)
{
    int ret = -1;
    int fdm;
    char pts_name[20] = {0};

    /* Open the master side of the PTY */
    fdm = posix_openpt(O_RDWR | O_NOCTTY);
    if (fdm < 0) {
        printf("Error %d on posix_openpt()\n", errno);
        return ret;
    }

    printf("pty master fd is :%d\n", fdm);

    ret = grantpt(fdm);
    if (ret != 0) {
        printf("Error %d on grantpt()\n", errno);
        goto err_close_pty;
    }

    ret = unlockpt(fdm);
    if (ret != 0) {
        printf("Error %d on unlockpt()\n", errno);
        goto err_close_pty;
    }

    /* Open the slave side of the PTY */
    ret = ptsname_r(fdm, pts_name, sizeof(pts_name));
    if (ret != 0) {
        printf("Error %d on ptsname_r()\n", errno);
        goto err_close_pty;
    }

    printf("pls open %s to talk with client OS\n", pts_name);

    *pfdm = fdm;

    return 0;

err_close_pty:
    close(fdm);
}

static int is_final_byte(char c) {
    // csi序列结束符
    return (c > 0x39 && c < 0x7F);
}

// 只能识别csi序列
static int in_escape_sequence(int fdm, char c)
{
    if (g_escape_state == STAT_NOMAL_KEY) {
        if (c == CMD_KEY_ESC_VALUE) {
            g_escape_state = STAT_ESC_KEY;
        }
    } else if (g_escape_state == STAT_ESC_KEY) {
        if (c == CMD_KEY_COMBINATION_VALUE) {
            g_escape_state = STAT_CSI_KEY;
        } else {
            printf("unrecognized escape\n");
            g_escape_state = STAT_NOMAL_KEY;
        }
    } else if (is_final_byte(c)) {
        g_escape_state = STAT_NOMAL_KEY;
        return 1;
    }
    return (g_escape_state != STAT_NOMAL_KEY);
}

static void *pty_thread(void *arg)
{
    int ret;
    int fdm;
    unsigned char cmd[1];
    struct pty_ep_data * pty_ep;

    pty_ep = (struct pty_ep_data *)arg;

    printf("pty_thread for %s is runnning\n", rpmsg_service_endpoint_name(pty_ep->ep_id));
    fdm = pty_ep->fd_master;

    /* wait endpoint bound */
    while(!rpmsg_service_endpoint_is_bound(pty_ep->ep_id));

    while (1) {
        ret = read(fdm, cmd, 1);   /* get command from ptmx */
        if (ret <= 0) {
            printf("shell_user: get from ptmx failed: %d\n", ret);
            ret = -1;
            break;
        }

        if (cmd[0] == KEY_CTRL_D) {  /* special key: ctrl+d */
            ret = 0;  /* exit this thread, the same as pthread_exit */
            break;
        }

        int in_escape = in_escape_sequence(fdm, cmd[0]);
        // echo input char
        if (!in_escape) {
            if (cmd[0] == '\n') {
                write(fdm, "\r\n[TEST]", 8);
            } else if (cmd[0] == 0x09) { /* 0x09: tab */
                // no echo for tab
            } else {
                write(fdm, cmd, ret);
            }
        }

        ret = rpc_server_send(pty_ep->ep_id, 0, RPMSG_RPC_OK, cmd, ret);
        if (ret < 0) {
            printf("rpmsg_service_send error %d\n", ret);
            ret = -1;
            break;
        }
    }

    pty_endpoint_exit(pty_ep);

    return INT_TO_PTR(ret);
}

static struct pty_ep_data *pty_service_create(const char * ep_name)
{
    if (ep_name == NULL) {
        return NULL;
    }

    int ret;
    struct pty_ep_data * pty_ep;

    pty_ep = (struct pty_ep_data * )malloc(sizeof(struct pty_ep_data));
    if (pty_ep == NULL) {
        return NULL;
    }

    ret = open_pty(&pty_ep->fd_master);
    if (ret != 0) {
        goto err_free_resource_struct;
    }

    pty_ep->f = fdopen(pty_ep->fd_master, "r+");
    if (pty_ep->f == NULL) {
        close(pty_ep->fd_master);
        goto err_free_resource_struct;
    }

    pty_ep->ep_id = rpmsg_service_register_endpoint(ep_name, pty_endpoint_cb,
                                            pty_endpoint_unbind_cb, pty_ep);
    if (pty_ep->ep_id < 0) {
        printf("register endpoint %s failed\n", ep_name);
        goto err_close_pty;
    }

    if (pthread_create(&pty_ep->pty_thread, NULL, pty_thread, pty_ep) != 0) {
        printf("pty thread create failed\n");
        goto err_unregister_endpoint;
    }
    if (pthread_detach(pty_ep->pty_thread) != 0) {
        printf("pty thread detach failed\n");
        goto err_cancel_thread;
    }

    return pty_ep;

err_cancel_thread:
    pthread_cancel(pty_ep->pty_thread);
err_unregister_endpoint:
    rpmsg_service_unregister_endpoint(pty_ep->ep_id);
err_close_pty:
    fclose(pty_ep->f);
err_free_resource_struct:
    free(pty_ep);
    return NULL;
}

int rpmsg_app_start(struct client_os_inst *client)
{
    int ret;
    g_rpmsg_app_resource.pty_ep_uart = pty_service_create("uart");
    if (g_rpmsg_app_resource.pty_ep_uart == NULL) {
        return -1;
    }

    g_rpmsg_app_resource.pty_ep_console = pty_service_create("console");
    if (g_rpmsg_app_resource.pty_ep_console == NULL) {
        ret = -1;
        goto err_free_uart;
    }

    ret = rpmsg_service_init();
    if (ret != 0) {
        ret = -errno;
        goto err_free_console;
    }
    cmd_workers_init(g_rpmsg_app_resource.pty_ep_console);

    if (pthread_create(&g_rpmsg_app_resource.rpmsg_loop_thread, NULL, rpmsg_loop_thread, client) != 0) {
        perror("create rpmsg loop thread failed\n");
        ret = -errno;
        goto err_free_console;
    }
    if (pthread_detach(g_rpmsg_app_resource.rpmsg_loop_thread) != 0) {
        perror("detach rpmsg loop thread failed\n");
        ret = -errno;
        goto err_cancel_thread;
    }

    return 0;

err_cancel_thread:
    pthread_cancel(g_rpmsg_app_resource.rpmsg_loop_thread);
err_free_console:
    pty_endpoint_exit(g_rpmsg_app_resource.pty_ep_console);
err_free_uart:
    pty_endpoint_exit(g_rpmsg_app_resource.pty_ep_uart);
    return ret;
}

static void *rpmsg_loop_thread(void *args)
{
    struct client_os_inst *client = (struct client_os_inst *)args;
    printf("start polling for remote interrupts\n");
    rpmsg_service_receive_loop(client);
    return NULL;
}

void rpmsg_app_stop(void)
{
    pthread_cancel(g_rpmsg_app_resource.rpmsg_loop_thread);
    pty_endpoint_exit(g_rpmsg_app_resource.pty_ep_uart);
    pty_endpoint_exit(g_rpmsg_app_resource.pty_ep_console);
    printf("rpmsg apps have stopped\n");
}