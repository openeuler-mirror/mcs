#ifndef RPMSG_UMT_H
#define RPMSG_UMT_H

#include <mica/mica.h>
#include <pthread.h>
#include <semaphore.h>
#include <user_msg/user_msg.h>

int create_rpmsg_umt_service(struct mica_client *client);

struct rpmsg_umt_service {
        int instance_id; /* 当前不支持多实例，这里实例号赋值为0，支持以后赋值实际的实例号 */
        struct rpmsg_endpoint ept;
        sem_t sem;
        pthread_mutex_t lock;
        int active;
        sem_t *sem_user_to_micad;
        sem_t *sem_micad_to_user;
        process_shared_data_t *process_shared_memory;
        struct metal_list node;
};


typedef struct umt_send_msg {
        unsigned long phy_addr;
        int data_len;
} umt_send_msg_t;

extern struct metal_list *g_umt_list_head;

#endif  /* RPMSG_UMT_H */