/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_PLATFORM_TIME_H
#define MICA_PLATFORM_TIME_H
#include <stdint.h>

typedef uint64_t mica_deadline_t; /* absolute monotonic nanoseconds */

/* Round positive remaining time up to finite ticks, saturating before any
 * multiplication/addition can overflow. hz/max_finite must both be positive. */
static inline uint32_t mica_timeout_ticks(uint64_t delta_ns, uint32_t hz,
                                           uint32_t max_finite)
{
    uint64_t seconds = delta_ns / 1000000000ULL;
    uint64_t remainder = delta_ns % 1000000000ULL;
    if (seconds > max_finite / hz)
        return max_finite;
    uint64_t ticks = seconds * hz;
    uint64_t fractional = (remainder * hz + 999999999ULL) / 1000000000ULL;
    if (fractional > max_finite - ticks)
        return max_finite;
    return (uint32_t)(ticks + fractional);
}

#include <mica/platform/system/@PROJECT_SYSTEM@/time.h>
#endif
