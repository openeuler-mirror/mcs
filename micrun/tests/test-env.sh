#!/bin/bash
# MicRun 测试环境配置
# 使用方式: source tests/test-env.sh

# ============================================
# 远程主机配置
# ============================================
export TEST_REMOTE_HOST="${TEST_REMOTE_HOST:-root@192.168.7.2}"

# ============================================
# 测试镜像配置
# ============================================
export TEST_IMAGE="${TEST_IMAGE:-localhost:5000/mica-uniproton-app:xen-0.1}"

# ============================================
# K3s 配置
# ============================================
export K3S_MASTER_URL="${K3S_MASTER_URL:-https://192.168.1.100:6443}"
export K3S_MASTER_NODE="${K3S_MASTER_NODE:-root@192.168.1.100}"
export K3S_WORKER_NODES="${K3S_WORKER_NODES:-root@192.168.1.101}"

# ============================================
# 测试超时配置
# ============================================
export TEST_TIMEOUT_DEFAULT="${TEST_TIMEOUT_DEFAULT:-60}"
export TEST_TIMEOUT_POD="${TEST_TIMEOUT_POD:-120}"
export TEST_TIMEOUT_CONTAINER="${TEST_TIMEOUT_CONTAINER:-30}"

# ============================================
# 测试日志目录
# ============================================
export TEST_LOG_DIR="${TEST_LOG_DIR:-/tmp/micrun-tests}"

# 创建日志目录
mkdir -p "$TEST_LOG_DIR"

# ============================================
# 兼容性: 保持旧变量名
# ============================================
export REMOTE_HOST="$TEST_REMOTE_HOST"

# ============================================
# 显示环境配置
# ============================================
echo "MicRun 测试环境:"
echo "  REMOTE_HOST=$TEST_REMOTE_HOST"
echo "  TEST_IMAGE=$TEST_IMAGE"
echo "  K3S_MASTER=$K3S_MASTER_NODE"
echo "  K3S_WORKERS=$K3S_WORKER_NODES"
echo "  LOG_DIR=$TEST_LOG_DIR"
echo ""
