/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_DEBUG_H
#define MICA_DEBUG_H
#include <stddef.h>
#ifdef __cplusplus
extern "C" {
#endif
/* Optional domain-lifetime output, independent of mica_init/TTY/stop/remove.
 * Normal startup/task context after OS mapping and allocator are available;
 * not an IRQ/panic/NMI console. No worker/thread or semaphore is required by
 * the builtin debug producer. One coordinated producer
 * owner per domain: do not run another uncoordinated PV-ring frontend.
 * Unsupported provider/pedestal: -EOPNOTSUPP; supported but uninitialized:
 * -ENODEV. Successful init is idempotent; concurrent producer/init: -EAGAIN.
 * Init does not initialize domain shared_info/events/xenstore. */
int mica_debug_init(void);
/* Returns published bytes (possibly short), not a display guarantee. No NUL,
 * blocking or automatic retry. len=0 returns0; NULL+len>0:-EINVAL;
 * len>INT_MAX:-EOVERFLOW. Full/busy with no publication:-EAGAIN. A notification
 * failure AFTER publication still returns the published count: never resend
 * that prefix. Invalid ring state:-EPROTO without modifying shared storage. */
int mica_debug_write(const void *bytes,size_t len);
/* Fixed256-byte stack formatting including terminator. Invalid format/result:
 * -EINVAL; truncation:-EMSGSIZE and nothing published. No retry on short write.
 * Does not redirect printf/PRT_Printf/mica_log or shell. */
int mica_debug_printf(const char *fmt,...);
#ifdef __cplusplus
}
#endif
#endif
