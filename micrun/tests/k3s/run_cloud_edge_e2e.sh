#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../lib/test_utils.sh"
source "${SCRIPT_DIR}/../test-env.sh"

K3S_BIN="${K3S_BIN:-/usr/bin/k3s}"
REMOTE_HOST="${TEST_REMOTE_HOST:-root@192.168.7.2}"
EDGE_NODE_NAME="${K3S_EDGE_NODE_NAME:-qemu-aarch64}"
EDGE_NODE_IP="${K3S_EDGE_NODE_IP:-192.168.7.2}"
PAUSE_TAR="${K3S_PAUSE_TAR:-/tmp/pause-image-arm64.tar}"
PAUSE_IMAGE="${K3S_PAUSE_IMAGE:-rancher/mirrored-pause:3.6}"
PAUSE_IMAGE_CANONICAL="${K3S_PAUSE_IMAGE_CANONICAL:-docker.io/rancher/mirrored-pause:3.6}"
IMAGE_TAR="${K3S_IMAGE_TAR:-/tmp/localhost_5000_mica-uniproton-app_xen-0.1.tar}"
SOURCE_IMAGE_REF="${K3S_SOURCE_IMAGE_REF:-docker.io/local/mica-uniproton-app:xen-arm64-0.1}"
CONTAINER_COMMAND="${K3S_CONTAINER_COMMAND:-/micrun-placeholder}"
RUNTIME_CLASS_NAME="${K3S_RUNTIME_CLASS_NAME:-micrun}"
POD_NAME="${K3S_E2E_POD_NAME:-rtos-cloud-demo}"
EDGE_BOOTSTRAP_SCRIPT="${K3S_EDGE_BOOTSTRAP_SCRIPT:-/tmp/k3s-cloud-edge-bootstrap.sh}"
EDGE_LOG_FILE="${K3S_EDGE_LOG_FILE:-/tmp/k3s-agent.log}"
AGENT_SERVICE="${K3S_EDGE_AGENT_SERVICE:-micrun-k3s-agent.service}"
EDGE_CONTAINERD_MODE="${K3S_EDGE_CONTAINERD_MODE:-bundled}"
EDGE_CONTAINERD_NS="${K3S_EDGE_CONTAINERD_NS:-k8s.io}"
if [ "$EDGE_CONTAINERD_MODE" = "external" ]; then
    CONTAINERD_ADDRESS="${K3S_CONTAINERD_ADDRESS:-/run/containerd/containerd.sock}"
else
    CONTAINERD_ADDRESS="${K3S_CONTAINERD_ADDRESS:-/run/k3s/containerd/containerd.sock}"
fi
CRI_ENDPOINT="${K3S_CRI_ENDPOINT:-unix://${CONTAINERD_ADDRESS}}"
EDGE_CTR_BIN="${K3S_EDGE_CTR_BIN:-$K3S_BIN}"
EDGE_CTR_SUBCOMMAND="${K3S_EDGE_CTR_SUBCOMMAND-ctr}"
DEFAULT_KUBELET_ARGS="${K3S_DEFAULT_KUBELET_ARGS:---kubelet-arg=cgroups-per-qos=false --kubelet-arg=enforce-node-allocatable=}"
KUBELET_ARGS="${K3S_KUBELET_ARGS-$DEFAULT_KUBELET_ARGS}"
AUTO_CLOSE_TIMEOUT="${K3S_AUTO_CLOSE_TIMEOUT:-0}"
POD_DELETE_TIMEOUT="${K3S_POD_DELETE_TIMEOUT:-60}"
FORCE_DELETE_PODS="${K3S_FORCE_DELETE_PODS:-false}"
KEEP_POD="${K3S_E2E_KEEP_POD:-false}"
CONTAINER_ID=""
POD_CLEANUP_DONE="false"

