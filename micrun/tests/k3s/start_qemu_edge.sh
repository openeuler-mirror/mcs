#!/bin/bash
set -euo pipefail

# Start the K3s edge guest in the background and run the serial-console
# bootstrap (SSH credentials + tap IP) the same way the QEMU suites do.
# The old version launched QEMU in the foreground and skipped the bootstrap
# entirely, so a pristine image (PermitRootLogin prohibit-password) had no
# usable SSH access afterwards.
#
# Console output is mirrored to the log file after the bootstrap finishes
# (the QEMU serial socket serves a single client): `tail -f` it to watch.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../common/assert.sh"
source "${SCRIPT_DIR}/../common/qemu.sh"

QEMU_LOCAL_SUDO_PASSWORD="${QEMU_LOCAL_SUDO_PASSWORD:-${K3S_LOCAL_SUDO_PASSWORD:-}}"

if [ -n "${QEMU_LOCAL_SUDO_PASSWORD:-}" ]; then
    printf 'note: QEMU_LOCAL_SUDO_PASSWORD 已设置，请确认它是当前宿主机可用的 sudo 密码，而不是示例值。\n\n'
fi

LOG_FILE="${QEMU_EDGE_CONSOLE_LOG:-/tmp/micrun-tests/qemu-edge-console.log}"

qemu_resolve_assets
qemu_start_background "$LOG_FILE"

printf 'qemu network mode: %s\n' "$QEMU_RESOLVED_NET_MODE"
# The console bootstrap may have replaced the default password with the
# strong bootstrap secret (stored in guest.pass); pick that up so the
# printed hint matches what actually works on a fresh guest.
qemu_load_guest_password
if qemu_net_mode_has_user; then
    printf 'usernet ssh entry: ssh -p %s root@127.0.0.1 (password: %s)\n' \
        "$QEMU_SSH_FWD_PORT" "${QEMU_GUEST_PASSWORD:-micrun}"
fi
if [ "${QEMU_RESOLVED_NET_MODE#*tap}" != "$QEMU_RESOLVED_NET_MODE" ]; then
    printf 'tap ssh entry:    ssh root@%s (password: %s)\n' \
        "${QEMU_GUEST_TAP_IP%%/*}" "${QEMU_GUEST_PASSWORD:-micrun}"
fi
printf 'console log (read-only mirror): %s\n' "$LOG_FILE"
printf 'stop with: tests/k3s/cleanup.sh or sudo pkill -f "[q]emu-system-aarch64"\n'
