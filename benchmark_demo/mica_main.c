/* SPDX-License-Identifier: MulanPSL-2.0*/

#include <getopt.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdio.h>
#include <termios.h>
#include <time.h>
#include <unistd.h>

#include "benchmark.h"
#include "openamp_module.h"

/* Globals */
static pthread_t thread_id;
static int flag_pty_test = 0;  // Mark whether bench_mark is carried out.
static int flag_ping = 0;
static int flag_long_ping = 0;
static int loop = 0;
static struct client_os_inst client_os = {
    /* physical address start of shared device mem */
    .phy_shared_mem = 0x70000000,
    /* size of shared device mem */
    .shared_mem_size = 0x30000,
    .vring_size = VRING_SIZE,
    .vdev_status_size = VDEV_STATUS_SIZE,
};

static const struct rpmsg_virtio_config vdev_config = {
    .h2r_buf_size = 4096,  // 4096 Bytes
    .r2h_buf_size = 512,  // 512 Bytes
    .split_shpool = false,
};

#define MSG_SIZE 1024 * 4

static void setKernelPrintkValue(const char *value) {
    FILE *file;
    if ((file = fopen("/proc/sys/kernel/printk", "w")) == NULL) {
        perror("Error opening /proc/sys/kernel/printk");
        exit(1);
    }

    fprintf(file, "%s", value);

    fclose(file);
}

static void cleanup(int sig)
{
    int ret;

    /* Close the thread of rpc test */
    ret = pthread_cancel(thread_id);
    if (ret != 0) {
        perror("pthread_cancel");
        exit(EXIT_FAILURE);
    }

    ret = pthread_join(thread_id, NULL);
    if (ret != 0) {
        perror("pthread_join");
        exit(EXIT_FAILURE);
    }

    openamp_deinit(&client_os);

    setKernelPrintkValue("7 4 1 7");
    exit(0);
}

static int *do_benchamrk_test(void *arg)
{
    /* Waiting for endpoint binding */
    sleep(4);

    if (flag_ping)
        ping(loop);
    else if (flag_long_ping)
        long_ping(MSG_SIZE, loop);

    pthread_exit(NULL);
}

static void bencharmk()
{
    int ret;
    struct pty_ep_data *pty_shell;

    /* Initialize the endpoint related to benchamrk */
    benchmark_service_init();

    if (flag_pty_test) {
        pty_shell = pty_ping_create("pty-ping");
        if (pty_shell == NULL)
            printf("failed to init pty-ping shell\n");
    }

    /* Start testing based on rpc communication */
    ret = pthread_create(&thread_id, NULL, (void *)do_benchamrk_test, NULL);
    if (ret != 0)
        printf("Error: pthread_create failed\n");
}

static int rpmsg_app_master(struct client_os_inst *client)
{
    bencharmk();
    rpmsg_service_receive_loop(client);
    return 0;
}

int main(int argc, char **argv)
{
    int ret;
    int opt;
    char *cpu_id;
    char *target_binfile;
    char *target_binaddr;
    char *target_entry = NULL;

    struct option long_options[] = {{"pty", no_argument, NULL, 'P'},
                                    {"ping", no_argument, NULL, 'p'},
                                    {"long-ping", no_argument, NULL, 'l'},
                                    {"loop", required_argument, NULL, 'L'},
                                    {NULL, 0, NULL, 0}};
    /* ctrl+c signal, do cleanup before program exit */
    signal(SIGINT, cleanup);

    /* Set the kernel message level to "0 0 0 0" */
    setKernelPrintkValue("0 0 0 0");

    /* do: parameter check */
    while ((opt = getopt_long(argc, argv, "c:t:a:e::", long_options, NULL)) !=
           -1) {
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
        case 'e':
            target_entry = optarg;
            break;
        case 'P':
            flag_pty_test = 1;
            break;
        case 'p':
            flag_ping = 1;
            break;
        case 'l':
            flag_long_ping = 1;
            break;
        case 'L':
            loop = atoi(optarg);
            break;
        case '?':
            printf("Unknown option: %c ", (char)optopt);
        default:
            break;
        }
    }

    client_os.cpu_id = strtol(cpu_id, NULL, STR_TO_DEC);
    client_os.load_address = strtol(target_binaddr, NULL, STR_TO_HEX);
    client_os.entry = target_entry ? strtol(target_entry, NULL, STR_TO_HEX)
                                   : client_os.load_address;
    client_os.path = target_binfile;
    client_os.config = &vdev_config;

    printf("cpu:%d, ld:%lx, entry:%lx, path:%s, pty_test:%s\n",
           client_os.cpu_id, client_os.load_address, client_os.entry,
           client_os.path, flag_pty_test ? "True" : "False");

    ret = openamp_init(&client_os);
    if (ret) {
        printf("openamp init failed: %d\n", ret);
        return ret;
    }
    ret = rpmsg_app_master(&client_os);
    if (ret) {
        printf("rpmsg app master failed: %d\n", ret);
        openamp_deinit(&client_os);
        return ret;
    }
    if (ret == 1)
        openamp_deinit(&client_os);

    return 0;
}
