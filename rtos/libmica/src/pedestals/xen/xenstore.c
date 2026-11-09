/* SPDX-License-Identifier: BSD-2-Clause
 * xenbus.c: static, synchronous, read-only xenbus client for hvmloader.
 * Copyright (c) 2009 Tim Deegan, Citrix Systems (R&D) Ltd.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 *
 * Modified 2026 from Xen RELEASE-4.18.2 tools/firmware/hvmloader/xenbus.c:
 * OS GPA mapping, serialized read/write transactions, checked wrap/capacity,
 * bounded waits and sticky poisoned stream after incomplete IO. No discovery,
 * infinite SCHEDOP_poll, ring reset, watches or backend path logic.
 */
#include "support.h"
#include <string.h>
static struct xen_xs_ring *rings;
static void *ring_view;
static mica_lock_t xs_lock;
static bool xs_lock_acquired;
static atomic_bool sync_failed;
static mica_sem_t xs_pause;
static bool xs_pause_acquired;
static uint32_t event, request_id;
static bool poisoned;
static char payload[XS_PAYLOAD_MAX + 1];

static int pause_until(uint64_t deadline)
{
    uint64_t now = mica_now_ns();

    if (now >= deadline)
        return -ETIMEDOUT;
    uint64_t until = deadline - now > UINT64_C(1000000) ? now + UINT64_C(1000000) : deadline;
    int rc = mica_sem_wait_until(&xs_pause, until);

    return rc == -ETIMEDOUT ? 0 : rc;
}

static int transfer(void *buffer, uint32_t len, bool write, uint64_t deadline)
{
    char *data = buffer;

    while (len)
    {
        if (mica_now_ns() >= deadline)
            return -ETIMEDOUT;
        uint32_t cons = __atomic_load_n(write ? &rings->req_cons : &rings->rsp_cons, __ATOMIC_ACQUIRE);
        uint32_t prod = __atomic_load_n(write ? &rings->req_prod : &rings->rsp_prod, __ATOMIC_ACQUIRE);
        uint32_t used = prod - cons;

        if (used > XS_RING_SIZE)
            return -EPROTO;
        uint32_t part = write ? XS_RING_SIZE - used : used;

        if (!part)
        {
            int rc = pause_until(deadline);

            if (rc)
                return rc;
            continue;
        }

        uint32_t index = (write ? prod : cons) & (XS_RING_SIZE - 1);

        if (part > XS_RING_SIZE - index)
            part = XS_RING_SIZE - index;
        if (part > len)
            part = len;
        if (write)
        {
            memcpy(rings->req + index, data, part);
            __atomic_store_n(&rings->req_prod, prod + part, __ATOMIC_RELEASE);
        }
        else
        {
            memcpy(data, rings->rsp + index, part);
            __atomic_store_n(&rings->rsp_cons, cons + part, __ATOMIC_RELEASE);
        }

        int rc = xen_event_send(event);

        if (rc)
            return rc;
        data += part;
        len -= part;
    }
    return 0;
}

static size_t bounded_length(const char *s)
{
    size_t n = 0;

    while (n <= XS_PAYLOAD_MAX && s[n])
        ++n;
    return n;
}

static int remote_error(const char *name)
{
    static const struct
    {
        const char *name;
        int code;
    }
    errors[] = {
        {
            "ENOENT",
            ENOENT
        },
        {
            "EACCES",
            EACCES
        },
        {
            "EINVAL",
            EINVAL
        },
        {
            "EEXIST",
            EEXIST
        },
        {
            "ENOSPC",
            ENOSPC
        },
        {
            "ENOMEM",
            ENOMEM
        },
        {
            "EAGAIN",
            EAGAIN
        },
        {
            "EPERM",
            EPERM
        },
        {
            "EIO",
            EIO
        },
        {
            "EBUSY",
            EBUSY
        },
        {
            "EROFS",
            EROFS
        },
        {
            "ENOSYS",
            ENOSYS
        }
    };
    for (size_t n = 0; n < sizeof(errors) / sizeof(errors[0]); ++n)
        if (!strcmp(name, errors[n].name))
        return -errors[n].code;
    return -EIO;
}

