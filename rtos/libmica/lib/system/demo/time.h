/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_DEMO_TIME_H
#define MICA_DEMO_TIME_H
#include <demo_time.h>
#define MICA_OS_TICK_HZ DEMO_TICK_HZ
#define MICA_OS_MAX_FINITE_TICKS DEMO_MAX_FINITE_TICKS
static inline uint64_t mica_now_ns(void)
{
    return demo_monotonic_ns();
}
#endif
