#include <sys/types.h>
#include <sys/stat.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <fcntl.h>
#include <stdarg.h>
#include <unistd.h>
#include <string.h>
#include <errno.h>
#include <netdb.h>
#include <poll.h>
#include <sys/select.h>

#include <openamp/rpmsg_rpc_client_server.h>
#include <openamp/rpmsg.h>

#include "rpc_internal_model.h"
#include "rpc_server_internal.h"
#include "rpmsg_rpc_service.h"
#include "rpmsg_endpoint.h"
#include "rpc_err.h"

#define DEFINE_VARS(name)                        \
    void *req_ptr = data;                        \
    rpc_##name##_req_t *req = req_ptr;           \
    rpc_##name##_resp_t resp;                    \
    int payload_size = sizeof(resp);             \
    int ret;

#ifdef MULTI_WORKERS
#define CLEANUP(data) free(data)
#else
#define CLEANUP(data) 
#endif

#define MIN(x,y) (((x) < (y)) ? (x) : (y))

#define ADDR        0xFF
#define DEST_ADDR   0xFE

static struct rpmsg_endpoint g_ept;

static int rpc_server_send(struct rpc_instance *inst, uint32_t rpc_id,
                int status, void *request_param, size_t param_size)
{
    struct rpmsg_endpoint *ept = inst->ept;
    struct rpmsg_rpc_answer msg;

    if (param_size > (MAX_BUF_LEN - sizeof(msg.status)))
        return -EINVAL;

    msg.id = rpc_id;
    msg.status = status;
    memcpy(msg.params, request_param, param_size);
    return rpmsg_send(ept, &msg, MAX_FUNC_ID_LEN + param_size);
}

static int rpmsg_endpoint_server_cb(struct rpmsg_endpoint *, void *,
                    size_t, uint32_t, void *);
static int rpmsg_handle_open(void *data, struct rpc_instance *inst);
static int rpmsg_handle_read(void *data, struct rpc_instance *inst);
static int rpmsg_handle_write(void *data, struct rpc_instance *inst);
static int rpmsg_handle_close(void *data, struct rpc_instance *inst);
static int rpmsg_handle_lseek(void *data, struct rpc_instance *inst);
static int rpmsg_handle_fcntl(void *data, struct rpc_instance *inst);
static int rpmsg_handle_ioctl(void *data, struct rpc_instance *inst);
static int rpmsg_handle_unlink(void *data, struct rpc_instance *inst);

static int rpmsg_handle_freeaddrinfo(void *data, struct rpc_instance *inst);
static int rpmsg_handle_getaddrinfo(void *data, struct rpc_instance *inst);
static int rpmsg_handle_gethostbyaddr(void *data, struct rpc_instance *inst);
static int rpmsg_handle_gethostbyname(void *data, struct rpc_instance *inst);
static int rpmsg_handle_poll(void *data, struct rpc_instance *inst);
static int rpmsg_handle_getpeername(void *data, struct rpc_instance *inst);
static int rpmsg_handle_gethostname(void *data, struct rpc_instance *inst);
static int rpmsg_handle_getsockname(void *data, struct rpc_instance *inst);
static int rpmsg_handle_getsockopt(void *data, struct rpc_instance *inst);
static int rpmsg_handle_select(void *data, struct rpc_instance *inst);
static int rpmsg_handle_accept(void *data, struct rpc_instance *inst);
static int rpmsg_handle_bind(void *data, struct rpc_instance *inst);
static int rpmsg_handle_connect(void *data, struct rpc_instance *inst);
static int rpmsg_handle_listen(void *data, struct rpc_instance *inst);
static int rpmsg_handle_recv(void *data, struct rpc_instance *inst);
static int rpmsg_handle_recvfrom(void *data, struct rpc_instance *inst);
static int rpmsg_handle_send(void *data, struct rpc_instance *inst);
static int rpmsg_handle_sendto(void *data, struct rpc_instance *inst);
static int rpmsg_handle_setsockopt(void *data, struct rpc_instance *inst);
static int rpmsg_handle_shutdown(void *data, struct rpc_instance *inst);
static int rpmsg_handle_socket(void *data, struct rpc_instance *inst);
static int rpmsg_handle_printf(void *data, struct rpc_instance *inst);

