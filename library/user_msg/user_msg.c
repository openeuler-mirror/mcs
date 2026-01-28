#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/stat.h>
#include <syslog.h>
#include <time.h>
#include <errno.h>
#include <memory/shm_pool.h>
#include <user_msg/user_msg.h>

/* ============================================================================
 * Private: UMT communication context (shared memory, semaphores, etc.)
 * ============================================================================ */
struct umt_context {
    int target_instance;
    process_shared_data_t *process_shared_memory;
    struct core_msg_mem_info core_shared_memory_info;
    sem_t *sem_user_to_micad;
    sem_t *sem_micad_to_user;
    int locked;
};

/* ============================================================================
 * Lock helpers (spinlock via test-and-set)
 * ============================================================================ */
static void umt_context_lock(umt_context_t *ctx)
{
    while (__sync_lock_test_and_set(&ctx->process_shared_memory->lock, 1)) {
        /* spin until acquired */
    }
    ctx->locked = 1;
}

static void umt_context_unlock(umt_context_t *ctx)
{
    if (ctx->locked) {
        __sync_lock_release(&ctx->process_shared_memory->lock);
        ctx->locked = 0;
    }
}

/* ============================================================================
 * Internal: send/receive helpers (caller must hold lock for _do_send)
 * ============================================================================ */
static int _do_send(umt_context_t *ctx, int offset, void *data, int data_len)
{
    if (offset < 0 || offset + data_len > OPENAMP_SHM_COPY_SIZE) {
        syslog(LOG_ERR, "Invalid offset %d or data_len %d (max: %d)\n", 
               offset, data_len, OPENAMP_SHM_COPY_SIZE);
        return -EINVAL;
    }

    memcpy(ctx->core_shared_memory_info.vir_addr + offset, data, data_len);
    ctx->process_shared_memory->phy_addr = ctx->core_shared_memory_info.phy_addr + offset;
    ctx->process_shared_memory->data_len = data_len;
    sem_post(ctx->sem_user_to_micad);

    return 0;
}

static int wait_semaphore_with_timeout(sem_t *sem, int timeout_ms)
{
    struct timespec ts;

    if (timeout_ms == 0) {
        if (sem_wait(sem) != 0) {
            syslog(LOG_ERR, "sem_wait failed\n");
            return -EAGAIN;
        }
    } else {
        if (clock_gettime(CLOCK_REALTIME, &ts) != 0) {
            syslog(LOG_ERR, "clock_gettime failed\n");
            return -EAGAIN;
        }
        ts.tv_sec += timeout_ms / 1000;
        ts.tv_nsec += (timeout_ms % 1000) * 1000000;
        if (ts.tv_nsec >= 1000000000) {
            ts.tv_sec++;
            ts.tv_nsec -= 1000000000;
        }
        if (sem_timedwait(sem, &ts) != 0) {
            syslog(LOG_ERR, "Receive timeout\n");
            return -ETIMEDOUT;
        }
    }
    return 0;
}

