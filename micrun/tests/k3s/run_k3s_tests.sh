#!/bin/bash
# MicRun K3s 云化测试套件入口
# 使用: ./run_k3s_tests.sh [scenario]
#
# scenario 用语义名（无参数 = 跑通用集）：
#   preflight        环境预检
#   runtimeclass     RuntimeClass 创建
#   pod-lifecycle    Pod 启动/停止
#   deployment       Deployment 扩缩容
#   pod-logs         Pod 日志获取
#   resource-limits  资源限制
#   cpu-pinning      vCPU pinning 注解组合
#   multi-node       多节点部署
#   self-healing     故障恢复
#   interaction      RuntimeClass Pod 交互与清理（kubectl attach）
#   ota              Deployment OTA 滚动升级
# 旧的 K3S-00x 编号仍可用（兼容别名，见 canonical_test_id）。
#
# 两个环境要点：
#   - 边侧 k3s 为 agent-only 构建时没有 kubectl 子命令，导出
#     K3S_LOCAL_KUBECONFIG 后套件自动回退为宿主 kubectl 直连。
#   - 无 CNI 部署形态（控制面节点 NotReady）下 Pod 类用例需要
#     K3S_HOST_NETWORK=true 与 K3S_TOLERATE_NOTREADY=true。


# 非交互加固：桌面会话可能全局设置 ksshaskpass（SSH_ASKPASS_REQUIRE=prefer），
# 任何 ssh/git 凭据路径都会弹 GUI 密码窗并挂死自动化；入口处统一摘除
unset SSH_ASKPASS SUDO_ASKPASS GIT_ASKPASS
export SSH_ASKPASS_REQUIRE=never
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../lib/test_utils.sh"
source "${SCRIPT_DIR}/../test-env.sh"

# 测试配置
CATEGORY="K3s 云化"

# 结果存储
declare -a TEST_NAMES=()
declare -a TEST_RESULTS=()
declare -a TEST_DETAILS=()
declare -a TEST_TIMES=()

# 在 K3s 控制面执行一段含 kubectl 调用的 shell 片段。默认经 SSH 到边侧
# 节点执行；当边侧 k3s 构建不含 kubectl 子命令（agent-only 交付镜像）而
# 宿主导出了 K3S_LOCAL_KUBECONFIG 时，切换为宿主直连（K3S_KUBECTL_BIN
# 指向 kubectl --kubeconfig），两个后端执行同一段片段。
run_kubectl_snippet() {
    local node="$1"
    local snippet="$2"
    if [ "${K3S_KUBECTL_LOCAL:-}" = "true" ] && [ -n "${K3S_LOCAL_KUBECONFIG:-}" ] \
        && [ -f "${K3S_LOCAL_KUBECONFIG}" ]; then
        bash -c "
            export PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:\$PATH
            $snippet
        "
    else
        remote "$node" "
            export PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:\$PATH
            $snippet
        " 2>/dev/null
    fi
}

remote_kubectl() {
    local node="$1"
    local args="$2"
    run_kubectl_snippet "$node" "
        if command -v ${K3S_KUBECTL_BIN%% *} >/dev/null 2>&1; then
            ${K3S_KUBECTL_BIN} $args
        elif command -v k3s >/dev/null 2>&1; then
            k3s kubectl $args
        else
            exit 127
        fi
    " 2>/dev/null
}

remote_ctr() {
    local node="$1"
    local args="$2"
    remote "$node" "
        export PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:\$PATH
        if command -v ${K3S_CTR_BIN%% *} >/dev/null 2>&1; then
            ${K3S_CTR_BIN} $args
        elif command -v ctr >/dev/null 2>&1; then
            ctr $args
        else
            exit 127
        fi
    " 2>/dev/null
}

# 等待 Pod 进入 Running/Succeeded：慢环境下固定 sleep 不可靠（RTOS
# domain 启动为秒级，负载下常超 10s），按 pod-logs 用例同款轮询
wait_pod_running() {
    local node="$1" pod="$2" timeout_s="${3:-60}"
    local waited=0
    while [ "$waited" -lt "$timeout_s" ]; do
        if remote_kubectl "$node" "get pod $pod -o jsonpath='{.status.phase}' 2>/dev/null" | grep -qE "Running|Succeeded"; then
            return 0
        fi
        sleep 5
        waited=$((waited + 5))
    done
    return 1
}

