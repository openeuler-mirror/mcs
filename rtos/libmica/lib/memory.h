/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_PLATFORM_MEMORY_H
#define MICA_PLATFORM_MEMORY_H
#include <stddef.h>
#include <stdint.h>

/* Actual allocations and local views, not grant mappings or region objects.
 * physical is this guest's GPA/IPA. alloc/map succeed with usable storage/view;
 * failure leaves the output NULL and acquires nothing. Callers supply valid
 * outputs and a nonzero power-of-two allocation alignment.
 * Page alignment, GPA and local views require platform hooks with paired
 * allocation/free. Failed free/unmap retains ownership; borrowed views are
 * not freed. */
#include <mica/platform/system/@PROJECT_SYSTEM@/memory.h>
#endif
