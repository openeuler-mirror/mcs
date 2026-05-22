#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../lib/test_utils.sh"
source "${SCRIPT_DIR}/../test-env.sh"

REMOTE_HOST="${TEST_REMOTE_HOST:-root@192.168.7.2}"
MODE="${K3S_INTERACTION_MODE:-auto}"
NAMESPACE="${K3S_NAMESPACE:-default}"
POD_NAME="${K3S_INTERACTION_POD_NAME:-rtos-interaction}"
CONTAINER_NAME="${K3S_INTERACTION_CONTAINER_NAME:-rtos-app}"
RUNTIME_CLASS_NAME="${K3S_RUNTIME_CLASS_NAME:-micrun}"
EXPECT_MODE="${K3S_INTERACTION_EXPECT:-auto}"
ATTACH_TIMEOUT="${K3S_ATTACH_TIMEOUT:-60}"
ATTACH_INPUT_DELAY="${K3S_ATTACH_INPUT_DELAY:-6}"
ATTACH_LINE_DELAY="${K3S_ATTACH_LINE_DELAY:-3}"
ATTACH_TAIL_DELAY="${K3S_ATTACH_TAIL_DELAY:-6}"
ATTACH_INPUT="${K3S_ATTACH_INPUT:-$'\nhelp\nuname\n'}"
KEEP_POD="${K3S_INTERACTION_KEEP_POD:-false}"
IMPORT_IMAGES="${K3S_INTERACTION_IMPORT_IMAGES:-true}"
HOST_NETWORK="${K3S_INTERACTION_HOST_NETWORK:-true}"
AUTO_CLOSE_TIMEOUT="${K3S_AUTO_CLOSE_TIMEOUT:-0}"
POD_TTY="${K3S_INTERACTION_TTY:-false}"
NODE_SELECTOR="${K3S_INTERACTION_NODE_SELECTOR:-}"
POD_WAIT_SECONDS="${K3S_POD_WAIT_SECONDS:-120}"
POD_DELETE_TIMEOUT="${K3S_POD_DELETE_TIMEOUT:-60}"
EDGE_TASK_WAIT_SECONDS="${K3S_EDGE_TASK_WAIT_SECONDS:-60}"
EDGE_CLEANUP_WAIT_SECONDS="${K3S_EDGE_CLEANUP_WAIT_SECONDS:-60}"
EDGE_CONTAINERD_ADDR="${K3S_EDGE_CONTAINERD_ADDR:-/run/k3s/containerd/containerd.sock}"
EDGE_CONTAINERD_NS="${K3S_EDGE_CONTAINERD_NS:-k8s.io}"
EDGE_CTR_BIN="${K3S_EDGE_CTR_BIN:-$K3S_BIN}"
EDGE_CTR_SUBCOMMAND="${K3S_EDGE_CTR_SUBCOMMAND-ctr}"
FORCE_DELETE_PODS="${K3S_FORCE_DELETE_PODS:-false}"
EDGE_DELETE_FALLBACK="${K3S_INTERACTION_EDGE_DELETE_FALLBACK:-true}"
PAUSE_IMAGE="${K3S_PAUSE_IMAGE:-rancher/mirrored-pause:3.6}"
PAUSE_IMAGE_CANONICAL="${K3S_PAUSE_IMAGE_CANONICAL:-docker.io/rancher/mirrored-pause:3.6}"
SOURCE_IMAGE_REF="${K3S_SOURCE_IMAGE_REF:-docker.io/local/mica-uniproton-app:xen-arm64-0.1}"
CLOUD_KUBECTL_BIN="${K3S_CLOUD_KUBECTL_BIN:-kubectl}"
CLOUD_KUBECTL_SUBCOMMAND="${K3S_CLOUD_KUBECTL_SUBCOMMAND-kubectl}"
LOCAL_KUBECTL_BIN="${K3S_LOCAL_KUBECTL_BIN:-${K3S_LOCAL_SERVER_BIN:-kubectl}}"
LOCAL_KUBECTL_SUBCOMMAND="${K3S_LOCAL_KUBECTL_SUBCOMMAND-kubectl}"
LOCAL_KUBECONFIG="${K3S_LOCAL_KUBECONFIG:-}"

