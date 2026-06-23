#!/usr/bin/env python3
"""Run MICA smoke in QEMU through an SSH control channel.

The script boots the original rootfs cpio.gz, logs in on the serial console as
root with the image's empty password, injects a temporary SSH public key, then
executes the smoke sequence over QEMU user networking with host port forwarding.
It does not unpack or modify the rootfs image.
"""

from __future__ import annotations

import argparse
import hashlib
import os
import selectors
import shutil
import socket
import subprocess
import sys
import time
from pathlib import Path


SMOKE_COMMAND = r'''set -eu
echo "MICA_SMOKE_BEGIN"
echo "1 4 1 7" > /proc/sys/kernel/printk || true

if command -v depmod >/dev/null 2>&1; then
    depmod || true
fi

systemctl start micad.service || /usr/bin/micad || true
sleep 3

CONF=/etc/mica/qemu-zephyr-rproc.conf
NAME=qemu-zephyr
RESULT=FAIL

status_has_instance() {
    mica status | awk '{print $1}' | grep -qx "$NAME"
}

status_is_running() {
    mica status | awk -v name="$NAME" '$1 == name && $3 == "Running" {found = 1} END {exit !found}'
}

status_is_absent() {
    ! status_has_instance
}

cleanup_instance() {
    if status_has_instance; then
        mica stop "$NAME" || true
        sleep 2
        mica rm "$NAME" || true
        sleep 2
    fi
}

require_absent() {
    if ! status_is_absent; then
        echo "MICA_SMOKE_ERROR=$NAME still exists"
        mica status || true
        return 1
    fi
}

require_running() {
    if ! status_is_running; then
        echo "MICA_SMOKE_ERROR=$NAME is not Running"
        mica status || true
        return 1
    fi
}

require_stopped() {
    if ! status_has_instance || status_is_running; then
        echo "MICA_SMOKE_ERROR=$NAME did not stop cleanly"
        mica status || true
        return 1
    fi
}

create_instance() {
    mica create "$CONF"
    sleep 2
    mica status || true
    status_has_instance
}

start_instance() {
    mica start "$NAME"
    sleep 8
    mica status || true
    require_running
}

stop_instance() {
    mica stop "$NAME"
    sleep 2
    mica status || true
    require_stopped
}

remove_instance() {
    mica rm "$NAME"
    sleep 2
    mica status || true
    require_absent
}

check_tty_screen() {
    dev=$(mica status | sed -n 's/.*rpmsg-tty([^)]*\(\/dev\/ttyRPMSG[0-9]*\)).*/\1/p' | head -n 1 || true)
    if [ -z "$dev" ]; then
        dev=$(ls /dev/ttyRPMSG* 2>/dev/null | head -n 1 || true)
    fi
    if [ -z "$dev" ]; then
        echo "MICA_SMOKE_ERROR=no /dev/ttyRPMSG* device"
        return 1
    fi
    echo "MICA_SMOKE_TTY_DEVICE=$dev"

    if ! command -v screen >/dev/null 2>&1; then
        echo "MICA_SMOKE_TTY_SCREEN=SKIP screen-not-installed"
        return 0
    fi

    session="mica-smoke-tty"
    marker="mica_tty_smoke"
    capture="/tmp/mica-tty-hardcopy.txt"
    screen -S "$session" -X quit >/dev/null 2>&1 || true
    rm -f "$capture"

    if ! TERM=vt100 screen -dmS "$session" "$dev"; then
        echo "MICA_SMOKE_TTY_SCREEN=FAIL open $dev"
        return 1
    fi

    sleep 2
    screen -S "$session" -X stuff "$(printf '\r%s\r' "$marker")" || true
    sleep 3
    screen -S "$session" -X hardcopy "$capture" || true
    screen -S "$session" -X quit >/dev/null 2>&1 || true

    if [ ! -s "$capture" ]; then
        echo "MICA_SMOKE_TTY_SCREEN=FAIL no-hardcopy $dev"
        return 1
    fi

    echo "MICA_SMOKE_TTY_HARDCOPY_BEGIN"
    sed -n '1,20p' "$capture" || true
    echo "MICA_SMOKE_TTY_HARDCOPY_END"

    if grep -a -q "$marker" "$capture"; then
        echo "MICA_SMOKE_TTY_SCREEN=PASS echo $dev"
        return 0
    fi

    echo "MICA_SMOKE_TTY_SCREEN=FAIL no-echo $dev"
    return 1
}

echo "MICA_SMOKE_STEP=status-initial"
mica status || true

echo "MICA_SMOKE_STEP=cleanup-initial"
cleanup_instance
require_absent

echo "MICA_SMOKE_STEP=create-rm-pair"
create_instance
remove_instance

echo "MICA_SMOKE_STEP=create-start-stop-start-stop-rm"
create_instance
start_instance
check_tty_screen
stop_instance
start_instance
stop_instance
remove_instance

RESULT=PASS

echo "MICA_SMOKE_RESULT=$RESULT"
test "$RESULT" = PASS
'''


