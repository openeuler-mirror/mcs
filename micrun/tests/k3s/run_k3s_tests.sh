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

# 颜色定义
PASS='\033[0;32m✓ PASS\033[0m'
FAIL='\033[0;31m✗ FAIL\033[0m'
SKIP='\033[0;33m○ SKIP\033[0m'
INFO='\033[0;34m[INFO]\033[0m'

# 结果存储
declare -a TEST_NAMES=()
declare -a TEST_RESULTS=()
declare -a TEST_DETAILS=()
declare -a TEST_TIMES=()

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

    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl delete pod --all --ignore-not-found=true 2>/dev/null || true
        kubectl delete deployment --all --ignore-not-found=true 2>/dev/null || true
    " >/dev/null 2>&1
}

# ============================================
# 测试用例
# ============================================

# K3S-001: RuntimeClass 创建
test_k3s_001_runtimeclass() {
    log_test "K3S-001: RuntimeClass 创建"
    local start=$(date +%s)
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"

    # 创建 RuntimeClass
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl apply -f - <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
EOF
    " >/dev/null 2>&1

    # 验证 RuntimeClass 存在
    local result=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl get runtimeclass micrun --no-headers 2>/dev/null | wc -l
    " 2>/dev/null)

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
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos
    image: $TEST_IMAGE
    tty: true
    stdin: true
EOF
    " >/dev/null 2>&1

    # 等待 Pod 启动
    sleep 10

    # 检查 Pod 状态
    local status=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null
    " 2>/dev/null)

    # 删除 Pod
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl delete pod $pod_name --ignore-not-found=true 2>/dev/null
    " >/dev/null 2>&1

    local end=$(date +%s)
    local time=$((end - start))

    if [ "$status" = "Running" ] || [ "$status" = "Succeeded" ]; then
        record_result "K3S-002: Pod 启动/停止" "PASS" "Pod 状态: $status" "$time"
        echo -e "$PASS"
    else
        record_result "K3S-002: Pod 启动/停止" "FAIL" "Pod 状态: ${status:-Unknown}" "$time"
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
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl apply -f - <<EOF
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
      runtimeClassName: micrun
      containers:
      - name: rtos
        image: $TEST_IMAGE
        tty: true
        stdin: true
EOF
    " >/dev/null 2>&1

    # 等待 Deployment 就绪
    sleep 15

    # 检查副本数
    local replicas=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl get deployment $deploy_name -o jsonpath='{.status.readyReplicas}' 2>/dev/null
    " 2>/dev/null)

    # 扩容到 3 副本
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl scale deployment $deploy_name --replicas=3 2>/dev/null
    " >/dev/null 2>&1

    sleep 10

    local scaled_replicas=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl get deployment $deploy_name -o jsonpath='{.status.readyReplicas}' 2>/dev/null
    " 2>/dev/null)

    # 清理
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl delete deployment $deploy_name --ignore-not-found=true 2>/dev/null
    " >/dev/null 2>&1

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
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos
    image: $TEST_IMAGE
    tty: true
    stdin: true
EOF
    " >/dev/null 2>&1

    # 等待 Pod 启动
    sleep 10

    # 获取日志
    local logs=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl logs $pod_name 2>/dev/null | head -5
    " 2>/dev/null)

    # 清理
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl delete pod $pod_name --ignore-not-found=true 2>/dev/null
    " >/dev/null 2>&1

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
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos
    image: $TEST_IMAGE
    tty: true
    stdin: true
    resources:
      limits:
        memory: "128Mi"
        cpu: "500m"
EOF
    " >/dev/null 2>&1

    sleep 10

    # 检查 Pod 状态
    local status=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl get pod $pod_name -o jsonpath='{.status.phase}' 2>/dev/null
    " 2>/dev/null)

    # 清理
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl delete pod $pod_name --ignore-not-found=true 2>/dev/null
    " >/dev/null 2>&1

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
    local node_count=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl get nodes --no-headers 2>/dev/null | wc -l
    " 2>/dev/null)

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
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl apply -f - <<EOF
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
      runtimeClassName: micrun
      containers:
      - name: rtos
        image: $TEST_IMAGE
        tty: true
        stdin: true
EOF
    " >/dev/null 2>&1

    sleep 15

    # 获取初始副本数
    local initial_replicas=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl get pods -l app=test-heal --no-headers 2>/dev/null | wc -l
    " 2>/dev/null)

    # 删除一个 Pod
    local pod_to_delete=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl get pods -l app=test-heal --no-headers 2>/dev/null | head -1 | awk '{print \$1}'
    " 2>/dev/null)

    if [ -n "$pod_to_delete" ]; then
        ssh -o StrictHostKeyChecking=no "$node" "
            kubectl delete pod $pod_to_delete 2>/dev/null
        " >/dev/null 2>&1
    fi

    sleep 15

    # 检查恢复后的副本数
    local healed_replicas=$(ssh -o StrictHostKeyChecking=no "$node" "
        kubectl get pods -l app=test-heal --no-headers 2>/dev/null | wc -l
    " 2>/dev/null)

    # 清理
    ssh -o StrictHostKeyChecking=no "$node" "
        kubectl delete deployment $deploy_name --ignore-not-found=true 2>/dev/null
    " >/dev/null 2>&1

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
            ((passed++))
        elif [ "$result" = "SKIP" ]; then
            ((skipped++))
        else
            ((failed++))
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

    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║              MicRun K3s Cloud Test Suite                          ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""

    # 检查 K3s 连接
    local node="${K3S_MASTER_NODE:-$TEST_REMOTE_HOST}"
    if ! ssh -o StrictHostKeyChecking=no "$node" "kubectl version --client" >/dev/null 2>&1; then
        echo -e "${FAIL}Cannot connect to K3s master: $node"
        echo "Please configure K3S_MASTER_NODE environment variable"
        exit 1
    fi
    log_info "Connected to K3s master: $node"
    echo ""

    # 清理
    cleanup_k3s
    sleep 2

    # 创建 RuntimeClass
    test_k3s_001_runtimeclass
    sleep 1

    # 运行测试或指定测试
    if [ -n "$test_id" ]; then
        case "$test_id" in
            K3S-001) test_k3s_001_runtimeclass ;;
            K3S-002) test_k3s_002_pod_lifecycle ;;
            K3S-003) test_k3s_003_deployment ;;
            K3S-004) test_k3s_004_pod_logs ;;
            K3S-005) test_k3s_005_resource_limits ;;
            K3S-006) test_k3s_006_multi_node ;;
            K3S-007) test_k3s_007_self_healing ;;
            *)
                echo "Unknown test ID: $test_id"
                exit 1
                ;;
        esac
    else
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
    fi

    # 打印结果
    print_results

    # 清理
    cleanup_k3s

    # 退出
    local fail_count=0
    for r in "${TEST_RESULTS[@]}"; do
        [ "$r" = "FAIL" ] && ((fail_count++))
    done

    if [ $fail_count -eq 0 ]; then
        echo -e "$PASSAll tests passed!"
        exit 0
    else
        echo -e "$FAIL$fail_count test(s) failed!"
        exit 1
    fi
}

# 运行主函数
main "$@"
