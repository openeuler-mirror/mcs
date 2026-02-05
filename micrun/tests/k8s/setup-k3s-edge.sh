#!/bin/bash
#
# K3s Edge Setup Script
# 此脚本在边侧节点安装和配置 K3s Agent
#

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查参数
if [ $# -lt 2 ]; then
    log_error "用法: $0 <cloud-ip> <node-token>"
    log_error ""
    log_error "参数说明:"
    log_error "  cloud-ip    : 云侧节点的 IP 地址"
    log_error "  node-token  : K3s node token"
    log_error ""
    log_error "示例:"
    log_error "  $0 192.168.7.1 'K10abc...::server:xyz...'"
    exit 1
fi

CLOUD_IP="$1"
NODE_TOKEN="$2"

log_info "========================================="
log_info "K3s Edge Setup Script"
log_info "========================================="
log_info ""
log_info "云侧 IP: $CLOUD_IP"
log_info ""

# 检查网络连通性
log_info "检查网络连通性..."
if ! ping -c 3 -W 2 "$CLOUD_IP" &> /dev/null; then
    log_error "无法连接到云侧节点 ($CLOUD_IP)"
    log_error "请检查:"
    log_error "  1. 云侧节点是否已安装 K3s Server"
    log_error "  2. 网络是否连通"
    log_error "  3. 防火墙是否允许 6443 端口"
    exit 1
fi

log_info "网络连通性正常"

# 测试 API Server 连接
log_info "测试 K3s API Server 连接..."
if ! curl -k --connect-timeout 5 "https://$CLOUD_IP:6443/healthz" &> /dev/null; then
    log_error "无法连接到 K3s API Server ($CLOUD_IP:6443)"
    log_error "请检查云侧 K3s Server 是否正常运行"
    exit 1
fi

log_info "API Server 连接正常"

# 设置环境变量
export K3S_URL="https://$CLOUD_IP:6443"
export K3S_TOKEN="$NODE_TOKEN"

# 下载并安装 K3s Agent
log_info "安装 K3s Agent..."
log_info "K3S_URL: $K3S_URL"
log_info "K3S_TOKEN: ${NODE_TOKEN:0:30}..."

# 下载安装脚本
if [ ! -f /tmp/install-k3s.sh ]; then
    curl -sfL https://get.k3s.io -o /tmp/install-k3s.sh
fi

# 安装 K3s Agent
curl -sfL https://get.k3s.io | \
    K3S_URL=${K3S_URL} \
    K3S_TOKEN=${K3S_TOKEN} \
    sh -

log_info "等待 K3s Agent 启动..."
sleep 30

# 检查 K3s Agent 状态
if systemctl is-active --quiet k3s-agent; then
    log_info "K3s Agent 运行正常"
else
    log_error "K3s Agent 启动失败"
    log_error "查看日志:"
    log_error "  journalctl -u k3s-agent -n 50 --no-pager"
    exit 1
fi

# 显示 K3s Agent 日志（最后 20 行）
log_info "K3s Agent 日志（最后 20 行）:"
journalctl -u k3s-agent -n 20 --no-pager

log_info ""
log_info "========================================="
log_info "边侧节点部署完成！"
log_info "========================================="
log_info ""
log_info "下一步（在云侧节点执行）:"
log_info "  kubectl get nodes -o wide"
log_info ""
log_info "预期输出:"
log_info "  NAME    STATUS   ROLES                       AGE   VERSION"
log_info "  cloud   Ready    control-plane,etcd,master   5m    v1.28.X+k3s1"
log_info "  edge    Ready    <none>                      1m    v1.28.X+k3s1"
log_info "========================================="
