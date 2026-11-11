/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_PLATFORM_THREAD_H
#define MICA_PLATFORM_THREAD_H
#include <mica/platform/time.h>

/* Native storage; acquisition is tracked separately by the communication owner.
 * Entry is OS-compatible void *entry(void *). Only the operation owner joins,
 * with an absolute monotonic deadline, and records success before other cleanup.
 * A failed join leaves the task and its argument alive. No detach/cancel. */
#include <mica/platform/system/@PROJECT_SYSTEM@/thread.h>
#endif
