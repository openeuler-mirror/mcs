#!/bin/bash
#
# Deploy RTOS Test Pod
# 此脚本创建并部署一个测试 RTOS Pod
#

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
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

log_step() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# 检查 kubectl
if ! command -v kubectl &> /dev/null; then
    log_error "kubectl 未安装，请先安装"
    exit 1
fi

# 检查集群连接
if ! kubectl get nodes &> /dev/null; then
    log_error "无法连接到 Kubernetes 集群"
    log_error "请检查 kubeconfig 配置"
    exit 1
fi

# 默认配置
IMAGE_NAME="${RTOS_IMAGE_NAME:-localhost:5000/mica-uniproton-app:xen-0.1}"
POD_NAME="${RTOS_POD_NAME:-rtos-test}"
NODE_NAME="${EDGE_NODE_NAME:-edge}"

log_info "========================================="
log_info "Deploy RTOS Test Pod"
log_info "========================================="
log_info ""
log_info "配置:"
log_info "  Pod 名称: $POD_NAME"
log_info "  镜像: $IMAGE_NAME"
log_info "  目标节点: $NODE_NAME"
log_info ""

# 步骤 1: 检查 RuntimeClass
log_step "1. 检查 RuntimeClass..."
if ! kubectl get runtimeclass micrun &> /dev/null; then
    log_warn "RuntimeClass 'micrun' 不存在，正在创建..."

    kubectl apply -f - <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
EOF

    log_info "RuntimeClass 创建成功"
else
    log_info "RuntimeClass 'micrun' 已存在"
fi

# 步骤 2: 检查边侧节点
log_step "2. 检查边侧节点..."
if ! kubectl get nodes "$NODE_NAME" &> /dev/null; then
    log_error "节点 '$NODE_NAME' 不存在"
    log_info "可用节点:"
    kubectl get nodes
    exit 1
fi

NODE_STATUS=$(kubectl get nodes "$NODE_NAME" -o jsonpath='{.status.phase}')
if [ "$NODE_STATUS" != "Ready" ] && [ "$NODE_STATUS" != "Running" ]; then
    log_warn "节点 '$NODE_NAME' 状态: $NODE_STATUS"
    log_warn "Pod 可能无法正常运行"
fi

log_info "边侧节点 '$NODE_NAME' 状态正常"

# 步骤 3: 创建 Pod 配置
log_step "3. 创建 Pod 配置..."

cat > /tmp/$POD_NAME.yaml <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $POD_NAME
  labels:
    app: rtos-test
spec:
  runtimeClassName: micrun
  nodeName: $NODE_NAME
  containers:
  - name: rtos-app
    image: $IMAGE_NAME
    command: ["/bin/sh"]
    tty: true
    stdin: true
    resources:
      limits:
        cpu: "1000m"
        memory: "512Mi"
    env:
    - name: MIKA_RUN_ANNOTATION
      value: "org.openeuler.micrun.container.auto_close=true"
EOF

log_info "Pod 配置已创建: /tmp/$POD_NAME.yaml"

# 步骤 4: 部署 Pod
log_step "4. 部署 Pod..."

if kubectl get pod "$POD_NAME" &> /dev/null; then
    log_warn "Pod '$POD_NAME' 已存在，删除旧 Pod..."
    kubectl delete pod "$POD_NAME"
    sleep 5
fi

kubectl apply -f /tmp/$POD_NAME.yaml

log_info "Pod 已部署"

# 步骤 5: 等待 Pod 启动
log_step "5. 等待 Pod 启动..."

log_info "等待 Pod 就绪（最多 120 秒）..."
local max_attempts=60
local attempt=0

while [ $attempt -lt $max_attempts ]; do
    POD_PHASE=$(kubectl get pod "$POD_NAME" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Pending")

    if [ "$POD_PHASE" = "Running" ]; then
        log_info "Pod 已就绪"
        break
    fi

    if [ "$POD_PHASE" = "Failed" ] || [ "$POD_PHASE" = "Error" ]; then
        log_error "Pod 启动失败，状态: $POD_PHASE"
        log_error "查看详细信息:"
        kubectl describe pod "$POD_NAME"
        exit 1
    fi

    attempt=$((attempt + 1))
    if [ $((attempt % 10)) -eq 0 ]; then
        log_info "等待中... ($attempt/$max_attempts) 状态: $POD_PHASE"
    fi
    sleep 2
done

if [ $attempt -eq $max_attempts ]; then
    log_error "Pod 未在预期时间内就绪"
    log_error "当前状态: $POD_PHASE"
    log_error ""
    log_error "查看 Pod 详细信息:"
    kubectl describe pod "$POD_NAME"
    log_error ""
    log_error "查看 Pod 日志:"
    kubectl logs "$POD_NAME" --tail=50 || true
    exit 1
fi

# 步骤 6: 显示 Pod 状态
log_step "6. 显示 Pod 状态..."

kubectl get pods "$POD_NAME" -o wide

log_info ""
log_info "Pod 详细信息:"
kubectl describe pod "$POD_NAME" | tail -20

# 步骤 7: 查看日志
log_step "7. 查看 Pod 日志..."

log_info ""
log_info "Pod 日志（最后 50 行）:"
kubectl logs "$POD_NAME" --tail=50 || log_warn "无法获取 Pod 日志"

log_info ""
log_info "========================================="
log_info "Pod 部署成功！"
log_info "========================================="
log_info ""
log_info "常用命令:"
log_info "  查看状态: kubectl get pods $POD_NAME"
log_info "  查看日志: kubectl logs $POD_NAME -f"
log_info "  删除 Pod: kubectl delete pod $POD_NAME"
log_info "  进入 Pod: kubectl exec -it $POD_NAME -- /bin/sh"
log_info ""
log_info "在边侧节点验证:"
log_info "  ctr task ls | grep $POD_NAME"
log_info "  ps aux | grep containerd-shim-mica-v2"
log_info "========================================="
