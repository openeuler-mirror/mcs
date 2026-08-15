#!/usr/bin/env python3
"""Expect-style serial console bootstrap for QEMU test guests.

Sole client of the QEMU serial unix socket while it runs (QEMU's socket
chardev serves exactly one client, so no log tee may be attached at the
same time). It mirrors the console stream into a log file and drives the
getty login with real pattern matching — no blind sleeps:

    login prompt  -> send root
    Password:     -> send the image preset password
    shell prompt  -> send the one-shot preparation command
    marker line   -> verify and exit

The preparation (set root password, enable PermitRootLogin, restart
sshd) is guest runtime preparation in the sense of AGENTS.md; the build
artifact itself is never modified.

Configuration comes from the environment:
    CONSOLE_SOCK             unix socket path of the QEMU serial chardev
    LOG_FILE                 file receiving the mirrored console stream
    INITIAL_ROOT_PASSWORD    image preset root password (with -rtc fixed)
    NEW_PASS                 root password to set for SSH
    BOOTSTRAP_TIMEOUT        overall timeout in seconds (default 420)

Exit code 0 on verified preparation, 1 otherwise.
"""

import os
import re
import select
import socket
import sys
import time

SOCK = os.environ.get("CONSOLE_SOCK", "")
LOG = os.environ.get("LOG_FILE", "")
INITIAL_PASS = os.environ.get("INITIAL_ROOT_PASSWORD", "openEuler@2021")
NEW_PASS = os.environ.get("NEW_PASS", "micrun")
# Temporary secret used only to satisfy the interactive passwd-expire change
# (image variants with an expired root account drop straight into
# "New password:" after login, before any shell). It must clear
# pam_pwquality; the real SSH secret is set afterwards by the preparation
# step (chpasswd -e with a host-side hash), so its value never persists.
EXPIRE_PASS = os.environ.get("EXPIRE_PASS", "Micrun-Bootstrap-2026!")
# Precomputed crypt(3) hash of NEW_PASS (qemu.sh makes it with openssl).
# chpasswd -e consumes the hash directly and bypasses pam_pwquality,
# which rejects the short test password ("password not changed").
NEW_PASS_HASH = os.environ.get("NEW_PASS_HASH", "")
TIMEOUT = float(os.environ.get("BOOTSTRAP_TIMEOUT", "420"))

# chpasswd -e (preencrypted hash) leaves the shadow lastchg field empty on
# some shadow-utils variants; an empty/never lastchg makes PAM treat the
# account as expired, so SSH password auth drops into a forced
# password-change conversation that sshpass cannot answer. Setting the
# change date explicitly closes it. Use UTC minus one day: the guest clock
# follows the QEMU -rtc UTC base, and a lastchg in the future relative to
# the guest clock is rejected by strict PAM/shadow validation.
CHAGE_FIX = "chage -d %s root" % time.strftime(
    "%Y-%m-%d", time.gmtime(time.time() - 86400)
)

if NEW_PASS_HASH:
    PASSWD_LINE = (
        "echo 'root:%s' | chpasswd -e && %s" % (NEW_PASS_HASH, CHAGE_FIX)
    )
else:
    PASSWD_LINE = "echo 'root:%s' | chpasswd && %s" % (NEW_PASS, CHAGE_FIX)

