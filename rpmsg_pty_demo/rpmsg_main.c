#include <stdio.h>
#include <stdarg.h>
#include <pthread.h>
#include "rpmsg_pty.h"

#include "openamp_module.h"

char *cpu_id;
char *target_binfile;
char *target_binaddr;

static void cleanup(int sig)
{
    openamp_deinit();
    exit(0);
}

int rpmsg_app_master(void)
{
    struct pty_ep_data *pty_shell;

    pty_shell = pty_service_create("uart");

    if (pty_shell == NULL) {
        return -1;
    }

    rpmsg_service_receive_loop(NULL);

    return 0;
}

int main(int argc, char **argv)
{
    int ret;
    int opt;

    /* ctrl+c signal, do cleanup before program exit */
    signal(SIGINT, cleanup);

    while ((opt = getopt(argc, argv, "c:t:a:")) != -1) {
        switch (opt) {
        case 'c':
            cpu_id = optarg;
            break;
        case 't':
            target_binfile = optarg;
            break;
        case 'a':
            target_binaddr = optarg;
            break;
        default:
            break;
        }
    }

    ret = openamp_init();
    if (ret) {
        printf("openamp init failed: %d\n", ret);
        openamp_deinit();
        return ret;
    }

    ret = rpmsg_app_master();
    if (ret) {
        printf("rpmsg app master failed: %d\n", ret);
        openamp_deinit();
        return ret;
    }

    openamp_deinit();

    return 0;
}
