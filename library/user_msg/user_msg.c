#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <unistd.h>
#include <sys/stat.h>
#include <syslog.h>
#include <time.h>
#include <errno.h>
#include <pthread.h>
#include <fcntl.h>
#include <sys/mman.h>
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
    /* Receive callback: one per context; thread started on register */
    pthread_t rcv_thread;
    umt_rcv_cb_t rcv_cb;
    void *rcv_priv;
    volatile int rcv_stop;
    char *rcv_buf;  /* OPENAMP_SHM_COPY_SIZE, used only by rcv thread */
};

/* ============================================================================
 * Lock helpers
 * ============================================================================ */
static void umt_context_lock(umt_context_t *ctx)
{
    pthread_mutex_lock(&ctx->process_shared_memory->lock);
    ctx->locked = 1;
}

static void umt_context_unlock(umt_context_t *ctx)
{
    if (ctx->locked) {
        pthread_mutex_unlock(&ctx->process_shared_memory->lock);
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

/* Copy received data (rcv_phy_addr, rcv_data_len in process_shared_memory) into buf; caller must hold lock. */
static int _read_rcv_to_buf(umt_context_t *ctx, void *buf, int buf_size, int *out_len)
{
    unsigned long phy_addr;
    int data_len;
    uint32_t page_size;
    uint64_t page_base;
    uint64_t page_offset;
    uint64_t map_size;
    void *mapped_addr = NULL;
    int fd = -1;
    int ret = 0;
    char *src, *dst;
    int copy_len;

    data_len = ctx->process_shared_memory->rcv_data_len;
    phy_addr = ctx->process_shared_memory->rcv_phy_addr;

    if (data_len <= 0 || data_len > OPENAMP_SHM_COPY_SIZE || data_len > buf_size) {
        ret = -EINVAL;
        goto out;
    }

    fd = open("/dev/mem", O_RDONLY | O_SYNC);
    if (fd < 0) {
        ret = -ENODEV;
        goto out;
    }
    page_size = (uint32_t)sysconf(_SC_PAGESIZE);
    page_base = phy_addr & ~(page_size - 1);
    page_offset = phy_addr - page_base;
    map_size = page_offset + (uint64_t)data_len;
    if (map_size % page_size != 0)
        map_size = ((map_size / page_size) + 1) * page_size;

    mapped_addr = mmap(NULL, (size_t)map_size, PROT_READ, MAP_SHARED, fd, (off_t)page_base);
    if (mapped_addr == MAP_FAILED) {
        ret = -EFAULT;
        goto out;
    }
    src = (char *)mapped_addr + page_offset;
    dst = (char *)buf;
    copy_len = data_len;
    if (((uintptr_t)src | (uintptr_t)dst | copy_len) & 3) {
        for (int i = 0; i < copy_len; i++)
            dst[i] = src[i];
    } else {
        memcpy(dst, src, (size_t)copy_len);
    }
    *out_len = data_len;

out:
    if (mapped_addr != NULL && mapped_addr != MAP_FAILED)
        munmap(mapped_addr, (size_t)map_size);
    if (fd >= 0)
        close(fd);
    return ret;
}

static int _do_receive(umt_context_t *ctx, void *rcv_data, int *rcv_data_len,
                       int timeout_ms)
{
    int ret;

    umt_context_unlock(ctx);
    ret = wait_semaphore_with_timeout(ctx->sem_micad_to_user, timeout_ms);
    if (ret != 0)
        return ret;
    umt_context_lock(ctx);

    if (ctx->process_shared_memory->rcv_data_len == 0) {
        syslog(LOG_ERR, "rcv_data_len is 0\n");
        return -ENODATA;
    }
    if (ctx->process_shared_memory->rcv_data_len > OPENAMP_SHM_COPY_SIZE) {
        syslog(LOG_ERR, "Invalid rcv_data_len: %d\n", ctx->process_shared_memory->rcv_data_len);
        return -EINVAL;
    }
    syslog(LOG_INFO, "phy_addr = 0x%lx, data_len = %d\n",
           (unsigned long)ctx->process_shared_memory->rcv_phy_addr,
           ctx->process_shared_memory->rcv_data_len);

    ret = _read_rcv_to_buf(ctx, rcv_data, OPENAMP_SHM_COPY_SIZE, rcv_data_len);
    return ret;
}

/* Callback mode: no receive timeout; thread waits for data until unregister/destroy.
 * We use sem_timedwait(1s) only to periodically check rcv_stop so unregister can complete. */
static void *rcv_callback_thread(void *arg)
{
    umt_context_t *ctx = (umt_context_t *)arg;
    struct timespec ts;
    int ret, len;

    while (!ctx->rcv_stop) {
        if (clock_gettime(CLOCK_REALTIME, &ts) != 0)
            continue;
        ts.tv_sec += 1;
        if (sem_timedwait(ctx->sem_micad_to_user, &ts) != 0)
            continue;  /* timeout or error: re-check rcv_stop, then wait again */

        umt_context_lock(ctx);
        ret = _read_rcv_to_buf(ctx, ctx->rcv_buf, OPENAMP_SHM_COPY_SIZE, &len);
        umt_context_unlock(ctx);

        if (ret == 0 && ctx->rcv_cb)
            ctx->rcv_cb(ctx->rcv_buf, len, ctx->rcv_priv);
    }
    return NULL;
}

static int _stop_rcv_callback(umt_context_t *ctx)
{
    if (ctx->rcv_cb == NULL)
        return -1;
    ctx->rcv_stop = 1;
    pthread_join(ctx->rcv_thread, NULL);
    ctx->rcv_cb = NULL;
    ctx->rcv_priv = NULL;
    ctx->rcv_stop = 0;
    free(ctx->rcv_buf);
    ctx->rcv_buf = NULL;
    return 0;
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

    if (ctx->rcv_cb != NULL)
        _stop_rcv_callback(ctx);

    if (ctx->locked && ctx->process_shared_memory)
        pthread_mutex_unlock(&ctx->process_shared_memory->lock);
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

int umt_register_rcv_cb(umt_context_t *ctx, umt_rcv_cb_t callback, void *priv)
{
    if (ctx == NULL || callback == NULL) {
        syslog(LOG_ERR, "umt_register_rcv_cb: invalid ctx or callback\n");
        return -EINVAL;
    }
    if (ctx->rcv_cb != NULL) {
        syslog(LOG_ERR, "umt_register_rcv_cb: already registered\n");
        return -EINVAL;
    }
    ctx->rcv_buf = malloc(OPENAMP_SHM_COPY_SIZE);
    if (ctx->rcv_buf == NULL) {
        syslog(LOG_ERR, "umt_register_rcv_cb: malloc rcv_buf failed\n");
        return -ENOMEM;
    }
    ctx->rcv_cb = callback;
    ctx->rcv_priv = priv;
    ctx->rcv_stop = 0;
    if (pthread_create(&ctx->rcv_thread, NULL, rcv_callback_thread, ctx) != 0) {
        free(ctx->rcv_buf);
        ctx->rcv_buf = NULL;
        ctx->rcv_cb = NULL;
        ctx->rcv_priv = NULL;
        syslog(LOG_ERR, "umt_register_rcv_cb: pthread_create failed\n");
        return -EAGAIN;
    }
    return 0;
}

int umt_unregister_rcv_cb(umt_context_t *ctx)
{
    if (ctx == NULL)
        return -EINVAL;
    return _stop_rcv_callback(ctx);
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