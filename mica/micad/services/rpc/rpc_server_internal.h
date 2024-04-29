/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#ifndef RPC_SERVER_INTERNAL_H
#define RPC_SERVER_INTERNAL_H

#include <netdb.h>
#include <openamp/rpmsg.h>

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

void enqueue_req(req_t *req);

req_t *build_req(unsigned char *data, const struct rpc_service *service,
				 struct rpc_instance *inst, void *priv);

int workers_init(void);

int lprintf(const char *fmt, ...);

void freeaddrlist(struct addrinfo *ai);
int encode_addrlist(const struct addrinfo *ai, char *buf, int *buflen);
int decode_addrlist(const char *buf, int cnt, int buflen, struct addrinfo **out);
int decode_hostent(struct hostent **ppht, char *src_buf, int buflen);
int encode_hostent(struct hostent *ht, char *buf, int buflen);

int rpc_server_send(struct rpmsg_endpoint *ept, uint32_t rpc_id, int status, void *request_param, size_t param_size);

#endif /* _RPC_SERVER_INTERNAL_H */