# The preparation runs as SHORT per-step lines, each verified by its own
# marker: the emulated serial path degrades on long lines (observed twice:
# a ~430+ byte line lost its tail mid-stream even when paced, while <450
# byte lines always arrived intact). Short lines stay far from that cliff
# and make every retry cheap and localized.
#
# Critical steps chain their marker with &&, so a marker line proves the
# step actually succeeded — not merely that the line was reached. The tap
# step is best-effort (no matching NIC is not an error) and keeps ;
#
# Stderr redirects ("2>/dev/null") are safe in step lines: the PS2 check
# anchors on a line-leading continuation prompt, not the bare ">" that
# older revisions matched.
PREP_STEPS = [
    (
        PASSWD_LINE + " && echo PREP_PASS_DONE",
        re.compile(rb"(?m)^PREP_PASS_DONE\r?$"),
    ),
    (
        # The grep||echo fallback tolerates a PermitRootLogin line the sed
        # pattern missed; a failed sed (readonly fs) still suppresses the
        # marker and forces a retry. Steps are kept short on purpose: the
        # serial line truncates reliably somewhere below ~400 bytes, so
        # each sshd setting travels as its own step.
        "(sed -i 's/^#\\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config; "
        "grep -q '^PermitRootLogin yes' /etc/ssh/sshd_config "
        "|| echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config) "
        "&& echo PREP_SSHCFG_DONE",
        re.compile(rb"(?m)^PREP_SSHCFG_DONE\r?$"),
    ),
    (
        # PasswordAuthentication must be forced on as well: some image
        # variants ship it disabled (only publickey/keyboard-interactive
        # offered), which the sshpass-based test tooling cannot
        # authenticate with.
        "(sed -i 's/^#\\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config; "
        "grep -q '^PasswordAuthentication yes' /etc/ssh/sshd_config "
        "|| echo 'PasswordAuthentication yes' >> /etc/ssh/sshd_config) "
        "&& echo PREP_SSHPW_DONE",
        re.compile(rb"(?m)^PREP_SSHPW_DONE\r?$"),
    ),
    (
        # A sshd_config.d drop-in would win over the main file (first-read
        # wins), so its PasswordAuthentication lines are commented out.
        "sed -i 's/^PasswordAuthentication.*/#&/' /etc/ssh/sshd_config.d/*.conf; "
        "echo PREP_SSHDROPIN_DONE",
        re.compile(rb"(?m)^PREP_SSHDROPIN_DONE\r?$"),
    ),
    (
        # LTS-Next's PAM stack accepts keyboard-interactive authentication but
        # fails pam_setcred immediately afterwards for the forwarded
        # (10.0.2.2) session.  OpenSSH then closes the connection after the
        # key exchange, which looks like a transport failure to the harness.
        # Password authentication is healthy, so keep the test guest on the
        # password path.  This is guest runtime preparation; the rootfs is
        # never modified.
        "sed -i 's/^#\\?KbdInteractiveAuthentication.*/KbdInteractiveAuthentication no/' /etc/ssh/sshd_config; "
        "grep -q '^KbdInteractiveAuthentication no' /etc/ssh/sshd_config "
        "|| echo 'KbdInteractiveAuthentication no' >> /etc/ssh/sshd_config; "
        # Soft verification only: sshd -T fails outright on configs with
        # Match blocks (other image variants), which must not abort the
        # step — the config edit above is what matters.
        "sshd -T 2>/dev/null | grep -q '^kbdinteractiveauthentication no$'; "
        "echo PREP_SSHKBI_DONE",
        re.compile(rb"(?m)^PREP_SSHKBI_DONE\r?$"),
    ),
]

# Optional: give the tap-side NIC its static test IP. The image ships
# /etc/systemd/network/10-eth-static.network matching "eth*", but udev
# predictable names (enp0s*) never match, so the tap NIC stays unconfigured
# and K3s cloud-edge tests cannot reach the guest over tap. This assigns
# the address to the non-usernet interface at runtime (guest runtime
# preparation; the artifact itself is untouched).
GUEST_TAP_IP = os.environ.get("GUEST_TAP_IP", "")
if GUEST_TAP_IP:
    PREP_STEPS.append(
        (
            'for d in /sys/class/net/*; do n=${d##*/}; [ "$n" = lo ] && continue; '
            "ip -o -4 addr show dev $n | grep -q '10\\.0\\.2\\.' && continue; "
            "ip addr add %s dev $n; ip link set $n up; done; "
            "echo PREP_TAP_DONE" % GUEST_TAP_IP,
            re.compile(rb"(?m)^PREP_TAP_DONE\r?$"),
        )
    )

PREP_STEPS.append(
    (
        # The image ships /etc/hosts.deny rules that make the libwrap-built
        # sshd close every connection from non-localhost sources — the QEMU
        # usernet forward (source 10.0.2.2) included. Empty the deny file so
        # the forwarded test connections reach sshd at all.
        ": > /etc/hosts.deny; echo PREP_HOSTSDENY_DONE",
        re.compile(rb"(?m)^PREP_HOSTSDENY_DONE\r?$"),
    ),
)

PREP_STEPS.append(
    (
        # A default firewall ruleset (nftables/iptables INPUT policy) can
        # REJECT connections arriving from the QEMU usernet forward (source
        # 10.0.2.2) even though localhost self-connects pass. Flush the
        # ruleset for this test guest so forwarded connections reach sshd.
        "nft flush ruleset 2>/dev/null; iptables -F 2>/dev/null; "
        "echo PREP_FW_DONE",
        re.compile(rb"(?m)^PREP_FW_DONE\r?$"),
    ),
)

