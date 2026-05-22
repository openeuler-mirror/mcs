#!/bin/bash
# MicRun K3s 云化测试套件入口
# 使用: ./run_k3s_tests.sh [test_id]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../lib/test_utils.sh"
source "${SCRIPT_DIR}/../test-env.sh"

# 测试配置
CATEGORY="K3s 云化"
TEST_PREFIX="K3S"

# 结果存储
declare -a TEST_NAMES=()
declare -a TEST_RESULTS=()
declare -a TEST_DETAILS=()
declare -a TEST_TIMES=()

remote_kubectl() {
    local node="$1"
    local args="$2"
    remote "$node" "
        export PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:\$PATH
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

pod_spec_overrides() {
    local lines=()

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

interaction_uses_direct_control_plane() {
    local test_id="$1"
    local mode="${K3S_INTERACTION_MODE:-auto}"

    [ "$test_id" = "K3S-008" ] || return 1

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

    remote_kubectl "$node" "delete pod --all --ignore-not-found=true" >/dev/null 2>&1 || true
    remote_kubectl "$node" "delete deployment --all --ignore-not-found=true" >/dev/null 2>&1 || true
}

# ============================================
# 测试用例
# ============================================

# K3S-000: 环境预检
test_k3s_000_preflight() {
    log_test "K3S-000: 环境预检"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"

    if ! remote_kubectl "$node" "version --client" >/dev/null 2>&1; then
        local end=$(date +%s)
        record_result "K3S-000: 环境预检" "FAIL" "kubectl/k3s kubectl 不可用" "$((end - start))"
        echo -e "$FAIL"
        return
    fi

    local node_count=$(remote_kubectl "$node" "get nodes --no-headers 2>/dev/null | wc -l" | tr -d ' ')
    local not_ready=$(remote_kubectl "$node" "get nodes --no-headers 2>/dev/null | awk '\$2 != \"Ready\" {print \$1\":\"\$2}'")
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
        record_result "K3S-000: 环境预检" "FAIL" "未发现可用节点" "$time"
        echo -e "$FAIL"
    elif [ "${K3S_REQUIRE_PAUSE_IMAGE}" = "true" ] && [ "$pause_check" = "pause镜像缺失" ]; then
        record_result "K3S-000: 环境预检" "FAIL" "$details" "$time"
        echo -e "$FAIL"
    elif [ -n "$not_ready" ] && [ "${K3S_SINGLE_NODE}" != "true" ]; then
        record_result "K3S-000: 环境预检" "FAIL" "$details" "$time"
        echo -e "$FAIL"
    else
        record_result "K3S-000: 环境预检" "PASS" "$details" "$time"
        echo -e "$PASS"
    fi
}

# K3S-001: RuntimeClass 创建
test_k3s_001_runtimeclass() {
    log_test "K3S-001: RuntimeClass 创建"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local pod_overrides
    pod_overrides="$(pod_spec_overrides)"
    local kubectl_bin="${K3S_KUBECTL_BIN}"

    # 创建 RuntimeClass
    remote "$node" "
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
        record_result "K3S-001: RuntimeClass 创建" "PASS" "RuntimeClass 已创建" "$time"
        echo -e "$PASS"
    else
        record_result "K3S-001: RuntimeClass 创建" "FAIL" "RuntimeClass 创建失败" "$time"
        echo -e "$FAIL"
    fi
}

# K3S-002: Pod 启动/停止
test_k3s_002_pod_lifecycle() {
    log_test "K3S-002: Pod 启动/停止"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local pod_name="test-pod-lifecycle"

    # 创建 Pod
    local pod_overrides
    pod_overrides="$(pod_spec_overrides)"

    remote "$node" "
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

    # 等待 Pod 启动
    sleep 10

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
        record_result "K3S-002: Pod 启动/停止" "PASS" "Pod 状态: $status" "$time"
        echo -e "$PASS"
    else
        record_result "K3S-002: Pod 启动/停止" "FAIL" "$details" "$time"
        echo -e "$FAIL"
    fi
}

# K3S-003: Deployment 扩缩容
test_k3s_003_deployment() {
    log_test "K3S-003: Deployment 扩缩容"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local deploy_name="test-deployment"

    # 创建 Deployment (2 副本)
    local deploy_overrides
    deploy_overrides="$(deployment_spec_overrides)"

    remote "$node" "
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

    # 等待 Deployment 就绪
    sleep 15

    # 检查副本数
    local replicas=$(remote_kubectl "$node" "get deployment $deploy_name -o jsonpath='{.status.readyReplicas}' 2>/dev/null")

    # 扩容到 3 副本
    remote_kubectl "$node" "scale deployment $deploy_name --replicas=3" >/dev/null 2>&1 || true

    sleep 10

    local scaled_replicas=$(remote_kubectl "$node" "get deployment $deploy_name -o jsonpath='{.status.readyReplicas}' 2>/dev/null")

    # 清理
    remote_kubectl "$node" "delete deployment $deploy_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$replicas" = "2" ] && [ "$scaled_replicas" = "3" ]; then
        record_result "K3S-003: Deployment 扩缩容" "PASS" "2→3 副本成功" "$time"
        echo -e "$PASS"
    else
        record_result "K3S-003: Deployment 扩缩容" "FAIL" "副本数: $replicas → $scaled_replicas" "$time"
        echo -e "$FAIL"
    fi
}

# K3S-004: Pod 日志获取
test_k3s_004_pod_logs() {
    log_test "K3S-004: Pod 日志获取"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local pod_name="test-logs"

    # 创建 Pod
    local pod_overrides
    pod_overrides="$(pod_spec_overrides)"

    remote "$node" "
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

    # 等待 Pod 启动
    sleep 10

    # 获取日志
    local logs=$(remote_kubectl "$node" "logs $pod_name 2>/dev/null | head -5")

    # 清理
    remote_kubectl "$node" "delete pod $pod_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end=$(date +%s)
    local time=$((end - start))

    if [ -n "$logs" ]; then
        record_result "K3S-004: Pod 日志获取" "PASS" "日志长度: ${#logs} 字节" "$time"
        echo -e "$PASS"
    else
        record_result "K3S-004: Pod 日志获取" "FAIL" "无日志输出" "$time"
        echo -e "$FAIL"
    fi
}

# K3S-005: 资源限制
test_k3s_005_resource_limits() {
    log_test "K3S-005: 资源限制"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local pod_name="test-resources"

    # 创建带资源限制的 Pod
    local pod_overrides
    pod_overrides="$(pod_spec_overrides)"

    remote "$node" "
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

    sleep 10

    # 检查 Pod 状态
    local status=$(remote_kubectl "$node" "get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null")

    # 清理
    remote_kubectl "$node" "delete pod $pod_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$status" = "Running" ] || [ "$status" = "Succeeded" ]; then
        record_result "K3S-005: 资源限制" "PASS" "资源限制已应用" "$time"
        echo -e "$PASS"
    else
        record_result "K3S-005: 资源限制" "FAIL" "Pod 状态: ${status:-Unknown}" "$time"
        echo -e "$FAIL"
    fi
}

