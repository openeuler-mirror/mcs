#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../lib/test_utils.sh"
source "${SCRIPT_DIR}/../test-env.sh"

CLOUD_CONTAINER="${K3S_CLOUD_SERVER_CONTAINER:-micrun-k3s-server}"
CLOUD_KUBECTL_BIN="${K3S_CLOUD_KUBECTL_BIN:-k3s}"
CLOUD_KUBECTL_SUBCOMMAND="${K3S_CLOUD_KUBECTL_SUBCOMMAND-kubectl}"
REMOTE_HOST="${TEST_REMOTE_HOST:-root@192.168.7.2}"
EDGE_NODE="${K3S_EDGE_NODE_NAME:-qemu-aarch64}"
EDGE_CONTAINERD_ADDR="${K3S_EDGE_CONTAINERD_ADDR:-${K3S_CONTAINERD_ADDRESS:-/run/containerd/containerd.sock}}"
EDGE_CONTAINERD_NS="${K3S_EDGE_CONTAINERD_NS:-k8s.io}"
EDGE_CTR_BIN="${K3S_EDGE_CTR_BIN:-ctr}"
EDGE_CTR_SUBCOMMAND="${K3S_EDGE_CTR_SUBCOMMAND-}"
NAMESPACE="${K3S_NAMESPACE:-default}"
RUNTIME_CLASS_NAME="${K3S_RUNTIME_CLASS_NAME:-micrun}"
DEPLOYMENT="${K3S_OTA_DEPLOYMENT_NAME:-rtos-ota-demo}"
APP_LABEL="${K3S_OTA_APP_LABEL:-micrun-ota-demo}"
CONTAINER_NAME="${K3S_OTA_CONTAINER_NAME:-rtos-app}"
CONTAINER_COMMAND="${K3S_CONTAINER_COMMAND:-/micrun-placeholder}"
V1_IMAGE="${K3S_OTA_V1_IMAGE:-$TEST_IMAGE}"
V2_IMAGE="${K3S_OTA_V2_IMAGE:-localhost:5000/mica-uniproton-app:xen-0.2}"
V1_IMAGE_TAR="${K3S_OTA_V1_IMAGE_TAR:-$K3S_IMAGE_TAR}"
V2_IMAGE_TAR="${K3S_OTA_V2_IMAGE_TAR:-/tmp/localhost_5000_mica-uniproton-app_xen-0.2.tar}"
V1_SOURCE_IMAGE_REF="${K3S_OTA_V1_SOURCE_IMAGE_REF:-$K3S_SOURCE_IMAGE_REF}"
V2_SOURCE_IMAGE_REF="${K3S_OTA_V2_SOURCE_IMAGE_REF:-$V2_IMAGE}"
IMPORT_IMAGES="${K3S_OTA_IMPORT_IMAGES:-true}"
KEEP_DEPLOYMENT="${K3S_OTA_KEEP_DEPLOYMENT:-false}"
ATTACH_INPUT="${K3S_OTA_ATTACH_INPUT:-$'\nhelp\nuname\n'}"
ATTACH_TIMEOUT="${K3S_ATTACH_TIMEOUT:-70}"
ATTACH_INPUT_DELAY="${K3S_ATTACH_INPUT_DELAY:-6}"
ATTACH_LINE_DELAY="${K3S_ATTACH_LINE_DELAY:-3}"
ATTACH_TAIL_DELAY="${K3S_ATTACH_TAIL_DELAY:-6}"
POD_DELETE_TIMEOUT="${K3S_POD_DELETE_TIMEOUT:-60}"
POD_WAIT_SECONDS="${K3S_POD_WAIT_SECONDS:-180}"
EDGE_TASK_WAIT_SECONDS="${K3S_EDGE_TASK_WAIT_SECONDS:-60}"
EDGE_CLEANUP_WAIT_SECONDS="${K3S_EDGE_CLEANUP_WAIT_SECONDS:-60}"
OLD_TASK_CLEANUP_WAIT_SECONDS="${K3S_OTA_OLD_TASK_CLEANUP_WAIT_SECONDS:-60}"

POD_CLEANUP_DONE="false"
NEW_CONTAINER_ID=""

cloud_kubectl() {
    if [ -n "$CLOUD_KUBECTL_SUBCOMMAND" ]; then
        docker exec "$CLOUD_CONTAINER" \
            "$CLOUD_KUBECTL_BIN" "$CLOUD_KUBECTL_SUBCOMMAND" "$@"
    else
        docker exec "$CLOUD_CONTAINER" "$CLOUD_KUBECTL_BIN" "$@"
    fi
}

