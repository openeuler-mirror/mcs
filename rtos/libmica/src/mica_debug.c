/* SPDX-License-Identifier: MulanPSL-2.0 */
#include <errno.h>
#include <limits.h>
#include <stdarg.h>
#include <stdio.h>
#include <mica/debug.h>
#ifdef OS_OPTION_XEN
#include "pedestals/xen/provider.h"
#endif
int mica_debug_init(void)
{
#ifdef OS_OPTION_XEN
    const struct mica_xen_provider_ops *p = mica_xen_provider_get();

    if (p && p->debug_init && p->debug_write)
        return p->debug_init();
#endif
    return -EOPNOTSUPP;
}

int mica_debug_write(const void *bytes, size_t len)
{
    if (!len)
        return 0;
    if (!bytes)
        return -EINVAL;
    if (len > INT_MAX)
        return -EOVERFLOW;
#ifdef OS_OPTION_XEN
    const struct mica_xen_provider_ops *p = mica_xen_provider_get();

    if (p && p->debug_init && p->debug_write)
        return p->debug_write(bytes, len);
#endif
    return -EOPNOTSUPP;
}

int mica_debug_printf(const char *fmt, ...)
{
    if (!fmt)
        return -EINVAL;
    char bytes[256];

    va_list ap;
    va_start(ap, fmt);

    int n = vsnprintf(bytes, sizeof(bytes), fmt, ap);

    va_end(ap);
    if (n < 0)
        return -EINVAL;
    if ((size_t)n >= sizeof(bytes))
        return -EMSGSIZE;
    return mica_debug_write(bytes, (size_t)n);
}