# 等待 Deployment readyReplicas 达到期望值（扩容后副本逐个拉起，
# 3 副本串行启动可能远超 10s）
wait_deployment_ready() {
    local node="$1" deploy="$2" want="$3" timeout_s="${4:-90}"
    local waited=0 replicas=""
    while [ "$waited" -lt "$timeout_s" ]; do
        replicas=$(remote_kubectl "$node" "get deployment $deploy -o jsonpath='{.status.readyReplicas}' 2>/dev/null")
        if [ "$replicas" = "$want" ]; then
            return 0
        fi
        sleep 5
        waited=$((waited + 5))
    done
    return 1
}

pod_spec_overrides() {
    local lines=()

    # RTOS Pod 只能落在跑 micrun runtime 的边侧节点：无约束时调度器可能
    # 把它派给控制面节点（micrun 不在那），钉 nodeName 消除歧义
    if [ -n "${K3S_EDGE_NODE_NAME:-}" ]; then
        lines+=("  nodeName: ${K3S_EDGE_NODE_NAME}")
    fi

    if [ "${K3S_HOST_NETWORK}" = "true" ]; then
        lines+=("  hostNetwork: true")
    fi

    if [ "${K3S_TOLERATE_NOTREADY}" = "true" ]; then
        lines+=("  tolerations:")
        lines+=("  - key: node.kubernetes.io/not-ready")
        lines+=("    operator: Exists")
        lines+=("    effect: NoSchedule")
    fi

    printf '%s\n' "${lines[@]}"
}

deployment_spec_overrides() {
    local lines=()

    if [ -n "${K3S_EDGE_NODE_NAME:-}" ]; then
        lines+=("      nodeName: ${K3S_EDGE_NODE_NAME}")
    fi

    if [ "${K3S_HOST_NETWORK}" = "true" ]; then
        lines+=("      hostNetwork: true")
    fi

    if [ "${K3S_TOLERATE_NOTREADY}" = "true" ]; then
        lines+=("      tolerations:")
        lines+=("      - key: node.kubernetes.io/not-ready")
        lines+=("        operator: Exists")
        lines+=("        effect: NoSchedule")
    fi

    printf '%s\n' "${lines[@]}"
}

# 场景选择器使用语义名（preflight / runtimeclass / pod-lifecycle /
# deployment / pod-logs / resource-limits / multi-node / self-healing /
# interaction / ota）。旧的 K3S-00x 编号仍被接受为兼容别名，进入 main 时
# 统一归一化为语义名。
canonical_test_id() {
    case "$1" in
        K3S-000) echo "preflight" ;;
        K3S-001) echo "runtimeclass" ;;
        K3S-002) echo "pod-lifecycle" ;;
        K3S-003) echo "deployment" ;;
        K3S-004) echo "pod-logs" ;;
        K3S-005) echo "resource-limits" ;;
        K3S-006) echo "multi-node" ;;
        K3S-007) echo "self-healing" ;;
        K3S-008) echo "interaction" ;;
        K3S-009) echo "ota" ;;
        *) echo "$1" ;;
    esac
}

interaction_uses_direct_control_plane() {
    local test_id="$1"
    local mode="${K3S_INTERACTION_MODE:-auto}"

    if [ "$test_id" = "ota" ]; then
        command -v docker >/dev/null 2>&1 &&
            [ "$(docker inspect -f '{{.State.Running}}' "$K3S_CLOUD_SERVER_CONTAINER" 2>/dev/null || true)" = "true" ]
        return
    fi

    [ "$test_id" = "interaction" ] || return 1

    case "$mode" in
        cloud|local)
            return 0
            ;;
        auto)
            if command -v docker >/dev/null 2>&1 &&
                [ "$(docker inspect -f '{{.State.Running}}' "$K3S_CLOUD_SERVER_CONTAINER" 2>/dev/null || true)" = "true" ]; then
                return 0
            fi
            if [ -n "${K3S_LOCAL_KUBECONFIG:-}" ] && [ -f "$K3S_LOCAL_KUBECONFIG" ]; then
                return 0
            fi
            return 1
            ;;
        *)
            return 1
            ;;
    esac
}

# ============================================
# 辅助函数
# ============================================