/* Service table */
static struct rpc_instance service_inst;
static struct rpc_service service_table[] = {
    {OPEN_ID, &rpmsg_handle_open},
    {READ_ID, &rpmsg_handle_read},
    {WRITE_ID, &rpmsg_handle_write},
    {CLOSE_ID, &rpmsg_handle_close},
    {LSEEK_ID, &rpmsg_handle_lseek},
    {FCNTL_ID, &rpmsg_handle_fcntl},
    {IOCTL_ID, &rpmsg_handle_ioctl},
    {UNLINK_ID, &rpmsg_handle_unlink},
    {FREEADDRINFO_ID, &rpmsg_handle_freeaddrinfo},
    {GETADDRINFO_ID, &rpmsg_handle_getaddrinfo},
    {GETHOSTBYADDR_ID, &rpmsg_handle_gethostbyaddr},
    {GETHOSTBYNAME_ID, &rpmsg_handle_gethostbyname},
    {POLL_ID, &rpmsg_handle_poll},
    {GETPEERNAME_ID, &rpmsg_handle_getpeername},
    {GETHOSTNAME_ID, &rpmsg_handle_gethostname},
    {GETSOCKNAME_ID, &rpmsg_handle_getsockname},
    {GETSOCKOPT_ID, &rpmsg_handle_getsockopt},
    {SELECT_ID, &rpmsg_handle_select},
    {ACCEPT_ID, &rpmsg_handle_accept},
    {BIND_ID, &rpmsg_handle_bind},
    {CONNECT_ID, &rpmsg_handle_connect},
    {LISTEN_ID, &rpmsg_handle_listen},
    {RECV_ID, &rpmsg_handle_recv},
    {RECVFROM_ID, &rpmsg_handle_recvfrom},
    {SEND_ID, &rpmsg_handle_send},
    {SENDTO_ID, &rpmsg_handle_sendto},
    {SETSOCKOPT_ID, &rpmsg_handle_setsockopt},
    {SHUTDOWN_ID, &rpmsg_handle_shutdown},
    {SOCKET_ID, &rpmsg_handle_socket},
    {PRINTF_ID, &rpmsg_handle_printf},
};

#define LOG_PATH "/tmp/accesslog"
#define WBUF_LEN         0x200

static int lfd;

static int __lprintf(const char *fmt, va_list list)
{
    int len;
    char buf[WBUF_LEN] = {0};

    len = vsnprintf(buf, WBUF_LEN, fmt, list);
    if (len < 0) {
        return len;
    }
    return write(lfd, buf, len);
}

static int lprintf(const char *fmt, ...)
{
    va_list list;
    int count;

    va_start(list, fmt);
    count = __lprintf(fmt, list);
    va_end(list);
    return count;
}

static void dump(char *buf, int len)
{
    for(int i = 0; i < len; i++) {
        lprintf("%x ", buf[i]);
    }
    lprintf("\n");
}

static void lerror(int ret, int errnum)
{
    if (ret < 0) {
        lprintf("errstr:%s\n", strerror(errnum));
    }
}

static inline void set_rsp_base(rpc_resp_base_t *base, uint32_t trace_id)
{
    base->trace_id = trace_id;
    base->errnum = errno;
    errno = 0;
}

static int rpmsg_init_rpc_server(struct rpmsg_device *rdev, struct rpc_instance *inst,
              const struct rpc_service *services, int len)
{
    int ret;

    /* parameter check */
    if (inst == NULL || services == NULL || len == 0) {
        return -1;
    }

    inst->services = services;
    inst->n_services = len;

    ret = rpmsg_create_ept(&g_ept, rdev, RPMSG_RPC_SERVICE_NAME,
                           ADDR,
                           DEST_ADDR,
                           rpmsg_endpoint_server_cb,
                           NULL);

    if (ret < 0) {
        lprintf("Creating endpoint %s failed with error %d", RPMSG_RPC_SERVICE_NAME, ret);
        return ret;
    }
    inst->ept = &g_ept;

    return ret;
}