CLOUD_SERVER_IMAGE="${K3S_CLOUD_SERVER_IMAGE:-rancher/k3s:v1.27.15-k3s1}"
CLOUD_SERVER_CONTAINER="${K3S_CLOUD_SERVER_CONTAINER:-micrun-k3s-server}"
CLOUD_SERVER_NAME="${K3S_CLOUD_SERVER_NAME:-cloud-srv}"
CLOUD_SERVER_IP="${K3S_CLOUD_SERVER_IP:-192.168.7.10}"
CLOUD_SERVER_SNAPSHOTTER="${K3S_CLOUD_SERVER_SNAPSHOTTER:-native}"
CLOUD_SERVER_EXTRA_ARGS="${K3S_CLOUD_SERVER_EXTRA_ARGS:-}"
CLOUD_KUBECTL_BIN="${K3S_CLOUD_KUBECTL_BIN:-k3s}"
CLOUD_KUBECTL_SUBCOMMAND="${K3S_CLOUD_KUBECTL_SUBCOMMAND-kubectl}"
CLOUD_NETWORK_NAME="${K3S_CLOUD_NETWORK_NAME:-micrun-cloud}"
CLOUD_NETWORK_SUBNET="${K3S_CLOUD_NETWORK_SUBNET:-192.168.7.0/24}"
CLOUD_NETWORK_GATEWAY="${K3S_CLOUD_NETWORK_GATEWAY:-192.168.7.1}"
CLOUD_NETWORK_PARENT="${K3S_CLOUD_NETWORK_PARENT:-tap0}"
K3S_TOKEN_PLAIN="${K3S_TOKEN_PLAIN:-micrun-dev-token}"

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

wait_for_local() {
    local command="$1"
    local retries="${2:-60}"
    local sleep_seconds="${3:-2}"
    local i

    for i in $(seq 1 "$retries"); do
        if bash -lc "$command" >/dev/null 2>&1; then
            return 0
        fi
        sleep "$sleep_seconds"
    done

    return 1
}

cloud_kubectl() {
    if [ -n "$CLOUD_KUBECTL_SUBCOMMAND" ]; then
        docker exec "$CLOUD_SERVER_CONTAINER" \
            "$CLOUD_KUBECTL_BIN" "$CLOUD_KUBECTL_SUBCOMMAND" "$@"
    else
        docker exec "$CLOUD_SERVER_CONTAINER" "$CLOUD_KUBECTL_BIN" "$@"
    fi
}

cloud_kubectl_stdin() {
    if [ -n "$CLOUD_KUBECTL_SUBCOMMAND" ]; then
        docker exec -i "$CLOUD_SERVER_CONTAINER" \
            "$CLOUD_KUBECTL_BIN" "$CLOUD_KUBECTL_SUBCOMMAND" "$@"
    else
        docker exec -i "$CLOUD_SERVER_CONTAINER" "$CLOUD_KUBECTL_BIN" "$@"
    fi
}

wait_for_cloud_kubectl() {
    local retries="$1"
    local sleep_seconds="$2"
    local i
    shift 2

    for i in $(seq 1 "$retries"); do
        if cloud_kubectl "$@" >/dev/null 2>&1; then
            return 0
        fi
        sleep "$sleep_seconds"
    done

    return 1
}

require_remote_file() {
    local path="$1"
    remote "$REMOTE_HOST" "test -f '$path'"
}

validate_edge_containerd_mode() {
    case "$EDGE_CONTAINERD_MODE" in
        bundled|external)
            ;;
        *)
            log_error "invalid K3S_EDGE_CONTAINERD_MODE: $EDGE_CONTAINERD_MODE (expected bundled or external)"
            exit 1
            ;;
    esac
}

ensure_local_requirements() {
    command -v docker >/dev/null 2>&1 || {
        log_error "docker command not found on local host"
        exit 1
    }

    ip link show "$CLOUD_NETWORK_PARENT" >/dev/null 2>&1 || {
        log_error "network parent interface not found: $CLOUD_NETWORK_PARENT"
        exit 1
    }
}