record_result() {
    local name="$1"
    local result="$2"
    local details="$3"
    local time="${4:-0}"

    TEST_NAMES+=("$name")
    TEST_RESULTS+=("$result")
    TEST_DETAILS+=("$details")
    TEST_TIMES+=("$time")
}

cleanup_k3s() {
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"

    # Delete controllers before their pods, and never wait: a pod stuck
    # Terminating (e.g. sandbox never created on a CNI-less node) would
    # otherwise block `delete pod --all` indefinitely and stall the suite
    # at startup cleanup.
    remote_kubectl "$node" "delete deployment --all --ignore-not-found=true --wait=false" >/dev/null 2>&1 || true
    remote_kubectl "$node" "delete pod --all --ignore-not-found=true --force --grace-period=0" >/dev/null 2>&1 || true
}

# ============================================
# 测试用例
# ============================================

# preflight: 环境预检
test_k3s_000_preflight() {
    log_test "preflight: 环境预检"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"

    if ! remote_kubectl "$node" "version --client" >/dev/null 2>&1; then
        local end=$(date +%s)
        record_result "preflight: 环境预检" "FAIL" "kubectl/k3s kubectl 不可用" "$((end - start))"
        echo -e "$FAIL"
        return
    fi

    local node_count=$(remote_kubectl "$node" "get nodes --no-headers 2>/dev/null | wc -l" | tr -d ' ')
    # NotReady 只统计无角色的节点：无 CNI 部署形态（flannel-backend=none）
    # 下控制面节点自身 NotReady 是预期状态，不是环境问题
    local not_ready=$(remote_kubectl "$node" "get nodes --no-headers 2>/dev/null | awk '\$2 != \"Ready\" && \$3 == \"<none>\" {print \$1\":\"\$2}'")
    # 无 CNI 形态提示：控制面 NotReady 时 Pod 类用例必须带 hostNetwork 与
    # not-ready toleration，否则 sandbox 网络创建失败（loopback 插件缺失）
    local cp_notready=$(remote_kubectl "$node" "get nodes --no-headers 2>/dev/null | awk '\$2 != \"Ready\" && \$3 != \"<none>\"' | wc -l" | tr -d ' ')
    if [ "${cp_notready:-0}" -ge 1 ] && [ "${K3S_HOST_NETWORK}" != "true" ]; then
        log_info "集群含 NotReady 控制面节点（无 CNI 部署形态）：跑 Pod 类用例前请设置 K3S_HOST_NETWORK=true 与 K3S_TOLERATE_NOTREADY=true"
    fi
    local pause_check="未检查"

    if [ "${K3S_REQUIRE_PAUSE_IMAGE}" = "true" ]; then
        if remote_ctr "$node" "image ls" | grep -Fq "$K3S_PAUSE_IMAGE"; then
            pause_check="pause镜像已存在"
        else
            pause_check="pause镜像缺失"
        fi
    fi

    local details="nodes=${node_count:-0}"
    if [ -n "$not_ready" ]; then
        details="$details, notReady=$not_ready"
    fi
    details="$details, $pause_check"

    local end=$(date +%s)
    local time=$((end - start))

    if [ -z "$node_count" ] || [ "$node_count" -lt 1 ]; then
        record_result "preflight: 环境预检" "FAIL" "未发现可用节点" "$time"
        echo -e "$FAIL"
    elif [ "${K3S_REQUIRE_PAUSE_IMAGE}" = "true" ] && [ "$pause_check" = "pause镜像缺失" ]; then
        record_result "preflight: 环境预检" "FAIL" "$details" "$time"
        echo -e "$FAIL"
    elif [ -n "$not_ready" ] && [ "${K3S_SINGLE_NODE}" != "true" ]; then
        record_result "preflight: 环境预检" "FAIL" "$details" "$time"
        echo -e "$FAIL"
    else
        record_result "preflight: 环境预检" "PASS" "$details" "$time"
        echo -e "$PASS"
    fi
}

# runtimeclass: RuntimeClass 创建
test_k3s_001_runtimeclass() {
    log_test "runtimeclass: RuntimeClass 创建"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local pod_overrides
    pod_overrides="$(pod_spec_overrides)"
    local kubectl_bin="${K3S_KUBECTL_BIN}"

    # 创建 RuntimeClass
    run_kubectl_snippet "$node" "
        ${kubectl_bin} apply -f - <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
EOF
    " >/dev/null 2>&1

    # 验证 RuntimeClass 存在
    local result=$(remote_kubectl "$node" "get runtimeclass micrun --no-headers 2>/dev/null | wc -l")

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$result" -eq 1 ]; then
        record_result "runtimeclass: RuntimeClass 创建" "PASS" "RuntimeClass 已创建" "$time"
        echo -e "$PASS"
    else
        record_result "runtimeclass: RuntimeClass 创建" "FAIL" "RuntimeClass 创建失败" "$time"
        echo -e "$FAIL"
    fi
}