CONTAINER_ID=""
LOCAL_ATTACH_SCRIPT=""
POD_CLEANUP_DONE="false"
DELETE_USED_FORCE="false"

cleanup_local() {
    [ -n "$LOCAL_ATTACH_SCRIPT" ] && rm -f "$LOCAL_ATTACH_SCRIPT"
    return 0
}

cleanup_pod() {
    [ "$KEEP_POD" = "true" ] && return 0
    [ "$POD_CLEANUP_DONE" = "true" ] && return 0
    delete_test_pod >/dev/null 2>&1 || true
}

cleanup() {
    cleanup_pod
    cleanup_local
}
trap cleanup EXIT

detect_mode() {
    if [ "$MODE" != "auto" ]; then
        printf '%s\n' "$MODE"
        return 0
    fi

    if command -v docker >/dev/null 2>&1 &&
       [ "$(docker inspect -f '{{.State.Running}}' "$K3S_CLOUD_SERVER_CONTAINER" 2>/dev/null || true)" = "true" ]; then
        printf '%s\n' "cloud"
        return 0
    fi

    if [ -n "$LOCAL_KUBECONFIG" ] && [ -f "$LOCAL_KUBECONFIG" ]; then
        printf '%s\n' "local"
        return 0
    fi

    printf '%s\n' "edge"
}

shell_quote() {
    printf '%q' "$1"
}

local_kubectl_base() {
    local command

    command="$(shell_quote "$LOCAL_KUBECTL_BIN")"
    if [ -n "$LOCAL_KUBECTL_SUBCOMMAND" ]; then
        command="$command $(shell_quote "$LOCAL_KUBECTL_SUBCOMMAND")"
    fi
    if [ -n "$LOCAL_KUBECONFIG" ]; then
        command="$command --kubeconfig $(shell_quote "$LOCAL_KUBECONFIG")"
    fi

    printf '%s\n' "$command"
}

cloud_kubectl_base() {
    local command

    command="$(shell_quote "$CLOUD_KUBECTL_BIN")"
    if [ -n "$CLOUD_KUBECTL_SUBCOMMAND" ]; then
        command="$command $(shell_quote "$CLOUD_KUBECTL_SUBCOMMAND")"
    fi

    printf '%s\n' "$command"
}

kubectl_sh() {
    local command="$1"

    case "$MODE" in
        cloud)
            docker exec "$K3S_CLOUD_SERVER_CONTAINER" sh -lc "$(cloud_kubectl_base) $command"
            ;;
        local)
            bash -lc "$(local_kubectl_base) $command"
            ;;
        edge)
            remote_output "$REMOTE_HOST" "$(shell_quote "$K3S_BIN") kubectl $command"
            ;;
        *)
            log_error "invalid K3S_INTERACTION_MODE: $MODE"
            return 1
            ;;
    esac
}

kubectl_apply_manifest() {
    local tmp
    local remote_tmp

    case "$MODE" in
        cloud)
            if [ -n "$CLOUD_KUBECTL_SUBCOMMAND" ]; then
                docker exec -i "$K3S_CLOUD_SERVER_CONTAINER" \
                    "$CLOUD_KUBECTL_BIN" "$CLOUD_KUBECTL_SUBCOMMAND" apply -f -
            else
                docker exec -i "$K3S_CLOUD_SERVER_CONTAINER" \
                    "$CLOUD_KUBECTL_BIN" apply -f -
            fi
            ;;
        local)
            bash -lc "$(local_kubectl_base) apply -f -"
            ;;
        edge)
            tmp="$(mktemp /tmp/micrun-k3s-manifest.XXXXXX.yaml)"
            cat >"$tmp"
            remote_tmp="/tmp/$(basename "$tmp")"
            copy_to_remote "$tmp" "$REMOTE_HOST" "$remote_tmp"
            rm -f "$tmp"
            remote_output "$REMOTE_HOST" "$(shell_quote "$K3S_BIN") kubectl apply -f '$remote_tmp'"
            remote "$REMOTE_HOST" "rm -f '$remote_tmp'" >/dev/null 2>&1 || true
            ;;
        *)
            log_error "invalid K3S_INTERACTION_MODE: $MODE"
            return 1
            ;;
    esac
}

