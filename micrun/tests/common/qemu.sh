#!/bin/bash

# Never trigger GUI askpass prompts (SSH_ASKPASS / SUDO_ASKPASS): tests must
# fail fast instead of waiting for a desktop password dialog. Every entry
# point hardens itself so the guarantee does not depend on the caller's
# environment (a desktop session exports SSH_ASKPASS_REQUIRE=prefer).
unset SSH_ASKPASS SUDO_ASKPASS
export SSH_ASKPASS_REQUIRE=never

if [ -n "${MICRUN_TEST_QEMU_SH_LOADED:-}" ]; then
    return 0
fi
MICRUN_TEST_QEMU_SH_LOADED=1

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${COMMON_DIR}/assert.sh"
source "${COMMON_DIR}/remote.sh"

QEMU_TAP_IF="${QEMU_TAP_IF:-tap0}"
QEMU_SSH_FWD_PORT="${QEMU_SSH_FWD_PORT:-10022}"
QEMU_CONSOLE_SOCK="${QEMU_CONSOLE_SOCK:-/tmp/micrun-tests/qemu-console.sock}"
# -rtc pins the guest clock to the host's current UTC time: the image's
# root password carries a build-time last-change stamp, and a guest clock
# below that stamp (the default 1970 RTC) makes pam_unix reject every
# password (change date in the future) — console login on a pristine image
# is impossible without this. Using the live host time instead of a pinned
# date keeps the stamp always in the past, including for freshly rebuilt
# images. Pure QEMU cmdline, no artifact change.
QEMU_MACHINE_MEM_MB="${QEMU_MACHINE_MEM_MB:-4096}"
QEMU_GUEST_MEM_MB="${QEMU_GUEST_MEM_MB:-3072}"
QEMU_SMP="${QEMU_SMP:-4}"
QEMU_CPU="${QEMU_CPU:-cortex-a53}"
QEMU_USE_USERNET="${QEMU_USE_USERNET:-true}"
QEMU_NET_MODE="${QEMU_NET_MODE:-}"
QEMU_STOP_OLD="${QEMU_STOP_OLD:-true}"
QEMU_ROOTFS_PATTERN="${QEMU_ROOTFS_PATTERN:-openeuler-image-qemu-aarch64-*.rootfs.cpio.gz}"
# Helpers only call sshpass when this is non-empty. Bare ssh has no TTY and
# fails on empty-root guests; those guests accept any non-empty secret.
QEMU_GUEST_PASSWORD="${QEMU_GUEST_PASSWORD:-${TEST_REMOTE_PASSWORD:-micrun}}"
QEMU_SMOKE_SKIP_XEN_PROBE="${QEMU_SMOKE_SKIP_XEN_PROBE:-false}"
# Persistent store for a guest password set via the expect-based first-boot
# flow. Lets multiple test scripts (separate processes) share the password.
QEMU_GUEST_PASS_FILE="${QEMU_GUEST_PASS_FILE:-/tmp/micrun-tests/guest.pass}"

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

# ---------------------------------------------------------------------------
# Guest SSH access for QEMU tests.
#
# The standard openEuler-image ships with PermitRootLogin prohibit-password
# (the OpenSSH default) and a preset root password whose build-time
# last-change stamp is in the guest's future unless QEMU's -rtc pins the
# clock to 2026 (with the default 1970 RTC pam_unix rejects every
# password).  The build artifact is NEVER modified (no unpacking,
# repacking, or cpio concatenation — user hard constraint, 2026-08-17).
# Instead, qemu_console_bootstrap performs guest runtime preparation over
# the serial console socket: it logs in with the preset password, sets a
# fresh root password, enables PermitRootLogin, and restarts sshd.  This
# is guest runtime preparation in the sense of AGENTS.md.
# ---------------------------------------------------------------------------


# ---------------------------------------------------------------------------
# Fallback: expect-based password setup for passwd-expire images.
# Only used when the serial bootstrap did not run and normal sshpass auth
# fails after repeated retries.
# ---------------------------------------------------------------------------