PREP_STEPS.append(
    (
        # pam_faillock (deny=3, even_deny_root, unlock_time=300) fights the
        # automated harness: one connection racing the sshd restart logs a
        # failed auth and locks root for 300s, longer than the suites'
        # SSH-ready window. Disable the lockout for this test guest (guest
        # runtime preparation, same standing as PermitRootLogin above) and
        # drop any counters already recorded.
        "sed -i 's/^auth.*pam_faillock/#&/' /etc/pam.d/common-auth; "
        "rm -f /run/faillock/root /var/run/faillock/root; "
        "mkdir -p /run/sshd /var/empty/sshd; "
        "echo PREP_FAILLOCK_DONE",
        re.compile(rb"(?m)^PREP_FAILLOCK_DONE\r?$"),
    )
)

PREP_STEPS.append(
    (
        # Wait (bounded) for the sshd listener to actually accept a TCP
        # connection: "systemctl restart" returning does not mean the
        # socket is serving yet, and the suite's very first SSH attempt
        # racing that window gets reset, poisoning later attempts on the
        # usernet forward. The self-connect closes before any auth, so it
        # adds no faillock counters.
        "(systemctl restart sshd || systemctl restart ssh) && id; "
        "i=0; while [ $i -lt 10 ]; do "
        "(exec 3<>/dev/tcp/127.0.0.1/22) 2>/dev/null && break; "
        "sleep 1; i=$((i+1)); done; "
        "echo BOOTSTRAP_DONE",
        re.compile(rb"(?m)^BOOTSTRAP_DONE\r?$"),
    )
)

# Prompts end without a newline, so match them near the end of the
# rolling tail. The window tolerates a stray status line interleaved by
# systemd between the prompt and our read.
LOGIN_RE = re.compile(rb"login:\s*$")
PASS_RE = re.compile(rb"Password:\s*$")
# passwd-expire first-login flow: the account is dropped straight into a
# mandatory password change before any shell appears.
NEWPW_RE = re.compile(rb"New password:\s*$")
RETYPE_RE = re.compile(rb"Retype new password:\s*$")
SHELL_RE = re.compile(rb"#\s*$")
UID_RE = re.compile(rb"uid=0")
FAIL_RE = re.compile(rb"Login incorrect")

PROMPT_WINDOW = 256
TAIL_LIMIT = 16384
WAKE_INTERVAL = 15.0
STATE_TIMEOUT = 90.0
# Per-step wait must stay well under the image's bash TMOUT auto-logout
# (observed firing at ~60s while sitting at a continuation prompt).
STEP_TIMEOUT = 30.0
MAX_LOGIN_ATTEMPTS = 3
MAX_STEP_ATTEMPTS = 3
# The emulated serial port drops input when a long line is written in one
# burst (observed: a ~380 byte command arrived split by a stray CR and
# truncated, leaving the shell at a continuation prompt). Trickling in
# small chunks keeps the UART/tty path reliable.
SEND_CHUNK = 32
SEND_DELAY = 0.05
# A real bash continuation prompt is "> " at the start of a fresh line.
# Matching a bare ">" anywhere at the tail end false-positives on shell
# metacharacters (e.g. "2>") while the command echoes back.
PS2_RE = re.compile(rb"\r\n> $")


def note(msg):
    sys.stderr.write("console bootstrap: %s\n" % msg)
    sys.stderr.flush()


