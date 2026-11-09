/* SPDX-License-Identifier: MulanPSL-2.0 */
#ifndef MICA_XEN_DEBUG_CONSOLE_H
#define MICA_XEN_DEBUG_CONSOLE_H
#include <stddef.h>
int xen_debug_init(void);
int xen_debug_write(const void *bytes,size_t len);
#endif