qemu_load_guest_password() {
    [ -n "${QEMU_GUEST_PASSWORD:-}" ] && [ "${QEMU_GUEST_PASSWORD}" != "micrun" ] && return 0
    if [ -f "${QEMU_GUEST_PASS_FILE:-}" ]; then
        local stored
        stored="$(cat "$QEMU_GUEST_PASS_FILE" 2>/dev/null)" || return 0
        if [ -n "$stored" ]; then
            QEMU_GUEST_PASSWORD="$stored"
        fi
    fi
}

qemu_save_guest_password() {
    local pass="$1"
    mkdir -p "$(dirname "$QEMU_GUEST_PASS_FILE")" 2>/dev/null || true
    (umask 077; printf '%s' "$pass" > "$QEMU_GUEST_PASS_FILE") 2>/dev/null || true
}

qemu_setup_guest_password() {
    command -v expect >/dev/null 2>&1 || return 1

    local new_pass old_pass ssh_target ssh_port_args exp_file
    new_pass="$(python3 -c 'import secrets,string; a=string.ascii_letters+string.digits; print("".join(secrets.choice(a) for _ in range(16)))' 2>/dev/null)"
    [ -n "$new_pass" ] || return 1
    old_pass="${QEMU_GUEST_PASSWORD:-}"

    if [ -n "${QEMU_RESOLVED_NET_MODE:-}" ] && ! qemu_net_mode_has_user; then
        ssh_target="${TEST_REMOTE_HOST:-root@192.168.7.2}"
        ssh_port_args=""
    else
        ssh_target="root@127.0.0.1"
        ssh_port_args="-p $QEMU_SSH_FWD_PORT"
    fi

    exp_file="${COMMON_DIR}/qemu_setup_password.exp"
    [ -f "$exp_file" ] || return 1

    log_info "attempting expect-based guest password setup (passwd-expire flow)"

    if NEW_PASS="$new_pass" expect -f "$exp_file" $ssh_port_args "$ssh_target" >/dev/null 2>&1; then
        QEMU_GUEST_PASSWORD="$new_pass"
        if qemu_local_ssh "echo ok" >/dev/null 2>&1; then
            qemu_save_guest_password "$new_pass"
            log_info "guest root password set via expect"
            return 0
        fi
        QEMU_GUEST_PASSWORD="$old_pass"
    fi

    log_warn "expect-based password setup did not succeed; continuing with existing password"
    return 1
}

qemu_local_ssh() {
    local password="${QEMU_GUEST_PASSWORD}"

    # Never trigger SSH_ASKPASS — fail cleanly instead of popping a GUI
    # password dialog when auth fails. Export so ssh children always see
    # these, even when the caller's environment never exported them.
    export SSH_ASKPASS=
    export SSH_ASKPASS_REQUIRE=never

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
        # LTS-Next accepts keyboard-interactive and then fails pam_setcred;
        # keep non-interactive test SSH on the known-good password method.
        sshpass -p "$password" ssh \
            -p "$QEMU_SSH_FWD_PORT" \
            -o StrictHostKeyChecking=no \
            -o UserKnownHostsFile=/dev/null \
            -o PreferredAuthentications=password \
            -o PubkeyAuthentication=no \
            -o KbdInteractiveAuthentication=no \
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

    # Reuse a password saved by a previous test process if available.
    qemu_load_guest_password

    local setup_attempted=0
    # Give the freshly restarted sshd a moment before the first attempt:
    # a connection racing the restart counts as a failed auth against
    # pam_faillock, and deny=3 locks root for 300s.
    sleep 3
    for i in $(seq 1 "$retries"); do
        if qemu_local_ssh "echo ok" >/dev/null 2>&1; then
            return 0
        fi
        # After the SSH daemon has had time to accept connections but auth
        # keeps failing, the guest likely has passwd-expire: the first SSH
        # forces a keyboard-interactive password change that sshpass cannot
        # drive.  Attempt the expect-based setup once, then keep retrying
        # with the new password.
        if [ "$setup_attempted" -eq 0 ] && [ "$i" -ge 8 ]; then
            setup_attempted=1
            qemu_setup_guest_password || true
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
        -chardev "socket,id=con0,path=${QEMU_CONSOLE_SOCK},server=on,wait=off"
        -serial chardev:con0
        -rtc base="$(date -u +%Y-%m-%dT%H:%M:%S)"
        -display none
        -kernel "$QEMU_RESOLVED_XEN_KERNEL"
        -append "root=/dev/ram0 rw debugshell mem=${QEMU_GUEST_MEM_MB}M console=ttyAMA0,115200"
        -dtb "$QEMU_RESOLVED_DTB"
    )
}


