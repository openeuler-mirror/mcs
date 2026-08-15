#!/bin/bash
# Performance stage measurement for the micrun QEMU stack.
#
# Runs a standard load (N rounds of create -> RUNNING -> attach -> kill ->
# delete) against a freshly booted guest, with MICRUN_PERF=1 injected into
# containerd's environment so every shim emits [PERF] stage lines. Collects:
#   - wall-clock metrics on the guest (create->running, kill->gone)
#   - shim stage lines (create_guest / initial_config / guest_start /
#     cpu_settings / memory_setup / dial_tty / stop_sandbox) from journald
# and writes a JSON result file that tests/perf/compare.sh diffs against
# tests/perf/baseline.json.
#
# Usage:
#   tests/perf/measure.sh                 # fresh guest, 5 rounds, result to /tmp
#   ROUNDS=1 tests/perf/measure.sh        # quick smoke of the harness itself
#   RESULT=/tmp/r.json tests/perf/measure.sh
#
# Requires the usual QEMU test env (qemu.sh resolves artifacts) and NOPASSWD
# sudo. The build artifacts are never modified; MICRUN_PERF only flips a
# log flag in the running containerd.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MICRUN_REPO="$(cd "$SCRIPT_DIR/../.." && pwd)"
source "$MICRUN_REPO/tests/common/assert.sh"
source "$MICRUN_REPO/tests/common/remote.sh"
source "$MICRUN_REPO/tests/common/qemu.sh"

ROUNDS="${ROUNDS:-5}"
IMAGE="${TEST_IMAGE:-docker.io/local/mica-uniproton-app:xen-arm64-0.1}"
RESULT="${RESULT:-/tmp/micrun-perf-result.json}"
LOG="${PERF_LOG:-/tmp/micrun-perf-guest.log}"
CONSOLE="${QEMU_SMOKE_LOG_FILE:-/tmp/micrun-tests/qemu-perf-console.log}"

unset SSH_ASKPASS SUDO_ASKPASS
export SSH_ASKPASS_REQUIRE=never

qemu_resolve_assets || exit 1
qemu_start_background "$CONSOLE" || { echo "PERF_GUEST_START_FAIL"; exit 1; }
qemu_wait_for_ssh 90 || { echo "PERF_SSH_FAIL"; exit 1; }
trap 'qemu_stop_existing >/dev/null 2>&1 || true' EXIT

guest() { qemu_local_ssh "$1" 2>/dev/null; }

# Deploy the workspace shim: stage lines come from the perf instrumentation
# (support/perf), which may be newer than the shim baked into the rootfs.
# Cross-build (cgo on because -race style flags are not used here, plain
# static cross build) and swap it in the running guest — the pristine
# artifacts on the host stay untouched.
if [ "${PERF_DEPLOY_SHIM:-1}" = "1" ]; then
    (cd "$MICRUN_REPO" && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -mod=vendor -o /tmp/perf-shim .)         || { echo "PERF_SHIM_BUILD_FAIL"; exit 1; }
    timeout 90 sshpass -p "${QEMU_GUEST_PASSWORD:-micrun}" scp -P "${QEMU_SSH_FWD_PORT:-10022}"         -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null         /tmp/perf-shim root@127.0.0.1:/usr/bin/containerd-shim-mica-v2-perf 2>/dev/null         || { echo "PERF_SHIM_SHIP_FAIL"; exit 1; }
    guest 'cp /usr/bin/containerd-shim-mica-v2 /tmp/shim-rootfs-backup && cp /usr/bin/containerd-shim-mica-v2-perf /usr/bin/containerd-shim-mica-v2 && chmod +x /usr/bin/containerd-shim-mica-v2' || true
fi

# Ship the image tar over (QEMU_IMAGE_TAR is a HOST path).
if [ -n "${QEMU_IMAGE_TAR:-}" ] && [ -f "${QEMU_IMAGE_TAR:-}" ]; then
    timeout 180 sshpass -p "${QEMU_GUEST_PASSWORD:-micrun}" scp -P "${QEMU_SSH_FWD_PORT:-10022}" \
        -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        "$QEMU_IMAGE_TAR" root@127.0.0.1:/tmp/ 2>/dev/null || { echo "PERF_TAR_FAIL"; exit 1; }
fi