# pod-lifecycle: Pod 启动/停止
test_k3s_002_pod_lifecycle() {
    log_test "pod-lifecycle: Pod 启动/停止"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local pod_name="test-pod-lifecycle"

    # 创建 Pod
    local pod_overrides
    pod_overrides="$(pod_spec_overrides)"

    run_kubectl_snippet "$node" "
        ${K3S_KUBECTL_BIN} apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
spec:
$pod_overrides
  runtimeClassName: micrun
  containers:
  - name: rtos
    image: $TEST_IMAGE
    command: [\"$K3S_CONTAINER_COMMAND\"]
    tty: false
    stdin: true
EOF
    " >/dev/null 2>&1

    wait_pod_running "$node" "$pod_name" 60 || true

    # 检查 Pod 状态
    local status=$(remote_kubectl "$node" "get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null")
    local details="Pod 状态: ${status:-Unknown}"
    if [ "$status" != "Running" ] && [ "$status" != "Succeeded" ]; then
        details=$(remote_kubectl "$node" "describe pod $pod_name 2>/dev/null | tail -20")
    fi

    # 删除 Pod
    remote_kubectl "$node" "delete pod $pod_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$status" = "Running" ] || [ "$status" = "Succeeded" ]; then
        record_result "pod-lifecycle: Pod 启动/停止" "PASS" "Pod 状态: $status" "$time"
        echo -e "$PASS"
    else
        record_result "pod-lifecycle: Pod 启动/停止" "FAIL" "$details" "$time"
        echo -e "$FAIL"
    fi
}

# deployment: Deployment 扩缩容
test_k3s_003_deployment() {
    log_test "deployment: Deployment 扩缩容"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local deploy_name="test-deployment"

    # 创建 Deployment (2 副本)
    local deploy_overrides
    deploy_overrides="$(deployment_spec_overrides)"

    run_kubectl_snippet "$node" "
        ${K3S_KUBECTL_BIN} apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $deploy_name
spec:
  replicas: 2
  selector:
    matchLabels:
      app: test-rtos
  template:
    metadata:
      labels:
        app: test-rtos
    spec:
$deploy_overrides
      runtimeClassName: micrun
      containers:
      - name: rtos
        image: $TEST_IMAGE
        command: [\"$K3S_CONTAINER_COMMAND\"]
        tty: false
        stdin: true
EOF
    " >/dev/null 2>&1

    wait_deployment_ready "$node" "$deploy_name" 2 90 || true
    local replicas=$(remote_kubectl "$node" "get deployment $deploy_name -o jsonpath='{.status.readyReplicas}' 2>/dev/null")

    # 扩容到 3 副本
    remote_kubectl "$node" "scale deployment $deploy_name --replicas=3" >/dev/null 2>&1 || true

    wait_deployment_ready "$node" "$deploy_name" 3 90 || true
    local scaled_replicas=$(remote_kubectl "$node" "get deployment $deploy_name -o jsonpath='{.status.readyReplicas}' 2>/dev/null")

    # 清理
    remote_kubectl "$node" "delete deployment $deploy_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$replicas" = "2" ] && [ "$scaled_replicas" = "3" ]; then
        record_result "deployment: Deployment 扩缩容" "PASS" "2→3 副本成功" "$time"
        echo -e "$PASS"
    else
        record_result "deployment: Deployment 扩缩容" "FAIL" "副本数: $replicas → $scaled_replicas" "$time"
        echo -e "$FAIL"
    fi
}

