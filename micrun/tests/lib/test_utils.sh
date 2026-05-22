#!/bin/bash
# MicRun 测试工具函数库
# 提供测试常用的辅助函数

_MICRUN_TEST_UTILS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${_MICRUN_TEST_UTILS_DIR}/../common/assert.sh"
source "${_MICRUN_TEST_UTILS_DIR}/../common/remote.sh"

# ============================================
# Container 操作
# ============================================

# 清理指定容器
cleanup_container() {
    local name="$1"
    local host="${2:-$TEST_REMOTE_HOST}"

    remote "$host" "
        ctr task kill -s 9 $name 2>/dev/null || true
        ctr task delete $name 2>/dev/null || true
        ctr container delete $name 2>/dev/null || true
    " >/dev/null 2>&1
}

# 清理所有测试容器
cleanup_all_containers() {
    local host="${1:-$TEST_REMOTE_HOST}"
    local prefix="${2:-test-}"

    remote "$host" "
        pkill -9 ctr 2>/dev/null || true
        sleep 1
        rm -rf /run/containerd/io.containerd.runtime.v2.task/default/* 2>/dev/null || true
        rm -rf /run/containerd/fifo/* 2>/dev/null || true
        for c in \$(ctr container ls -q 2>/dev/null | grep '^$prefix'); do
            ctr container delete \$c 2>/dev/null || true
        done
    " >/dev/null 2>&1
}

# 清理 MicRun/micad 临时运行态
cleanup_micrun_runtime_state() {
    local host="${1:-$TEST_REMOTE_HOST}"

    remote "$host" '
        set +e
        cleanup_ctr_namespace() {
            ns="$1"
            command -v ctr >/dev/null 2>&1 || return 0

            for id in $(ctr -n "$ns" tasks ls -q 2>/dev/null); do
                if command -v mica >/dev/null 2>&1; then
                    mica stop "$id" 2>/dev/null || true
                    mica rm "$id" 2>/dev/null || true
                fi
                ctr -n "$ns" tasks kill -s 9 "$id" 2>/dev/null || true
                ctr -n "$ns" tasks delete --force "$id" 2>/dev/null || true
                ctr -n "$ns" tasks rm --force "$id" 2>/dev/null || true
                xl destroy "$id" 2>/dev/null || true
            done

            for id in $(ctr -n "$ns" containers ls -q 2>/dev/null); do
                if command -v mica >/dev/null 2>&1; then
                    mica rm "$id" 2>/dev/null || true
                fi
                ctr -n "$ns" containers delete "$id" 2>/dev/null || true
            done
        }

        cleanup_ctr_namespace default
        cleanup_ctr_namespace k8s.io

        if command -v mica >/dev/null 2>&1; then
            for path in /run/micrun/containers/* /run/micrun/runtime/container/*; do
                [ -e "$path" ] || continue
                id="${path##*/}"
                case "$id" in
                    ""|qemu-*) continue ;;
                esac
                mica stop "$id" 2>/dev/null || true
                mica rm "$id" 2>/dev/null || true
            done

            if mica status 2>/dev/null |
                awk '\''NR > 1 && $1 !~ /^qemu-/ { found=1 } END { exit found ? 0 : 1 }'\''; then
                systemctl restart micad >/dev/null 2>&1 || true
            fi
        fi

        pkill -9 -f "[c]ontainerd-shim-mica-v2" 2>/dev/null || true

        xl list 2>/dev/null | awk '\''NR > 1 && $1 != "Domain-0" { print $1 }'\'' | while read -r id; do
            [ -n "$id" ] && xl destroy "$id" 2>/dev/null || true
        done

        rm -rf /run/micrun/containers/* \
               /run/micrun/runtime/container/* \
               /run/micrun/runtime/sandbox/* 2>/dev/null || true

        if command -v ctr >/dev/null 2>&1; then
            for ns in default k8s.io; do
                if ctr -n "$ns" tasks ls -q 2>/dev/null | grep -q .; then
                    systemctl restart containerd >/dev/null 2>&1 || true
                    break
                fi
            done
            sleep 1
            cleanup_ctr_namespace default
            cleanup_ctr_namespace k8s.io
        fi
    ' >/dev/null 2>&1 || true
}

# 获取容器状态
get_container_status() {
    local name="$1"
    local host="${2:-$TEST_REMOTE_HOST}"

    local status=$(remote "$host" "ctr task ls | grep $name | awk '{print \$2}'" | head -1)
    echo "${status:-NOT_FOUND}"
}

# 等待容器状态
wait_for_container_status() {
    local name="$1"
    local expected_status="$2"
    local timeout="${3:-30}"
    local host="${4:-$TEST_REMOTE_HOST}"

    local elapsed=0
    while [ $elapsed -lt $timeout ]; do
        local status=$(get_container_status "$name" "$host")
        if [ "$status" = "$expected_status" ]; then
            return 0
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done

    log_error "Timeout waiting for $name to be $expected_status (got: $status)"
    return 1
}

# 检查容器是否存在
container_exists() {
    local name="$1"
    local host="${2:-$TEST_REMOTE_HOST}"

    local exists=$(remote "$host" "ctr container ls | grep -c $name || echo 0")
    [ "$exists" -gt 0 ]
}

# ============================================
# K3s/K8s 操作
# ============================================

# Kubectl 执行（支持远程节点）
kubectl_cmd() {
    local node="${1}"
    shift
    local args="$@"

    if [ -n "$node" ]; then
        remote "$node" "kubectl $args"
    else
        kubectl $args 2>/dev/null
    fi
}

# 等待 Pod 就绪
wait_for_pod() {
    local pod_name="$1"
    local namespace="${2:-default}"
    local timeout="${3:-120}"
    local node="${4:-}"

    local elapsed=0
    while [ $elapsed -lt $timeout ]; do
        local status=$(kubectl_cmd "$node" "get pod $pod_name -n $namespace -o jsonpath='{.status.phase}'" 2>/dev/null)
        if [ "$status" = "Running" ]; then
            local ready=$(kubectl_cmd "$node" "get pod $pod_name -n $namespace -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'" 2>/dev/null)
            if [ "$ready" = "True" ]; then
                return 0
            fi
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done

    log_error "Timeout waiting for pod $pod_name to be ready"
    return 1
}

# 删除 Pod
delete_pod() {
    local pod_name="$1"
    local namespace="${2:-default}"
    local node="${3:-}"

    kubectl_cmd "$node" "delete pod $pod_name -n $namespace --ignore-not-found=true" >/dev/null 2>&1
}

# 获取 Pod 状态
get_pod_status() {
    local pod_name="$1"
    local namespace="${2:-default}"
    local node="${3:-}"

    kubectl_cmd "$node" "get pod $pod_name -n $namespace -o jsonpath='{.status.phase}'" 2>/dev/null
}

# 检查镜像是否存在
image_exists() {
    local image="$1"
    local host="${2:-$TEST_REMOTE_HOST}"

    local exists=$(remote "$host" "ctr image ls | grep -c $image || echo 0")
    [ "$exists" -gt 0 ]
}

# 导入镜像（从 tarball）
import_image() {
    local tarball="$1"
    local host="${2:-$TEST_REMOTE_HOST}"

    if [ ! -f "$tarball" ]; then
        log_error "Image tarball not found: $tarball"
        return 1
    fi

    local filename=$(basename "$tarball")
    local remote_path="/tmp/$filename"

    # 上传 tarball
    copy_to_remote "$tarball" "$host" "$remote_path"

    # 导入镜像
    remote "$host" "ctr image import $remote_path" >/dev/null 2>&1

    # 清理
    remote "$host" "rm -f $remote_path" >/dev/null 2>&1

    log_success "Image imported: $tarball"
}

# ============================================
# 日志操作
# ============================================

# 获取测试日志目录
get_log_dir() {
    echo "${TEST_LOG_DIR:-/tmp/micrun-tests}"
}

# 创建日志文件
create_log_file() {
    local test_name="$1"
    local log_dir=$(get_log_dir)
    mkdir -p "$log_dir"

    local timestamp=$(date +%Y%m%d-%H%M%S)
    echo "$log_dir/${test_name}-${timestamp}.log"
}

# ============================================
# 网络操作
# ============================================

# 检查主机连通性
check_host() {
    local host="$1"
    local timeout="${2:-5}"

    if ping -c 1 -W "$timeout" "$host" >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

# 检查端口连通性
check_port() {
    local host="$1"
    local port="$2"
    local timeout="${3:-5}"

    if timeout "$timeout" bash -c "cat < /dev/null > /dev/tcp/$host/$port" 2>/dev/null; then
        return 0
    fi
    return 1
}

# ============================================
# 数值提取
# ============================================

# 从输出中提取数字
extract_number() {
    local output="$1"
    # 移除换行和空格，提取第一个数字
    echo "$output" | tr -d '\n ' | grep -oE '[0-9]+' | head -1 || echo "0"
}

# ============================================
# 输出格式化
# ============================================

# 打印表格头
print_table_header() {
    echo ""
    printf "+%-4s+%-60s+%-6s+%-8s+\n" "----" "------------------------------------------------------------" "------" "--------"
    printf "| %-2s | %-58s | %-4s | %-6s |\n" "No" "Test Name" "Time" "Result"
    printf "+%-4s+%-60s+%-6s+%-8s+\n" "----" "------------------------------------------------------------" "------" "--------"
}

# 打印表格行
print_table_row() {
    local num="$1"
    local name="$2"
    local time="$3"
    local result="$4"

    printf "| %-2s | %-58s | %-4s | %-6s |\n" "$num" "$name" "$time" "$result"
}

# 打印表格尾
print_table_footer() {
    printf "+%-4s+%-60s+%-6s+%-8s+\n" "----" "------------------------------------------------------------" "------" "--------"
}

# 导出所有函数供子脚本使用
export -f log_info log_test log_success log_error log_warn
export -f remote remote_output copy_to_remote copy_from_remote remote_can_connect forget_known_host
export -f cleanup_container cleanup_all_containers cleanup_micrun_runtime_state
export -f get_container_status wait_for_container_status container_exists
export -f kubectl_cmd wait_for_pod delete_pod get_pod_status
export -f assert_equals assert_contains assert_not_empty assert_success wait_until
export -f timer_start timer_end
export -f image_exists import_image
export -f get_log_dir create_log_file
export -f check_host check_port
export -f extract_number
export -f print_table_header print_table_row print_table_footer