delete_test_pod() {
    DELETE_USED_FORCE="false"

    if kubectl_sh "delete pod '$POD_NAME' -n '$NAMESPACE' --ignore-not-found=true --wait=true --timeout=${POD_DELETE_TIMEOUT}s" >/dev/null 2>&1; then
        return 0
    fi

    if [ "$FORCE_DELETE_PODS" = "true" ]; then
        DELETE_USED_FORCE="true"
        kubectl_sh "delete pod '$POD_NAME' -n '$NAMESPACE' --force --grace-period=0 --ignore-not-found=true" >/dev/null 2>&1
        return $?
    fi

    return 1
}

cleanup_edge_pod_runtime_objects() {
    remote "$REMOTE_HOST" "
        set +e
        ids=\"\"
        for id in \$(ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' containers ls -q); do
            if ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' containers info \"\$id\" 2>/dev/null | grep -Fq '\"io.kubernetes.pod.name\": \"$POD_NAME\"'; then
                ids=\"\$ids \$id\"
            fi
        done
        for id in \$ids; do
            if command -v mica >/dev/null 2>&1; then
                mica stop \"\$id\" 2>/dev/null || true
                mica rm \"\$id\" 2>/dev/null || true
            fi
            ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' tasks kill -s 9 \"\$id\" 2>/dev/null || true
            ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' tasks delete --force \"\$id\" 2>/dev/null || true
            xl destroy \"\$id\" 2>/dev/null || true
            ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' containers delete \"\$id\" 2>/dev/null || true
            rm -rf \"/run/micrun/containers/\$id\" \"/run/micrun/runtime/container/\$id\" \"/run/micrun/runtime/sandbox/\$id\" 2>/dev/null || true
        done
    " >/dev/null
}

wait_for_kubectl() {
    local command="$1"
    local retries="${2:-60}"
    local sleep_seconds="${3:-2}"
    local i

    for i in $(seq 1 "$retries"); do
        if kubectl_sh "$command" >/dev/null 2>&1; then
            return 0
        fi
        sleep "$sleep_seconds"
    done

    return 1
}

wait_for_remote_edge() {
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

sanitize_attach_output() {
    tr -d '\000' | tr '\r' '\n' |
        sed -E 's/\x1B\[[0-9;?]*[[:alpha:]]//g'
}

has_shell_markers() {
    grep -Eq 'openEuler UniProton #|support shell commond|UniProton [0-9]'
}

has_hello_markers() {
    grep -Eq 'Hello, (UniProton|Zephyr)!'
}

attach_input_requests_uname() {
    printf '%s\n' "$ATTACH_INPUT" | grep -Eq '(^|[[:space:]])uname([[:space:]]|$)'
}

has_uname_output() {
    grep -Eq 'UniProton [0-9]|Zephyr'
}

validate_shell_output() {
    local output="$1"

    printf '%s\n' "$output" | has_shell_markers || return 1
    if attach_input_requests_uname; then
        printf '%s\n' "$output" | has_uname_output
        return
    fi
}

validate_attach_output() {
    local output="$1"

    case "$EXPECT_MODE" in
        shell)
            validate_shell_output "$output"
            ;;
        hello)
            printf '%s\n' "$output" | has_hello_markers
            ;;
        auto|any)
            validate_shell_output "$output" ||
                printf '%s\n' "$output" | has_hello_markers ||
                { ! attach_input_requests_uname && printf '%s\n' "$output" | grep -Eq 'help|uname|Available commands'; }
            ;;
        *)
            log_error "invalid K3S_INTERACTION_EXPECT: $EXPECT_MODE"
            return 1
            ;;
    esac
}