# qemu_console_bootstrap performs guest runtime preparation over the serial
# console: the image ships PermitRootLogin prohibit-password (the OpenSSH
# default), which blocks root SSH password login. Per AGENTS.md the build
# artifact is never modified (no repack, no concatenation); instead we log
# in on the console with the image preset password (usable thanks to the
# -rtc clock pin), set a fresh root password, allow PermitRootLogin, and
# restart sshd. Everything here is guest runtime preparation and must be
# labeled as such.
#
# The Python driver is the ONLY client of the console socket while it runs:
# QEMU's socket chardev serves a single client, so a concurrently attached
# log tee steals the active slot and starves the login interaction (that
# race made the earlier socat/expect beat sequences flaky). The driver
# waits for real prompts instead of sleeping fixed beats, and mirrors
# everything it reads into the log file.
qemu_console_bootstrap() {
    local log_file="$1"

    # NOTE: the plaintext chpasswd path runs through the guest's
    # pam_pwquality, so a custom QEMU_GUEST_PASSWORD must satisfy that
    # policy (the default short password is replaced with a strong one
    # below for exactly that reason).
    # A fresh guest gets its root secret set here. A stored guest.pass
    # (from an earlier boot of the same campaign) wins so multi-suite runs
    # keep one secret. The default short test password is replaced by a
    # strong one for the bootstrap itself: the plaintext chpasswd path
    # (a single short serial line, far more reliable than a ~130-byte
    # hash line that the emulated serial occasionally corrupts) goes
    # through pam_pwquality, which rejects short secrets.
    qemu_load_guest_password
    local password="${QEMU_GUEST_PASSWORD:-micrun}"
    if [ "$password" = "micrun" ]; then
        password="${QEMU_BOOTSTRAP_PASSWORD:-Micrun-2026!guest}"
    fi

    sudo -n rm -f "$log_file"

    # Tap mode: also hand the guest's tap-side NIC its static test IP (the
    # image's own networkd rule matches eth*, which predictable naming never
    # produces). Empty QEMU_GUEST_TAP_IP disables (note the ${var-default}
    # form: an explicitly empty value must not fall back to the default).
    local tap_ip=""
    case "${QEMU_RESOLVED_NET_MODE:-}" in
        tap|both) tap_ip="${QEMU_GUEST_TAP_IP-192.168.7.2/24}" ;;
    esac

    if sudo -n env \
        CONSOLE_SOCK="${QEMU_CONSOLE_SOCK}" \
        LOG_FILE="${log_file}" \
        INITIAL_ROOT_PASSWORD="${QEMU_INITIAL_ROOT_PASSWORD:-openEuler@2021}" \
        NEW_PASS="${password}" \
        GUEST_TAP_IP="${tap_ip}" \
        BOOTSTRAP_TIMEOUT="${QEMU_CONSOLE_BOOTSTRAP_TIMEOUT:-420}" \
        python3 "${COMMON_DIR}/qemu_console_bootstrap.py"; then
        log_info "console runtime preparation done (root password + PermitRootLogin)"
        qemu_save_guest_password "$password"
        return 0
    fi
    log_warn "console bootstrap failed; SSH may be unavailable (see $log_file)"
    return 1
}