def repo_root_from_script() -> Path:
    return Path(__file__).resolve().parents[6]


def run(cmd: list[str], *, input_text: str | None = None, timeout: int | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        input=input_text,
        timeout=timeout,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )


def create_dtb(repo_root: Path, work_dir: Path) -> Path:
    qemu_dir = work_dir / "qemu"
    qemu_dir.mkdir(parents=True, exist_ok=True)
    subprocess.run([str(repo_root / "tools/create_dtb.sh"), "qemu-a53"], cwd=qemu_dir, check=True)
    return qemu_dir / "qemu.dtb"


def wait_for_port(host: str, port: int, timeout: int) -> bool:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=2):
                return True
        except OSError:
            time.sleep(1)
    return False


def write_console(proc: subprocess.Popen[bytes], text: str) -> None:
    if proc.stdin is None:
        raise RuntimeError("QEMU stdin is not available")
    data = memoryview(text.encode())
    fd = proc.stdin.fileno()
    offset = 0
    while offset < len(data):
        if proc.poll() is not None:
            raise RuntimeError(f"QEMU exited while writing console input: {proc.returncode}")
        try:
            written = os.write(fd, data[offset:])
        except BlockingIOError:
            time.sleep(0.1)
            continue
        if written == 0:
            raise RuntimeError("QEMU stdin closed while writing console input")
        offset += written


def wait_console_and_configure_ssh(
    proc: subprocess.Popen[bytes],
    log_file: Path,
    public_key: str,
    password: str,
    timeout: int,
) -> None:
    if proc.stdout is None:
        raise RuntimeError("QEMU stdout is not available")

    selector = selectors.DefaultSelector()
    selector.register(proc.stdout, selectors.EVENT_READ)
    deadline = time.time() + timeout
    buffer = ""
    sent_login = False
    sent_new_password = False
    sent_retyped_password = False
    sent_setup = False

    setup = (
        "mkdir -p /root/.ssh && chmod 700 /root/.ssh\n"
        f"cat > /root/.ssh/authorized_keys <<'EOF'\n{public_key}\nEOF\n"
        "chmod 600 /root/.ssh/authorized_keys\n"
        "chage -d 1 -M 99999 -I -1 -E -1 root || true\n"
        "sed -i 's/^PasswordAuthentication .*/PasswordAuthentication yes/' /etc/ssh/sshd_config || true\n"
        "systemctl restart sshd.service || /usr/sbin/sshd || true\n"
        "echo MICA_SSH_READY\n"
    )

    with log_file.open("ab") as log:
        while time.time() < deadline:
            if proc.poll() is not None:
                raise RuntimeError(f"QEMU exited before SSH setup completed: {proc.returncode}")
            events = selector.select(timeout=1)
            for key, _ in events:
                chunk = os.read(key.fileobj.fileno(), 4096)
                if not chunk:
                    continue
                sys.stdout.buffer.write(chunk)
                sys.stdout.buffer.flush()
                log.write(chunk)
                log.flush()
                text = chunk.decode(errors="replace")
                buffer = (buffer + text)[-4096:]

                if not sent_login and " login:" in buffer:
                    write_console(proc, "root\n")
                    sent_login = True
                    continue

                if sent_login and not sent_new_password and "New password:" in buffer:
                    write_console(proc, password + "\n")
                    sent_new_password = True
                    continue

                if sent_new_password and not sent_retyped_password and "BAD PASSWORD:" in buffer:
                    sent_new_password = False
                    buffer = ""
                    continue

                if sent_new_password and not sent_retyped_password and "Retype new password:" in buffer:
                    write_console(proc, password + "\n")
                    sent_retyped_password = True
                    continue

                if sent_login and not sent_setup and ("root@" in buffer or "# " in buffer):
                    write_console(proc, setup)
                    sent_setup = True
                    continue

                if sent_setup and "MICA_SSH_READY" in buffer:
                    return
    raise TimeoutError("timed out waiting for serial login and SSH setup")


def ssh_base_args(port: int, key: Path) -> list[str]:
    return [
        "ssh",
        "-i", str(key),
        "-p", str(port),
        "-o", "StrictHostKeyChecking=no",
        "-o", "UserKnownHostsFile=/dev/null",
        "-o", "ConnectTimeout=5",
        "-o", "BatchMode=yes",
        "root@127.0.0.1",
    ]


def scp_base_args(port: int, key: Path) -> list[str]:
    return [
        "scp",
        "-i", str(key),
        "-P", str(port),
        "-o", "StrictHostKeyChecking=no",
        "-o", "UserKnownHostsFile=/dev/null",
        "-o", "ConnectTimeout=5",
        "-o", "BatchMode=yes",
    ]