ensure_mode_ready() {
    case "$MODE" in
        cloud)
            command -v docker >/dev/null 2>&1 || {
                log_error "docker command not found for cloud interaction mode"
                return 1
            }
            docker inspect "$K3S_CLOUD_SERVER_CONTAINER" >/dev/null 2>&1 || {
                log_error "cloud K3s server container not found: $K3S_CLOUD_SERVER_CONTAINER"
                return 1
            }
            ;;
        local)
            [ -x "$LOCAL_KUBECTL_BIN" ] || command -v "$LOCAL_KUBECTL_BIN" >/dev/null 2>&1 || {
                log_error "local kubectl binary not found: $LOCAL_KUBECTL_BIN"
                return 1
            }
            [ -n "$LOCAL_KUBECONFIG" ] && [ -f "$LOCAL_KUBECONFIG" ] || {
                log_error "local kubeconfig not found: ${LOCAL_KUBECONFIG:-<unset>}"
                return 1
            }
            kubectl_sh "get nodes >/dev/null" || {
                log_error "local K3s control plane is not reachable"
                return 1
            }
            ;;
        edge)
            remote "$REMOTE_HOST" "test -x '$K3S_BIN'" >/dev/null 2>&1 || {
                log_error "k3s binary not found on edge node: $K3S_BIN"
                return 1
            }
            ;;
    esac
}

ensure_edge_images() {
    [ "$IMPORT_IMAGES" = "true" ] || return 0

    remote "$REMOTE_HOST" "test -f '$K3S_PAUSE_TAR'" >/dev/null 2>&1 || {
        log_error "pause image tar not found on edge node: $K3S_PAUSE_TAR"
        return 1
    }
    remote "$REMOTE_HOST" "test -f '$K3S_IMAGE_TAR'" >/dev/null 2>&1 || {
        log_error "RTOS image tar not found on edge node: $K3S_IMAGE_TAR"
        return 1
    }

    remote "$REMOTE_HOST" "
        set -eu
        edge_ctr() {
            if [ -n '$EDGE_CTR_SUBCOMMAND' ]; then
                '$EDGE_CTR_BIN' '$EDGE_CTR_SUBCOMMAND' -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' \"\$@\"
            else
                '$EDGE_CTR_BIN' -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' \"\$@\"
            fi
        }
        edge_ctr images import '$K3S_PAUSE_TAR' >/dev/null
        edge_ctr images import '$K3S_IMAGE_TAR' >/dev/null
        edge_ctr images tag '$PAUSE_IMAGE' '$PAUSE_IMAGE_CANONICAL' >/dev/null 2>&1 || true
        edge_ctr images tag '$PAUSE_IMAGE_CANONICAL' '$PAUSE_IMAGE' >/dev/null 2>&1 || true
        edge_ctr images tag '$SOURCE_IMAGE_REF' '$TEST_IMAGE' >/dev/null 2>&1 || true
        edge_ctr images ls -q | grep -Fx '$PAUSE_IMAGE_CANONICAL' >/dev/null
        edge_ctr images ls -q | grep -Fx '$TEST_IMAGE' >/dev/null
    " >/dev/null
}

selector_block() {
    local selector="$NODE_SELECTOR"
    local key
    local value

    if [ -z "$selector" ] && { [ "$MODE" = "cloud" ] || [ "$MODE" = "local" ]; }; then
        selector="kubernetes.io/hostname=${K3S_EDGE_NODE_NAME}"
    fi

    [ -n "$selector" ] || return 0
    key="${selector%%=*}"
    value="${selector#*=}"

    cat <<EOF
  nodeSelector:
    $key: $value
EOF
}

toleration_block() {
    [ "${K3S_TOLERATE_NOTREADY:-false}" = "true" ] || return 0

    cat <<'EOF'
  tolerations:
  - key: node.kubernetes.io/not-ready
    operator: Exists
    effect: NoSchedule
EOF
}

host_network_line() {
    [ "$HOST_NETWORK" = "true" ] && printf '  hostNetwork: true\n'
}

ensure_namespace() {
    [ "$NAMESPACE" = "default" ] && return 0

    kubectl_sh "get namespace '$NAMESPACE' >/dev/null 2>&1 || create namespace '$NAMESPACE' >/dev/null"
}

