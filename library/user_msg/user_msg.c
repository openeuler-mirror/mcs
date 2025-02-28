#include <memory/shm_pool.h>
#include <user_msg/user_msg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/stat.h>
#include <syslog.h>


/**
 * @brief 发送数据到指定实例的RTOS并等待接收返回结果。
 *
 * 该函数用于将数据发送到指定实例的RTOS（实时操作系统），并等待接收返回结果。
 *
 * @param data 指向要发送的数据内容的指针。调用者应确保该缓冲区包含正确的数据内容。
 * @param data_len 要发送的数据长度。调用者应提供实际要发送的数据长度。
 * @param target_instance 要发送的目标实例ID, uniproton 启动配置文件中"InstanceID"值。
 *               注：     当前不支持多实例，target_instance赋值为0；支持多实例以后修改成具体实例号
 * @param rcv_data 指向接收数据缓冲区的指针。调用者应确保缓冲区已分配足够的内存来存储接收到的数据。
 * @param rcv_data_len 指向接收数据长度的指针。调用者应提供一个整型变量的地址，用于存储接收到的数据长度。
 *
 * @return int 返回值表示函数执行结果。
 *             - 0 表示成功发送数据并接收返回结果。
 *             - -1 表示发送失败。
 */
int send_data_to_rtos_and_wait_rcv(void *data, int data_len, int target_instance, void *rcv_data, int *rcv_data_len)
{
    int ret;
    process_shared_data_t *process_shared_memory = NULL;
    struct core_msg_mem_info core_shared_memory_info = {0};
    sem_t *sem_user_to_micad;
    sem_t *sem_micad_to_user;

    if (data_len > OPENAMP_SHM_COPY_SIZE) {
         syslog(LOG_ERR, "The data length exceeds the maximum limit %d\n", OPENAMP_SHM_COPY_SIZE);
         return -1;
    }

    /* 获取与micad 进程通信的共享内存 */
    process_shared_memory = init_process_shared_memory(target_instance);
    if (process_shared_memory == NULL) {
        syslog(LOG_ERR, "init_process_shared_memory failed\n");
        return -1;
    }

    /* 判断目标实例是否创建 */
    if (process_shared_memory->instance_id != target_instance) {
        syslog(LOG_ERR, "target_instance is incorrect\n");
        return -1;
    }
    /* 创建于micad 通信的信号量 */
    ret = create_sem(target_instance, &sem_user_to_micad, &sem_micad_to_user);

    /* 获取锁*/
    while (__sync_lock_test_and_set(&process_shared_memory->lock, 1)) {
        // 自旋等待
    }

    /* 核间通信内存 */
    core_shared_memory_info.instance_id = target_instance;

    ret = init_core_shared_memory(&core_shared_memory_info);
    if (ret != 0) {
        goto err;
    }

    /* 拷贝用户数据到核间通信共享内存中 */
    memcpy(core_shared_memory_info.vir_addr, data, data_len);
    process_shared_memory->phy_addr = core_shared_memory_info.phy_addr;
    process_shared_memory->data_len = data_len;

    /* 发送消息给micad */
    sem_post(sem_user_to_micad);

    /* 等待返回消息 */
    sem_wait(sem_micad_to_user);
    memcpy(rcv_data, core_shared_memory_info.vir_addr + OPENAMP_SHM_COPY_SIZE, process_shared_memory->rcv_data_len);
    *rcv_data_len = process_shared_memory->rcv_data_len;

    sem_close(sem_user_to_micad);
    sem_close(sem_micad_to_user);
    __sync_lock_release(&process_shared_memory->lock);

    if (process_shared_memory != NULL)
        munmap(process_shared_memory, sizeof(process_shared_data_t));
    if (core_shared_memory_info.vir_addr != NULL)
        munmap(core_shared_memory_info.vir_addr, core_shared_memory_info.align_size);

    return 0;
err:
    if (process_shared_memory != NULL)
        munmap(process_shared_memory, sizeof(process_shared_data_t));
    if (core_shared_memory_info.vir_addr != NULL)
        munmap(core_shared_memory_info.vir_addr, core_shared_memory_info.align_size);
    return -1;
}
