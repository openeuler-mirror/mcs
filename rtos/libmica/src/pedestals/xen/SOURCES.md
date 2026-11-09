# Xen builtin support: sources and modifications

Paths below are relative to `src/pedestals/xen/` unless explicitly rooted at
`src/`. The frontend is `xen.c`; CPU assembly is `src/arch/arm64/xen/hypercall.S`
relative to the libmica root. Upstream Z/X paths retain their original spelling.
The provider contract installs as `include/mica/platform/xen/provider.h`.
OS primitives use the shared `include/mica/platform/{irq,memory,...}.h`
functional headers; there is no separate Xen OS contract header.

Fixed reference tags: Zephyr **v4.2.0**, Xen **RELEASE-4.18.2**.
URLs below are relative to these exact, official prefixes:

- Z: https://github.com/zephyrproject-rtos/zephyr/blob/v4.2.0/
- X: https://github.com/xen-project/xen/blob/RELEASE-4.18.2/

These are the official upstream projects, not personal mirrors. Sources are
grouped by provenance: (1) Zephyr support, including its MIT grant-table source;
(2) Xen public ABI (MIT); (3) Xen hvmloader xenbus (BSD-2-Clause); and (4) original
MCS debug producer (MulanPSL-2.0). Per-file licensing below remains authoritative.

These are implementation/ABI attribution tags, not a guest dependency upgrade.
The old UniProton implementation was read for behavior/wiring only; its
unattributed assembly was not copied. This is expressly not a clean-room claim.

## Per-file record (modified 2026)

- `src/arch/arm64/xen/hypercall.S`: Z `arch/arm64/core/xen/hypercall.S`,
  Apache-2.0, copyright 2021-2023 EPAM Systems. Standalone ELF macro replaces
  Zephyr toolchain/section macros; seven ARM64 entries, no domctl. Added
  xen_version number 17 from X `xen/include/public/xen.h`; hvc tag 0xea1 from
  X `xen/include/public/arch-arm.h` (MIT notices in LICENSES/THIRD_PARTY_LICENSES.txt).
  x0-x2 arguments and x0 result are unchanged; x16 is the hypercall selector.
- `memory.c`: Z `drivers/xen/memory.c`, Apache-2.0, 2023 EPAM Systems.
  Uses explicit GPA rather than VA casting. Ordinary RAM is handed back with
  decrease-reservation before replacement; special mappings are detached with
  remove-from-physmap. Populate restores RAM before allocator release.
  A01 adds checked per-page restore and special-page rollback ledger,
  retaining failed unmap/remove/restore resources; adds common finite deadline.
- `hvm.c`: Z `drivers/xen/hvm.c`, Apache-2.0, 2021 EPAM Systems.
  Reduces to checked get-param; frozen OS-neutral private types.
- `shared_info.c`: Z `arch/arm64/core/xen/enlighten.c`, Apache-2.0,
  2021 EPAM Systems. OS-reserved GPA and special-page alias replace static VA
  assumption; domain-owned ledger retains every successful/partial resource.
  A01 releases local RAM backing before replacement; failed setup reverses CPU
  alias, Xen replacement and backing removal, retaining failed cleanup state.
- `events.c`: Z `drivers/xen/events.c`, Apache-2.0, 2021 EPAM Systems and
  2022 Arm Limited (or its affiliates), all rights reserved. Retains two-level
  pending dispatch and Xen event protocol; replaces kernel/DT wiring with OS
  IRQ hook. Permanent per-port slots fence late/entered callbacks; revoke masks,
  closes, then drains references before releasing callback target. Domain IRQ
  lives for the domain, not for one binding. Targets vCPU0 legacy event ABI.
