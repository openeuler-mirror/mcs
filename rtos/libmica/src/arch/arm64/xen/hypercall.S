/* SPDX-License-Identifier: Apache-2.0 */
/* Copyright (c) 2021-2023 EPAM Systems
 * Modified 2026: Zephyr v4.2.0 arch/arm64/core/xen/hypercall.S adapted
 * to standalone ELF assembly. Added xen_version from Xen RELEASE-4.18.2
 * MIT public xen.h; numbering/tag attribution in src/xen/SOURCES.md.
 */
.text
.macro XEN_CALL name, number
.p2align 2
.global HYPERVISOR_\name
.type HYPERVISOR_\name, %function
HYPERVISOR_\name:
    mov x16, #\number
    hvc #0xea1
    ret
.size HYPERVISOR_\name, .-HYPERVISOR_\name
.endm
XEN_CALL console_io, 18
XEN_CALL grant_table_op, 20
XEN_CALL sched_op, 29
XEN_CALL event_channel_op, 32
XEN_CALL hvm_op, 34
XEN_CALL memory_op, 12
XEN_CALL xen_version, 17
.section .note.GNU-stack,"",%progbits