static int _do_receive(umt_context_t *ctx, void *rcv_data, int *rcv_data_len, 
                       int timeout_ms)
{
    umt_rcv_msg_t msg;
    void *mapped_addr = NULL;
    int fd = -1;
    int ret = 0;
    uint32_t page_size;
    uint64_t page_base;
    uint32_t page_offset;
    uint32_t map_size;
    char *src = NULL;
    char *dst = NULL;
    int copy_len = 0;

    umt_context_unlock(ctx);
    ret = wait_semaphore_with_timeout(ctx->sem_micad_to_user, timeout_ms);
    if (ret != 0)
        return ret;
    umt_context_lock(ctx);

    if (ctx->process_shared_memory->rcv_data_len == 0) {
        syslog(LOG_ERR, "rcv_data_len is 0\n");
        ret = -ENODATA;
        goto cleanup;
    }
    msg.data_len = ctx->process_shared_memory->rcv_data_len;
    msg.phy_addr = ctx->process_shared_memory->rcv_phy_addr;

    if (msg.data_len <= 0 || msg.data_len > OPENAMP_SHM_COPY_SIZE) {
        syslog(LOG_ERR, "Invalid rcv_data_len: %d\n", msg.data_len);
        ret = -EINVAL;
        goto cleanup;
    }
    syslog(LOG_INFO, "phy_addr = 0x%lx, data_len = %d\n", msg.phy_addr, msg.data_len);

    fd = open("/dev/mem", O_RDONLY | O_SYNC);
    if (fd < 0) {
        syslog(LOG_ERR, "Failed to open /dev/mem\n");
        ret = -ENODEV;
        goto cleanup;
    }

    page_size = sysconf(_SC_PAGESIZE);
    page_base = msg.phy_addr & ~(page_size - 1);
    page_offset = msg.phy_addr - page_base;
    map_size = page_offset + msg.data_len;
    if (map_size % page_size != 0)
        map_size = ((map_size / page_size) + 1) * page_size;

    mapped_addr = mmap(NULL, map_size, PROT_READ, MAP_SHARED, fd, page_base);
    if (mapped_addr == MAP_FAILED) {
        syslog(LOG_ERR, "mmap failed for phy_addr 0x%lx\n", msg.phy_addr);
        ret = -EFAULT;
        goto cleanup;
    }

    syslog(LOG_INFO, "memcpy mapped_addr %p, page_offset 0x%x, data_len = %d\n", mapped_addr, page_offset, msg.data_len);

    src = (char *)mapped_addr + page_offset;
    dst = (char *)rcv_data;
    copy_len = msg.data_len;
    if (((uintptr_t)src | (uintptr_t)dst | copy_len) & 3) {
        for (int i = 0; i < copy_len; i++)
            dst[i] = src[i];
    } else {
        memcpy(dst, src, copy_len);
    }
    *rcv_data_len = msg.data_len;

cleanup:
    if (mapped_addr != NULL && mapped_addr != MAP_FAILED)
        munmap(mapped_addr, map_size);
    if (fd >= 0)
        close(fd);
    return ret;

}

/* ============================================================================
 * Context create/destroy
 * ============================================================================ */

umt_context_t* umt_context_create(int target_instance, enum mcs_km_pedestal_type ped_type)
{
    umt_context_t *ctx = malloc(sizeof(umt_context_t));
    if (ctx == NULL) {
        syslog(LOG_ERR, "malloc umt_context_t failed\n");
        return NULL;
    }

    memset(ctx, 0, sizeof(umt_context_t));
    ctx->target_instance = target_instance;

    if (target_instance != 0) {
        syslog(LOG_ERR, "Only instance 0 supported\n");
        goto err_free_ctx;
    }

    ctx->process_shared_memory = init_process_shared_memory(target_instance);
    if (ctx->process_shared_memory == NULL) {
        syslog(LOG_ERR, "init_process_shared_memory failed\n");
        goto err_free_ctx;
    }

    if (ctx->process_shared_memory->instance_id != target_instance) {
        syslog(LOG_ERR, "Instance ID mismatch\n");
        goto err_unmap_process_shm;
    }

    if (create_sem(target_instance, &ctx->sem_user_to_micad, 
                   &ctx->sem_micad_to_user) != 0) {
        syslog(LOG_ERR, "create_sem failed\n");
        goto err_unmap_process_shm;
    }

    ctx->core_shared_memory_info.instance_id = target_instance;
    if (init_core_shared_memory(&ctx->core_shared_memory_info, ped_type) != 0) {
        syslog(LOG_ERR, "init_core_shared_memory failed\n");
        goto err_close_sem;
    }

    return ctx;

err_close_sem:
    if (ctx->sem_user_to_micad) sem_close(ctx->sem_user_to_micad);
    if (ctx->sem_micad_to_user) sem_close(ctx->sem_micad_to_user);
err_unmap_process_shm:
    munmap(ctx->process_shared_memory, sizeof(process_shared_data_t));
err_free_ctx:
    free(ctx);
    return NULL;
}

