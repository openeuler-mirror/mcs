#include <stdio.h>
#include <stdarg.h>
#include <pthread.h>
#include <unistd.h>
#include <string.h>

#include "rpc_server_internal.h"
#include "rpmsg_rpc_service.h"

static pthread_t pids[WORKERS];

static rpc_queue_t rx_q;

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
    err = __enqueue(rq, req);
    if (!err) {
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
    enqueue(&rx_q, req);
}

req_t *build_req(unsigned char *data, const struct rpc_service *service, struct rpc_instance *inst, void *priv)
{
    req_t *req = (req_t *)malloc(sizeof(req_t));
    if (req == NULL) {
        return NULL;
    }
    req->data = data;
    req->inst = inst;
    req->service = service;
    req->priv = priv;
    return req;
}

static void *worker_thread(void *args) {
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
    }
}

int workers_init()
{
    rq_init(&rx_q, MAX_QUEUE_SIZE);
    for (int i = 0; i < WORKERS; i++) {
        if (pthread_create(&pids[i], NULL, worker_thread, NULL) < 0) {
            printf("worker thread create failed\n");
            return -1;
        }
        printf("worker %d created\n", i);
        pthread_detach(pids[i]);
    }
    return 0;
}