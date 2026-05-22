#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../lib/test_utils.sh"
source "${SCRIPT_DIR}/../test-env.sh"

K3S_BIN="${K3S_BIN:-/usr/bin/k3s}"
REMOTE_HOST="${TEST_REMOTE_HOST:-root@192.168.7.2}"
TEST_IMAGE="${TEST_IMAGE:-localhost:5000/mica-uniproton-app:xen-0.1}"
PAUSE_IMAGE="${K3S_PAUSE_IMAGE:-rancher/mirrored-pause:3.6}"
PAUSE_IMAGE_CANONICAL="${K3S_PAUSE_IMAGE_CANONICAL:-docker.io/rancher/mirrored-pause:3.6}"
SOURCE_IMAGE_REF="${K3S_SOURCE_IMAGE_REF:-docker.io/local/mica-uniproton-app:xen-arm64-0.1}"
CONTAINER_COMMAND="${K3S_CONTAINER_COMMAND:-/micrun-placeholder}"
PAUSE_TAR="${K3S_PAUSE_TAR:-/tmp/pause-image-arm64.tar}"
IMAGE_TAR="${K3S_IMAGE_TAR:-/tmp/localhost_5000_mica-uniproton-app_xen-0.1.tar}"
LOG_FILE="${K3S_LOG_FILE:-/tmp/k3s-single-node.log}"
REMOTE_BOOTSTRAP_SCRIPT="${REMOTE_BOOTSTRAP_SCRIPT:-/tmp/k3s-single-node-bootstrap.sh}"
MIN_AVAILABLE_MB="${K3S_SINGLE_NODE_MIN_AVAILABLE_MB:-1024}"
DEFAULT_KUBELET_ARGS="${K3S_DEFAULT_KUBELET_ARGS:---kubelet-arg=cgroups-per-qos=false --kubelet-arg=enforce-node-allocatable=}"
KUBELET_ARGS="${K3S_KUBELET_ARGS-$DEFAULT_KUBELET_ARGS}"
AUTO_CLOSE_TIMEOUT="${K3S_AUTO_CLOSE_TIMEOUT:-0}"
POD_DELETE_TIMEOUT="${K3S_POD_DELETE_TIMEOUT:-60}"
FORCE_DELETE_PODS="${K3S_FORCE_DELETE_PODS:-false}"

require_remote_file() {
    local path="$1"
    remote "$REMOTE_HOST" "test -f '$path'"
}

wait_for_remote() {
    local command="$1"
    local retries="${2:-60}"
    local sleep_seconds="${3:-2}"

    local i
    for i in $(seq 1 "$retries"); do
        if remote "$REMOTE_HOST" "$command" >/dev/null 2>&1; then
            return 0
        fi
        sleep "$sleep_seconds"
    done
    return 1
}

remote_mem_available_mb() {
    remote "$REMOTE_HOST" "awk '/MemAvailable/ {print int(\$2/1024)}' /proc/meminfo"
}

remote_swap_device_count() {
    remote "$REMOTE_HOST" "swapon --show --noheadings 2>/dev/null | wc -l | tr -d ' '"
}

