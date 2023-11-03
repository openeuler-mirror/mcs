#ifndef _RPC_SERVER_INTERNAL_H
#define _RPC_SERVER_INTERNAL_H

#include <netdb.h>
#include <openamp/rpmsg.h>
#include "../mica_demo/rpmsg_pty.h"

#define WORKERS 5
#define MAX_QUEUE_SIZE 256

struct rpc_instance;
typedef int (*rpc_cb_t)(void *params, struct rpc_instance *inst, void *priv);

struct rpc_service {
    uint32_t id;
    rpc_cb_t cb_function;
};

struct rpc_instance {
    const struct rpc_service *services; /* service table */
    unsigned int n_services; /* number of services */
};

typedef struct {
    unsigned char *data;
    const struct rpc_service *service;
    struct rpc_instance *inst;
    void *priv;
} req_t;

typedef struct {
    req_t *q[MAX_QUEUE_SIZE];
    int head;
    int tail;
    int size;
    pthread_mutex_t lock;
    pthread_cond_t cond;
} rpc_queue_t;

extern void enqueue_req(req_t *req);

extern req_t *build_req(unsigned char *data, const struct rpc_service *service,
                 struct rpc_instance *inst, void *priv);

extern int workers_init();
extern int cmd_workers_init(struct pty_ep_data *pty_ep);

extern int rpmsg_service_init();
extern int lprintf(const char *fmt, ...);

extern void freeaddrlist(struct addrinfo *ai);
extern int encode_addrlist(const struct addrinfo *ai, char *buf, int *buflen);
extern int decode_addrlist(const char *buf, int cnt, int buflen, struct addrinfo **out);
extern int decode_hostent(struct hostent **ppht, char *src_buf, int buflen);
extern int encode_hostent(struct hostent *ht, char *buf, int buflen);

int rpc_server_send(unsigned int ept_id, uint32_t rpc_id, int status, void *request_param, size_t param_size);
int rpmsg_endpoint_server_cb(struct rpmsg_endpoint *, void *, size_t, uint32_t, void *);

#endif /* _RPC_SERVER_INTERNAL_H */