deploy_pod() {
    delete_test_pod >/dev/null 2>&1 || true
    wait_for_kubectl "get pod '$POD_NAME' -n '$NAMESPACE' >/dev/null 2>&1 && exit 1 || exit 0" 30 1 || {
        log_error "old RTOS pod was not deleted: $NAMESPACE/$POD_NAME"
        return 1
    }

    ensure_namespace

    {
        cat <<EOF
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
  namespace: $NAMESPACE
  labels:
    app: micrun-k3s-interaction
  annotations:
    org.openeuler.micrun.container.auto_close_timeout: "$AUTO_CLOSE_TIMEOUT"
spec:
$(host_network_line)
  runtimeClassName: $RUNTIME_CLASS_NAME
$(selector_block)
$(toleration_block)
  containers:
  - name: $CONTAINER_NAME
    image: $TEST_IMAGE
    imagePullPolicy: IfNotPresent
    command: ["$K3S_CONTAINER_COMMAND"]
    tty: $POD_TTY
    stdin: true
EOF
    } | kubectl_apply_manifest >/dev/null
}

wait_for_pod_running() {
    wait_for_kubectl "get pod '$POD_NAME' -n '$NAMESPACE' -o jsonpath='{.status.phase}' 2>/dev/null | grep -qx Running" \
        "$((POD_WAIT_SECONDS / 2))" 2 || {
        log_error "RTOS pod did not reach Running"
        kubectl_sh "describe pod '$POD_NAME' -n '$NAMESPACE'" || true
        return 1
    }
}

make_attach_script() {
    LOCAL_ATTACH_SCRIPT="$(mktemp /tmp/micrun-k3s-attach.XXXXXX.sh)"

    {
        cat <<'EOS'
#!/bin/sh
set -eu

if [ -n "${KUBECTL_SUBCOMMAND:-}" ]; then
    set -- "$KUBECTL_BIN" "$KUBECTL_SUBCOMMAND"
else
    set -- "$KUBECTL_BIN"
fi
if [ -n "${KUBECTL_KUBECONFIG:-}" ]; then
    set -- "$@" --kubeconfig "$KUBECTL_KUBECONFIG"
fi
set -- "$@" attach -n "$K3S_NAMESPACE" -i "$K3S_POD_NAME" -c "$K3S_CONTAINER_NAME"

(
    sleep "$K3S_ATTACH_INPUT_DELAY"
    while IFS= read -r line || [ -n "$line" ]; do
        printf '%s\n' "$line"
        sleep "$K3S_ATTACH_LINE_DELAY"
    done <<'K3S_ATTACH_INPUT_EOF'
EOS
        printf '%s' "$ATTACH_INPUT"
        cat <<'EOS'
K3S_ATTACH_INPUT_EOF
    sleep "$K3S_ATTACH_TAIL_DELAY"
) | timeout "$K3S_ATTACH_TIMEOUT" "$@" 2>&1 || true
EOS
    } >"$LOCAL_ATTACH_SCRIPT"
}

