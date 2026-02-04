#ifndef USER_MSG_H
#define USER_MSG_H

#include <stdint.h>
#include <semaphore.h>
#include <pthread.h>
#include <mica/mica_client.h>

#define OPENAMP_SHM_SIZE  0x1000000    /* 16M */
#define OPENAMP_SHM_COPY_SIZE 0x100000 /* 1M */

#define SHM_NAME "/my_shared_memory_%d"
#define SEM_USER_TO_MICAD "/sem_user_to_mciad_%d"
#define SEM_MICAD_TO_USER "/sem_mciad_to_user_%d"

typedef struct {
    unsigned long phy_addr;
    int data_len;
    int instance_id; /* 当前不支持多实例，使用时赋值为0；支持多实例以后修改成具体实例号 */
    unsigned long rcv_phy_addr;
    int rcv_data_len;
    pthread_mutex_t lock;
} process_shared_data_t;

struct core_msg_mem_info {
    unsigned int instance_id; /* 当前不支持多实例，使用时赋值为0；支持多实例以后修改成具体实例号 */
    unsigned long phy_addr;
    void *vir_addr;
    size_t size;
    size_t align_size;
};

typedef struct umt_rcv_msg {
    unsigned long phy_addr;
    int data_len;
} umt_rcv_msg_t;

typedef struct umt_context umt_context_t;

extern int init_core_shared_memory(struct core_msg_mem_info *info, enum mcs_km_pedestal_type ped_type);
extern process_shared_data_t *init_process_shared_memory(int instance_id);
extern int create_sem(int instance_id, sem_t **sem_user_to_micad, sem_t **sem_micad_to_user);

/**
 * Callback type for UMT receive: invoked when data arrives (data valid only during call).
 * @param data  Received data (do not use after callback returns)
 * @param data_len  Received length
 * @param priv  Same as passed to umt_register_rcv_cb (opaque pointer, e.g. application context)
 */
 typedef void (*umt_rcv_cb_t)(const void *data, int data_len, void *priv);

/**
 * @brief Create UMT communication context
 *
 * Allocates and initializes resources.
 *
 * @param target_instance Target instance ID (only 0 supported)
 * @param ped_type Pedestal type: MCS_KM_PED_BAREMETAL or MCS_KM_PED_RISCV
 * @return Context handle on success, NULL on failure
 * @note Call umt_context_destroy when done
 */
extern umt_context_t* umt_context_create(int target_instance, enum mcs_km_pedestal_type ped_type);

/**
 * @brief Destroy UMT communication context
 *
 * @param ctx Context handle
 * @note Safe if ctx is NULL; releases any held lock before destroy
 */
extern void umt_context_destroy(umt_context_t *ctx);

/**
 * @brief Send data using context (no reply wait)
 *
 * @param ctx Context handle
 * @param offset Offset in umt shared memory (bytes), 0 ~ (OPENAMP_SHM_COPY_SIZE - data_len)
 * @param data Data to send
 * @param data_len Data length
 * @return 0 on success, -1 on failure
 * @note Lock is acquired/released internally
 */
extern int send_data_with_umt_context(umt_context_t *ctx, int offset, void *data, int data_len);

/**
 * @brief Receive data using context (block until data arrives or timeout)
 *
 * @param ctx Context handle
 * @param rcv_data Receive buffer
 * @param rcv_data_len Output: received length
 * @param timeout_ms Timeout in ms; 0 = wait forever
 * @return 0 on success, -1 on failure (including timeout)
 * @note Lock released during wait, re-acquired after
 * @note Do not use the same context for both blocking receive and callback; use one mode per context.
 */
extern int receive_data_with_umt_context(umt_context_t *ctx, void *rcv_data, int *rcv_data_len, int timeout_ms);

/**
 * @brief Register receive callback (library runs an internal thread that waits for data and calls callback)
 *
 * One callback per context. Callback and blocking receive_data_with_umt_context must not be used on the same context.
 *
 * @param ctx Context handle
 * @param callback  Called with (data, data_len, priv); data is valid only during the call
 * @param priv  Opaque pointer passed to callback (e.g. application context)
 * @return 0 on success, -1 on failure (e.g. already registered)
 */
extern int umt_register_rcv_cb(umt_context_t *ctx, umt_rcv_cb_t callback, void *priv);

/**
 * @brief Unregister receive callback and stop the internal receive thread
 *
 * @param ctx Context handle
 * @return 0 on success, -1 if no callback was registered
 */
extern int umt_unregister_rcv_cb(umt_context_t *ctx);

/**
 * @brief One-shot send to RTOS (creates/destroys context internally; offset 0)
 *
 * @param data Data to send
 * @param data_len Data length
 * @param target_instance Target instance ID (must be 0)
 * @param ped_type MCS_KM_PED_BAREMETAL or MCS_KM_PED_RISCV
 * @return 0 on success, -1 on failure
 */
extern int send_data_to_rtos(void *data, int data_len, int target_instance, enum mcs_km_pedestal_type ped_type);

/**
 * @brief One-shot receive from RTOS (creates/destroys context internally)
 *
 * @param rcv_data Receive buffer
 * @param rcv_data_len Output: received length
 * @param target_instance Target instance ID (must be 0)
 * @param timeout_ms Timeout in ms; 0 = wait forever
 * @param ped_type MCS_KM_PED_BAREMETAL or MCS_KM_PED_RISCV
 * @return 0 on success, -1 on failure (including timeout)
 */
int receive_data_from_rtos(void *rcv_data, int *rcv_data_len, int target_instance, int timeout_ms, enum mcs_km_pedestal_type ped_type);

/**
 * @brief One-shot send and wait for reply (legacy API; uses BAREMETAL)
 *
 * Use send_data_to_rtos_and_wait_rcv_ped to specify pedestal.
 *
 * @param data Data to send
 * @param data_len Data length
 * @param target_instance Target instance ID (must be 0)
 * @param rcv_data Receive buffer
 * @param rcv_data_len Output: received length
 * @return 0 on success, -1 on failure
 */
extern int send_data_to_rtos_and_wait_rcv(void *data, int data_len, int target_instance, void *rcv_data, int *rcv_data_len);

/**
 * @brief One-shot send and wait for reply with specified pedestal type
 */
extern int send_data_to_rtos_and_wait_rcv_ped(void *data, int data_len, int target_instance, void *rcv_data, int *rcv_data_len, enum mcs_km_pedestal_type ped_type);

#endif	/* USER_MSG_H */