# Enable stage timing: shims inherit containerd's environment.
guest 'mkdir -p /etc/systemd/system/containerd.service.d && printf "[Service]\nEnvironment=MICRUN_PERF=1\n" > /etc/systemd/system/containerd.service.d/perf.conf && systemctl daemon-reload && systemctl restart containerd && sleep 2 && systemctl is-active containerd' | tail -1
guest "ctr image import --all-platforms /tmp/$(basename "${QEMU_IMAGE_TAR:-none}") >/dev/null 2>&1; ctr image ls -q | grep -c ${IMAGE##*:} || true" | tail -1

# Standard load. Wall times measured on the guest with date +%s.%N.
# Render the load script locally (no nested-heredoc escaping pitfalls),
# ship it over, then execute it on the guest.
cat > /tmp/perf_load_rendered.sh <<LOAD
#!/bin/sh
set -u
ROUNDS=$ROUNDS
IMAGE='$IMAGE'
for r in \$(seq 1 \$ROUNDS); do
  id=perf-\$r
  t0=\$(date +%s.%N)
  ctr run --runtime io.containerd.mica.v2 --detach --annotation org.openeuler.micrun.container.auto_close=false \$IMAGE \$id >/dev/null 2>&1 || { echo "\$id run_fail"; continue; }
  w=0; while :; do st=\$(ctr tasks ls 2>/dev/null | awk -v i=\$id '\$1==i{print \$3}'); [ "\$st" = RUNNING ] && break; w=\$((w+1)); [ \$w -gt 120 ] && { echo "\$id wait_running_timeout"; break; }; sleep 1; done
  t1=\$(date +%s.%N)
  ta0=\$(date +%s.%N)
  printf '\nhelp\n' | timeout 3 ctr task attach \$id > /tmp/att.out 2>&1 || true
  ta1=\$(date +%s.%N)
  tk0=\$(date +%s.%N)
  ctr task kill -s 9 \$id >/dev/null 2>&1 || true
  w=0; while :; do g=\$(xl list 2>/dev/null | awk -v i=\$id '\$1==i{f=1} END{print f+0}'); [ "\$g" = 0 ] && break; w=\$((w+1)); [ \$w -gt 120 ] && { echo "\$id wait_gone_timeout"; break; }; sleep 1; done
  tk1=\$(date +%s.%N)
  ctr task delete \$id >/dev/null 2>&1; ctr container delete \$id >/dev/null 2>&1
  dur() { awk -v a="\$1" -v b="\$2" 'BEGIN{printf "%.3f", b-a}'; }
  echo "\$id create_running \$(dur \$t0 \$t1) attach \$(dur \$ta0 \$ta1) kill_gone \$(dur \$tk0 \$tk1)"
done
LOAD
timeout 60 sshpass -p "${QEMU_GUEST_PASSWORD:-micrun}" scp -P "${QEMU_SSH_FWD_PORT:-10022}"     -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null     /tmp/perf_load_rendered.sh root@127.0.0.1:/tmp/perf_load.sh 2>/dev/null || { echo "PERF_LOAD_SHIP_FAIL"; exit 1; }
qemu_local_ssh "sh /tmp/perf_load.sh" > "$LOG" 2>/dev/null || true

# Shim stage lines from this window.
guest 'cat /var/log/mica/mica-runtime.log 2>/dev/null | grep -o "\[PERF\] .*"; journalctl -u containerd --since "15 minutes ago" --no-pager 2>/dev/null | grep -o "\[PERF\] .*"' | sort | uniq -c > "${LOG}.stages" || true

# Aggregate to JSON (median per wall metric; stage lines listed raw).
python3 - "$LOG" "${LOG}.stages" "$RESULT" <<'PY'
import json, statistics, sys
wall, stages, out = sys.argv[1:4]
metrics = {}
for line in open(wall):
    p = line.split()
    # format: <id> create_running <v> attach <v> kill_gone <v>
    if len(p) == 7:
        for m, v in ((p[1], p[2]), (p[3], p[4]), (p[5], p[6])):
            try:
                metrics.setdefault(m, []).append(float(v))
            except ValueError:
                pass
doc = {
    "failed_rounds": [l.split()[0] for l in open(wall) if "run_fail" in l or "timeout" in l],
    "wall": {m: {"median": round(statistics.median(v), 3), "max": round(max(v), 3), "n": len(v)}
             for m, v in metrics.items()},
    "stages": [l.strip() for l in open(stages) if l.strip()],
}
json.dump(doc, open(out, "w"), indent=1)
print(json.dumps(doc["wall"], indent=1))
print("stage lines:", len(doc["stages"]), "->", out)
PY
