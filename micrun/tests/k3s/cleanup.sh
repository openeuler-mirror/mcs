#!/bin/bash
# MicRun K3s 环境清理脚本

set -e

echo "================================"
echo "MicRun K3s 环境清理"
echo "================================"
echo ""

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${YELLOW}此脚本将清理所有MicRun K3s测试环境${NC}"
echo "包括: QEMU虚拟机、网络接口、K3s服务器、临时文件"
echo ""
read -p "确认清理? (yes/no): " confirm

if [ "$confirm" != "yes" ]; then
    echo "清理已取消"
    exit 0
fi

LOCAL_SUDO_PASSWORD="${QEMU_LOCAL_SUDO_PASSWORD:-${K3S_LOCAL_SUDO_PASSWORD:-}}"

sudo_password_note() {
    echo -e "${RED}需要宿主机 sudo 权限。请设置 QEMU_LOCAL_SUDO_PASSWORD 或 K3S_LOCAL_SUDO_PASSWORD，并填写当前宿主机可用的 sudo 密码，不要填写示例值。${NC}" >&2
}

sudo_run() {
    if [ -n "$LOCAL_SUDO_PASSWORD" ]; then
        if ! printf '%s\n' "$LOCAL_SUDO_PASSWORD" | sudo -S "$@"; then
            sudo_password_note
            return 1
        fi
        return 0
    fi

    if ! sudo "$@"; then
        sudo_password_note
        return 1
    fi
}

echo -e "${YELLOW}[1/6] 停止K3s服务器...${NC}"
if pgrep -f "k3s.*server" > /dev/null; then
    sudo_run pkill -f "k3s.*server" || true
    sudo_run rm -rf /tmp/micrun-k3s-server* || true
    echo -e "${GREEN}✓${NC} K3s服务器已停止"
else
    echo -e "${YELLOW}○${NC} K3s服务器未运行"
fi

echo -e "${YELLOW}[2/6] 停止QEMU虚拟机...${NC}"
if pgrep -f "qemu.*xen" > /dev/null; then
    sudo_run pkill -f "qemu-system-aarch64.*xen-qemu-aarch64" || true
    sleep 2
    if pgrep -f "qemu.*xen" > /dev/null; then
        sudo_run pkill -9 -f "qemu.*xen" || true
    fi
    echo -e "${GREEN}✓${NC} QEMU虚拟机已停止"
else
    echo -e "${YELLOW}○${NC} QEMU虚拟机未运行"
fi

echo -e "${YELLOW}[3/6] 清理网络接口...${NC}"
if ip addr show tap0 &>/dev/null; then
    sudo_run ip link set tap0 down || true
    sudo_run ip tuntap del dev tap0 mode tap || true
    echo -e "${GREEN}✓${NC} tap0接口已清理"
else
    echo -e "${YELLOW}○${NC} tap0接口不存在"
fi

echo -e "${YELLOW}[4/6] 清理SSH密钥...${NC}"
if ssh-keygen -F "[127.0.0.1]:10022" &>/dev/null; then
    ssh-keygen -R "[127.0.0.1]:10022" 2>/dev/null || true
    echo -e "${GREEN}✓${NC} SSH密钥已清理"
else
    echo -e "${YELLOW}○${NC} SSH密钥不存在"
fi

echo -e "${YELLOW}[5/6] 清理临时文件...${NC}"
rm -f /tmp/qemu_startup.log /tmp/k3s-server.log /tmp/k3s_test_result.log 2>/dev/null || true
echo -e "${GREEN}✓${NC} 临时文件已清理"

echo -e "${YELLOW}[6/6] 清理K3s资产...${NC}"
# 保留K3s资产供下次使用，如果需要完全删除，取消下面的注释
# rm -rf /tmp/micrun-k3s-assets 2>/dev/null || true
echo -e "${GREEN}✓${NC} K3s资产保留 (供下次使用)"

echo ""
echo -e "${GREEN}================================${NC}"
echo -e "${GREEN}环境清理完成！${NC}"
echo -e "${GREEN}================================${NC}"
echo ""
echo "已清理:"
echo "  • QEMU虚拟机和进程"
echo "  • tap0网络接口"
echo "  • K3s服务器进程"
echo "  • SSH已知主机密钥"
echo "  • 临时日志文件"
echo ""
echo "保留:"
echo "  • K3s资产文件 (/tmp/micrun-k3s-assets/)"
echo "  • 部署脚本和文档"
echo ""
echo "如需完全清理，删除以下目录:"
echo "  rm -rf /tmp/micrun-k3s-assets"
echo ""
