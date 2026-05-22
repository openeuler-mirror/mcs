#!/bin/bash

if [ -n "${MICRUN_TEST_QEMU_SH_LOADED:-}" ]; then
    return 0
fi
MICRUN_TEST_QEMU_SH_LOADED=1

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${COMMON_DIR}/assert.sh"
source "${COMMON_DIR}/remote.sh"

QEMU_TAP_IF="${QEMU_TAP_IF:-tap0}"
QEMU_SSH_FWD_PORT="${QEMU_SSH_FWD_PORT:-10022}"
QEMU_MACHINE_MEM_MB="${QEMU_MACHINE_MEM_MB:-4096}"
QEMU_GUEST_MEM_MB="${QEMU_GUEST_MEM_MB:-3072}"
QEMU_SMP="${QEMU_SMP:-4}"
QEMU_CPU="${QEMU_CPU:-cortex-a53}"
QEMU_USE_USERNET="${QEMU_USE_USERNET:-true}"
QEMU_NET_MODE="${QEMU_NET_MODE:-}"
QEMU_STOP_OLD="${QEMU_STOP_OLD:-true}"
QEMU_ROOTFS_PATTERN="${QEMU_ROOTFS_PATTERN:-openeuler-image-qemu-aarch64-*.rootfs.cpio.gz}"
QEMU_GUEST_PASSWORD="${QEMU_GUEST_PASSWORD:-${TEST_REMOTE_PASSWORD:-}}"
QEMU_SMOKE_SKIP_XEN_PROBE="${QEMU_SMOKE_SKIP_XEN_PROBE:-false}"

qemu_local_sudo_password_note() {
    cat >&2 <<'EOF'
宿主机需要 sudo 权限来创建/清理 tap 设备或启动/停止 QEMU。
如果当前主机不是免密 sudo，请设置 QEMU_LOCAL_SUDO_PASSWORD 或 K3S_LOCAL_SUDO_PASSWORD，
并填写当前宿主机可用的 sudo 密码，不要填写示例值。
EOF
}

qemu_default_output_dir() {
    local candidates=(
        "${QEMU_OUTPUT_DIR:-}"
        "$PWD"
        "$PWD/output/test"
        "$PWD/../output/test"
    )
    local candidate

    for candidate in "${candidates[@]}"; do
        if [ -z "$candidate" ] || [ ! -d "$candidate" ]; then
            continue
        fi
        if [ -n "${QEMU_OUTPUT_DIR:-}" ] || {
            [ -f "$candidate/Image" ] &&
            [ -f "$candidate/xen-qemu-aarch64" ] &&
            [ -f "$candidate/openeuler-image-qemu-aarch64.qemuboot.dtb" ]
        }; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done

    return 1
}

qemu_resolve_output_dir() {
    if [ -n "${QEMU_OUTPUT_DIR:-}" ] && [ ! -d "$QEMU_OUTPUT_DIR" ]; then
        log_error "QEMU_OUTPUT_DIR does not exist: $QEMU_OUTPUT_DIR"
        return 1
    fi

    QEMU_RESOLVED_OUTPUT_DIR="$(qemu_default_output_dir)" || {
        log_error "unable to locate qemu output directory"
        log_error "set QEMU_OUTPUT_DIR to the directory containing Image, xen-qemu-aarch64, rootfs, and dtb"
        return 1
    }
}

qemu_find_rootfs_image() {
    if [ -n "${QEMU_ROOTFS_IMAGE:-}" ]; then
        [ -f "$QEMU_ROOTFS_IMAGE" ] || {
            log_error "qemu rootfs path is not a file: $QEMU_ROOTFS_IMAGE"
            return 1
        }
        printf '%s\n' "$QEMU_ROOTFS_IMAGE"
        return 0
    fi

    find "$QEMU_RESOLVED_OUTPUT_DIR" -maxdepth 1 -type f \
        \( -name "$QEMU_ROOTFS_PATTERN" \
        -o -name "rootfs.cpio.gz" \
        -o -name "openeuler-image-qemu-aarch64.cpio.gz" \
        -o -name "openeuler-image-qemu-aarch64*.cpio.gz" \) \
        ! -name '*.bak' \
        | sort | tail -n 1
}

qemu_resolve_bin() {
    local candidates=(
        "${QEMU_BIN:-}"
        "$(command -v qemu-system-aarch64 2>/dev/null || true)"
        "/usr/bin/qemu-system-aarch64"
    )
    local candidate

    for candidate in "${candidates[@]}"; do
        if [ -n "$candidate" ] && [ -x "$candidate" ]; then
            QEMU_RESOLVED_BIN="$candidate"
            return 0
        fi
    done

    log_error "unable to locate qemu-system-aarch64"
    return 1
}