cloud_kubectl_stdin() {
    if [ -n "$CLOUD_KUBECTL_SUBCOMMAND" ]; then
        docker exec -i "$CLOUD_CONTAINER" \
            "$CLOUD_KUBECTL_BIN" "$CLOUD_KUBECTL_SUBCOMMAND" "$@"
    else
        docker exec -i "$CLOUD_CONTAINER" "$CLOUD_KUBECTL_BIN" "$@"
    fi
}

wait_for_command() {
    local timeout="$1"
    local interval="$2"
    shift 2

    local end=$((SECONDS + timeout))
    while [ "$SECONDS" -lt "$end" ]; do
        if "$@" >/dev/null 2>&1; then
            return 0
        fi
        sleep "$interval"
    done

    "$@"
}

wait_for_remote_edge() {
    local command="$1"
    local timeout="${2:-60}"

    wait_for_command "$timeout" 2 remote "$REMOTE_HOST" "$command"
}

edge_ctr_script() {
    cat <<EOF
edge_ctr() {
    if [ -n '$EDGE_CTR_SUBCOMMAND' ]; then
        '$EDGE_CTR_BIN' '$EDGE_CTR_SUBCOMMAND' -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' "\$@"
    else
        '$EDGE_CTR_BIN' -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' "\$@"
    fi
}
EOF
}

ensure_cloud_ready() {
    command -v docker >/dev/null 2>&1 || {
        log_error "docker command not found"
        exit 1
    }

    [ "$(docker inspect -f '{{.State.Running}}' "$CLOUD_CONTAINER" 2>/dev/null || true)" = "true" ] || {
        log_error "cloud K3s server container is not running: $CLOUD_CONTAINER"
        log_error "run tests/bin/test-k3s-cloud-edge first, or set K3S_CLOUD_SERVER_CONTAINER"
        exit 1
    }

    cloud_kubectl get node "$EDGE_NODE" >/dev/null 2>&1 || {
        log_error "edge node is not registered in cloud K3s: $EDGE_NODE"
        cloud_kubectl get nodes -o wide || true
        exit 1
    }

    [ "$(cloud_kubectl get node "$EDGE_NODE" -o 'jsonpath={.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)" = "True" ] || {
        log_error "edge node is not Ready: $EDGE_NODE"
        cloud_kubectl get nodes -o wide || true
        exit 1
    }
}

ensure_namespace() {
    [ "$NAMESPACE" = "default" ] && return 0
    cloud_kubectl get namespace "$NAMESPACE" >/dev/null 2>&1 ||
        cloud_kubectl create namespace "$NAMESPACE" >/dev/null
}

ensure_edge_images() {
    [ "$IMPORT_IMAGES" = "true" ] || return 0

    remote "$REMOTE_HOST" "
        set -eu
        $(edge_ctr_script)

        ensure_image() {
            tar_path=\"\$1\"
            source_ref=\"\$2\"
            target_ref=\"\$3\"

            if edge_ctr images ls -q | grep -Fx \"\$target_ref\" >/dev/null 2>&1; then
                return 0
            fi
            test -f \"\$tar_path\"
            edge_ctr images import \"\$tar_path\" >/dev/null
            edge_ctr images tag \"\$source_ref\" \"\$target_ref\" >/dev/null 2>&1 || true
            edge_ctr images ls -q | grep -Fx \"\$target_ref\" >/dev/null
        }

        if [ -f '$K3S_PAUSE_TAR' ]; then
            edge_ctr images import '$K3S_PAUSE_TAR' >/dev/null
            edge_ctr images tag '$K3S_PAUSE_IMAGE' '$K3S_PAUSE_IMAGE_CANONICAL' >/dev/null 2>&1 || true
            edge_ctr images tag '$K3S_PAUSE_IMAGE_CANONICAL' '$K3S_PAUSE_IMAGE' >/dev/null 2>&1 || true
        fi

        ensure_image '$V1_IMAGE_TAR' '$V1_SOURCE_IMAGE_REF' '$V1_IMAGE'
        ensure_image '$V2_IMAGE_TAR' '$V2_SOURCE_IMAGE_REF' '$V2_IMAGE'
    " >/dev/null || {
        log_error "failed to ensure v1/v2 OTA images on edge containerd"
        log_error "v1 tar=$V1_IMAGE_TAR source=$V1_SOURCE_IMAGE_REF target=$V1_IMAGE"
        log_error "v2 tar=$V2_IMAGE_TAR source=$V2_SOURCE_IMAGE_REF target=$V2_IMAGE"
        exit 1
    }
}