run_kubectl_attach() {
    local remote_script

    make_attach_script

    case "$MODE" in
        cloud)
            docker exec -i \
                -e KUBECTL_BIN="$CLOUD_KUBECTL_BIN" \
                -e KUBECTL_SUBCOMMAND="$CLOUD_KUBECTL_SUBCOMMAND" \
                -e KUBECTL_KUBECONFIG="" \
                -e K3S_NAMESPACE="$NAMESPACE" \
                -e K3S_POD_NAME="$POD_NAME" \
                -e K3S_CONTAINER_NAME="$CONTAINER_NAME" \
                -e K3S_ATTACH_TIMEOUT="$ATTACH_TIMEOUT" \
                -e K3S_ATTACH_INPUT_DELAY="$ATTACH_INPUT_DELAY" \
                -e K3S_ATTACH_LINE_DELAY="$ATTACH_LINE_DELAY" \
                -e K3S_ATTACH_TAIL_DELAY="$ATTACH_TAIL_DELAY" \
                "$K3S_CLOUD_SERVER_CONTAINER" sh -s <"$LOCAL_ATTACH_SCRIPT"
            ;;
        local)
            KUBECTL_BIN="$LOCAL_KUBECTL_BIN" \
                KUBECTL_SUBCOMMAND="$LOCAL_KUBECTL_SUBCOMMAND" \
                KUBECTL_KUBECONFIG="$LOCAL_KUBECONFIG" \
                K3S_NAMESPACE="$NAMESPACE" \
                K3S_POD_NAME="$POD_NAME" \
                K3S_CONTAINER_NAME="$CONTAINER_NAME" \
                K3S_ATTACH_TIMEOUT="$ATTACH_TIMEOUT" \
                K3S_ATTACH_INPUT_DELAY="$ATTACH_INPUT_DELAY" \
                K3S_ATTACH_LINE_DELAY="$ATTACH_LINE_DELAY" \
                K3S_ATTACH_TAIL_DELAY="$ATTACH_TAIL_DELAY" \
                sh "$LOCAL_ATTACH_SCRIPT"
            ;;
        edge)
            remote_script="/tmp/$(basename "$LOCAL_ATTACH_SCRIPT")"
            copy_to_remote "$LOCAL_ATTACH_SCRIPT" "$REMOTE_HOST" "$remote_script"
            remote_output "$REMOTE_HOST" "
                KUBECTL_BIN=$(shell_quote "$K3S_BIN") \
                KUBECTL_SUBCOMMAND=kubectl \
                KUBECTL_KUBECONFIG= \
                K3S_NAMESPACE=$(shell_quote "$NAMESPACE") \
                K3S_POD_NAME=$(shell_quote "$POD_NAME") \
                K3S_CONTAINER_NAME=$(shell_quote "$CONTAINER_NAME") \
                K3S_ATTACH_TIMEOUT=$(shell_quote "$ATTACH_TIMEOUT") \
                K3S_ATTACH_INPUT_DELAY=$(shell_quote "$ATTACH_INPUT_DELAY") \
                K3S_ATTACH_LINE_DELAY=$(shell_quote "$ATTACH_LINE_DELAY") \
                K3S_ATTACH_TAIL_DELAY=$(shell_quote "$ATTACH_TAIL_DELAY") \
                sh '$remote_script'
            "
            remote "$REMOTE_HOST" "rm -f '$remote_script'" >/dev/null 2>&1 || true
            ;;
    esac
}

verify_interaction() {
    local raw
    local clean

    raw="$(run_kubectl_attach 2>&1 || true)"
    clean="$(printf '%s\n' "$raw" | sanitize_attach_output)"

    if validate_attach_output "$clean"; then
        printf '%s\n' "$clean" | tail -n 40
        return 0
    fi

    log_error "kubectl attach did not expose expected RTOS interaction markers"
    printf '%s\n' "$clean" | tail -n 80
    return 1
}

resolve_container_id() {
    CONTAINER_ID="$(
        kubectl_sh "get pod '$POD_NAME' -n '$NAMESPACE' -o jsonpath='{.status.containerStatuses[0].containerID}'" \
            | sed 's#containerd://##' | tr -d '[:space:]'
    )"

    [ -n "$CONTAINER_ID" ] || {
        log_error "unable to resolve pod container ID"
        return 1
    }
}

verify_edge_runtime_objects() {
    resolve_container_id

    wait_for_remote_edge "ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' tasks ls | awk -v id='$CONTAINER_ID' '\$1 == id && \$3 == \"RUNNING\" {found=1} END {exit found ? 0 : 1}'" \
        "$((EDGE_TASK_WAIT_SECONDS / 2))" 2 || {
        log_error "edge running containerd task not found: $CONTAINER_ID"
        remote_output "$REMOTE_HOST" "ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' tasks ls" || true
        return 1
    }

    wait_for_remote_edge "xl list | awk 'NR>2 && \$1 == \"$CONTAINER_ID\" {found=1} END {exit found ? 0 : 1}'" \
        "$((EDGE_TASK_WAIT_SECONDS / 2))" 2 || {
        log_error "edge Xen domain not found: $CONTAINER_ID"
        remote_output "$REMOTE_HOST" "xl list" || true
        return 1
    }
}