qemu_resolve_library_path() {
    local bindir prefix entries=() path

    if [ -n "${QEMU_LD_LIBRARY_PATH:-}" ]; then
        QEMU_RESOLVED_LD_LIBRARY_PATH="$QEMU_LD_LIBRARY_PATH"
        return 0
    fi

    bindir="$(dirname "$QEMU_RESOLVED_BIN")"
    prefix="$(cd "$bindir/.." && pwd)"

    for path in \
        "$prefix/lib/x86_64-linux-gnu" \
        "$prefix/lib64" \
        "$prefix/lib"; do
        if [ -d "$path" ]; then
            entries+=("$path")
        fi
    done

    QEMU_RESOLVED_LD_LIBRARY_PATH="$(IFS=:; echo "${entries[*]}")"
}

qemu_resolve_assets() {
    qemu_resolve_output_dir
    qemu_resolve_bin
    qemu_resolve_library_path

    QEMU_RESOLVED_ROOTFS_IMAGE="$(qemu_find_rootfs_image)" || return 1
    QEMU_RESOLVED_KERNEL_IMAGE="${QEMU_RESOLVED_OUTPUT_DIR}/Image"
    QEMU_RESOLVED_XEN_KERNEL="${QEMU_RESOLVED_OUTPUT_DIR}/xen-qemu-aarch64"
    QEMU_RESOLVED_DTB="${QEMU_RESOLVED_OUTPUT_DIR}/openeuler-image-qemu-aarch64.qemuboot.dtb"

    local missing=0
    local file
    for file in \
        "$QEMU_RESOLVED_ROOTFS_IMAGE" \
        "$QEMU_RESOLVED_KERNEL_IMAGE" \
        "$QEMU_RESOLVED_XEN_KERNEL" \
        "$QEMU_RESOLVED_DTB"; do
        if [ ! -f "$file" ]; then
            log_error "missing qemu asset: $file"
            missing=1
        fi
    done

    [ "$missing" -eq 0 ]
}

qemu_sudo_password() {
    printf '%s' "${QEMU_LOCAL_SUDO_PASSWORD:-${K3S_LOCAL_SUDO_PASSWORD:-}}"
}

qemu_sudo_env_run() {
    local password
    local env_args=()

    password="$(qemu_sudo_password)"
    if [ -n "${QEMU_RESOLVED_LD_LIBRARY_PATH:-}" ]; then
        env_args+=("LD_LIBRARY_PATH=${QEMU_RESOLVED_LD_LIBRARY_PATH}")
    fi

    if [ -n "$password" ]; then
        if [ -t 0 ]; then
            if ! printf '%s\n' "$password" | sudo -S -v; then
                qemu_local_sudo_password_note
                return 1
            fi
            if ! sudo env "${env_args[@]}" "$@"; then
                qemu_local_sudo_password_note
                return 1
            fi
            return 0
        fi

        if ! printf '%s\n' "$password" | sudo -S env "${env_args[@]}" "$@"; then
            qemu_local_sudo_password_note
            return 1
        fi
        return 0
    fi

    if ! sudo env "${env_args[@]}" "$@"; then
        qemu_local_sudo_password_note
        return 1
    fi
}

qemu_sudo_shell() {
    local password

    password="$(qemu_sudo_password)"
    if [ -n "$password" ]; then
        if ! printf '%s\n' "$password" | sudo -S bash -lc "$1"; then
            qemu_local_sudo_password_note
            return 1
        fi
        return 0
    fi

    if ! sudo bash -lc "$1"; then
        qemu_local_sudo_password_note
        return 1
    fi
}

qemu_supports_usernet() {
    env LD_LIBRARY_PATH="${QEMU_RESOLVED_LD_LIBRARY_PATH:-}" \
        "$QEMU_RESOLVED_BIN" -machine none -netdev help 2>&1 | grep -q '^user$'
}

qemu_normalize_net_mode() {
    local mode="${QEMU_NET_MODE:-}"

    if [ -z "$mode" ]; then
        if [ "$QEMU_USE_USERNET" = "true" ]; then
            mode="both"
        else
            mode="tap"
        fi
    fi

    case "$mode" in
        usernet)
            mode="user"
            ;;
        tap|user|both)
            ;;
        *)
            log_error "invalid QEMU_NET_MODE: $mode (expected tap, user, or both)"
            return 1
            ;;
    esac

    QEMU_RESOLVED_NET_MODE="$mode"
}

qemu_net_mode_has_user() {
    case "${QEMU_RESOLVED_NET_MODE:-}" in
        user|both)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

qemu_forget_forwarded_hostkey() {
    forget_known_host "[127.0.0.1]:${QEMU_SSH_FWD_PORT}"
}