pod_name_for_deployment() {
    cloud_kubectl get pod -n "$NAMESPACE" \
        -l "app=$APP_LABEL" \
        --sort-by=.metadata.creationTimestamp \
        -o 'jsonpath={.items[-1:].metadata.name}'
}

pod_container_id() {
    local pod="$1"

    cloud_kubectl get pod "$pod" -n "$NAMESPACE" \
        -o 'jsonpath={.status.containerStatuses[0].containerID}' |
        sed 's#containerd://##' |
        tr -d '[:space:]'
}

pod_spec_image() {
    local pod="$1"

    cloud_kubectl get pod "$pod" -n "$NAMESPACE" \
        -o 'jsonpath={.spec.containers[0].image}' |
        tr -d '[:space:]'
}

verify_edge_task_and_domain() {
    local cid="$1"

    wait_for_remote_edge "
        $(edge_ctr_script)
        edge_ctr tasks ls |
            awk -v id='$cid' '\$1 == id && \$3 == \"RUNNING\" {found=1} END {exit found ? 0 : 1}'
    " "$EDGE_TASK_WAIT_SECONDS" || {
        log_error "running edge containerd task not found: $cid"
        remote_output "$REMOTE_HOST" "$(edge_ctr_script); edge_ctr tasks ls" || true
        exit 1
    }

    wait_for_remote_edge "
        xl list |
            awk -v id='$cid' 'NR>2 && \$1 == id {found=1} END {exit found ? 0 : 1}'
    " "$EDGE_TASK_WAIT_SECONDS" || {
        log_error "Xen domain not found for edge task: $cid"
        remote_output "$REMOTE_HOST" "xl list" || true
        exit 1
    }
}

verify_edge_task_and_domain_absent() {
    local cid="$1"

    wait_for_remote_edge "
        $(edge_ctr_script)
        edge_ctr tasks ls |
            awk -v id='$cid' '\$1 == id {found=1} END {exit found ? 1 : 0}'
    " "$EDGE_CLEANUP_WAIT_SECONDS" || {
        log_error "edge containerd task still exists: $cid"
        remote_output "$REMOTE_HOST" "$(edge_ctr_script); edge_ctr tasks ls" || true
        exit 1
    }

    wait_for_remote_edge "
        xl list |
            awk -v id='$cid' 'NR>2 && \$1 == id {found=1} END {exit found ? 1 : 0}'
    " "$EDGE_CLEANUP_WAIT_SECONDS" || {
        log_error "edge Xen domain still exists: $cid"
        remote_output "$REMOTE_HOST" "xl list" || true
        exit 1
    }
}