# pod-logs: Pod 日志获取
test_k3s_004_pod_logs() {
    log_test "pod-logs: Pod 日志获取"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local pod_name="test-logs"

    # 创建 Pod
    local pod_overrides
    pod_overrides="$(pod_spec_overrides)"

    run_kubectl_snippet "$node" "
        ${K3S_KUBECTL_BIN} apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
spec:
$pod_overrides
  runtimeClassName: micrun
  containers:
  - name: rtos
    image: $TEST_IMAGE
    command: [\"$K3S_CONTAINER_COMMAND\"]
    tty: false
    stdin: true
EOF
    " >/dev/null 2>&1

    # 等待 Pod Running：慢环境下固定 sleep 不可靠（Pod 未启动时
    # kubectl logs 直接报错），按 lifecycle 用例同款轮询
    local waited=0
    while [ "$waited" -lt 60 ]; do
        if remote_kubectl "$node" "get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null" | grep -q Running; then
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done

    # UniProton 交付固件不产生启动期日志（用户通路是 kubectl attach 交互），
    # 因此 logs 断言验证的是日志通道可用（命令成功），不强制非空输出
    local logs_rc=0
    remote_kubectl "$node" "logs $pod_name >/dev/null 2>&1" || logs_rc=$?

    # 清理
    remote_kubectl "$node" "delete pod $pod_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$logs_rc" = "0" ]; then
        record_result "pod-logs: Pod 日志获取" "PASS" "logs 通道可用（UniProton 固件无启动日志）" "$time"
        echo -e "$PASS"
    else
        record_result "pod-logs: Pod 日志获取" "FAIL" "logs 命令失败 rc=$logs_rc" "$time"
        echo -e "$FAIL"
    fi
}

