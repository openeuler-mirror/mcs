#!/bin/bash
# MicRun 测试套件 - 全局入口
# 使用: ./run_all_tests.sh [category] [test_id]
#
# 示例:
#   ./run_all_tests.sh              # 运行所有测试
#   ./run_all_tests.sh io           # 运行 IO 测试
#   ./run_all_tests.sh k3s          # 运行 K3s 测试
#   ./run_all_tests.sh io IO-001    # 运行指定测试

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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
  $0 io IO-001        # 运行指定测试

测试类别:
  io         IO 测试 - 标准输入输出、TTY、回声抑制
  k3s        K3s 云化测试 - Kubernetes 集成、Pod 生命周期
  lifecycle  生命周期测试 - 容器创建、启动、停止、删除
  performance 性能测试 - 启动时间、资源占用、吞吐量

环境变量:
  TEST_REMOTE_HOST    远程测试主机 (默认: root@192.168.7.2)
  TEST_IMAGE          测试镜像
  K3S_MASTER_NODE     K3s Master 节点
  TEST_LOG_DIR        测试日志目录

EOF
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
            if [ -f "${SCRIPT_DIR}/k3s/run_k3s_tests.sh" ]; then
                bash "${SCRIPT_DIR}/k3s/run_k3s_tests.sh" "$test_id"
            else
                echo "K3s 测试脚本不存在: ${SCRIPT_DIR}/k3s/run_k3s_tests.sh"
                return 1
            fi
            ;;
        lifecycle)
            if [ -d "${SCRIPT_DIR}/lifecycle" ]; then
                echo "运行生命周期测试..."
                # TODO: 实现生命周期测试入口
            else
                echo "生命周期测试目录不存在"
                return 1
            fi
            ;;
        performance)
            if [ -d "${SCRIPT_DIR}/performance" ]; then
                echo "运行性能测试..."
                # TODO: 实现性能测试入口
            else
                echo "性能测试目录不存在"
                return 1
            fi
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
            ((total_passed+=7))
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
        if bash "${SCRIPT_DIR}/k3s/run_k3s_tests.sh"; then
            ((total_passed+=7))
        else
            local failed=$?
            ((total_failed+=failed))
        fi
    fi

    # 汇总
    echo ""
    echo "╔══════════════════════════════════════════════════════════════════════╗"
    echo "║                      Final Summary                                ║"
    echo "╚══════════════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Total Passed: $total_passed"
    echo "Total Failed: $total_failed"
    echo "Total Skipped: $total_skipped"
    echo "Total: $((total_passed + total_failed + total_skipped))"
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