cleanup_edge_objects_for_pod() {
    local pod="$1"

    remote "$REMOTE_HOST" "
        set +e
        $(edge_ctr_script)
        ids=\"\"
        for id in \$(edge_ctr containers ls -q 2>/dev/null); do
            if edge_ctr containers info \"\$id\" 2>/dev/null |
                grep -Fq '\"io.kubernetes.pod.name\": \"$pod\"'; then
                ids=\"\$ids \$id\"
            fi
        done
        for id in \$ids; do
            pid=\"\$(edge_ctr tasks ls 2>/dev/null | awk -v id=\"\$id\" '\$1 == id {print \$2; exit}')\"
            [ -n \"\$pid\" ] && kill -9 \"\$pid\" 2>/dev/null || true
            mica stop \"\$id\" 2>/dev/null || true
            mica rm \"\$id\" 2>/dev/null || true
            edge_ctr tasks kill -s 9 \"\$id\" 2>/dev/null || true
            edge_ctr tasks delete --force \"\$id\" 2>/dev/null || true
            edge_ctr tasks rm --force \"\$id\" 2>/dev/null || true
            xl destroy \"\$id\" 2>/dev/null || true
            edge_ctr containers delete \"\$id\" 2>/dev/null || true
            rm -rf \"/run/micrun/containers/\$id\" \"/run/micrun/runtime/container/\$id\" \"/run/micrun/runtime/sandbox/\$id\" 2>/dev/null || true
        done
    " >/dev/null 2>&1 || true
}

cleanup_edge_objects_for_app() {
    local pods

    pods="$(cloud_kubectl get pod -n "$NAMESPACE" -l "app=$APP_LABEL" -o name 2>/dev/null | sed 's#pod/##' || true)"
    for pod in $pods; do
        cleanup_edge_objects_for_pod "$pod"
    done

    remote "$REMOTE_HOST" "
        set +e
        $(edge_ctr_script)
        ids=\"\"
        for id in \$(edge_ctr containers ls -q 2>/dev/null); do
            if edge_ctr containers info \"\$id\" 2>/dev/null |
                grep -Fq '\"io.kubernetes.pod.name\": \"$DEPLOYMENT'; then
                ids=\"\$ids \$id\"
            fi
        done
        for id in \$ids; do
            pid=\"\$(edge_ctr tasks ls 2>/dev/null | awk -v id=\"\$id\" '\$1 == id {print \$2; exit}')\"
            [ -n \"\$pid\" ] && kill -9 \"\$pid\" 2>/dev/null || true
            mica stop \"\$id\" 2>/dev/null || true
            mica rm \"\$id\" 2>/dev/null || true
            edge_ctr tasks kill -s 9 \"\$id\" 2>/dev/null || true
            edge_ctr tasks delete --force \"\$id\" 2>/dev/null || true
            edge_ctr tasks rm --force \"\$id\" 2>/dev/null || true
            xl destroy \"\$id\" 2>/dev/null || true
            edge_ctr containers delete \"\$id\" 2>/dev/null || true
            rm -rf \"/run/micrun/containers/\$id\" \"/run/micrun/runtime/container/\$id\" \"/run/micrun/runtime/sandbox/\$id\" 2>/dev/null || true
        done
    " >/dev/null 2>&1 || true
}

cleanup() {
    [ "$KEEP_DEPLOYMENT" = "true" ] && return 0
    [ "$POD_CLEANUP_DONE" = "true" ] && return 0

    if docker inspect -f '{{.State.Running}}' "$CLOUD_CONTAINER" 2>/dev/null | grep -qx true; then
        cleanup_edge_objects_for_app
        cloud_kubectl delete deployment "$DEPLOYMENT" -n "$NAMESPACE" \
            --ignore-not-found=true --wait=true --timeout="${POD_DELETE_TIMEOUT}s" >/dev/null 2>&1 ||
            cloud_kubectl delete deployment "$DEPLOYMENT" -n "$NAMESPACE" \
                --force --grace-period=0 --ignore-not-found=true >/dev/null 2>&1 || true
        cloud_kubectl delete runtimeclass "$RUNTIME_CLASS_NAME" \
            --ignore-not-found=true >/dev/null 2>&1 || true
        cleanup_edge_objects_for_app
    fi

    POD_CLEANUP_DONE="true"
}
trap cleanup EXIT

apply_v1_deployment() {
    ensure_namespace

    cat <<EOF | cloud_kubectl_stdin apply -f - >/dev/null
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: $RUNTIME_CLASS_NAME
handler: micrun
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $DEPLOYMENT
  namespace: $NAMESPACE
spec:
  replicas: 1
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  selector:
    matchLabels:
      app: $APP_LABEL
  template:
    metadata:
      labels:
        app: $APP_LABEL
      annotations:
        org.openeuler.micrun.container.auto_close_timeout: "0"
    spec:
      hostNetwork: true
      runtimeClassName: $RUNTIME_CLASS_NAME
      nodeSelector:
        kubernetes.io/hostname: $EDGE_NODE
      containers:
      - name: $CONTAINER_NAME
        image: $V1_IMAGE
        imagePullPolicy: IfNotPresent
        command: ["$CONTAINER_COMMAND"]
        tty: false
        stdin: true
EOF
}

wait_for_rollout() {
    local timeout="$1"

    cloud_kubectl rollout status deployment/"$DEPLOYMENT" \
        -n "$NAMESPACE" --timeout="${timeout}s"
}

attach_and_validate() {
    local pod="$1"
    local output

    output="$(
        {
            sleep "$ATTACH_INPUT_DELAY"
            while IFS= read -r line || [ -n "$line" ]; do
                printf '%s\n' "$line"
                sleep "$ATTACH_LINE_DELAY"
            done <<EOF
$ATTACH_INPUT
EOF
            sleep "$ATTACH_TAIL_DELAY"
        } | timeout "$ATTACH_TIMEOUT" docker exec -i "$CLOUD_CONTAINER" \
            "$CLOUD_KUBECTL_BIN" ${CLOUD_KUBECTL_SUBCOMMAND:+"$CLOUD_KUBECTL_SUBCOMMAND"} \
            attach -n "$NAMESPACE" -i "$pod" -c "$CONTAINER_NAME" 2>&1 || true
    )"

    output="$(printf '%s\n' "$output" | tr -d '\000' | tr '\r' '\n' | sed -E 's/\x1B\[[0-9;?]*[[:alpha:]]//g')"

    if printf '%s\n' "$output" | grep -Eq 'openEuler UniProton #|support shell commond|UniProton [0-9]|Hello, (UniProton|Zephyr)!'; then
        printf '%s\n' "$output" | tail -n 40
        return 0
    fi

    log_error "kubectl attach did not expose expected RTOS markers"
    printf '%s\n' "$output" | tail -n 80
    return 1
}

verify_old_workload_cleaned() {
    local old_pod="$1"
    local old_cid="$2"

    wait_for_remote_edge "
        xl list |
            awk -v id='$old_cid' 'NR>2 && \$1 == id {found=1} END {exit found ? 1 : 0}'
    " "$EDGE_CLEANUP_WAIT_SECONDS" || {
        log_error "old v1 Xen domain still exists: $old_cid"
        remote_output "$REMOTE_HOST" "xl list" || true
        exit 1
    }
    log_info "old v1 Xen domain cleaned: $old_cid"

    if wait_for_remote_edge "
        $(edge_ctr_script)
        edge_ctr tasks ls |
            awk -v id='$old_cid' '\$1 == id {found=1} END {exit found ? 1 : 0}'
    " "$OLD_TASK_CLEANUP_WAIT_SECONDS"; then
        log_info "old v1 containerd task removed: $old_cid"
        return 0
    fi

    log_info "old v1 containerd residual detected; cleaning edge objects for pod $old_pod"
    cleanup_edge_objects_for_pod "$old_pod"
    cloud_kubectl delete pod "$old_pod" -n "$NAMESPACE" \
        --force --grace-period=0 --ignore-not-found=true >/dev/null 2>&1 || true
    verify_edge_task_and_domain_absent "$old_cid"
}