preclean_single_node_runtime() {
    local k3s_proc

    k3s_proc="$(basename "$K3S_BIN")"
    remote "$REMOTE_HOST" "
        set +e
        k3s_proc='$k3s_proc'
        for pid in \$(pidof \"\$k3s_proc\" 2>/dev/null || true); do
            cmd=\$(tr '\\000' ' ' <\"/proc/\$pid/cmdline\" 2>/dev/null || true)
            printf '%s\n' \"\$cmd\" | grep -Eq \"(^|/)\${k3s_proc} (server|agent)( |$)\" &&
                kill -9 \"\$pid\" 2>/dev/null || true
        done
        systemctl stop k3s 2>/dev/null || true
        systemctl stop micrun-k3s-agent.service 2>/dev/null || true
        find /run/k3s -mindepth 1 \\( -name rootfs -o -name shm \\) -exec umount -l {} \\; 2>/dev/null || true
        ctr -n k8s.io tasks ls -q 2>/dev/null | while read -r id; do
            [ -n \"\$id\" ] && ctr -n k8s.io tasks kill -s 9 \"\$id\" 2>/dev/null || true
            [ -n \"\$id\" ] && ctr -n k8s.io tasks rm \"\$id\" 2>/dev/null || true
        done
        ctr -n k8s.io containers ls -q 2>/dev/null | while read -r id; do
            [ -n \"\$id\" ] && ctr -n k8s.io containers rm \"\$id\" 2>/dev/null || true
        done
        xl list 2>/dev/null | awk 'NR>2 && \$1 != \"Domain-0\" {print \$1}' | while read -r id; do
            [ -n \"\$id\" ] && xl destroy \"\$id\" 2>/dev/null || true
        done
        rm -rf /var/lib/rancher/k3s/agent/etc/containerd /var/lib/rancher/k3s/agent/containerd /run/k3s /etc/rancher/k3s 2>/dev/null || true
    " >/dev/null 2>&1 || true

    cleanup_micrun_runtime_state "$REMOTE_HOST"
}

log_test "Single-node K3s + MicRun end-to-end"

require_remote_file "$PAUSE_TAR" || {
    log_error "pause image tar not found: $PAUSE_TAR"
    exit 1
}
require_remote_file "$IMAGE_TAR" || {
    log_error "RTOS image tar not found: $IMAGE_TAR"
    exit 1
}
require_remote_file "$K3S_BIN" || {
    log_error "k3s binary not found on edge node: $K3S_BIN"
    exit 1
}

preclean_single_node_runtime

AVAILABLE_MB="$(remote_mem_available_mb | tr -d '[:space:]')"
SWAP_COUNT="$(remote_swap_device_count | tr -d '[:space:]')"

if [ -n "$AVAILABLE_MB" ] && [ "$AVAILABLE_MB" -lt "$MIN_AVAILABLE_MB" ] && [ "${SWAP_COUNT:-0}" -eq 0 ]; then
    log_warn "skip single-node k3s: MemAvailable=${AVAILABLE_MB}MB, swap=${SWAP_COUNT}, threshold=${MIN_AVAILABLE_MB}MB"
    exit 0
fi

cat > /tmp/k3s-single-node-bootstrap.local.sh <<EOF
#!/bin/sh
set -eu
date -u -s '2026-03-12 15:00:00' >/dev/null 2>&1 || true
pkill -9 -f '$K3S_BIN server' 2>/dev/null || true
pkill -9 -f '$K3S_BIN agent' 2>/dev/null || true
systemctl stop k3s 2>/dev/null || true
systemctl stop micrun-k3s-agent.service 2>/dev/null || true
for id in \$(xl list | awk 'NR>2 && \$1 != "Domain-0" {print \$1}'); do
    xl destroy "\$id" 2>/dev/null || true
done
rm -rf /var/lib/rancher/k3s/agent/etc/containerd /var/lib/rancher/k3s/agent/containerd /run/k3s /etc/rancher/k3s 2>/dev/null || true
mkdir -p /var/lib/rancher/k3s/agent/etc/containerd
cat > /var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl <<'EOT'
{{ template "base" . }}

[plugins."io.containerd.cri.v1.runtime".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun.*"]
  container_annotations = ["org.openeuler.micrun.*"]

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun.*"]
  container_annotations = ["org.openeuler.micrun.*"]
EOT
rm -f '$LOG_FILE'
nohup '$K3S_BIN' server \
  --write-kubeconfig-mode=644 \
  --disable traefik \
  --disable servicelb \
  --disable local-storage \
  --disable metrics-server \
  --disable coredns \
  --disable-network-policy \
  --flannel-backend=none \
  $KUBELET_ARGS \
  >'$LOG_FILE' 2>&1 &
echo "started pid=\$!"
EOF

chmod +x /tmp/k3s-single-node-bootstrap.local.sh
copy_to_remote /tmp/k3s-single-node-bootstrap.local.sh "$REMOTE_HOST" "$REMOTE_BOOTSTRAP_SCRIPT"
remote "$REMOTE_HOST" "sh '$REMOTE_BOOTSTRAP_SCRIPT'" >/dev/null

wait_for_remote "$K3S_BIN ctr image ls" 90 2 || {
    log_error "k3s bundled containerd did not become ready"
    remote_output "$REMOTE_HOST" "tail -n 120 '$LOG_FILE'" || true
    exit 1
}

wait_for_remote "$K3S_BIN kubectl get nodes >/dev/null 2>&1" 90 2 || {
    log_error "k3s apiserver did not become ready"
    remote_output "$REMOTE_HOST" "tail -n 120 '$LOG_FILE'" || true
    exit 1
}

remote "$REMOTE_HOST" "
    set -eu
    '$K3S_BIN' ctr image import '$PAUSE_TAR'
    '$K3S_BIN' ctr image import '$IMAGE_TAR'
    '$K3S_BIN' ctr images tag '$PAUSE_IMAGE' '$PAUSE_IMAGE_CANONICAL' >/dev/null 2>&1 || true
    '$K3S_BIN' ctr images tag '$PAUSE_IMAGE_CANONICAL' '$PAUSE_IMAGE' >/dev/null 2>&1 || true
    '$K3S_BIN' ctr images tag '$SOURCE_IMAGE_REF' '$TEST_IMAGE' >/dev/null 2>&1 || true
    '$K3S_BIN' ctr images ls -q | grep -Fx '$PAUSE_IMAGE_CANONICAL' >/dev/null
    '$K3S_BIN' ctr images ls -q | grep -Fx '$TEST_IMAGE' >/dev/null
    if ! '$K3S_BIN' kubectl delete pod rtos-demo --ignore-not-found=true --wait=true --timeout='${POD_DELETE_TIMEOUT}s' >/dev/null 2>&1; then
        if [ '$FORCE_DELETE_PODS' = 'true' ]; then
            '$K3S_BIN' kubectl delete pod rtos-demo --force --grace-period=0 --ignore-not-found=true >/dev/null 2>&1 || true
        else
            echo 'timed out deleting old rtos-demo pod' >&2
            exit 1
        fi
    fi
    for i in \$(seq 1 30); do
        if ! '$K3S_BIN' kubectl get pod rtos-demo >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done
    if '$K3S_BIN' kubectl get pod rtos-demo >/dev/null 2>&1; then
        echo 'timed out waiting for old rtos-demo pod to be deleted' >&2
        exit 1
    fi
    '$K3S_BIN' kubectl delete runtimeclass micrun --ignore-not-found=true >/dev/null 2>&1 || true
    '$K3S_BIN' kubectl apply -f - <<'EOF'
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
EOF
    '$K3S_BIN' kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: rtos-demo
  annotations:
    org.openeuler.micrun.container.auto_close_timeout: \"$AUTO_CLOSE_TIMEOUT\"
spec:
  hostNetwork: true
  runtimeClassName: micrun
  tolerations:
  - key: node.kubernetes.io/not-ready
    operator: Exists
    effect: NoSchedule
  containers:
  - name: rtos-app
    image: $TEST_IMAGE
    command: [\"$CONTAINER_COMMAND\"]
    tty: false
    stdin: true
EOF
"

wait_for_remote "$K3S_BIN kubectl get pod rtos-demo -o jsonpath='{.status.phase}' 2>/dev/null | grep -qx Running" 60 2 || {
    log_error "rtos-demo did not reach Running"
    remote_output "$REMOTE_HOST" "$K3S_BIN kubectl describe pod rtos-demo" || true
    remote_output "$REMOTE_HOST" "tail -n 200 '$LOG_FILE'" || true
    exit 1
}

CONTAINER_ID="$(
    remote "$REMOTE_HOST" "$K3S_BIN kubectl get pod rtos-demo -o jsonpath='{.status.containerStatuses[0].containerID}'" |
        sed 's#containerd://##' | tr -d '[:space:]'
)"

wait_for_remote "$K3S_BIN ctr -n k8s.io tasks ls | awk -v id='$CONTAINER_ID' '\$1 == id && \$3 == \"RUNNING\" {found=1} END {exit found ? 0 : 1}'" 30 2 || {
    log_error "running containerd task not found: $CONTAINER_ID"
    remote_output "$REMOTE_HOST" "$K3S_BIN ctr -n k8s.io tasks ls" || true
    exit 1
}

wait_for_remote "xl list | awk -v id='$CONTAINER_ID' 'NR>2 && \$1 == id {found=1} END {exit found ? 0 : 1}'" 30 2 || {
    log_error "Xen domain not found for task: $CONTAINER_ID"
    remote_output "$REMOTE_HOST" "xl list" || true
    exit 1
}

log_success "Single-node RTOS pod is running via MicRun"
echo
remote_output "$REMOTE_HOST" "$K3S_BIN kubectl get pods -o wide"
echo "---"
remote_output "$REMOTE_HOST" "$K3S_BIN ctr task ls"
echo "---"
remote_output "$REMOTE_HOST" "xl list"