static const struct rpc_service *find_service(struct rpc_instance *inst,
                             unsigned int id)
{
    const struct rpc_service *service;

    for (unsigned int i = 0; i < inst->n_services; i++) {
        service = &inst->services[i];

        if (service->id == id) {
            return service;
        }
    }
    return NULL;
}

static int rpmsg_endpoint_server_cb(struct rpmsg_endpoint *ept, void *data,
                    size_t len,
                    uint32_t src, void *priv)
{
#ifdef MULTI_WORKERS
    unsigned char *buf;
#else
    unsigned char buf[MAX_BUF_LEN];
#endif
    unsigned int id;
    struct rpc_instance *inst;
    (void)priv;
    (void)src;

    lprintf("ccb: src %x, len %lu\n", src, len);

    inst = &service_inst;
    id = *(int *)data;
    lprintf("fun_id:%d\n", id);
    if (len > MAX_BUF_LEN) {
        lprintf("overlong data\n");
        rpc_server_send(inst, id, RPC_EOVERLONG, NULL, 0);
        return -EINVAL;
    }
#ifdef MULTI_WORKERS
    buf = malloc(len * sizeof(unsigned char));
    if (buf == NULL) {
        rpc_server_send(inst, id, RPC_ENOMEM, NULL, 0);
        return RPMSG_ERR_NO_MEM;
    }
#endif
    memcpy(buf, data, len);

    const struct rpc_service *service = find_service(inst, id);
    if (service) {
#ifdef MULTI_WORKERS
        enqueue_req(build_req(buf, service, inst));
#else
        if (service->cb_function(buf, inst)) {
            /* In this case, the client proactively detects a timeout
               failure and we do not send a response for the failure.
            */
            lprintf("Service failed at rpc id: %u\r\n", id);
        }
#endif
    } else {
        lprintf("Handling remote procedure call errors: rpc id %u\r\n", id);
        rpc_server_send(inst, id, RPMSG_RPC_INVALID_ID, NULL, 0);
    }
    return RPMSG_SUCCESS;
}

int rpmsg_service_init(struct rpmsg_device *rdev)
{
    int ret;
    unsigned int n_services = sizeof(service_table)/ sizeof(struct rpmsg_rpc_service);

    lfd = open(LOG_PATH, O_CREAT | O_RDWR | O_APPEND, S_IRUSR | S_IWUSR);
    if (lfd < 0) {
        lfd = STDOUT_FILENO;
    }

    lprintf("number of services: %d, %p\n", n_services, service_table);
    ret = rpmsg_init_rpc_server(rdev, &service_inst, service_table, n_services);
#ifdef MULTI_WORKERS
    workers_init();
#endif
    return ret;
}

void terminate_rpc_app(void)
{
    lprintf("Destroying endpoint.\r\n");
}