main() {
    log_test "K3s MicRun OTA rollout end-to-end"

    ensure_cloud_ready
    ensure_edge_images

    log_info "Cleaning old OTA resources"
    cleanup
    POD_CLEANUP_DONE="false"

    log_info "Deploying v1 image: $V1_IMAGE"
    apply_v1_deployment
    wait_for_rollout "$POD_WAIT_SECONDS"

    local old_pod
    local old_cid
    local old_image
    old_pod="$(pod_name_for_deployment)"
    old_cid="$(pod_container_id "$old_pod")"
    old_image="$(pod_spec_image "$old_pod")"
    assert_not_empty "$old_cid" "unable to resolve v1 container ID"
    assert_equals "$V1_IMAGE" "$old_image" "v1 pod image does not match"
    verify_edge_task_and_domain "$old_cid"
    log_info "v1 running: pod=$old_pod cid=$old_cid"

    log_info "Patching Deployment to v2 image: $V2_IMAGE"
    cloud_kubectl set image deployment/"$DEPLOYMENT" -n "$NAMESPACE" \
        "$CONTAINER_NAME=$V2_IMAGE" >/dev/null
    wait_for_rollout "$POD_WAIT_SECONDS"

    local new_pod
    local new_cid
    local new_image
    new_pod="$(pod_name_for_deployment)"
    new_cid="$(pod_container_id "$new_pod")"
    new_image="$(pod_spec_image "$new_pod")"
    NEW_CONTAINER_ID="$new_cid"
    assert_not_empty "$new_cid" "unable to resolve v2 container ID"
    assert_equals "$V2_IMAGE" "$new_image" "v2 pod image does not match"
    [ "$new_cid" != "$old_cid" ] || {
        log_error "OTA rollout reused the old container ID: $old_cid"
        exit 1
    }
    verify_edge_task_and_domain "$new_cid"
    verify_old_workload_cleaned "$old_pod" "$old_cid"
    log_info "v2 running: pod=$new_pod cid=$new_cid"

    log_info "Validating kubectl attach against v2 pod"
    attach_and_validate "$new_pod"

    log_info "Cleaning OTA resources"
    cleanup
    if [ -n "$NEW_CONTAINER_ID" ]; then
        verify_edge_task_and_domain_absent "$NEW_CONTAINER_ID"
    fi

    log_success "K3s OTA rollout, v2 attach, edge task, Xen domain, and cleanup validated"
}

main "$@"
