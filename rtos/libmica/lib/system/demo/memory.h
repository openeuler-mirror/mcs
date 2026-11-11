/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_DEMO_MEMORY_H
#define MICA_DEMO_MEMORY_H
#include <demo_memory.h>
/* BSP supplies usable storage/views on success; alloc/map failure sets output
 * NULL with nothing acquired. free/unmap failure retains ownership. */
static inline int mica_mem_alloc(size_t bytes, size_t align, void **base)
{
    return demo_mem_alloc(bytes, align, base);
}
static inline int mica_mem_free(void *base)
{
    return demo_mem_free(base);
}
static inline int mica_mem_address(const void *local, uint64_t *physical)
{
    return demo_mem_address(local, physical);
}
static inline int mica_mem_map(uint64_t physical, size_t bytes, void **local)
{
    return demo_mem_map(physical, bytes, local);
}
static inline int mica_mem_unmap(void *local, size_t bytes)
{
    return demo_mem_unmap(local, bytes);
}
#endif
