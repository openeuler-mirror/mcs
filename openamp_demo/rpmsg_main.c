#include <stdio.h>
#include <stdarg.h>
#include <pthread.h>
#include "../rpmsg_pty_demo/rpmsg_pty.h"
#include "openamp_module.h"
#include "rpmsg_matrix_multiply.h"
#include "rpmsg_ping.h"
#define MSG_PTY_START 0x00000001
#define MSG_PTY_CLOSE 0x00000002
#define MSG_SHUTDOWN  0xFF000001
static int ept_echo, ept_onoff, ept_matrix;

enum{
    k_exit,
    k_test_echo,
    k_send_matrix,
    k_start_pty,
    k_close_pty,
    k_shutdown_clientOS,
    k_start_clientOS,
    k_test_ping,
    k_test_flood_ping
};
/*current select item*/
int nSelect;
static struct client_os_inst client_os = {
    /* physical address start of shared device mem */
    .phy_shared_mem = 0x70000000,
    /* size of shared device mem */
    .shared_mem_size = 0x30000,
    .vring_size = VRING_SIZE,
    .vdev_status_size = VDEV_STATUS_SIZE,
};

static void endpoint_exit(int ep_id)
{
    /* release the resources */
    rpmsg_service_unregister_endpoint(ep_id);
}

static void cleanup(int sig)
{
    shutdown(ept_onoff);
    exit(0);
}

static void endpoint_unbind_cb(struct rpmsg_endpoint *ept)
{
    printf("%s: get unbind request from client side\n", ept->name);
}

static int echo_endpoint_cb(struct rpmsg_endpoint *ept, void *data,
		size_t len, uint32_t src, void *priv){
    if(nSelect == k_test_ping){
        ping_cb(ept, data, len, src, priv);
    }else if(nSelect == k_test_flood_ping){
        flood_ping_cb(ept, data, len, src, priv);
    }
    return 0;
}

void register_endpoint()
{
    ept_echo = rpmsg_service_register_endpoint("echo", echo_endpoint_cb,
                                            endpoint_unbind_cb, NULL);
    ept_onoff = rpmsg_service_register_endpoint("onoff", echo_endpoint_cb,
                                            endpoint_unbind_cb, NULL);
    ept_matrix = rpmsg_service_register_endpoint("matrix", matrix_endpoint_cb,
                                            endpoint_unbind_cb, NULL);
}

void send_echo(int id)
{
    int i=0;
    if(!rpmsg_service_endpoint_is_bound(id)){
        printf("Channel is unestablished!");
    }
    while (i<5)
    {
        rpmsg_service_send(id,"hello!",strlen("hello!"));
        i++;
    }
}

void start_pty(int id){
    if(!rpmsg_service_endpoint_is_bound(id)){
        printf("Channel is unestablished!");
    }
    unsigned int msg = MSG_PTY_START;
    rpmsg_service_send(id,&msg,sizeof(msg));
}

void close_pty(int id)
{
    if(!rpmsg_service_endpoint_is_bound(id)){
        printf("Channel is unestablished!");
    }
    unsigned int msg = MSG_PTY_CLOSE;
    rpmsg_service_send(id,&msg,sizeof(msg));
}

void send_matrix(int id)
{
    if(!rpmsg_service_endpoint_is_bound(id)){
        printf("Channel is unestablished!");
    }
    cal_matrix(id);
}

void shutdown(int id)
{
    if(!rpmsg_service_endpoint_is_bound(id)){
        printf("Channel is unestablished!");
    }
    unsigned int msg = MSG_SHUTDOWN;
    rpmsg_service_send(id,&msg,sizeof(msg));
    endpoint_exit(ept_echo);
    endpoint_exit(ept_onoff);
    endpoint_exit(ept_matrix);
    openamp_deinit(&client_os);
}

void start_rtos(){
    int ret;
    register_endpoint();
    ret = openamp_init(&client_os);
    printf("ret = %d\n", ret);
    if (ret) {
        printf("start processor failed\n");
    }
}

void *cmd_thread(void *arg)
{
    int ret;
    while (true) {
        char cSelect=getchar();
        if(cSelect == 'h')
        {
            printf("please input number:<1-8>\n");
            printf("1. test echo\n");
            printf("2. send matrix\n");
            printf("3. start pty\n");
            printf("4. close pty\n");
            printf("5. shutdown clientOS\n");
            printf("6. start clientOS\n");
            printf("7. test ping\n");
            printf("8. test flood-ping\n");
            printf("0. exit\n");
        }
        nSelect = cSelect - '0';
        switch(nSelect){
            case k_exit:cleanup(SIGINT);break;
            case k_test_echo:send_echo(ept_echo);break;
            case k_send_matrix:send_matrix(ept_matrix);break;
            case k_start_pty:start_pty(ept_onoff);break;
            case k_close_pty:close_pty(ept_onoff);break;
            case k_shutdown_clientOS:shutdown(ept_onoff);break;
            case k_start_clientOS:start_rtos();break;
            case k_test_ping:ping(ept_echo);break;
            case k_test_flood_ping:flood_ping(ept_echo);break;
            default:break;
        }
        usleep(10000);
    }
}

int rpmsg_app_master(struct client_os_inst *client)
{
    register_endpoint();
    pthread_t pid;

    if (pthread_create(&pid, NULL, cmd_thread, NULL) < 0) {
        printf("cmd thread create failed\n");
        return -1;
    }
    pthread_detach(pid);

    struct pty_ep_data *pty_shell1;

    pty_shell1 = pty_service_create("pty");

    if (pty_shell1 == NULL) {
        return -1;
    }
    
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

    /* ctrl+c signal, do cleanup before program exit */
    signal(SIGINT, cleanup);

    /* \todo: parameter check */
    while ((opt = getopt(argc, argv, "c:t:a:e::")) != -1) {
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
        case '?':
            printf("Unknown option: %c ",(char)optopt);
        default:
            break;
        }
    }

    client_os.cpu_id = strtol(cpu_id, NULL, STR_TO_DEC);
    client_os.load_address = strtol(target_binaddr, NULL, STR_TO_HEX);
    client_os.entry = target_entry ? strtol(target_entry, NULL, STR_TO_HEX) :
                        client_os.load_address;
    client_os.path = target_binfile;

    printf("cpu:%d, ld:%lx, entry:%lx, path:%s\n",
        client_os.cpu_id,client_os.load_address, client_os.entry, client_os.path);

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

    openamp_deinit(&client_os);
    return 0;
}