static int rpmsg_handle_open(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(open)
    char *buf;
    int fd;

    if (!req || !inst)
        return -EINVAL;
    buf = req->buf;
    lprintf("==open(%s)\n", buf);
    /* Open remote fd */
    fd = open(buf, req->flags, req->mode);
    lprintf("==open ret:%d\n", fd);
    lerror(ret, errno);
    /* Construct rpc response */
    resp.ret = fd;
    set_rsp_base(&resp.super, req->trace_id);
    /* Transmit rpc response */
    ret = rpc_server_send(inst, OPEN_ID, RPMSG_RPC_OK, &resp,
                    payload_size);
    lprintf("==open send rsp:%d, %d\n", resp.ret, ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_close(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(close)

    if (!req || !inst)
        return -EINVAL;
    lprintf("==close(%d)\n", req->fd);
    /* Close remote fd */
    ret = close(req->fd);
    lprintf("==close ret(%d)\n", ret);
    lerror(ret, errno);
    /* Construct rpc response */
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    /* Transmit rpc response */
    ret = rpc_server_send(inst, CLOSE_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==close send rsp:%d, %d\n", req->fd, ret);    
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_read(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(read)
    ssize_t sret;

    if (!req || !inst)
        return -EINVAL;
    lprintf("==read(%d, %d)\n", req->fd, req->count);
    if (req->fd == 0) {
        sret = MAX_STRING_LEN;
        /* Perform read from fd for large size since this is a
         * STD/I request
         */
        sret = read(req->fd, resp.buf, sret);
    } else {
        /* Perform read from fd */
        sret = read(req->fd, resp.buf, req->count);
    }
    lprintf("==read ret %ld\n", sret);
    lerror(ret, errno);
    /* Construct rpc response */
    resp.ret = sret;
    set_rsp_base(&resp.super, req->trace_id);

    payload_size -= sizeof(resp.buf);
    if (sret > 0) {
        payload_size += sret; 
    } 

    /* Transmit rpc response */
    ret = rpc_server_send(inst, READ_ID, RPMSG_RPC_OK, &resp,
                    payload_size);
    lprintf("==read send rsp:%d, %d\n", req->fd, ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_write(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(write)
    ssize_t sret;

    if (!req || !inst)
        return -EINVAL;
    lprintf("==write(%d, %d)\n", req->fd, req->count);
    /* Write to remote fd */
    sret = write(req->fd, req->buf, req->count);
    lprintf("==write ret:%ld\n", sret);
    lerror(ret, errno);
    /* Construct rpc response */
    resp.ret = sret;
    set_rsp_base(&resp.super, req->trace_id);

    /* Transmit rpc response */
    ret = rpc_server_send(inst, WRITE_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==write send rsp:%d, %d\n", req->fd, ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_lseek(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(lseek)
    off_t off;

    if (!req || !inst)
        return -EINVAL;
    lprintf("==lseek(%d, %ld, %d)\n", req->fd, req->offset, req->whence);
    off = lseek(req->fd, req->offset, req->whence);
    lprintf("==lseek ret:%ld\n", off);
    lerror(ret, errno);
    resp.ret = off;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, LSEEK_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==lseek send rsp:%d,%d\n", req->fd, ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_fcntl(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(fcntl)

    if (!req || !inst)
        return -EINVAL;
    lprintf("==fcntl(%d, %d, %lu)\n", req->fd, req->cmd, req->arg);
    ret = fcntl(req->fd, req->cmd, req->arg);
    lprintf("==fcntl ret:%d\n", ret);
    lerror(ret, errno);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, FCNTL_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==fcntl send rsp:%d, %d\n", req->fd, ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_ioctl(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(ioctl)

    if (!req || !inst)
        return -EINVAL;
    lprintf("==ioctl(%d, %ld)\n", req->fd, req->request);
    ret = ioctl(req->fd, req->request, req->buf);
    lprintf("==ioctl ret:%d\n", ret);
    lerror(ret, errno);
    resp.ret = ret;
    resp.len = req->len;
    set_rsp_base(&resp.super, req->trace_id);
    payload_size -= sizeof(req->buf);
    if (req->len > 0) {
        memcpy(resp.buf, req->buf, req->len);
        payload_size += req->len;
    }
    ret = rpc_server_send(inst, IOCTL_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==ioctl send rsp:%d,%d\n", req->fd, ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_unlink(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(unlink)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==unlink(%s)\n", req->buf);
    ret = unlink(req->buf);
    lprintf("==unlink ret:%d\n", ret);
    lerror(ret, errno);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, IOCTL_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==unlink send rsp:%s,%d\n", req->buf,ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_freeaddrinfo(void *data, struct rpc_instance *inst)
{
    (void)data;
    (void)inst;
    lprintf("UNUSED\n");
    return 0;
}

static int rpmsg_handle_getaddrinfo(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(getaddrinfo)
    char *node = NULL, *service = NULL;
    struct addrinfo *hints = NULL, *res;

    if (!req || !inst || req->node >= req->buflen || req->service >= req->buflen)
        return -EINVAL;

    if (req->hints_cnt > 0) {
        ret = decode_addrlist(req->buf, req->hints_cnt, sizeof(req->buf), &hints);
        if (ret < 0) {
            lprintf("==getaddrinfo decode failed(%d)\n", ret);
            goto response;
        }
    }
    if (req->service > req->node) {
        node = &req->buf[req->node];
    }
    if (req->buflen > req->service) {
        service = &req->buf[req->service];
    }
    lprintf("==getaddrinfo(%s, %s)\n", node, service);
    ret = getaddrinfo(node, service, hints, &res);
    lprintf("==getaddrinfo ret:%d\n", ret);
    lerror(ret, errno);
    resp.cnt = 0;
    resp.buflen = sizeof(resp.buf);
    payload_size -= sizeof(resp.buf);
    if (res != NULL) {
        resp.cnt = encode_addrlist(res, resp.buf, &resp.buflen);
        payload_size += resp.buflen;
    } else {
        resp.cnt = 0;
        resp.buflen = 0;
    }
    if (!ret) {
        freeaddrinfo(res);
    }
response:
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, GETADDRINFO_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==getaddrinfo send rsp:%d,%d\n", payload_size, ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

/* paddr: print the IP address in a standard decimal dotted format */
static void paddr(unsigned char *a)
{
    lprintf("%d.%d.%d.%d\n", a[0], a[1], a[2], a[3]);
}

static void print_host(struct hostent *hp)
{
    int i;

    if (hp == NULL) {
        return;
    }
    lprintf("name:%s, %d\n", hp->h_name, hp->h_length);
    for (i = 0; hp->h_addr_list[i] != 0; i++) {
        paddr((unsigned char*) hp->h_addr_list[i]);
    }
    for (i = 0; hp->h_aliases[i] != 0; i++) {
        lprintf("alias:%s\n", hp->h_aliases[i]);
    }
}

static int rpmsg_handle_gethostbyaddr(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(gethostbyaddr)
    struct hostent *ht;
    if (!req || !inst)
        return -EINVAL;

    lprintf("==gethostbyaddr(%d, %d)\n", req->len, req->type);
    ht = gethostbyaddr(req->buf, req->len, req->type);
    lprintf("==gethostbyaddr ret:%p\n", ht);
    if (ht == NULL) {
        lprintf("errstr:%s\n", strerror(errno));
    }
    payload_size -= sizeof(resp.buf);
    set_rsp_base(&resp.super, req->trace_id);
    print_host(ht);
    if (ht == NULL) {
        resp.len = 0;
        goto response;
    }
    resp.len = encode_hostent(ht, resp.buf, sizeof(resp.buf));
    if (resp.len >= 0) {
        payload_size += resp.len;
    }
response:
    ret = rpc_server_send(inst, GETHOSTBYADDR_ID, RPMSG_RPC_OK,
                    &resp, payload_size);

    lprintf("==gethostbyaddr send rsp, %d, %d\n", ret, resp.len);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_gethostbyname(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(gethostbyname)
    struct hostent *ht;
    if (!req || !inst)
        return -EINVAL;

    lprintf("==gethostbyname(%s)\n", req->buf);
    ht = gethostbyname(req->buf);
    lprintf("==gethostbyname ret:%p\n", ht);
    if (ht == NULL) {
        lprintf("errstr:%s\n", strerror(errno));
    }
    payload_size -= sizeof(resp.buf);
    set_rsp_base(&resp.super, req->trace_id);
    print_host(ht);

    if (ht == NULL) {
        resp.len = 0;
        goto response;
    }
    resp.len = encode_hostent(ht, resp.buf, sizeof(resp.buf));
    if (resp.len >= 0) {
        payload_size += resp.len;
    }
response:
    ret = rpc_server_send(inst, GETHOSTBYNAME_ID, RPMSG_RPC_OK,
                    &resp, payload_size);

    lprintf("==gethostbyname send rsp, %d, %d\n", ret, resp.len);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_getpeername(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(getpeername)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==getpeername(%d, %d)\n", req->sockfd, req->addrlen);
    ret = getpeername(req->sockfd, (struct sockaddr *)req->addr_buf, 
                      (socklen_t *)&req->addrlen);
    lprintf("==getpeername ret:%d\n", ret);
    lerror(ret, errno);

    payload_size -= sizeof(resp.addr_buf);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);
    if (req->addrlen > 0) {
        payload_size += req->addrlen;
        memcpy(resp.addr_buf, req->addr_buf, req->addrlen);
    }
    resp.addrlen = req->addrlen;

    ret = rpc_server_send(inst, GETPEERNAME_ID, RPMSG_RPC_OK,
                    &resp, payload_size);

    lprintf("==getpeername send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_getsockname(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(getsockname)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==getsockname(%d, %d)\n", req->sockfd, req->addrlen);
    ret = getsockname(req->sockfd, (struct sockaddr *)req->addr_buf, 
                      (socklen_t *)&req->addrlen);
    lprintf("==getsockname ret:%d\n", ret);
    lerror(ret, errno);
    payload_size -= sizeof(resp.addr_buf);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);
    if (req->addrlen > 0) {
        payload_size += req->addrlen;
        memcpy(resp.addr_buf, req->addr_buf, req->addrlen);
    }
    resp.addrlen = req->addrlen;

    ret = rpc_server_send(inst, GETSOCKNAME_ID, RPMSG_RPC_OK,
                    &resp, payload_size);

    lprintf("==getsockname send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_accept(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(accept)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==accept(%d, %d)\n", req->sockfd, req->addrlen);
    if (req->addrlen > 0) {
        ret = accept(req->sockfd, (struct sockaddr *)req->addr_buf, &req->addrlen);
    } else {
        ret = accept(req->sockfd, NULL, NULL);
    }
    lprintf("==accept ret:%d, addrlen:%d\n", ret, req->addrlen);
    lerror(ret, errno);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);
    resp.addrlen = req->addrlen;
    payload_size -= sizeof(resp.addrlen);

    if (req->addrlen > 0) {
        memcpy(resp.buf, req->addr_buf, req->addrlen);
        payload_size += req->addrlen;
    }

    ret = rpc_server_send(inst, ACCEPT_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==accept send rsp,%d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_bind(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(bind)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==bind(%d, %d)\n", req->sockfd, req->addrlen);
    ret = bind(req->sockfd, (struct sockaddr *)req->addr_buf, req->addrlen);
    lprintf("==bind ret:%d, addrlen:%d\n", ret, req->addrlen);
    lerror(ret, errno);

    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, BIND_ID, RPMSG_RPC_OK,
                    &resp, payload_size);

    lprintf("==bind send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_connect(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(connect)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==connect(%d, %d)\n", req->sockfd, req->addrlen);
    ret = connect(req->sockfd, (struct sockaddr *)req->addr_buf, req->addrlen);
    lprintf("==connect ret:%d, addrlen:%d\n", ret, req->addrlen);
    lerror(ret, errno);

    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, CONNECT_ID, RPMSG_RPC_OK,
                    &resp, payload_size);

    lprintf("==connect send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}
static int rpmsg_handle_listen(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(listen)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==listen(%d, %d)\n", req->sockfd, req->backlog);
    ret = listen(req->sockfd, req->backlog);
    lprintf("==listen ret:%d\n", ret);
    lerror(ret, errno);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, LISTEN_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==listen send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_recv(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(recv)
    ssize_t sret;

    if (!req || !inst)
        return -EINVAL;

    lprintf("==recv(%d, %lu, %d)\n", req->fd, req->len, req->flags);
    sret = recv(req->fd, resp.buf, req->len, req->flags);
    lprintf("==recv ret:%ld\n", sret);
    lerror(ret, errno);
    payload_size -= sizeof(resp.buf);
    resp.ret = sret;
    set_rsp_base(&resp.super, req->trace_id);
    if (sret > 0) {
        payload_size += sret;
    }

    ret = rpc_server_send(inst, RECV_ID, RPMSG_RPC_OK,
                    &resp, payload_size);

    lprintf("==recv send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_recvfrom(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(recvfrom)
    ssize_t sret;
    int len;

    if (!req || !inst)
        return -EINVAL;

    lprintf("==recvfrom(%d, %lu, %d)\n", req->fd, req->len, req->flags);
    len = MIN(sizeof(resp.buf), req->len);
    sret = recvfrom(req->fd, resp.buf, len, req->flags, (struct sockaddr *)req->buf, 
                    (socklen_t *)&req->addrlen);
    lprintf("==recvfrom ret:%ld\n", sret);
    lerror(ret, errno);
    resp.ret = sret;
    set_rsp_base(&resp.super, req->trace_id);
    resp.addrlen = req->addrlen;
    payload_size -= sizeof(resp.buf);
    if (req->addrlen > sizeof(resp.addr)) {
        resp.ret = -RPC_EOVERLONG;
        lprintf("==recvfrom addr overflow:%d, %d\n", req->addrlen, sizeof(resp.addr));
        goto response;
    }
    if (req->addrlen > 0) {
        memcpy(resp.addr, req->buf, req->addrlen);
    }

    if (sret > 0) {
        payload_size += sret;
    }
response:
    ret = rpc_server_send(inst, RECVFROM_ID, RPMSG_RPC_OK,
                    &resp, payload_size);

    lprintf("==recv send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_send(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(send)
    ssize_t sret;

    if (!req || !inst)
        return -EINVAL;

    lprintf("==send(%d, %lu, %d)\n", req->fd, req->len, req->flags);
    sret = send(req->fd, req->buf, req->len, req->flags);
    lprintf("==send ret:%ld\n", sret);
    lerror(ret, errno);
    resp.ret = sret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, SEND_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==send send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_sendto(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(sendto)
    ssize_t sret;

    if (!req || !inst)
        return -EINVAL;

    lprintf("==sendto(%d, %lu, %d, %d)\n", req->fd, req->len, req->flags, req->addrlen);
    sret = sendto(req->fd, &req->buf[req->addrlen], req->len, req->flags, 
                  (struct sockaddr *)req->buf, req->addrlen);
    lprintf("==sendto ret:%ld\n", sret);
    lerror(ret, errno);
    resp.ret = sret;
    set_rsp_base(&resp.super, req->trace_id);
    ret = rpc_server_send(inst, SENDTO_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==sendto send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_setsockopt(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(setsockopt)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==setsockopt(%d, %d, %d, %d)\n", req->fd, req->level, req->optname,
            req->optlen);
    ret = setsockopt(req->fd, req->level, req->optname, req->optval, req->optlen);
    lprintf("==setsockopt ret:%d\n", ret);
    lerror(ret, errno);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, SETSOCKOPT_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==setsockopt send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_shutdown(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(shutdown)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==shutdown(%d, %d)\n", req->fd, req->how);
    ret = shutdown(req->fd, req->how);
    lprintf("==shutdown ret:%d\n", ret);
    lerror(ret, errno);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, SHUTDOWN_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==shutdown send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_socket(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(socket)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==socket(%d, %d, %d)\n", req->domain, req->type, req->protocol);
    ret = socket(req->domain, req->type, req->protocol);
    lprintf("==socket ret:%d\n", ret);
    lerror(ret, errno);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);

    ret = rpc_server_send(inst, SOCKET_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==socket send rsp, %d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}


static int rpmsg_handle_poll(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(poll)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==poll(%d,%d,%d,%ld,%d)\n", req->fds[0].fd, req->fds[0].events,
             req->fds[0].revents, req->nfds, req->timeout);
    ret = poll(req->fds, req->nfds, req->timeout);
    lprintf("==poll ret:%d\n", ret);
    lerror(ret, errno);

    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);
    memcpy(resp.fds, req->fds, sizeof(struct pollfd) * req->nfds);

    ret = rpc_server_send(inst, POLL_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==poll send rsp:(%d,%d,%d)\n", resp.fds[0].fd, resp.fds[0].revents, ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;

}

static int rpmsg_handle_select(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(select)

    if (!req || !inst)
        return -EINVAL;

    lprintf("\n==select(%d,%d,%d,%d,%d,%ld,%ld)\n", req->nfds, (int)req->is_readfds_not_null,
        (int)req->is_writefds_not_null, (int)req->is_exceptfds_not_null, (int)req->is_timeout_not_null, 
        req->timeout.tv_sec, req->timeout.tv_usec);

    fd_set *readfds = NULL;
    fd_set *writefds = NULL;
    fd_set *exceptfds = NULL;
    struct timeval *timeout = NULL;
    if (req->is_readfds_not_null) {
        readfds = &(req->readfds);
    }
    if (req->is_writefds_not_null) {
        writefds = &(req->writefds);
    }
    if (req->is_exceptfds_not_null) {
        exceptfds = &(req->exceptfds);
    }
    if (req->is_timeout_not_null) {
        timeout = &(req->timeout);
    }
    ret = select(req->nfds, readfds, writefds, exceptfds, timeout);
    lprintf("==select ret:%d\n", ret);
    lerror(ret, errno);
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);
    memcpy(&(resp.readfds), &(req->readfds), sizeof(fd_set));
    memcpy(&(resp.writefds), &(req->writefds), sizeof(fd_set));
    memcpy(&(resp.exceptfds), &(req->exceptfds), sizeof(fd_set));
    memcpy(&(resp.timeout), &(req->timeout), sizeof(struct timeval));
    ret = rpc_server_send(inst, POLL_ID, RPMSG_RPC_OK,
                    &resp, payload_size);
    lprintf("==select send rsp:%d\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_gethostname(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(gethostname)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==gethostname(%lu)\n", (unsigned long)req->len);
    if (req->len > MAX_STRING_LEN) {
        req->len = MAX_STRING_LEN;
    }

    ret = gethostname(resp.name, req->len);
    lprintf("==gethostname ret:%d\n", ret);
    lerror(ret, errno);
    payload_size -= sizeof(resp.name);
    resp.len = 0;
    if (!ret) {
        resp.len = strlen(resp.name) + 1;
        payload_size += resp.len;
    }
    resp.ret = ret;
    set_rsp_base(&resp.super, req->trace_id);
    ret = rpc_server_send(inst, GETHOSTNAME_ID, RPMSG_RPC_OK,
        &resp, payload_size);

    lprintf("==gethostname send rsp(%d)\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_getsockopt(void *data, struct rpc_instance *inst)
{
    DEFINE_VARS(getsockopt)

    if (!req || !inst)
        return -EINVAL;

    lprintf("==getsockopt(%d, %d, %d)\n", req->sockfd, req->level, 
        req->optname);

    ret = getsockopt(req->sockfd, req->level, req->optname, &resp.optval, &req->optlen);
    payload_size -= sizeof(resp.optval);
    if (resp.optlen > sizeof(resp.optval)) {
        ret = -RPC_EOVERLONG;
    } else {
        payload_size += req->optlen;
    }
    lprintf("==getsockopt(%d)\n", ret);
    lerror(ret, errno);
    resp.ret = ret;
    resp.optlen = req->optlen;
    set_rsp_base(&resp.super, req->trace_id);
    ret = rpc_server_send(inst, GETSOCKOPT_ID, RPMSG_RPC_OK,
        &resp, payload_size);
    lprintf("==getsockopt send rsp(%d)\n", ret);
    CLEANUP(data);
    return ret > 0 ?  0 : ret;
 }

static int rpmsg_handle_printf(void *data, struct rpc_instance *inst)
{
    rpc_printf_req_t *req = (rpc_printf_req_t *)data;
    int ret = 0;

    if (!req || !inst)
        return -EINVAL;

    ret = write(1, req->buf, MIN(sizeof(req->buf), req->len));
    lprintf("==printf(%d), ret(%d)\n", req->len, ret);

    return ret > 0 ?  0 : ret;
 }