# resource-limits: 资源限制
test_k3s_005_resource_limits() {
    log_test "resource-limits: 资源限制"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local pod_name="test-resources"

    # 创建带资源限制的 Pod
    local pod_overrides
    pod_overrides="$(pod_spec_overrides)"

    run_kubectl_snippet "$node" "
        ${K3S_KUBECTL_BIN} apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
spec:
$pod_overrides
  runtimeClassName: micrun
  containers:
  - name: rtos
    image: $TEST_IMAGE
    command: [\"$K3S_CONTAINER_COMMAND\"]
    tty: false
    stdin: true
    resources:
      limits:
        memory: "128Mi"
        cpu: "500m"
EOF
    " >/dev/null 2>&1

    wait_pod_running "$node" "$pod_name" 60 || true

    # 检查 Pod 状态
    local status=$(remote_kubectl "$node" "get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null")

    # 断言 Pod spec 确实携带了资源限制（生效语义由 features 用例 1/2/8
    # 的 xl/domain 级断言背书，本用例只看调度面）；必须在删除前读取
    local limits=""
    if [ "$status" = "Running" ] || [ "$status" = "Succeeded" ]; then
        limits=$(remote_kubectl "$node" "get pod $pod_name -o jsonpath='{.spec.containers[0].resources.limits.memory}' 2>/dev/null")
    fi

    # 清理
    remote_kubectl "$node" "delete pod $pod_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end2=$(date +%s)
    local time2=$((end2 - start))

    if [ "$status" = "Running" ] || [ "$status" = "Succeeded" ]; then
        if [ "$limits" = "128Mi" ]; then
            record_result "resource-limits: 资源限制" "PASS" "Pod 运行且 spec 携带 limits（memory=128Mi；生效断言见 features 1/2/8）" "$time2"
            echo -e "$PASS"
        else
            record_result "resource-limits: 资源限制" "FAIL" "Pod 运行但 spec limits.memory=${limits:-<empty>}" "$time2"
            echo -e "$FAIL"
        fi
    else
        record_result "resource-limits: 资源限制" "FAIL" "Pod 状态: ${status:-Unknown}" "$time2"
        echo -e "$FAIL"
    fi
}

# cpu-pinning: vCPU pinning 注解（回归看护：infra/stopped 容器不得被 pin 毒化）
test_k3s_010_cpu_pinning() {
    log_test "cpu-pinning: vCPU pinning 注解"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local pod_name="test-cpu-pinning"

    local pod_overrides
    pod_overrides="$(pod_spec_overrides)"

    # enable_vcpus_pinning 开启后 sandbox 会对全部容器下发 vcpu-pin；
    # infra(pause) 容器与 stopped 兄弟容器必须被跳过，否则 CreateContainer
    # 被失败毒化、Pod 无法运行——Pod Running 即证明过滤生效
    # （shared_cpu_pool 是配置文件键而非注解，不在此注入）
    run_kubectl_snippet "$node" "
        ${K3S_KUBECTL_BIN} apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
  annotations:
    org.openeuler.micrun.runtime.enable_vcpus_pinning: \"true\"
spec:
$pod_overrides
  runtimeClassName: micrun
  containers:
  - name: rtos
    image: $TEST_IMAGE
    command: [\"$K3S_CONTAINER_COMMAND\"]
    tty: false
    stdin: true
EOF
    " >/dev/null 2>&1

    local waited=0
    while [ "$waited" -lt 60 ]; do
        if remote_kubectl "$node" "get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null" | grep -qE "Running|Succeeded"; then
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done
    local status=$(remote_kubectl "$node" "get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null")

    # 记录边侧 pin 状态（best-effort：domain 的 vcpu affinity）
    local cid pin_info=""
    cid=$(remote_kubectl "$node" "get pod $pod_name -o jsonpath='{.status.containerStatuses[0].containerID}' 2>/dev/null" | sed 's|.*/||')
    if [ -n "$cid" ]; then
        pin_info=$(remote "$TEST_REMOTE_HOST" "xl vcpu-pin ${cid:0:10} 2>/dev/null | tail -n +2 | head -2" 2>/dev/null | tr '\n' ' ')
    fi

    # 清理
    remote_kubectl "$node" "delete pod $pod_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$status" = "Running" ] || [ "$status" = "Succeeded" ]; then
        record_result "cpu-pinning: vCPU pinning 注解" "PASS" "Pod 未被 pin 毒化; affinity: ${pin_info:-n/a}" "$time"
        echo -e "$PASS"
    else
        record_result "cpu-pinning: vCPU pinning 注解" "FAIL" "Pod 状态: ${status:-Unknown}（pin 可能毒化了容器创建）" "$time"
        echo -e "$FAIL"
    fi
}

# multi-node: 多节点部署（云边协同）
test_k3s_006_multi_node() {
    log_test "multi-node: 集群多节点拓扑"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"

    # 检查是否有多个节点
    local node_count=$(remote_kubectl "$node" "get nodes --no-headers 2>/dev/null | wc -l")

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$node_count" -ge 2 ]; then
        record_result "multi-node: 集群多节点拓扑" "PASS" "集群节点数: $node_count" "$time"
        echo -e "$PASS"
    else
        record_result "multi-node: 集群多节点拓扑" "SKIP" "需要至少 2 个节点 (当前: $node_count)" "$time"
        echo -e "$SKIP"
    fi
}

# self-healing: 故障恢复
test_k3s_007_self_healing() {
    log_test "self-healing: 故障恢复"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local deploy_name="test-healing"

    # 创建 Deployment
    local deploy_overrides
    deploy_overrides="$(deployment_spec_overrides)"

    run_kubectl_snippet "$node" "
        ${K3S_KUBECTL_BIN} apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $deploy_name
spec:
  replicas: 2
  selector:
    matchLabels:
      app: test-heal
  template:
    metadata:
      labels:
        app: test-heal
    spec:
$deploy_overrides
      runtimeClassName: micrun
      containers:
      - name: rtos
        image: $TEST_IMAGE
        command: [\"$K3S_CONTAINER_COMMAND\"]
        tty: false
        stdin: true
EOF
    " >/dev/null 2>&1

    wait_deployment_ready "$node" "$deploy_name" 2 90 || true

    # 获取初始副本数
    local initial_replicas=$(remote_kubectl "$node" "get pods -l app=test-heal --no-headers 2>/dev/null | wc -l")

    # 删除一个 Pod
    local pod_to_delete=$(remote_kubectl "$node" "get pods -l app=test-heal --no-headers 2>/dev/null | head -1 | awk '{print \$1}'")

    if [ -n "$pod_to_delete" ]; then
        remote_kubectl "$node" "delete pod $pod_to_delete" >/dev/null 2>&1 || true
    fi

    # 轮询等待 Deployment 控制器重建 Pod（重建的是新 Pod，计数前先等其入列）
    local healed_replicas=0 waited=0
    while [ "$waited" -lt 90 ]; do
        healed_replicas=$(remote_kubectl "$node" "get pods -l app=test-heal --no-headers 2>/dev/null | wc -l" | tr -d '[:space:]')
        if [ "$healed_replicas" = "$initial_replicas" ]; then
            break
        fi
        sleep 5
        waited=$((waited + 5))
    done

    # 清理
    remote_kubectl "$node" "delete deployment $deploy_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$initial_replicas" = "$healed_replicas" ] && [ "$healed_replicas" = "2" ]; then
        record_result "self-healing: 故障恢复" "PASS" "Pod 已自动重建" "$time"
        echo -e "$PASS"
    else
        record_result "self-healing: 故障恢复" "FAIL" "副本数: $initial_replicas → $healed_replicas" "$time"
        echo -e "$FAIL"
    fi
}

# interaction: RuntimeClass Pod 交互与清理
test_k3s_008_interaction() {
    log_test "interaction: RuntimeClass Pod 交互与清理"
    local start
    local end
    local time
    local out

    start=$(date +%s)
    out="$(bash "${SCRIPT_DIR}/run_interaction_e2e.sh" 2>&1)" && {
        end=$(date +%s)
        time=$((end - start))
        record_result "interaction: RuntimeClass Pod 交互与清理" "PASS" "kubectl attach、edge task、Xen domain 与删除清理通过" "$time"
        echo -e "$PASS"
        return
    }

    end=$(date +%s)
    time=$((end - start))
    # Keep the full output on disk for forensics: the retry marker sits far
    # above the tail that record_result shows, and a failure without it
    # reads as "no retry happened".
    mkdir -p /tmp/micrun-tests
    printf '%s\n' "$out" > /tmp/micrun-tests/k3s-interaction-fail.log
    record_result "interaction: RuntimeClass Pod 交互与清理" "FAIL" "$(printf '%s\n' "$out" | tail -n 20)" "$time"
    echo -e "$FAIL"
}

# ota: Deployment OTA 滚动升级
test_k3s_009_ota() {
    log_test "ota: Deployment OTA 滚动升级"
    local start
    local end
    local time
    local out

    start=$(date +%s)
    out="$(bash "${SCRIPT_DIR}/run_ota_e2e.sh" 2>&1)" && {
        end=$(date +%s)
        time=$((end - start))
        record_result "ota: Deployment OTA 滚动升级" "PASS" "v1->v2 rollout、edge task、Xen domain、kubectl attach 与清理通过" "$time"
        echo -e "$PASS"
        return
    }

    end=$(date +%s)
    time=$((end - start))
    record_result "ota: Deployment OTA 滚动升级" "FAIL" "$(printf '%s\n' "$out" | tail -n 20)" "$time"
    echo -e "$FAIL"
}

# ============================================
# 结果输出
# ============================================

print_results() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║                    MicRun K3s Test Results                         ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""

    print_table_header

    local passed=0
    local failed=0
    local skipped=0

    for i in "${!TEST_NAMES[@]}"; do
        local num=$((i + 1))
        local name="${TEST_NAMES[$i]}"
        local result="${TEST_RESULTS[$i]}"
        local time="${TEST_TIMES[$i]}s"

        print_table_row "$num" "$name" "$time" "$result"

        if [ "$result" = "PASS" ]; then
            passed=$((passed + 1))
        elif [ "$result" = "SKIP" ]; then
            skipped=$((skipped + 1))
        else
            failed=$((failed + 1))
        fi
    done

    print_table_footer
    echo ""
    echo "Summary: $passed passed, $failed failed, $skipped skipped, $((passed + failed + skipped)) total"
    echo ""

    # 显示失败详情
    if [ $failed -gt 0 ]; then
        echo "╔══════════════════════════════════════════════════════════════════════╗"
        echo "║                      Failed Test Details                            ║"
        echo "╚══════════════════════════════════════════════════════════════════════╝"
        echo ""
        for i in "${!TEST_NAMES[@]}"; do
            if [ "${TEST_RESULTS[$i]}" = "FAIL" ]; then
                echo -e "${COLOR_RED}✗ ${TEST_NAMES[$i]}${COLOR_NC}"
                echo "  ${TEST_DETAILS[$i]}"
                echo ""
            fi
        done
    fi

    return $failed
}

