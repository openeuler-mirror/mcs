#!/bin/bash
# MicRun 测试套件 - 全局入口
# 使用: ./run_all_tests.sh [category] [test_id]
#
# 示例:
#   ./run_all_tests.sh              # 运行所有测试
#   ./run_all_tests.sh io           # 运行 IO 测试
#   ./run_all_tests.sh k3s          # 运行 K3s 测试
#   ./run_all_tests.sh k3s K3S-008  # 运行 K3s 交互测试
#   ./run_all_tests.sh k3s K3S-009  # 运行 K3s OTA 测试
#   ./run_all_tests.sh io IO-001    # 运行指定测试

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
source "${SCRIPT_DIR}/test-env.sh"

# 颜色定义
PASS='\033[0;32m✓\033[0m'
FAIL='\033[0;31m✗\033[0m'
INFO='\033[0;34m[INFO]\033[0m'

# 测试类别注册
declare -A TEST_CATEGORIES=(
    ["io"]="IO 测试"
    ["k3s"]="K3s 云化测试"
    ["lifecycle"]="生命周期测试"
    ["performance"]="性能测试"
)

# 测试目录
TEST_DIRS=(
    "tests/io"
    "tests/k3s"
    "tests/lifecycle"
    "tests/performance"
)

# ============================================
# 帮助信息
# ============================================

show_help() {
    cat << EOF
MicRun 测试套件 - 全局入口

用法: $0 [category] [test_id]

参数:
  category    测试类别 (io, k3s, lifecycle, performance)
  test_id     测试用例 ID (如 IO-001, K3S-001)

示例:
  $0                  # 运行所有测试
  $0 io               # 运行 IO 测试
  $0 k3s              # 运行 K3s 测试
  $0 k3s K3S-008      # 运行 K3s 交互测试
  $0 k3s K3S-009      # 运行 K3s OTA 滚动升级测试
  $0 io IO-001        # 运行指定测试

测试类别:
  io         IO 测试 - 标准输入输出、TTY、回声抑制
  k3s        K3s 云化测试 - Kubernetes 集成、Pod 生命周期、kubectl attach 交互
  lifecycle  生命周期测试 - 容器创建、启动、停止、删除
  performance 性能测试 - 启动时间、资源占用、吞吐量

环境变量:
  TEST_REMOTE_HOST    远程测试主机 (默认: root@192.168.7.2)
  TEST_IMAGE          测试镜像
  NERDCTL_NETWORK_MODE nerdctl 网络模式 (qemu 推荐: none)
  IMAGE_PROFILE       镜像能力类型 (auto/shell/hello)
  QEMU_IMAGE_TAR      qemu 回归脚本使用的镜像 tar
  K3S_MASTER_NODE     K3s Master 节点
  K3S_INCLUDE_INTERACTION  k3s 类别无 test_id 时是否包含 K3S-008 (默认: true)
  K3S_INCLUDE_OTA     k3s 类别无 test_id 时是否包含 K3S-009 OTA (默认: false)
  TEST_LOG_DIR        测试日志目录
  PERF_BENCHTIME      performance 类别的 Go benchmark 时长 (默认: 100ms)
  RUN_PERFORMANCE_TESTS=1  无参数运行全部测试时包含 performance 类别

EOF
}

run_go_tests() {
    local label="$1"
    shift

    echo "运行${label}..."
    (
        cd "${REPO_ROOT}"
        go test "$@"
    )
}

run_lifecycle_tests() {
    local test_id="${1:-}"
    local run_regex="${test_id:-Lifecycle|Create|Start|Stop|Delete|Kill|Wait|Pause|Resume|CloseIO|State}"

    run_go_tests "生命周期测试" \
        ./internal/application/lifecycle \
        ./internal/application/task \
        ./internal/domain/container \
        ./internal/transport/shimv2 \
        -run "${run_regex}"
}

run_performance_tests() {
    local test_id="${1:-}"
    local run_regex="${test_id:-Performance|^$}"
    local bench_time="${PERF_BENCHTIME:-100ms}"

    run_go_tests "性能测试" \
        ./internal/adapters/io \
        ./internal/support/parse \
        -run "${run_regex}" \
        -bench Benchmark \
        -benchtime "${bench_time}"
}

run_k3s_tests() {
    local test_id="${1:-}"

    if [ ! -f "${SCRIPT_DIR}/k3s/run_k3s_tests.sh" ]; then
        echo "K3s 测试脚本不存在: ${SCRIPT_DIR}/k3s/run_k3s_tests.sh"
        return 1
    fi

    if [ -n "$test_id" ]; then
        bash "${SCRIPT_DIR}/k3s/run_k3s_tests.sh" "$test_id"
    else
        K3S_INCLUDE_INTERACTION="${K3S_INCLUDE_INTERACTION:-true}" \
            bash "${SCRIPT_DIR}/k3s/run_k3s_tests.sh"
    fi
}

