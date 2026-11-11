/* SPDX-License-Identifier: MulanPSL-2.0 */
#include "mica_internal.h"

static struct mica_context context;
atomic_flag mica_init_gate = ATOMIC_FLAG_INIT;
static atomic_uint entry_users;

/* Private common cancellation, never destroys backend resources. */
int mica_context_cancel(struct mica_context *ctx)
{
    atomic_store_explicit(&ctx->cancel, true, memory_order_release);
    int ret = 0;
    int posted = mica_sem_post(&ctx->receiver_wake);

    if (posted && posted != -EOVERFLOW)
    {
        atomic_store_explicit(&ctx->sync_failed, true, memory_order_release);
        if (!ret)
            ret = posted;
    }
    return ret;
}

struct mica_context *mica_context_get(void)
{
    return &context;
}

static int pin(struct mica_context *ctx, bool lifecycle)
{
    if (atomic_flag_test_and_set_explicit(&mica_init_gate, memory_order_acquire))
        return -EBUSY;
    int ret = !ctx->state_lock_acquired ? -ENODEV :
        !lifecycle && ctx->entry_closed ? -ESHUTDOWN :
        atomic_load_explicit(&ctx->sync_failed, memory_order_acquire) ? -EIO : 0;

    if (!ret)
        atomic_fetch_add_explicit(&entry_users, 1, memory_order_relaxed);
    atomic_flag_clear_explicit(&mica_init_gate, memory_order_release);
    return ret;
}

int mica_context_pin(struct mica_context *ctx)
{
    return pin(ctx, false);
}

int mica_context_entry_lock(struct mica_context *ctx)
{
    int ret = pin(ctx, true);

    if (ret)
        return ret;
    ret = mica_context_lock(ctx);

    if (ret)
        mica_context_unpin();
    return ret;
}

int mica_context_close(struct mica_context *ctx)
{
    if (atomic_flag_test_and_set_explicit(&mica_init_gate, memory_order_acquire))
        return -EBUSY;
    ctx->entry_closed = true;
    atomic_flag_clear_explicit(&mica_init_gate, memory_order_release);
    return 0;
}

int mica_context_open(struct mica_context *ctx)
{
    if (atomic_flag_test_and_set_explicit(&mica_init_gate, memory_order_acquire))
        return -EBUSY;
    int ret = atomic_load_explicit(&ctx->sync_failed, memory_order_acquire) ? -EIO : 0;

    if (!ret)
        ctx->entry_closed = false;
    atomic_flag_clear_explicit(&mica_init_gate, memory_order_release);
    return ret;
}

int mica_context_drain_entries(struct mica_context *ctx, mica_deadline_t deadline)
{
    while (mica_context_entry_users())
    {
        if (mica_now_ns() >= deadline)
            return -ETIMEDOUT;
        if (!ctx->references_changed_acquired)
            return -EBUSY;
        int ret = mica_sem_wait_until(&ctx->references_changed, mica_now_ns() + 1000000ULL);

        if (ret && ret != -ETIMEDOUT)
            return ret;
    }
    return 0;
}

void mica_context_unpin(void)
{
    atomic_fetch_sub_explicit(&entry_users, 1, memory_order_release);
}

unsigned int mica_context_entry_users(void)
{
    return atomic_load_explicit(&entry_users, memory_order_acquire);
}

int mica_context_entry_unlock(struct mica_context *ctx)
{
    int ret = mica_context_unlock(ctx);

    mica_context_unpin();
    return ret;
}

int mica_context_destroy_lock(struct mica_context *ctx)
{
    if (atomic_flag_test_and_set_explicit(&mica_init_gate, memory_order_acquire))
        return -EBUSY;
    if (!ctx->entry_closed || mica_context_entry_users())
    {
        atomic_flag_clear_explicit(&mica_init_gate, memory_order_release);
        return -EBUSY;
    }

    int ret = 0;

    if (atomic_load_explicit(&ctx->sync_failed, memory_order_acquire))
        ret = -EIO;
    else if (ctx->state_lock_acquired)
        metal_mutex_deinit(&ctx->state_lock);
    if (!ret)
    {
        ctx->state_lock_acquired = false;
        ctx->state = MICA_UNINITIALIZED;
        ctx->operation = false;
    }
    atomic_flag_clear_explicit(&mica_init_gate, memory_order_release);
    return ret;
}

/* Internal receiver/service/helper callers already own context lifetime (or
 * a lifecycle operation). Do not let a transient static ENTRY gate collision
 * discard a successful publication or kill a receiver. */
int mica_context_lock(struct mica_context *ctx)
{
    if (!ctx->state_lock_acquired)
        return -ENODEV;
    if (atomic_load_explicit(&ctx->sync_failed, memory_order_acquire))
        return -EIO;
    metal_mutex_acquire(&ctx->state_lock);
    return 0;
}

int mica_context_unlock(struct mica_context *ctx)
{
    metal_mutex_release(&ctx->state_lock);
    return 0;
}