void umt_context_destroy(umt_context_t *ctx)
{
    if (ctx == NULL)
        return;

    if (ctx->locked && ctx->process_shared_memory)
        __sync_lock_release(&ctx->process_shared_memory->lock);
    if (ctx->sem_user_to_micad)
        sem_close(ctx->sem_user_to_micad);
    if (ctx->sem_micad_to_user)
        sem_close(ctx->sem_micad_to_user);
    if (ctx->process_shared_memory)
        munmap(ctx->process_shared_memory, sizeof(process_shared_data_t));
    if (ctx->core_shared_memory_info.vir_addr)
        munmap(ctx->core_shared_memory_info.vir_addr, ctx->core_shared_memory_info.align_size);

    free(ctx);
}

/* ============================================================================
 * Context-based send/receive
 * ============================================================================ */
int send_data_with_umt_context(umt_context_t *ctx, int offset, void *data, int data_len)
{
    int ret;

    if (ctx == NULL) {
        syslog(LOG_ERR, "Invalid context\n");
        return -EINVAL;
    }
    if (offset < 0 || offset >= OPENAMP_SHM_COPY_SIZE - data_len) {
        syslog(LOG_ERR, "Invalid offset or data_len\n");
        return -EINVAL;
    }
    umt_context_lock(ctx);
    ret = _do_send(ctx, offset, data, data_len);
    umt_context_unlock(ctx);

    return ret;
}

int receive_data_with_umt_context(umt_context_t *ctx, void *rcv_data, 
                               int *rcv_data_len, int timeout_ms)
{
    int ret;

    if (ctx == NULL) {
        syslog(LOG_ERR, "Invalid context\n");
        return -EINVAL;
    }
    umt_context_lock(ctx);
    ret = _do_receive(ctx, rcv_data, rcv_data_len, timeout_ms);
    umt_context_unlock(ctx);

    return ret;
}

/* ============================================================================
 * One-shot send/receive (create/destroy context internally)
 * ============================================================================ */
int send_data_to_rtos(void *data, int data_len, int target_instance, enum mcs_km_pedestal_type ped_type)
{
    umt_context_t *ctx;
    int ret;

    ctx = umt_context_create(target_instance, ped_type);
    if (ctx == NULL) {
        return -ENOMEM;
    }
    ret = send_data_with_umt_context(ctx, 0, data, data_len);
    umt_context_destroy(ctx);
    return ret;
}

int receive_data_from_rtos(void *rcv_data, int *rcv_data_len, 
                           int target_instance, int timeout_ms, enum mcs_km_pedestal_type ped_type)
{
    umt_context_t *ctx;
    int ret;

    ctx = umt_context_create(target_instance, ped_type);
    if (ctx == NULL) {
        return -ENOMEM;
    }
    ret = receive_data_with_umt_context(ctx, rcv_data, rcv_data_len, timeout_ms);
    umt_context_destroy(ctx);
    return ret;
}

/* Legacy: default BAREMETAL */
int send_data_to_rtos_and_wait_rcv(void *data, int data_len, int target_instance, 
                                   void *rcv_data, int *rcv_data_len)
{
    return send_data_to_rtos_and_wait_rcv_ped(data, data_len, target_instance, rcv_data, rcv_data_len, MCS_KM_PED_BAREMETAL);
}

int send_data_to_rtos_and_wait_rcv_ped(void *data, int data_len, int target_instance, 
                                       void *rcv_data, int *rcv_data_len, enum mcs_km_pedestal_type ped_type)
{
    umt_context_t *ctx;
    int ret;

    ctx = umt_context_create(target_instance, ped_type);
    if (ctx == NULL) {
        return -ENOMEM;
    }
    umt_context_lock(ctx);
    ret = _do_send(ctx, 0, data, data_len);
    if (ret == 0) {
        ret = _do_receive(ctx, rcv_data, rcv_data_len, 0);
    }
    umt_context_unlock(ctx);
    umt_context_destroy(ctx);
    return ret;
}