qemu_start_background() {
    local log_file="${1:-/tmp/qemu-startup.log}"
    local cmd
    local arg

    # Kill leftovers from a previous suite first. Its exit cleanup kills
    # QEMU asynchronously, and the tun device stays busy until the process
    # is fully gone, which makes this launch fail with "Device or resource
    # busy" on the tap interface.
    qemu_stop_existing
    local wait_i
    for wait_i in 1 2 3 4 5; do
        pgrep -f "${QEMU_RESOLVED_BIN}.*${QEMU_CONSOLE_SOCK}" >/dev/null 2>&1 || break
        sleep 1
    done

    # Drop stale console clients and the stale socket before QEMU binds it.
    sudo -n pkill -f "so[c]at.*${QEMU_CONSOLE_SOCK}" 2>/dev/null || true
    sudo -n rm -f "$QEMU_CONSOLE_SOCK"

    qemu_start_command || return 1

    cmd="nohup env"
    if [ -n "${QEMU_RESOLVED_LD_LIBRARY_PATH:-}" ]; then
        cmd+=" $(printf '%q' "LD_LIBRARY_PATH=${QEMU_RESOLVED_LD_LIBRARY_PATH}")"
    fi
    for arg in "${QEMU_CMD[@]}"; do
        cmd+=" $(printf '%q' "$arg")"
    done
    cmd+=" >$(printf '%q' "$log_file") 2>&1 &"

    qemu_sudo_shell "$cmd" || return 1

    # Fail fast when QEMU dies on startup (e.g. tap still busy from a
    # just-killed previous instance): without this check the console
    # bootstrap would burn its whole timeout waiting for a socket that
    # never comes. The QEMU stderr is still in the log file at this
    # point (the bootstrap truncates it later), so surface it here.
    sleep 3
    if ! pgrep -f "${QEMU_RESOLVED_BIN}.*${QEMU_CONSOLE_SOCK}" >/dev/null 2>&1; then
        log_error "qemu exited immediately after launch; stderr follows"
        sudo -n tail -n 20 "$log_file" >&2 2>/dev/null || tail -n 20 "$log_file" >&2 || true
        return 1
    fi

    # Guest runtime preparation over the serial console (SSH credentials).
    # The rootfs is the pristine build artifact — see qemu_console_bootstrap.
    # The read-only console tee attaches only afterwards: QEMU's socket
    # chardev serves a single client, so the bootstrap driver must hold it
    # exclusively while it runs.
    local rc=0
    qemu_console_bootstrap "$log_file" || rc=$?
    sudo -n sh -c "nohup socat -u UNIX-CONNECT:'${QEMU_CONSOLE_SOCK}' OPEN:'${log_file}',creat,append >/dev/null 2>&1 &"
    return "$rc"
}


# qemu_warmup_runtime runs one throwaway container through the full
# create->RUNNING->kill->delete path right after a cold guest boot. The
# first containerd/micad/Xen round-trip on a fresh boot occasionally fails
# (service warm-up window); suites historically hit this as a flaky first
# case. Warming the path once up front costs ~10s and makes every real case
# face a hot system. Best-effort: a warmup failure is warned, not fatal —
# the suites' own assertions stay authoritative.
qemu_warmup_runtime() {
    local image="${TEST_IMAGE:-}"
    if [ -z "$image" ]; then
        return 0
    fi
    local out ec st w
    out=$(qemu_local_ssh "ctr run --runtime io.containerd.mica.v2 --detach --annotation org.openeuler.micrun.container.auto_close=false '$image' warmup-probe 2>&1"; echo "EC=$?")
    ec=$(printf '%s\n' "$out" | sed -n 's/^EC=//p' | tail -1)
    if [ "$ec" != "0" ]; then
        log_warn "warmup run failed (ec=$ec): $(printf '%s' "$out" | grep -v EC= | tail -2 | tr '\n' ' ')"
        return 0
    fi
    w=0
    while [ "$w" -lt 30 ]; do
        st=$(qemu_local_ssh "ctr tasks ls 2>/dev/null | awk '\$1==\"warmup-probe\"{print \$3}'" 2>/dev/null | tail -1)
        [ "$st" = "RUNNING" ] && break
        sleep 1
        w=$((w + 1))
    done
    qemu_local_ssh "ctr task kill -s 9 warmup-probe >/dev/null 2>&1; sleep 1; ctr task delete warmup-probe >/dev/null 2>&1; ctr container delete warmup-probe >/dev/null 2>&1; true" >/dev/null 2>&1 || true
    log_info "runtime warmup complete (hot path ready)"
}
