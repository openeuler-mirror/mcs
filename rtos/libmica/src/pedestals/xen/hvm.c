/* SPDX-License-Identifier: Apache-2.0
 * Copyright (c) 2021 EPAM Systems
 * Modified 2026 from Zephyr v4.2.0 drivers/xen/hvm.c: private types,
 * explicit output/error handling, no kernel dependency. See SOURCES.md.
 */
#include "support.h"
int xen_hvm_get(uint32_t index, uint64_t *value)
{
    struct xen_hvm_param param = {
        .domid = XEN_DOMID_SELF,
        .index = index
    };

    int rc = (int)HYPERVISOR_hvm_op(HVM_GET_PARAM, &param);

    if (!rc)
        *value = param.value;
    return rc;
}
