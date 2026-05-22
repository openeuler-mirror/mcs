#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../common/assert.sh"
source "${SCRIPT_DIR}/../common/qemu.sh"

QEMU_LOCAL_SUDO_PASSWORD="${QEMU_LOCAL_SUDO_PASSWORD:-${K3S_LOCAL_SUDO_PASSWORD:-}}"

if [ -n "${QEMU_LOCAL_SUDO_PASSWORD:-}" ]; then
    printf 'note: QEMU_LOCAL_SUDO_PASSWORD 已设置，请确认它是当前宿主机可用的 sudo 密码，而不是示例值。\n\n'
fi

qemu_resolve_assets
qemu_stop_existing
qemu_start_command

printf 'starting qemu with:\n'
printf '  sudo env'
if [ -n "${QEMU_RESOLVED_LD_LIBRARY_PATH:-}" ]; then
    printf ' %q' "LD_LIBRARY_PATH=${QEMU_RESOLVED_LD_LIBRARY_PATH}"
fi
printf ' %q' "${QEMU_CMD[@]}"
printf '\n\n'
printf 'qemu network mode: %s\n\n' "$QEMU_RESOLVED_NET_MODE"
if qemu_net_mode_has_user; then
    printf 'ssh entry after boot:\n'
    printf '  ssh -p %s root@127.0.0.1\n\n' "$QEMU_SSH_FWD_PORT"
fi

qemu_sudo_env_run "${QEMU_CMD[@]}"