- `gnttab.c`: Z `drivers/xen/gnttab.c`, **MIT**, (C) 2006 Cambridge University,
  (C) 2021-2024 EPAM Systems. Import-only implementation replaces fixed grant
  area/PRT allocator, batch status loss and put_page cleanup with per-page GPA,
  handle ledger and reverse Xen unmap. A01 removes backing before each grant
  map and restores it after grant unmap, keeping the whole original allocation
  until every restore succeeds. Failed unmap/restore retains slot/cookie;
  retries skip already completed operations and free the original region once.
  RAM release uses decrease-reservation, not only a P2M detach, so a restored
  dynamic guest allocation does not accumulate unreachable charged pages.
  Grant exporting is not required, so no export table is initialized. The
  Apache-2.0 Z `include/zephyr/xen/gnttab.h` is NOT copied into this implementation;
  its license does not override the MIT implementation.
- `xenstore.c`: X `tools/firmware/hvmloader/xenbus.c`, BSD-2-Clause,
  2009 Tim Deegan, Citrix Systems (R&D) Ltd. Full original notice retained.
  Ring-copy client reorganized for complete-transaction locking, read/write,
  explicit OS page mapping, checked message ranges and one-second deadline.
  Incomplete/malformed stream is poisoned rather than silently reused/reset.
  No MICA backend discovery, watch handling or unbounded sched poll.
- `abi.h`: excerpts of X `xen/include/public/{xen.h,arch-arm.h,memory.h,
  grant_table.h,event_channel.h,hvm/hvm_op.h,hvm/params.h,io/xs_wire.h,io/console.h}`,
  MIT. Original per-header notices and complete MIT terms are retained in
   `LICENSES/THIRD_PARTY_LICENSES.txt`. Names reduced/renamed to private ARM64-only types;
  explicit alignment/layout assertions. No mixed Xenbus-enum/header assembly:
  this support needs only xsd wire types, not Xenbus frontend states.
- `support.h`, `provider_builtin.c`: new Apache-2.0 organization/adapter for
  this migration, no copied OS framework. One sticky domain-init owner,
  acquire/release publication, operations gated by READY; no shutdown in MICA.
- `debug_console.c`, `debug_console.h`: original MulanPSL-2.0 output-only
  producer, not copied from a Xen frontend implementation. Uses the MIT
  console-ring layout and HVM parameter numbers attributed above; obtains the
  local view through the OS memory mapping contract. Optional debug
  hooks are independent of the domain-init READY gate, do not initialize other
  domain facilities, and require one coordinated console producer per domain.

## Distribution

`LICENSES/distribution.list` is the exact package data list, relative to libmica
root, for the build/install owner. `LICENSES/THIRD_PARTY_LICENSES.txt` preserves
all original notices and the complete modification NOTICE, FULL Apache-2.0
(including Appendix), BSD-2-Clause and one shared copy of the identical MIT
terms for both grant-table and public-ABI attribution groups. Distribute this
consolidated file, not just SPDX identifiers. The list is
not a public-header manifest. OS/provider migration contracts are supplied by
ordinary header installation, separately from this license data list; private
context/service headers are not installed.
No source is relicensed under the repository's MulanPSL license.

## A01 RAM-backed allocation ownership

The OS supplies ordinary RAM-backed storage through `mica_mem_alloc`.
Only the provider performs Xen reservation/mapping protocols; the OS port supplies
allocation, VA/GPA facts and local CPU mappings. Per-page state separates a
removed backing from a live grant handle. Restore failure retains the entire
allocation and is retryable; no P2M hole or live grant reaches `mica_mem_free`.
Special-page setup uses the same discipline. Successfully initialized domain
facilities live for the domain, not a MICA instance; there is no domain teardown
API. CPU aliases retain the attributes prescribed by the OS contract. No fixed
grant-area address, MMU enablement, cache-policy change or identity assumption
is introduced here. Actual mapping attributes and guest behavior must be
validated for each OS integration; these source references alone do not prove them.

The allocator must keep its metadata outside the requested page-aligned user
extent: replacing these pages does not preserve their contents. Releasing
ordinary RAM and detaching a shared-info mapping are different operations;
the latter must not release the shared-info page as allocator storage.
This implementation does not add a balloon manager or claim new static-memory
or direct-map platform validation.