verify_delete_cleanup() {
    [ "$KEEP_POD" = "true" ] && return 0
    [ -n "$CONTAINER_ID" ] || return 0

    if delete_test_pod >/dev/null; then
        POD_CLEANUP_DONE="true"
        if [ "$DELETE_USED_FORCE" = "true" ]; then
            log_info "Kubernetes graceful delete timed out; force delete accepted for $NAMESPACE/$POD_NAME"
        fi
    elif [ "$EDGE_DELETE_FALLBACK" = "true" ]; then
        log_info "Kubernetes delete did not complete; cleaning edge runtime objects for $NAMESPACE/$POD_NAME"
        cleanup_edge_pod_runtime_objects
        kubectl_sh "delete pod '$POD_NAME' -n '$NAMESPACE' --force --grace-period=0 --ignore-not-found=true" >/dev/null 2>&1 || true
        POD_CLEANUP_DONE="true"
    else
        log_error "RTOS pod was not deleted cleanly: $NAMESPACE/$POD_NAME"
        return 1
    fi

    if ! wait_for_remote_edge "ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' tasks ls | awk -v id='$CONTAINER_ID' '\$1 == id {found=1} END {exit found ? 1 : 0}'" \
        "$((EDGE_CLEANUP_WAIT_SECONDS / 2))" 2; then
        if [ "$EDGE_DELETE_FALLBACK" = "true" ]; then
            log_info "edge task still exists after pod deletion; cleaning runtime objects for $NAMESPACE/$POD_NAME"
            cleanup_edge_pod_runtime_objects
        else
            log_error "edge containerd task still exists after pod deletion: $CONTAINER_ID"
            remote_output "$REMOTE_HOST" "ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' tasks ls" || true
            return 1
        fi
    fi

    wait_for_remote_edge "ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' tasks ls | awk -v id='$CONTAINER_ID' '\$1 == id {found=1} END {exit found ? 1 : 0}'" \
        "$((EDGE_CLEANUP_WAIT_SECONDS / 2))" 2 || {
        log_error "edge containerd task still exists after fallback cleanup: $CONTAINER_ID"
        remote_output "$REMOTE_HOST" "ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' tasks ls" || true
        return 1
    }

    if ! wait_for_remote_edge "xl list | awk -v id='$CONTAINER_ID' 'NR>2 && \$1 == id {found=1} END {exit found ? 1 : 0}'" \
        "$((EDGE_CLEANUP_WAIT_SECONDS / 2))" 2; then
        if [ "$EDGE_DELETE_FALLBACK" = "true" ]; then
            log_info "edge Xen domain still exists after pod deletion; cleaning runtime objects for $NAMESPACE/$POD_NAME"
            cleanup_edge_pod_runtime_objects
        else
            log_error "edge Xen domain still exists after pod deletion: $CONTAINER_ID"
            remote_output "$REMOTE_HOST" "xl list" || true
            return 1
        fi
    fi

    wait_for_remote_edge "xl list | awk -v id='$CONTAINER_ID' 'NR>2 && \$1 == id {found=1} END {exit found ? 1 : 0}'" \
        "$((EDGE_CLEANUP_WAIT_SECONDS / 2))" 2 || {
        log_error "edge Xen domain still exists after fallback cleanup: $CONTAINER_ID"
        remote_output "$REMOTE_HOST" "xl list" || true
        return 1
    }

    remote_output "$REMOTE_HOST" "ctr -a '$EDGE_CONTAINERD_ADDR' -n '$EDGE_CONTAINERD_NS' containers ls" >/dev/null || true
}

main() {
    MODE="$(detect_mode)"
    log_test "K3s MicRun interaction end-to-end (${MODE})"

    ensure_mode_ready
    ensure_edge_images
    deploy_pod
    wait_for_pod_running
    verify_edge_runtime_objects
    verify_interaction
    verify_delete_cleanup

    log_success "K3s RuntimeClass, kubectl attach, edge task, Xen domain, and cleanup validated"
}

main "$@"