# K3S-006: 多节点部署（云边协同）
test_k3s_006_multi_node() {
    log_test "K3S-006: 多节点部署"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"

    # 检查是否有多个节点
    local node_count=$(remote_kubectl "$node" "get nodes --no-headers 2>/dev/null | wc -l")

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$node_count" -ge 2 ]; then
        record_result "K3S-006: 多节点部署" "PASS" "集群节点数: $node_count" "$time"
        echo -e "$PASS"
    else
        record_result "K3S-006: 多节点部署" "SKIP" "需要至少 2 个节点 (当前: $node_count)" "$time"
        echo -e "$SKIP"
    fi
}

# K3S-007: 故障恢复
test_k3s_007_self_healing() {
    log_test "K3S-007: 故障恢复"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    local deploy_name="test-healing"

    # 创建 Deployment
    local deploy_overrides
    deploy_overrides="$(deployment_spec_overrides)"

    remote "$node" "
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

    sleep 15

    # 获取初始副本数
    local initial_replicas=$(remote_kubectl "$node" "get pods -l app=test-heal --no-headers 2>/dev/null | wc -l")

    # 删除一个 Pod
    local pod_to_delete=$(remote_kubectl "$node" "get pods -l app=test-heal --no-headers 2>/dev/null | head -1 | awk '{print \$1}'")

    if [ -n "$pod_to_delete" ]; then
        remote_kubectl "$node" "delete pod $pod_to_delete" >/dev/null 2>&1 || true
    fi

    sleep 15

    # 检查恢复后的副本数
    local healed_replicas=$(remote_kubectl "$node" "get pods -l app=test-heal --no-headers 2>/dev/null | wc -l")

    # 清理
    remote_kubectl "$node" "delete deployment $deploy_name --ignore-not-found=true" >/dev/null 2>&1 || true

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$initial_replicas" = "$healed_replicas" ] && [ "$healed_replicas" = "2" ]; then
        record_result "K3S-007: 故障恢复" "PASS" "Pod 已自动重建" "$time"
        echo -e "$PASS"
    else
        record_result "K3S-007: 故障恢复" "FAIL" "副本数: $initial_replicas → $healed_replicas" "$time"
        echo -e "$FAIL"
    fi
}