# ============================================
# 运行测试类别
# ============================================

run_category() {
    local category="$1"
    local test_id="${2:-}"

    case "$category" in
        io)
            if [ -f "${SCRIPT_DIR}/io/run_all_io_tests.sh" ]; then
                bash "${SCRIPT_DIR}/io/run_all_io_tests.sh" "$test_id"
            else
                echo "IO 测试脚本不存在: ${SCRIPT_DIR}/io/run_all_io_tests.sh"
                return 1
            fi
            ;;
        k3s)
            run_k3s_tests "$test_id"
            ;;
        lifecycle)
            run_lifecycle_tests "$test_id"
            ;;
        performance)
            run_performance_tests "$test_id"
            ;;
        *)
            echo -e "${FAIL}未知的测试类别: $category"
            echo ""
            echo "可用的测试类别:"
            for cat in "${!TEST_CATEGORIES[@]}"; do
                echo "  - $cat: ${TEST_CATEGORIES[$cat]}"
            done
            return 1
            ;;
    esac
}

# ============================================
# 运行所有测试
# ============================================

run_all_tests() {
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║              MicRun Test Suite - All Categories                   ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""

    local total_passed=0
    local total_failed=0
    local total_skipped=0

    # IO 测试
    if [ -f "${SCRIPT_DIR}/io/run_all_io_tests.sh" ]; then
        echo ""
        echo -e "${INFO}═══════════════════════════════════════════════════"
        echo -e "${INFO} Running IO Tests"
        echo -e "${INFO}═══════════════════════════════════════════════════"
        if bash "${SCRIPT_DIR}/io/run_all_io_tests.sh"; then
            ((total_passed+=1))
        else
            local failed=$?
            ((total_failed+=failed))
        fi
    fi

    # K3s 测试
    if [ -f "${SCRIPT_DIR}/k3s/run_k3s_tests.sh" ]; then
        echo ""
        echo -e "${INFO}═══════════════════════════════════════════════════"
        echo -e "${INFO} Running K3s Cloud Tests"
        echo -e "${INFO}═══════════════════════════════════════════════════"
        if run_k3s_tests; then
            ((total_passed+=1))
        else
            local failed=$?
            ((total_failed+=failed))
        fi
    fi

    # 生命周期测试
    echo ""
    echo -e "${INFO}═══════════════════════════════════════════════════"
    echo -e "${INFO} Running Lifecycle Tests"
    echo -e "${INFO}═══════════════════════════════════════════════════"
    if run_lifecycle_tests; then
        ((total_passed+=1))
    else
        local failed=$?
        ((total_failed+=failed))
    fi

    # 性能测试默认不随全量入口执行，避免无意拉长常规回归。
    if [ "${RUN_PERFORMANCE_TESTS:-0}" = "1" ]; then
        echo ""
        echo -e "${INFO}═══════════════════════════════════════════════════"
        echo -e "${INFO} Running Performance Tests"
        echo -e "${INFO}═══════════════════════════════════════════════════"
        if run_performance_tests; then
            ((total_passed+=1))
        else
            local failed=$?
            ((total_failed+=failed))
        fi
    else
        ((total_skipped+=1))
        echo ""
        echo -e "${INFO}Skipping Performance Tests (set RUN_PERFORMANCE_TESTS=1 to include)"
    fi

    # 汇总
    echo ""
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║                      Final Summary                                ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Categories Passed: $total_passed"
    echo "Categories Failed: $total_failed"
    echo "Categories Skipped: $total_skipped"
    echo "Categories Total: $((total_passed + total_failed + total_skipped))"
    echo ""

    if [ $total_failed -eq 0 ]; then
        echo -e "${PASS}All tests passed!"
        return 0
    else
        echo -e "${FAIL}$total_failed test(s) failed!"
        return 1
    fi
}

# ============================================
# 主函数
# ============================================

main() {
    local category="${1:-}"
    local test_id="${2:-}"

    # 显示帮助
    if [ "$category" = "-h" ] || [ "$category" = "--help" ]; then
        show_help
        exit 0
    fi

    # 无参数：运行所有测试
    if [ -z "$category" ]; then
        run_all_tests
        exit $?
    fi

    # 运行指定类别
    run_category "$category" "$test_id"
    exit $?
}

# 运行主函数
main "$@"