ensure_cloud_network() {
    if docker network inspect "$CLOUD_NETWORK_NAME" >/dev/null 2>&1; then
        local current
        local current_subnet
        local current_gateway
        local current_parent

        current="$(docker network inspect -f '{{(index .IPAM.Config 0).Subnet}} {{(index .IPAM.Config 0).Gateway}} {{index .Options "parent"}}' "$CLOUD_NETWORK_NAME")"
        current_subnet="$(printf '%s\n' "$current" | awk '{print $1}')"
        current_gateway="$(printf '%s\n' "$current" | awk '{print $2}')"
        current_parent="$(printf '%s\n' "$current" | awk '{print $3}')"

        if [ "$current_subnet" = "$CLOUD_NETWORK_SUBNET" ] &&
           [ "$current_gateway" = "$CLOUD_NETWORK_GATEWAY" ] &&
           [ "$current_parent" = "$CLOUD_NETWORK_PARENT" ]; then
            return 0
        fi

        docker network rm "$CLOUD_NETWORK_NAME" >/dev/null
    fi

    docker network create \
        -d macvlan \
        --subnet "$CLOUD_NETWORK_SUBNET" \
        --gateway "$CLOUD_NETWORK_GATEWAY" \
        -o "parent=$CLOUD_NETWORK_PARENT" \
        "$CLOUD_NETWORK_NAME" >/dev/null
}

start_cloud_server() {
    local extra_args=()

    if [ -n "$CLOUD_SERVER_EXTRA_ARGS" ]; then
        # shellcheck disable=SC2206
        extra_args=($CLOUD_SERVER_EXTRA_ARGS)
    fi

    docker rm -f "$CLOUD_SERVER_CONTAINER" >/dev/null 2>&1 || true

    docker run -d --privileged \
        --name "$CLOUD_SERVER_CONTAINER" \
        --network "$CLOUD_NETWORK_NAME" \
        --ip "$CLOUD_SERVER_IP" \
        "$CLOUD_SERVER_IMAGE" server \
        --node-name "$CLOUD_SERVER_NAME" \
        --advertise-address "$CLOUD_SERVER_IP" \
        --node-ip "$CLOUD_SERVER_IP" \
        --tls-san "$CLOUD_SERVER_IP" \
        --token "$K3S_TOKEN_PLAIN" \
        --disable traefik \
        --disable servicelb \
        --disable local-storage \
        --disable metrics-server \
        --disable-network-policy \
        --disable coredns \
        --flannel-backend=none \
        --snapshotter "$CLOUD_SERVER_SNAPSHOTTER" \
        "${extra_args[@]}" >/dev/null

    wait_for_cloud_kubectl 90 2 get nodes || {
        log_error "cloud K3s server did not become ready"
        docker logs "$CLOUD_SERVER_CONTAINER" 2>&1 | tail -n 200 || true
        exit 1
    }
}

fetch_server_token() {
    docker exec "$CLOUD_SERVER_CONTAINER" cat /var/lib/rancher/k3s/server/node-token
}