static int transaction(uint32_t type, const char *key, const char *value, char *result, size_t capacity)
{
    if (!key || (type == XS_READ && (!result || !capacity)) || (type == XS_WRITE && !value))
        return -EINVAL;
    if (!xs_lock_acquired || !ring_view ||
        atomic_load_explicit(&sync_failed, memory_order_acquire))
        return -ENODEV;
    if (result)
        result[0] = '\0';
    size_t keylen = bounded_length(key), vallen = value ? bounded_length(value) : 0;

    if (keylen >= XS_PAYLOAD_MAX || vallen > XS_PAYLOAD_MAX || vallen > XS_PAYLOAD_MAX - keylen - 1)
        return -E2BIG;
    uint64_t deadline = xen_deadline();
    int rc = mica_lock_acquire(&xs_lock, deadline);

    if (rc)
        return rc;
    if (poisoned)
    {
        rc = -EIO;
        goto done;
    }
    memcpy(payload, key, keylen + 1);
    if (value)
        memcpy(payload + keylen + 1, value, vallen);
    struct xen_xs_msg req = {
        .type = type,
        .req_id = ++request_id,
        .len = (uint32_t)(keylen + 1 + vallen)
    };

    struct xen_xs_msg rsp;

    rc = transfer(&req, sizeof(req), true, deadline);
    if (!rc)
        rc = transfer(payload, req.len, true, deadline);
    if (!rc)
        rc = transfer(&rsp, sizeof(rsp), false, deadline);
    if (rc)
        goto corrupt;
    if (rsp.len > XS_PAYLOAD_MAX || rsp.req_id != req.req_id || rsp.tx_id || (rsp.type != type &&
        rsp.type != XS_ERROR))
    {
        rc = -EPROTO;
        goto corrupt;
    }
    rc = transfer(payload, rsp.len, false, deadline);
    if (rc)
        goto corrupt;
    payload[rsp.len] = '\0';
    if (rsp.type == XS_ERROR)
    {
        rc = remote_error(payload);
    }
    else if (type == XS_READ)
    {
        if (rsp.len >= capacity)
        {
            rc = -ENOSPC;
        }
        else
        {
            memcpy(result, payload, rsp.len);
            result[rsp.len] = '\0';
        }
    }
    else if (rsp.len != sizeof("OK") || memcmp(payload, "OK", sizeof("OK")))
    {
        /* XS_WRITE's supported success payload is exactly O,K,NUL. The
         * complete frame is already consumed, so a bad ACK need not poison
         * the stream (unlike incomplete IO or a corrupt frame header). */
        rc = -EPROTO;
    }
    goto done;
corrupt:
    /* An outstanding request or partial frame cannot safely be reused.
     * No silent reset or reconnection; domain remains owner of the ring. */
    poisoned = true;
done:
    {
        int released = mica_lock_release(&xs_lock);

        if (released)
        {
            atomic_store_explicit(&sync_failed, true, memory_order_release);
            if (!rc)
                rc = released;
        }
    }
    return rc;
}

int xen_xs_read(const char *key, char *value, size_t capacity)
{
    return transaction(XS_READ, key, NULL, value, capacity);
}

int xen_xs_write(const char *key, const char *value)
{
    return transaction(XS_WRITE, key, value, NULL, 0);
}

int xen_xs_init(void)
{
    uint64_t pfn = 0, port = 0;
    int rc = xen_hvm_get(HVM_STORE_PFN, &pfn);

    if (rc)
        return rc;
    rc = xen_hvm_get(HVM_STORE_EVTCHN, &port);
    if (rc)
        return rc;
    event = (uint32_t)port;

    void *va = NULL;

    if (!pfn || pfn > (UINT64_MAX - MICA_XEN_PAGE_SIZE) / MICA_XEN_PAGE_SIZE || port > UINT32_MAX)
        return -EINVAL;
    rc = mica_mem_map(pfn << 12, MICA_XEN_PAGE_SIZE, &va);
    if (rc)
        return rc;
    ring_view = va; /* Domain-owned borrowed view; never allocator-free it. */
    rings = va;
    rc = mica_lock_init(&xs_lock);
    if (rc)
        return rc;
    xs_lock_acquired = true;
    if (!xs_pause_acquired)
    {
        rc = mica_sem_init(&xs_pause, 0);
        if (rc)
            return rc;
        xs_pause_acquired = true;
    }
    return 0;
}