def wait_for_ssh_login(port: int, key: Path, timeout: int) -> None:
    deadline = time.time() + timeout
    last_output = ""
    while time.time() < deadline:
        if not wait_for_port("127.0.0.1", port, 2):
            continue
        proc = subprocess.run(
            ssh_base_args(port, key) + ["true"],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        )
        last_output = proc.stdout or ""
        if proc.returncode == 0:
            return
        time.sleep(2)
    if last_output:
        sys.stdout.write(last_output)
    raise TimeoutError("timed out waiting for SSH login")


def run_ssh(port: int, key: Path, script: str, timeout: int = 60) -> str:
    proc = subprocess.run(
        ssh_base_args(port, key) + ["sh -s"],
        input=script,
        text=True,
        timeout=timeout,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    output = proc.stdout or ""
    sys.stdout.write(output)
    if proc.returncode != 0:
        raise RuntimeError(f"remote command failed with {proc.returncode}")
    return output


def scp_to_guest(port: int, key: Path, src: Path, dest: str) -> None:
    if not src.exists():
        raise RuntimeError(f"artifact not found: {src}")
    proc = subprocess.run(
        scp_base_args(port, key) + [str(src), f"root@127.0.0.1:{dest}"],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    if proc.stdout:
        sys.stdout.write(proc.stdout)
    if proc.returncode != 0:
        raise RuntimeError(f"scp failed for {src}: {proc.returncode}")


def md5_file(path: Path) -> str:
    digest = hashlib.md5()
    with path.open("rb") as src:
        for chunk in iter(lambda: src.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def deploy_artifacts(port: int, key: Path, args: argparse.Namespace, log_file: Path) -> None:
    if not args.micad and not args.mica and not args.ko:
        return

    print("MICA_DEPLOY_BEGIN")
    run_ssh(port, key, "mkdir -p /tmp/mica-deploy\n", timeout=30)
    uploaded: list[str] = []
    expected_md5: dict[str, str] = {}
    if args.micad:
        scp_to_guest(port, key, args.micad, "/tmp/mica-deploy/micad")
        uploaded.append("micad")
        expected_md5["micad"] = md5_file(args.micad)
    if args.mica:
        scp_to_guest(port, key, args.mica, "/tmp/mica-deploy/mica")
        uploaded.append("mica")
        expected_md5["mica"] = md5_file(args.mica)
    if args.ko:
        scp_to_guest(port, key, args.ko, "/tmp/mica-deploy/mcs_km.ko")
        uploaded.append("mcs_km.ko")
        expected_md5["mcs_km.ko"] = md5_file(args.ko)

    deploy_script = f'''set -eu
echo "MICA_DEPLOY_UPLOADED={' '.join(uploaded)}"

chage -d 1 -M 99999 -I -1 -E -1 root || true

systemctl stop micad.service || true
pkill micad || true
sleep 1

if [ -f /tmp/mica-deploy/micad ]; then
    install -m 0755 /tmp/mica-deploy/micad /usr/bin/micad
    echo "MICA_DEPLOY_INSTALLED=/usr/bin/micad"
    md5sum /usr/bin/micad | awk '{{print "MICA_DEPLOY_MD5=/usr/bin/micad " $1}}'
fi

if [ -f /tmp/mica-deploy/mica ]; then
    install -m 0755 /tmp/mica-deploy/mica /usr/bin/mica
    echo "MICA_DEPLOY_INSTALLED=/usr/bin/mica"
    md5sum /usr/bin/mica | awk '{{print "MICA_DEPLOY_MD5=/usr/bin/mica " $1}}'
fi

if [ -f /tmp/mica-deploy/mcs_km.ko ]; then
    ko_target=$(find /lib/modules -path '*/extra/mcs_km.ko' | head -n 1)
    if [ -z "$ko_target" ]; then
        echo "MICA_DEPLOY_ERROR=no existing mcs_km.ko target"
        exit 1
    fi
    if lsmod | awk '{{print $1}}' | grep -qx mcs_km; then
        rmmod mcs_km
    fi
    install -m 0644 /tmp/mica-deploy/mcs_km.ko "$ko_target"
    depmod || true
    modprobe mcs_km || insmod "$ko_target"
    echo "MICA_DEPLOY_INSTALLED=$ko_target"
    md5sum "$ko_target" | awk '{{print "MICA_DEPLOY_MD5=" $2 " " $1}}'
fi

systemctl start micad.service || /usr/bin/micad &
sleep 3
systemctl status micad.service --no-pager || true
echo "MICA_DEPLOY_DONE"
'''
    output = run_ssh(port, key, deploy_script, timeout=90)
    seen: dict[str, str] = {}
    for line in output.splitlines():
        if not line.startswith("MICA_DEPLOY_MD5="):
            continue
        item = line.removeprefix("MICA_DEPLOY_MD5=")
        path_text, value = item.rsplit(" ", 1)
        seen[Path(path_text).name] = value
    for name, expected in expected_md5.items():
        actual = seen.get(name)
        if actual != expected:
            raise RuntimeError(f"MD5 mismatch for {name}: expected {expected}, got {actual}")
        print(f"MICA_DEPLOY_MD5_MATCH={name} {actual}")
    with log_file.open("ab") as log:
        log.write(output.encode())


def run_smoke_over_ssh(port: int, key: Path, log_file: Path, timeout: int) -> int:
    cmd = ssh_base_args(port, key) + ["sh -s"]
    proc = subprocess.run(
        cmd,
        input=SMOKE_COMMAND,
        text=True,
        timeout=timeout,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    output = proc.stdout or ""
    sys.stdout.write(output)
    with log_file.open("ab") as log:
        log.write(output.encode())
    if "MICA_SMOKE_RESULT=PASS" in output:
        return 0
    return 1


def run_qemu(package_dir: Path, dtb: Path, port: int, log_file: Path) -> subprocess.Popen[bytes]:
    rootfs_candidates = list(package_dir.glob("openeuler-image-qemu-aarch64-*.rootfs.cpio.gz"))
    if not rootfs_candidates:
        raise RuntimeError(f"rootfs cpio.gz not found in {package_dir}")
    cmd = [
        "qemu-system-aarch64",
        "-M", "virt,gic-version=3",
        "-cpu", "cortex-a53",
        "-nographic",
        "-m", "2G",
        "-smp", "4",
        "-append", "root=/dev/ram0 rw maxcpus=3 console=ttyAMA0 ramdisk_size=524288",
        "-kernel", str(package_dir / "zImage"),
        "-initrd", str(rootfs_candidates[0]),
        "-dtb", str(dtb),
        "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:22",
        "-device", "virtio-net-device,netdev=net0",
        "-no-reboot",
    ]
    log_file.parent.mkdir(parents=True, exist_ok=True)
    log_file.write_text("", encoding="utf-8")
    return subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)


def generate_key(work_dir: Path) -> tuple[Path, str]:
    key = work_dir / "ssh" / "id_ed25519"
    pub = key.with_suffix(key.suffix + ".pub")
    key.parent.mkdir(parents=True, exist_ok=True)
    if not key.exists():
        run(["ssh-keygen", "-t", "ed25519", "-N", "", "-f", str(key), "-q"])
    key.chmod(0o600)
    return key, pub.read_text(encoding="utf-8").strip()


def main() -> int:
    parser = argparse.ArgumentParser(description="Run MICA QEMU smoke test through SSH")
    parser.add_argument("--package-dir", type=Path, required=True)
    parser.add_argument("--work-dir", type=Path, default=Path("/tmp/opencode/mcs-qemu-ssh-smoke"))
    parser.add_argument("--repo-root", type=Path, default=repo_root_from_script())
    parser.add_argument("--host-port", type=int, default=2222)
    parser.add_argument("--boot-timeout", type=int, default=180)
    parser.add_argument("--ssh-timeout", type=int, default=60)
    parser.add_argument("--smoke-timeout", type=int, default=180)
    parser.add_argument("--root-password", default="OpenEuler@123")
    parser.add_argument("--micad", type=Path, help="local micad binary to deploy before smoke")
    parser.add_argument("--mica", type=Path, help="local mica command script to deploy before smoke")
    parser.add_argument("--ko", type=Path, help="local mcs_km.ko to deploy and reload before smoke")
    args = parser.parse_args()

    if not shutil.which("qemu-system-aarch64"):
        raise RuntimeError("qemu-system-aarch64 not found in PATH")
    if not shutil.which("ssh"):
        raise RuntimeError("ssh not found in PATH")
    if not shutil.which("scp"):
        raise RuntimeError("scp not found in PATH")
    if not shutil.which("ssh-keygen"):
        raise RuntimeError("ssh-keygen not found in PATH")

    args.work_dir.mkdir(parents=True, exist_ok=True)
    key, public_key = generate_key(args.work_dir)
    dtb = create_dtb(args.repo_root, args.work_dir)
    log_file = args.work_dir / "qemu-ssh-smoke.log"
    proc = run_qemu(args.package_dir, dtb, args.host_port, log_file)
    try:
        wait_console_and_configure_ssh(proc, log_file, public_key, args.root_password, args.boot_timeout)
        wait_for_ssh_login(args.host_port, key, args.ssh_timeout)
        deploy_artifacts(args.host_port, key, args, log_file)
        return run_smoke_over_ssh(args.host_port, key, log_file, args.smoke_timeout)
    finally:
        if proc.poll() is None:
            write_console(proc, "poweroff -f\n")
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.terminate()
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill()


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)