qemu_stop_existing() {
    [ "$QEMU_STOP_OLD" = "true" ] || return 0

    qemu_sudo_shell "
        ps -eo pid=,args= | awk \
            -v bin='${QEMU_RESOLVED_BIN}' \
            -v xen='${QEMU_RESOLVED_XEN_KERNEL}' \
            -v port='${QEMU_SSH_FWD_PORT}' '
            {
                pid = \$1
                \$1 = \"\"
                sub(/^ +/, \"\", \$0)
                args = \$0
                if (index(args, bin) != 1) {
                    next
                }
                if (index(args, xen) > 0 || index(args, \"hostfwd=tcp::\" port \"-:22\") > 0) {
                    print pid
                }
            }
        ' | xargs -r kill -9 2>/dev/null || true
    "
}

qemu_local_ssh() {
    local password="${QEMU_GUEST_PASSWORD}"

    if [ -n "${QEMU_RESOLVED_NET_MODE:-}" ] && ! qemu_net_mode_has_user; then
        local host="${TEST_REMOTE_HOST:-root@192.168.7.2}"

        if [ -n "$password" ]; then
            # shellcheck disable=SC2046
            sshpass -p "$password" ssh $(remote_ssh_opts) "$host" "$@"
        else
            # shellcheck disable=SC2046
            ssh $(remote_ssh_opts) "$host" "$@"
        fi
        return
    fi

    if [ -n "$password" ]; then
        sshpass -p "$password" ssh \
            -p "$QEMU_SSH_FWD_PORT" \
            -o StrictHostKeyChecking=no \
            -o UserKnownHostsFile=/dev/null \
            -o ConnectTimeout="${TEST_SSH_CONNECT_TIMEOUT:-10}" \
            root@127.0.0.1 "$@"
    else
        ssh \
            -p "$QEMU_SSH_FWD_PORT" \
            -o StrictHostKeyChecking=no \
            -o UserKnownHostsFile=/dev/null \
            -o ConnectTimeout="${TEST_SSH_CONNECT_TIMEOUT:-10}" \
            root@127.0.0.1 "$@"
    fi
}

qemu_wait_for_ssh() {
    local retries="${1:-60}"
    local i

    if qemu_net_mode_has_user; then
        qemu_forget_forwarded_hostkey
    fi

    for i in $(seq 1 "$retries"); do
        if qemu_local_ssh "echo ok" >/dev/null 2>&1; then
            return 0
        fi
        sleep 2
    done

    return 1
}

qemu_start_command() {
    local net_index=0

    qemu_normalize_net_mode || return 1

    QEMU_CMD=(
        "$QEMU_RESOLVED_BIN"
    )

    case "$QEMU_RESOLVED_NET_MODE" in
        tap|both)
            QEMU_CMD+=(
                -device "virtio-net-pci,netdev=net${net_index}"
                -netdev "tap,id=net${net_index},ifname=${QEMU_TAP_IF},script=/etc/qemu-ifup"
            )
            net_index=$((net_index + 1))
            ;;
    esac

    if qemu_net_mode_has_user; then
        if ! qemu_supports_usernet; then
            log_error "QEMU binary does not support user networking: $QEMU_RESOLVED_BIN"
            return 1
        fi
        QEMU_CMD+=(
            -device "virtio-net-pci,netdev=net${net_index}"
            -netdev "user,id=net${net_index},hostfwd=tcp::${QEMU_SSH_FWD_PORT}-:22"
        )
    fi

    QEMU_CMD+=(
        -initrd "$QEMU_RESOLVED_ROOTFS_IMAGE"
        -device "loader,file=${QEMU_RESOLVED_KERNEL_IMAGE},addr=0x45000000"
        -machine virt,gic-version=3
        -machine virtualization=true
        -cpu "$QEMU_CPU"
        -smp "$QEMU_SMP"
        -m "$QEMU_MACHINE_MEM_MB"
        -serial mon:stdio
        -nographic
        -kernel "$QEMU_RESOLVED_XEN_KERNEL"
        -append "root=/dev/ram0 rw debugshell mem=${QEMU_GUEST_MEM_MB}M console=ttyAMA0,115200"
        -dtb "$QEMU_RESOLVED_DTB"
    )
}

qemu_start_background() {
    local log_file="${1:-/tmp/qemu-startup.log}"
    local cmd
    local arg

    qemu_start_command || return 1

    cmd="nohup env"
    if [ -n "${QEMU_RESOLVED_LD_LIBRARY_PATH:-}" ]; then
        cmd+=" $(printf '%q' "LD_LIBRARY_PATH=${QEMU_RESOLVED_LD_LIBRARY_PATH}")"
    fi
    for arg in "${QEMU_CMD[@]}"; do
        cmd+=" $(printf '%q' "$arg")"
    done
    cmd+=" >$(printf '%q' "$log_file") 2>&1 &"

    qemu_sudo_shell "$cmd"
}