bootstrap_edge_agent() {
    local server_token="$1"
    local edge_time
    local local_bootstrap
    local agent_runtime_args=""

    if [ "$EDGE_CONTAINERD_MODE" = "external" ]; then
        agent_runtime_args="--container-runtime-endpoint=$CRI_ENDPOINT"
    fi

    edge_time="$(date -u '+%Y-%m-%d %H:%M:%S')"
    local_bootstrap="$(mktemp /tmp/k3s-cloud-edge-bootstrap.XXXXXX.sh)"

    cat >"$local_bootstrap" <<EOF
#!/bin/sh
set -eu

date -u -s '$edge_time' >/dev/null 2>&1 || true
systemctl stop k3s 2>/dev/null || true
systemctl stop '$AGENT_SERVICE' 2>/dev/null || true
pkill -9 -f '$K3S_BIN server' 2>/dev/null || true
pkill -9 -f '$K3S_BIN agent' 2>/dev/null || true
pkill -9 -f 'containerd-shim-mica-v2 -namespace k8s.io -address /run/k3s/containerd/containerd.sock' 2>/dev/null || true
pkill -9 -f 'containerd-shim-mica-v2 -namespace k8s.io -address /run/containerd/containerd.sock' 2>/dev/null || true
for id in \$(xl list | awk 'NR>2 && \$1 != "Domain-0" {print \$2}'); do
    xl destroy "\$id" 2>/dev/null || true
done
for mountpoint in \$(find /run/k3s -mindepth 1 \\( -name rootfs -o -name shm \\) 2>/dev/null); do
    umount -l "\$mountpoint" 2>/dev/null || true
done
rm -rf /var/lib/rancher/k3s/agent/containerd /run/k3s
mkdir -p /etc/cni/net.d /opt/cni

if [ '$EDGE_CONTAINERD_MODE' = 'external' ]; then
    mkdir -p /etc/containerd
cat > /etc/containerd/config.toml <<'EOT'
version = 2

[plugins."io.containerd.grpc.v1.cri"]
  sandbox_image = "$PAUSE_IMAGE_CANONICAL"

[plugins."io.containerd.grpc.v1.cri".cni]
  bin_dir = "/opt/cni/bin"
  conf_dir = "/etc/cni/net.d"

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun.*"]
  container_annotations = ["org.openeuler.micrun.*"]

[plugins."io.containerd.cri.v1.runtime".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun.*"]
  container_annotations = ["org.openeuler.micrun.*"]
EOT
    systemctl restart containerd
else
    mkdir -p /var/lib/rancher/k3s/agent/etc/containerd
    mkdir -p /var/lib/rancher/k3s/agent/etc/cni/net.d
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
fi

cat > /etc/cni/net.d/10-micrun.conflist <<'EOT'
{
  "cniVersion": "1.0.0",
  "name": "micrun-bridge",
  "plugins": [
    {
      "type": "bridge",
      "bridge": "cni0",
      "isGateway": true,
      "ipMasq": true,
      "promiscMode": true,
      "ipam": {
        "type": "host-local",
        "ranges": [[{ "subnet": "10.42.1.0/24" }]],
        "routes": [{ "dst": "0.0.0.0/0" }]
      }
    },
    {
      "type": "portmap",
      "capabilities": { "portMappings": true }
    }
  ]
}
EOT
if [ '$EDGE_CONTAINERD_MODE' != 'external' ]; then
    cp /etc/cni/net.d/10-micrun.conflist /var/lib/rancher/k3s/agent/etc/cni/net.d/10-micrun.conflist
fi
ln -sfn /var/lib/rancher/k3s/data/current/bin /opt/cni/bin
cat > /etc/systemd/system/$AGENT_SERVICE <<'EOT'
[Unit]
Description=MicRun K3s Edge Agent
After=network-online.target containerd.service
Wants=network-online.target

[Service]
Type=simple
Environment=K3S_URL=https://$CLOUD_SERVER_IP:6443
Environment=K3S_TOKEN=$server_token
ExecStart=$K3S_BIN agent $agent_runtime_args --node-ip $EDGE_NODE_IP --pause-image $PAUSE_IMAGE_CANONICAL $KUBELET_ARGS
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOT
systemctl daemon-reload
systemctl enable '$AGENT_SERVICE' >/dev/null 2>&1 || true
rm -f '$EDGE_LOG_FILE'
systemctl restart '$AGENT_SERVICE'
EOF

chmod +x "$local_bootstrap"
copy_to_remote "$local_bootstrap" "$REMOTE_HOST" "$EDGE_BOOTSTRAP_SCRIPT"
remote "$REMOTE_HOST" "sh '$EDGE_BOOTSTRAP_SCRIPT'" >/dev/null
rm -f "$local_bootstrap"
}

wait_for_edge_runtime() {
    local runtime_check

    if [ "$EDGE_CONTAINERD_MODE" = "external" ]; then
        if [ -n "$EDGE_CTR_SUBCOMMAND" ]; then
            runtime_check="'$EDGE_CTR_BIN' '$EDGE_CTR_SUBCOMMAND' -a '$CONTAINERD_ADDRESS' version"
        else
            runtime_check="'$EDGE_CTR_BIN' -a '$CONTAINERD_ADDRESS' version"
        fi
    else
        runtime_check="$K3S_BIN crictl --runtime-endpoint '$CRI_ENDPOINT' info"
    fi

    wait_for_remote "$runtime_check" 90 2 || {
        log_error "edge containerd did not become CRI-ready"
        remote_output "$REMOTE_HOST" "systemctl status '$AGENT_SERVICE' --no-pager -l | sed -n '1,120p'" || true
        remote_output "$REMOTE_HOST" "tail -n 200 '$EDGE_LOG_FILE'" || true
        remote_output "$REMOTE_HOST" "tail -n 200 /var/lib/rancher/k3s/agent/containerd/containerd.log" || true
        exit 1
    }
}

import_edge_images() {
    remote "$REMOTE_HOST" "
        set -eu
        edge_ctr() {
            if [ -n '$EDGE_CTR_SUBCOMMAND' ]; then
                '$EDGE_CTR_BIN' '$EDGE_CTR_SUBCOMMAND' -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' \"\$@\"
            else
                '$EDGE_CTR_BIN' -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' \"\$@\"
            fi
        }
        edge_ctr images import '$PAUSE_TAR'
        edge_ctr images import '$IMAGE_TAR'
        edge_ctr images tag '$PAUSE_IMAGE' '$PAUSE_IMAGE_CANONICAL' >/dev/null 2>&1 || true
        edge_ctr images tag '$PAUSE_IMAGE_CANONICAL' '$PAUSE_IMAGE' >/dev/null 2>&1 || true
        edge_ctr images tag '$SOURCE_IMAGE_REF' '$TEST_IMAGE' >/dev/null 2>&1 || true
        edge_ctr images ls -q | grep -Fx '$PAUSE_IMAGE_CANONICAL' >/dev/null
        edge_ctr images ls -q | grep -Fx '$TEST_IMAGE' >/dev/null
    " >/dev/null
}

wait_for_edge_node() {
    local i

    for i in $(seq 1 90); do
        if [ "$(cloud_kubectl get node "$EDGE_NODE_NAME" \
            -o 'jsonpath={.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)" = "True" ]; then
            return 0
        fi
        sleep 2
    done

    {
        log_error "edge node did not register as Ready"
        cloud_kubectl get nodes -o wide || true
        cloud_kubectl get events -A --sort-by=.metadata.creationTimestamp | tail -n 120 || true
        remote_output "$REMOTE_HOST" "systemctl status '$AGENT_SERVICE' --no-pager -l | sed -n '1,120p'" || true
        remote_output "$REMOTE_HOST" "tail -n 200 '$EDGE_LOG_FILE'" || true
        exit 1
    }
}

deploy_cloud_pod() {
    local i

    if ! cloud_kubectl delete pod "$POD_NAME" --ignore-not-found=true \
        --wait=true --timeout="${POD_DELETE_TIMEOUT}s" >/dev/null 2>&1; then
        if [ "$FORCE_DELETE_PODS" = "true" ]; then
            cloud_kubectl delete pod "$POD_NAME" --force --grace-period=0 \
                --ignore-not-found=true >/dev/null 2>&1 || true
        else
            log_error "timed out deleting old pod: $POD_NAME"
            exit 1
        fi
    fi

    for i in $(seq 1 30); do
        if ! cloud_kubectl get pod "$POD_NAME" >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done
    if cloud_kubectl get pod "$POD_NAME" >/dev/null 2>&1; then
        log_error "timed out waiting for old pod to be deleted: $POD_NAME"
        exit 1
    fi

    cloud_kubectl delete runtimeclass "$RUNTIME_CLASS_NAME" \
        --ignore-not-found=true >/dev/null 2>&1 || true

    cat <<EOF | cloud_kubectl_stdin apply -f - >/dev/null
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: $RUNTIME_CLASS_NAME
handler: micrun
---
apiVersion: v1
kind: Pod
metadata:
  name: $POD_NAME
  annotations:
    org.openeuler.micrun.container.auto_close_timeout: "$AUTO_CLOSE_TIMEOUT"
spec:
  hostNetwork: true
  runtimeClassName: $RUNTIME_CLASS_NAME
  nodeSelector:
    kubernetes.io/hostname: $EDGE_NODE_NAME
  containers:
  - name: rtos-app
    image: $TEST_IMAGE
    imagePullPolicy: IfNotPresent
    command: ["$CONTAINER_COMMAND"]
    tty: false
    stdin: true
EOF
}

wait_for_cloud_pod() {
    local i

    for i in $(seq 1 90); do
        if [ "$(cloud_kubectl get pod "$POD_NAME" \
            -o 'jsonpath={.status.phase}' 2>/dev/null || true)" = "Running" ]; then
            return 0
        fi
        sleep 2
    done

    {
        log_error "cloud-edge RTOS pod did not reach Running"
        cloud_kubectl describe pod "$POD_NAME" || true
        remote_output "$REMOTE_HOST" "journalctl -u '$AGENT_SERVICE' --since '5 minutes ago' --no-pager | tail -n 200" || true
        remote_output "$REMOTE_HOST" "ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' containers ls" || true
        exit 1
    }
}

verify_edge_runtime_objects() {
    CONTAINER_ID="$(
        cloud_kubectl get pod "$POD_NAME" \
            -o 'jsonpath={.status.containerStatuses[0].containerID}' \
            | sed 's#containerd://##'
    )"

    wait_for_remote "ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' tasks ls | awk -v id='$CONTAINER_ID' '\$1 == id && \$3 == \"RUNNING\" {found=1} END {exit found ? 0 : 1}'" 30 2 || {
        log_error "running containerd task not found on edge: $CONTAINER_ID"
        remote_output "$REMOTE_HOST" "ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' tasks ls" || true
        exit 1
    }

    wait_for_remote "xl list | awk 'NR>2 && \$1 == \"$CONTAINER_ID\" {found=1} END {exit found ? 0 : 1}'" 30 2 || {
        log_error "Xen domain not found for edge task: $CONTAINER_ID"
        remote_output "$REMOTE_HOST" "xl list" || true
        exit 1
    }
}