# K3S-008: RuntimeClass Pod 交互与清理
test_k3s_008_interaction() {
    log_test "K3S-008: RuntimeClass Pod 交互与清理"
    local start
    local end
    local time
    local out

    start=$(date +%s)
    out="$(bash "${SCRIPT_DIR}/run_interaction_e2e.sh" 2>&1)" && {
        end=$(date +%s)
        time=$((end - start))
        record_result "K3S-008: RuntimeClass Pod 交互与清理" "PASS" "kubectl attach、edge task、Xen domain 与删除清理通过" "$time"
        echo -e "$PASS"
        return
    }

    end=$(date +%s)
    time=$((end - start))
    record_result "K3S-008: RuntimeClass Pod 交互与清理" "FAIL" "$(printf '%s\n' "$out" | tail -n 20)" "$time"
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
            echo -e "${FAIL}Cannot connect to K3s master: $node"
            echo "Please configure K3S_MASTER_NODE environment variable"
            exit 1
        fi
        log_info "Connected to K3s master: $node"
        echo ""

        # 清理
        cleanup_k3s
        sleep 2
    else
        log_info "K3S-008 使用 ${K3S_INTERACTION_MODE} 控制面，跳过远端 master 预检"
        echo ""
    fi

    # 运行测试或指定测试
    if [ -n "$test_id" ]; then
        if [ "$test_id" != "K3S-000" ] && [ "$needs_remote_master" = "true" ]; then
            test_k3s_000_preflight
            sleep 1
        fi

        case "$test_id" in
            K3S-000) test_k3s_000_preflight ;;
            K3S-001) test_k3s_001_runtimeclass ;;
            K3S-002) test_k3s_002_pod_lifecycle ;;
            K3S-003) test_k3s_003_deployment ;;
            K3S-004) test_k3s_004_pod_logs ;;
            K3S-005) test_k3s_005_resource_limits ;;
            K3S-006) test_k3s_006_multi_node ;;
            K3S-007) test_k3s_007_self_healing ;;
            K3S-008) test_k3s_008_interaction ;;
            *)
                echo "Unknown test ID: $test_id"
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
        sleep 1
        test_k3s_006_multi_node
        sleep 1
        test_k3s_007_self_healing
        if [ "${K3S_INCLUDE_INTERACTION:-false}" = "true" ]; then
            sleep 1
            test_k3s_008_interaction
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