def main():
    if not SOCK or not LOG:
        note("CONSOLE_SOCK and LOG_FILE must be set")
        return 1

    deadline = time.monotonic() + TIMEOUT

    # Connect with retry: QEMU creates the socket shortly after start.
    sock = None
    while time.monotonic() < deadline:
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        try:
            sock.connect(SOCK)
            break
        except OSError:
            sock.close()
            sock = None
            time.sleep(1)
    if sock is None:
        note("cannot connect to %s" % SOCK)
        return 1
    sock.setblocking(False)

    try:
        log = open(LOG, "ab", buffering=0)
    except OSError as exc:
        note("cannot open log file %s: %s" % (LOG, exc))
        sock.close()
        return 1

    def send(data):
        view = memoryview(data)
        while view:
            try:
                sent = sock.send(view[:SEND_CHUNK])
                view = view[sent:]
            except BlockingIOError:
                select.select([], [sock], [], 5.0)
            except OSError as exc:
                note("send failed: %s" % exc)
                raise
            if view:
                time.sleep(SEND_DELAY)

    tail = b""
    state = "login"
    attempts = 0
    step_idx = 0
    step_attempts = 0
    state_since = time.monotonic()
    last_rx = time.monotonic()
    last_wake = 0.0

    def enter(new_state):
        nonlocal state, state_since, tail
        state = new_state
        state_since = time.monotonic()
        tail = b""  # consume matched prompt; only fresh output counts

    def send_step():
        nonlocal step_attempts
        step_attempts += 1
        if step_attempts > MAX_STEP_ATTEMPTS:
            note("step %d failed %d times; giving up" % (step_idx + 1, step_attempts))
            return False
        note("sending step %d/%d (attempt %d)"
             % (step_idx + 1, len(PREP_STEPS), step_attempts))
        send(PREP_STEPS[step_idx][0].encode() + b"\r")
        enter("step")
        return True

    def advance_step():
        """Current step verified; send the next one (or report success)."""
        nonlocal step_idx, step_attempts
        step_idx += 1
        step_attempts = 0
        if step_idx >= len(PREP_STEPS):
            return True
        note("step %d verified, sending step %d/%d"
             % (step_idx, step_idx + 1, len(PREP_STEPS)))
        send(PREP_STEPS[step_idx][0].encode() + b"\r")
        enter("step")
        return False

    try:
        while time.monotonic() < deadline:
            ready, _, _ = select.select([sock], [], [], 1.0)
            now = time.monotonic()
            if ready:
                try:
                    chunk = sock.recv(4096)
                except OSError as exc:
                    note("recv failed: %s" % exc)
                    return 1
                if not chunk:
                    note("console socket closed by the QEMU side")
                    return 1
                log.write(chunk)
                tail = (tail + chunk)[-TAIL_LIMIT:]
                last_rx = now

            if state == "login":
                if LOGIN_RE.search(tail[-PROMPT_WINDOW:]):
                    attempts += 1
                    if attempts > MAX_LOGIN_ATTEMPTS:
                        note("login failed %d times; giving up" % attempts)
                        return 1
                    note("login prompt seen, sending user (attempt %d)" % attempts)
                    send(b"root\r")
                    enter("password")
                elif now - last_rx > WAKE_INTERVAL and now - last_wake > WAKE_INTERVAL:
                    # Getty may have printed its prompt before we connected;
                    # a bare CR makes it reprint. Boot output keeps last_rx
                    # fresh, so this only fires on a quiet console.
                    send(b"\r")
                    last_wake = now
            elif state == "password":
                window = tail[-PROMPT_WINDOW:]
                if NEWPW_RE.search(window):
                    note("passwd-expire flow: new-password prompt, sending temporary secret")
                    send(EXPIRE_PASS.encode() + b"\r")
                elif RETYPE_RE.search(window):
                    note("passwd-expire flow: retype prompt, confirming temporary secret")
                    send(EXPIRE_PASS.encode() + b"\r")
                    enter("shell")
                elif PASS_RE.search(window):
                    note("password prompt seen, sending preset password")
                    send(INITIAL_PASS.encode() + b"\r")
                    enter("shell")
                elif SHELL_RE.search(window):
                    # No password asked (already logged in); go on.
                    if not send_step():
                        return 1
                elif FAIL_RE.search(tail):
                    note("preset password rejected")
                    enter("login")
                elif now - state_since > STATE_TIMEOUT:
                    note("no password prompt; back to login watch")
                    enter("login")
            elif state == "shell":
                window = tail[-PROMPT_WINDOW:]
                if SHELL_RE.search(window):
                    if not send_step():
                        return 1
                elif PS2_RE.search(window):
                    # A mangled earlier line left the shell at a
                    # continuation prompt; Ctrl-C returns to PS1.
                    note("continuation prompt seen, sending Ctrl-C")
                    send(b"\x03")
                    enter("shell")
                elif FAIL_RE.search(tail):
                    note("login failed at password stage")
                    enter("login")
                elif now - state_since > STATE_TIMEOUT:
                    note("no shell prompt; back to login watch")
                    enter("login")
            elif state == "step":
                marker = PREP_STEPS[step_idx][1]
                if marker.search(tail):
                    last_step = step_idx == len(PREP_STEPS) - 1
                    if last_step and not UID_RE.search(tail):
                        # Marker raced ahead of the id output; keep waiting.
                        pass
                    elif last_step:
                        note("runtime preparation verified (uid=0 + BOOTSTRAP_DONE)")
                        return 0
                    elif not advance_step():
                        continue
                if PS2_RE.search(tail[-PROMPT_WINDOW:]):
                    note("step %d line was split; Ctrl-C and retry" % (step_idx + 1))
                    send(b"\x03")
                    enter("shell")
                elif now - state_since > STEP_TIMEOUT:
                    note("step %d marker missing; Ctrl-C and retry" % (step_idx + 1))
                    send(b"\x03")
                    enter("shell")
    finally:
        log.close()
        sock.close()

    note("timed out after %ds (state=%s); see %s" % (TIMEOUT, state, LOG))
    return 1


if __name__ == "__main__":
    sys.exit(main())