cleanup_edge_pod_runtime_objects() {
    remote "$REMOTE_HOST" "
        set +e
        ids=\"\"
        for id in \$(ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' containers ls -q 2>/dev/null); do
            if ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' containers info \"\$id\" 2>/dev/null | grep -Fq '\"io.kubernetes.pod.name\": \"$POD_NAME\"'; then
                ids=\"\$ids \$id\"
            fi
        done
        for id in \$ids; do
            if command -v mica >/dev/null 2>&1; then
                mica stop \"\$id\" 2>/dev/null || true
                mica rm \"\$id\" 2>/dev/null || true
            fi
            ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' tasks kill -s 9 \"\$id\" 2>/dev/null || true
            ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' tasks delete --force \"\$id\" 2>/dev/null || true
            ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' tasks rm --force \"\$id\" 2>/dev/null || true
            xl destroy \"\$id\" 2>/dev/null || true
            ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' containers delete \"\$id\" 2>/dev/null || true
            rm -rf \"/run/micrun/containers/\$id\" \"/run/micrun/runtime/container/\$id\" \"/run/micrun/runtime/sandbox/\$id\" 2>/dev/null || true
        done
    " >/dev/null
}

cleanup_cloud_pod() {
    [ "$KEEP_POD" = "true" ] && return 0
    [ "$POD_CLEANUP_DONE" = "true" ] && return 0

    if docker inspect -f '{{.State.Running}}' "$CLOUD_SERVER_CONTAINER" 2>/dev/null | grep -qx true; then
        if ! cloud_kubectl delete pod "$POD_NAME" --ignore-not-found=true \
            --wait=true --timeout="${POD_DELETE_TIMEOUT}s" >/dev/null 2>&1; then
            cloud_kubectl delete pod "$POD_NAME" --force --grace-period=0 \
                --ignore-not-found=true >/dev/null 2>&1 || true
        fi
        cloud_kubectl delete runtimeclass "$RUNTIME_CLASS_NAME" \
            --ignore-not-found=true >/dev/null 2>&1 || true
    fi

    cleanup_edge_pod_runtime_objects
    POD_CLEANUP_DONE="true"
}

verify_delete_cleanup() {
    [ "$KEEP_POD" = "true" ] && return 0
    [ -n "$CONTAINER_ID" ] || return 0

    log_info "Cleaning cloud-edge RTOS pod"
    cleanup_cloud_pod

    wait_for_remote "ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' tasks ls | awk -v id='$CONTAINER_ID' '\$1 == id {found=1} END {exit found ? 1 : 0}'" 30 2 || {
        log_error "edge containerd task still exists after cloud-edge cleanup: $CONTAINER_ID"
        remote_output "$REMOTE_HOST" "ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' tasks ls" || true
        exit 1
    }

    wait_for_remote "xl list | awk -v id='$CONTAINER_ID' 'NR>2 && \$1 == id {found=1} END {exit found ? 1 : 0}'" 30 2 || {
        log_error "edge Xen domain still exists after cloud-edge cleanup: $CONTAINER_ID"
        remote_output "$REMOTE_HOST" "xl list" || true
        exit 1
    }

    log_success "Cloud-edge RTOS pod cleanup validated"
}

cleanup() {
    cleanup_cloud_pod
}
trap cleanup EXIT

print_results() {
    log_success "Cloud-edge RTOS pod is running via MicRun"
    echo
    cloud_kubectl get nodes -o wide
    echo "---"
    cloud_kubectl get pod "$POD_NAME" -o wide
    echo "---"
    remote_output "$REMOTE_HOST" "ctr -a '$CONTAINERD_ADDRESS' -n '$EDGE_CONTAINERD_NS' tasks ls"
    echo "---"
    remote_output "$REMOTE_HOST" "xl list"
}

log_test "Cloud K3s server + edge MicRun agent end-to-end"

validate_edge_containerd_mode
ensure_local_requirements
require_remote_file "$K3S_BIN" || {
    log_error "k3s binary not found on edge node: $K3S_BIN"
    exit 1
}
require_remote_file "$PAUSE_TAR" || {
    log_error "pause image tar not found: $PAUSE_TAR"
    exit 1
}
require_remote_file "$IMAGE_TAR" || {
    log_error "RTOS image tar not found: $IMAGE_TAR"
    exit 1
}

log_info "Preparing cloud network $CLOUD_NETWORK_NAME on $CLOUD_NETWORK_PARENT"
ensure_cloud_network
log_info "Starting cloud K3s server $CLOUD_SERVER_CONTAINER"
start_cloud_server
remote "$REMOTE_HOST" "
    systemctl stop '$AGENT_SERVICE' 2>/dev/null || true
    systemctl stop k3s 2>/dev/null || true
    pkill -9 -f '$K3S_BIN agent' 2>/dev/null || true
    pkill -9 -f '$K3S_BIN server' 2>/dev/null || true
" >/dev/null 2>&1 || true
log_info "Cleaning stale edge MicRun runtime state"
cleanup_micrun_runtime_state "$REMOTE_HOST"
log_info "Bootstrapping edge K3s agent on $EDGE_NODE_NAME"
bootstrap_edge_agent "$(fetch_server_token)"
log_info "Waiting for edge container runtime"
wait_for_edge_runtime
log_info "Importing pause and RTOS images on edge"
import_edge_images
log_info "Waiting for edge node readiness"
wait_for_edge_node
log_info "Deploying MicRun RTOS pod on edge"
deploy_cloud_pod
log_info "Waiting for cloud-edge RTOS pod"
wait_for_cloud_pod
log_info "Verifying edge runtime objects"
verify_edge_runtime_objects
print_results
verify_delete_cleanup
