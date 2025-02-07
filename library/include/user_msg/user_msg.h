#ifndef USER_MSG_H
#define USER_MSG_H
#include <semaphore.h>

#define OPENAMP_SHM_SIZE  0x1000000    /* 16M */
#define OPENAMP_SHM_COPY_SIZE 0x100000 /* 1M */

#define SHM_NAME "/my_shared_memory_%d"
#define SEM_USER_TO_MICAD "/sem_user_to_mciad_%d"
#define SEM_MICAD_TO_USER "/sem_mciad_to_user_%d"
#define BUFFER_SIZE 256

typedef struct {
    unsigned long phy_addr;
    int data_len;
    int instance_id; /* 当前不支持多实例，使用时赋值为0；支持多实例以后修改成具体实例号 */
    int rcv_data_len;
    int lock;
    char rcv_buffer[BUFFER_SIZE];
} process_shared_data_t;

struct core_msg_mem_info {
    unsigned int instance_id; /* 当前不支持多实例，使用时赋值为0；支持多实例以后修改成具体实例号 */
    unsigned long phy_addr;
    void *vir_addr;
    size_t size;
    size_t align_size;
};

extern int init_core_shared_memory(struct core_msg_mem_info *info);
extern process_shared_data_t *init_process_shared_memory(int instance_id);
extern int create_sem(int instance_id, sem_t **sem_user_to_micad, sem_t **sem_micad_to_user);
extern int send_data_to_rtos_and_wait_rcv(void *data, int data_len, int target_instance, void *rcv_data, int *rcv_data_len);

#endif	/* USER_MSG_H */