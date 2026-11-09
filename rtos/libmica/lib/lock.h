/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_PLATFORM_LOCK_H
#define MICA_PLATFORM_LOCK_H
#include <mica/platform/time.h>

/* Xen transport/callback fences only (xs_lock, event_lock, notify_lock).
 * Ordinary context/service/TX mutexes use metal_mutex directly.
 * Native mutex storage. init/acquire/release/destroy return 0 or negative errno.
 * Handle zero is valid. Failed release has unknown ownership; failed destroy
 * retains the object. The caller must preserve storage in either case. */
#include <mica/platform/system/@PROJECT_SYSTEM@/lock.h>
#endif