# ============================================
# 主函数
# ============================================

main() {
    local test_id="${1:-}"
    local needs_remote_master="true"

    # 兼容旧的 K3S-00x 编号入参：归一化为语义名后，脚本内部只认语义名。
    if [ -n "$test_id" ]; then
        test_id="$(canonical_test_id "$test_id")"
    fi

    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║              MicRun K3s Cloud Test Suite                          ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""

    if interaction_uses_direct_control_plane "$test_id"; then
        needs_remote_master="false"
    fi

    if [ "$needs_remote_master" = "true" ]; then
        # 检查 K3s 连接
        local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
        if ! remote_kubectl "$node" "version --client" >/dev/null 2>&1; then
            # 边侧 k3s 构建可能不含 kubectl 子命令（agent-only 交付镜像）。
            # 此时若宿主导出了可用的 K3S_LOCAL_KUBECONFIG，回退为宿主直连。
            if [ -n "${K3S_LOCAL_KUBECONFIG:-}" ] && [ -f "${K3S_LOCAL_KUBECONFIG}" ] \
                && command -v kubectl >/dev/null 2>&1 \
                && kubectl --kubeconfig "${K3S_LOCAL_KUBECONFIG}" version >/dev/null 2>&1; then
                export K3S_KUBECTL_LOCAL=true
                export K3S_KUBECTL_BIN="kubectl --kubeconfig ${K3S_LOCAL_KUBECONFIG}"
                log_info "Edge kubectl unavailable on $node (agent-only image?); using local kubeconfig ${K3S_LOCAL_KUBECONFIG}"
            else
                echo -e "${FAIL}Cannot connect to K3s master: $node"
                echo "Set K3S_MASTER_NODE to a node with a working kubectl, or export"
                echo "K3S_LOCAL_KUBECONFIG pointing at the cluster kubeconfig to drive"
                echo "the suite from this host."
                exit 1
            fi
        else
            log_info "Connected to K3s master: $node"
        fi
        echo ""

        # 清理
        cleanup_k3s
        sleep 2
    else
        log_info "${test_id} 使用本机/云端控制面，跳过远端 master 预检"
        echo ""
    fi

    # 运行测试或指定测试
    if [ -n "$test_id" ]; then
        if [ "$test_id" != "preflight" ] && [ "$needs_remote_master" = "true" ]; then
            test_k3s_000_preflight
            sleep 1
        fi

        case "$test_id" in
            preflight) test_k3s_000_preflight ;;
            runtimeclass) test_k3s_001_runtimeclass ;;
            pod-lifecycle) test_k3s_002_pod_lifecycle ;;
            deployment) test_k3s_003_deployment ;;
            pod-logs) test_k3s_004_pod_logs ;;
            resource-limits) test_k3s_005_resource_limits ;;
            cpu-pinning) test_k3s_010_cpu_pinning ;;
            multi-node) test_k3s_006_multi_node ;;
            self-healing) test_k3s_007_self_healing ;;
            interaction) test_k3s_008_interaction ;;
            ota) test_k3s_009_ota ;;
            *)
                echo "未知场景: $test_id"
                echo "可用场景: preflight runtimeclass pod-lifecycle deployment pod-logs resource-limits cpu-pinning multi-node self-healing interaction ota"
                exit 1
                ;;
        esac
    else
        # 环境预检
        test_k3s_000_preflight
        sleep 1

        # 创建 RuntimeClass
        test_k3s_001_runtimeclass
        sleep 1

        # 运行所有测试
        test_k3s_002_pod_lifecycle
        sleep 1
        test_k3s_003_deployment
        sleep 1
        test_k3s_004_pod_logs
        sleep 1
        test_k3s_005_resource_limits
        test_k3s_010_cpu_pinning
        sleep 1
        test_k3s_006_multi_node
        sleep 1
        test_k3s_007_self_healing
        if [ "${K3S_INCLUDE_INTERACTION:-false}" = "true" ]; then
            sleep 1
            test_k3s_008_interaction
        fi
        if [ "${K3S_INCLUDE_OTA:-false}" = "true" ]; then
            sleep 1
            test_k3s_009_ota
        fi
    fi

    # 打印结果
    print_results

    # 清理
    if [ "$needs_remote_master" = "true" ]; then
        cleanup_k3s
    fi

    # 退出
    local fail_count=0
    for r in "${TEST_RESULTS[@]}"; do
        [ "$r" = "FAIL" ] && fail_count=$((fail_count + 1))
    done

    if [ $fail_count -eq 0 ]; then
        echo -e "${PASS} 全部测试通过"
        exit 0
    else
        echo -e "${FAIL} ${fail_count} 个测试失败"
        exit 1
    fi
}

# 运行主函数
main "$@"
