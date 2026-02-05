#!/bin/bash
#
# K3s Cloud Setup Script
# 此脚本在云侧（本地主机）安装和配置 K3s Server
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

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

# 检查是否为 root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "此脚本需要 root 权限，请使用 sudo 运行"
        exit 1
    fi
}

# 检查网络
check_network() {
    log_info "检查网络连接..."

    # 获取云侧 IP
    CLOUD_IP=$(ip addr show | grep "inet " | grep -v 127.0.0.1 | head -1 | awk '{print $2}' | cut -d/ -f1)

    if [ -z "$CLOUD_IP" ]; then
        log_error "无法获取本机 IP 地址"
        exit 1
    fi

    log_info "云侧 IP: $CLOUD_IP"
}

# 安装 K3s Server
install_k3s_server() {
    log_info "开始安装 K3s Server..."

    # 下载安装脚本
    if [ ! -f /tmp/install-k3s.sh ]; then
        curl -sfL https://get.k3s.io -o /tmp/install-k3s.sh
    fi

    # 安装 K3s
    # --write-kubeconfig-mode=644: 允许非 root 用户读取 kubeconfig
    # --tls-san: 添加 TLS SAN
    # --disable traefik: 禁用 traefik（嵌入式环境资源有限）
    export INSTALL_K3S_EXEC="--write-kubeconfig-mode=644 --tls-san $CLOUD_IP --disable traefik"

    sh /tmp/install-k3s.sh

    log_info "等待 K3s 启动..."
    sleep 30

    # 检查 K3s 状态
    if systemctl is-active --quiet k3s; then
        log_info "K3s Server 运行正常"
    else
        log_error "K3s Server 启动失败"
        journalctl -u k3s -n 50 --no-pager
        exit 1
    fi
}

# 安装 kubectl
install_kubectl() {
    log_info "检查 kubectl..."

    if command -v kubectl &> /dev/null; then
        log_info "kubectl 已安装: $(kubectl version --client --short 2>&1 | head -1)"
        return 0
    fi

    log_info "安装 kubectl..."

    # 获取最新稳定版本
    KUBECTL_VERSION=$(curl -L -s https://dl.k8s.io/release/stable.txt)

    # 下载
    curl -LO "https://dl.k8s.io/release/$KUBECTL_VERSION/bin/linux/amd64/kubectl"

    # 安装
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

    # 验证
    kubectl version --client

    log_info "kubectl 安装成功"
}

# 获取 node token
get_node_token() {
    log_info "获取 node token..."

    NODE_TOKEN=$(cat /var/lib/rancher/k3s/server/node-token)

    if [ -z "$NODE_TOKEN" ]; then
        log_error "无法获取 node token"
        exit 1
    fi

    log_info "Node Token: ${NODE_TOKEN:0:50}..."
    log_info "完整 Token: $NODE_TOKEN"

    # 保存到文件
    echo "$NODE_TOKEN" > /tmp/k3s-node-token.txt
    chmod 600 /tmp/k3s-node-token.txt

    log_info "Token 已保存到: /tmp/k3s-node-token.txt"
}

# 验证集群
verify_cluster() {
    log_info "验证集群状态..."

    # 等待节点就绪
    log_info "等待节点就绪..."
    local max_attempts=30
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        if kubectl get nodes &> /dev/null; then
            break
        fi
        attempt=$((attempt + 1))
        sleep 2
    done

    if [ $attempt -eq $max_attempts ]; then
        log_error "集群未在预期时间内就绪"
        exit 1
    fi

    # 显示节点状态
    log_info "集群节点状态:"
    kubectl get nodes -o wide

    # 显示版本信息
    log_info "K3s 版本:"
    k3s --version
}

# 打印连接信息
print_connection_info() {
    log_info "========================================="
    log_info "K3s Server 安装完成！"
    log_info "========================================="
    log_info ""
    log_info "云侧 IP: $CLOUD_IP"
    log_info "API Server: https://$CLOUD_IP:6443"
    log_info ""
    log_info "边侧节点加入命令:"
    log_info "-----------------------------------------"
    log_info "export K3S_URL=\"https://$CLOUD_IP:6443\""
    log_info "export K3S_TOKEN=\"$NODE_TOKEN\""
    log_info ""
    log_info "curl -sfL https://get.k3s.io | \\"
    log_info "  K3S_URL=\${K3S_URL} \\"
    log_info "  K3S_TOKEN=\${K3S_TOKEN} \\"
    log_info "  sh -"
    log_info "-----------------------------------------"
    log_info ""
    log_info "Token 文件: /tmp/k3s-node-token.txt"
    log_info ""
    log_info "下一步："
    log_info "1. 复制 token 到边侧节点"
    log_info "2. 在边侧节点执行加入命令"
    log_info "3. 运行 kubectl get nodes 验证"
    log_info "========================================="
}

# 主函数
main() {
    log_info "========================================="
    log_info "K3s Cloud Setup Script"
    log_info "========================================="
    log_info ""

    check_root
    check_network
    install_k3s_server
    install_kubectl
    get_node_token
    verify_cluster
    print_connection_info

    log_info "云侧部署完成！"
}

# 运行主函数
main "$@"
