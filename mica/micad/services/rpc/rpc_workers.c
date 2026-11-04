/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <stdio.h>
#include <stdarg.h>
#include <stdbool.h>
#include <pthread.h>
#include <unistd.h>
#include <string.h>

#include "rpc_server_internal.h"
#include "rpmsg_rpc_service.h"

static pthread_t pids[WORKERS];

static rpc_queue_t rx_q;
static pthread_once_t workers_once = PTHREAD_ONCE_INIT;
static pthread_cond_t workers_drained = PTHREAD_COND_INITIALIZER;
static int workers_err;
static bool accepting;	/* guarded by rx_q.lock */
static int pending;	/* queued plus in-flight requests, guarded by rx_q.lock */

static void rq_init(rpc_queue_t *rq, int size)
{
	memset(rq->q, 0, size * sizeof(req_t *));
	pthread_mutex_init(&rq->lock, NULL);
	pthread_cond_init(&rq->cond, NULL);
	rq->head = rq->tail = 0;
	rq->size = size;
}

static int __dequeue(rpc_queue_t *rq, req_t **ppreq)
{
	if (rq->head == rq->tail) {
		*ppreq = NULL;
		return -1;
	}
	*ppreq = rq->q[rq->head++];
	if (rq->head == rq->size) {
		rq->head = 0;
	}
	return 0;
}

static int __enqueue(rpc_queue_t *rq, req_t *req)
{
	if ((rq->tail + 1) % rq->size == rq->head) {
		lprintf("ERROR: queue is full!\n");
		return -1;
	}
	rq->q[rq->tail++] = req;
	if (rq->tail == rq->size) {
		rq->tail = 0;
	}
	return 0;
}

static int dequeue(rpc_queue_t *rq, req_t **req)
{
	int err = 0;

	(void)pthread_mutex_lock(&rq->lock);
	err = __dequeue(rq, req);
	if (err == -1) {
		pthread_cond_wait(&rq->cond, &rq->lock);
	}
	(void)pthread_mutex_unlock(&rq->lock);
	return err;
}

static int enqueue(rpc_queue_t *rq, req_t *req)
{
	int err;

	(void)pthread_mutex_lock(&rq->lock);
	if (!accepting) {
		(void)pthread_mutex_unlock(&rq->lock);
		return -1;
	}
	err = __enqueue(rq, req);
	if (!err) {
		pending++;
		pthread_cond_signal(&rq->cond);
	}
	(void)pthread_mutex_unlock(&rq->lock);
	return err;
}

void enqueue_req(req_t *req)
{
	if (req == NULL) {
		return;
	}
	if (enqueue(&rx_q, req) != 0) {
		/* The service is quiescing or the queue is full. */
		free(req->data);
		free(req);
	}
}

req_t *build_req(unsigned char *data, const struct rpc_service *service, struct rpc_instance *inst, void *priv)
{
	req_t *req = NULL;

	req = (req_t *) malloc(sizeof(req_t));
	if (req == NULL) {
		return NULL;
	}
	req->data = data;
	req->inst = inst;
	req->service = service;
	req->priv = priv;
	return req;
}

static void req_complete(void)
{
	(void)pthread_mutex_lock(&rx_q.lock);
	if (pending > 0) {
		pending--;
	}
	if (pending == 0) {
		pthread_cond_signal(&workers_drained);
	}
	(void)pthread_mutex_unlock(&rx_q.lock);
}

static void *worker_thread(void *args)
{
	req_t *req = NULL;
	const struct rpc_service *service;

	(void)args;
	while (1) {
		dequeue(&rx_q, &req);
		if (req == NULL) {
			continue;
		}
		service = req->service;
		if (service != NULL && service->cb_function != NULL) {
			service->cb_function(req->data, req->inst, req->priv);
		}
		free(req);
		req_complete();
	}
	pthread_exit(NULL);
}

static void workers_start(void)
{
	rq_init(&rx_q, MAX_QUEUE_SIZE);
	for (int i = 0; i < WORKERS; i++) {
		if (pthread_create(&pids[i], NULL, worker_thread, NULL) < 0) {
			printf("worker thread create failed\n");
			workers_err = -1;
			return;
		}
		printf("worker %d created\n", i);
		pthread_detach(pids[i]);
	}
}

/**
 * RPC support multi workers for proxy messages
 *
 * The pool is process-wide and lives as long as micad: client start/stop
 * must only gate it through workers_flush()/accepting. Re-initializing the
 * live queue per client would reset its mutex and condition variable under
 * the still-waiting workers and leak one WORKERS-sized thread set per start.
 */
int workers_init(void)
{
	int ret;

	ret = pthread_once(&workers_once, workers_start);
	if (ret != 0) {
		return ret;
	}

	(void)pthread_mutex_lock(&rx_q.lock);
	accepting = (workers_err == 0);
	(void)pthread_mutex_unlock(&rx_q.lock);
	return workers_err;
}

/**
 * Stop accepting requests and wait until every queued or in-flight request
 * has completed. Must be called before the caller releases the rpmsg
 * endpoint and the shared log handles, which pending requests still use.
 */
void workers_flush(void)
{
	(void)pthread_mutex_lock(&rx_q.lock);
	accepting = false;
	while (pending > 0) {
		pthread_cond_wait(&workers_drained, &rx_q.lock);
	}
	(void)pthread_mutex_unlock(&rx_q.lock);
}
