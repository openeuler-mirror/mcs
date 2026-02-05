# Kubernetes 云边协同测试脚本

本目录包含用于快速部署和测试 MicRun Kubernetes 云边协同功能的自动化脚本。

## 目录结构

```
tests/k8s/
├── setup-k3s-cloud.sh        # 云侧 K3s Server 安装脚本
├── setup-k3s-edge.sh         # 边侧 K3s Agent 安装脚本
├── deploy-rtos-pod.sh        # RTOS Pod 部署脚本
└── README.md                 # 本文档
```

## 前置条件

### 云侧节点（本地主机）

- 操作系统：Linux（支持 WSL2）
- 网络连通：能与边侧节点通信
- 权限：需要 root 权限（使用 sudo）

### 边侧节点（测试环境）

- 操作系统：openEuler Embedded
- 已安装组件：
  - containerd (v1.7.27+)
  - MicRun 运行时
  - RTOS 测试镜像

## 快速开始

### 步骤 1: 安装 MicRun 到边侧节点

如果边侧节点还没有安装 MicRun：

```bash
# 在云侧（开发机）构建 MicRun
cd /path/to/mcs/micrun
make build

# 传输到边侧
scp build/micrun root@192.168.7.2:/usr/local/bin/

# 在边侧设置权限
ssh root@192.168.7.2 "chmod +x /usr/local/bin/micrun"

# 在边侧注册到 containerd
ssh root@192.168.7.2
cat >> /etc/containerd/config.toml <<'EOF'

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun."]
EOF

# 重启 containerd
systemctl restart containerd
```

### 步骤 2: 在云侧安装 K3s Server

```bash
cd /path/to/mcs/micrun/tests/k8s

# 运行安装脚本（需要 root 权限）
sudo bash setup-k3s-cloud.sh
```

**脚本功能**：
- ✅ 检查网络配置
- ✅ 下载并安装 K3s Server
- ✅ 安装 kubectl
- ✅ 获取 node token
- ✅ 验证集群状态

**预期输出**：

```
=========================================
K3s Server 安装完成！
=========================================

云侧 IP: 192.168.7.1
API Server: https://192.168.7.1:6443

边侧节点加入命令:
-----------------------------------------
export K3S_URL="https://192.168.7.1:6443"
export K3S_TOKEN="K10abc...::server:xyz..."

curl -sfL https://get.k3s.io | \
  K3S_URL=${K3S_URL} \
  K3S_TOKEN=${K3S_TOKEN} \
  sh -
-----------------------------------------

Token 文件: /tmp/k3s-node-token.txt

下一步：
1. 复制 token 到边侧节点
2. 在边侧节点执行加入命令
3. 运行 kubectl get nodes 验证
=========================================
```

**重要**：
- 保存 node token（位于 `/tmp/k3s-node-token.txt`）
- 下一步在边侧节点使用此 token

### 步骤 3: 在边侧安装 K3s Agent

```bash
# 在云侧获取 token
NODE_TOKEN=$(cat /tmp/k3s-node-token.txt)
CLOUD_IP="192.168.7.1"

# 将脚本传输到边侧
scp setup-k3s-edge.sh root@192.168.7.2:/tmp/

# 在边侧执行（需要替换实际参数）
ssh root@192.168.7.2
cd /tmp
bash setup-k3s-edge.sh $CLOUD_IP "$NODE_TOKEN"
```

**脚本功能**：
- ✅ 检查网络连通性
- ✅ 测试 API Server 连接
- ✅ 下载并安装 K3s Agent
- ✅ 验证 K3s Agent 状态

**预期输出**：

```
=========================================
边侧节点部署完成！
=========================================

下一步（在云侧节点执行）:
  kubectl get nodes -o wide

预期输出:
  NAME    STATUS   ROLES                       AGE   VERSION
  cloud   Ready    control-plane,etcd,master   5m    v1.28.X+k3s1
  edge    Ready    <none>                      1m    v1.28.X+k3s1
=========================================
```

### 步骤 4: 验证节点加入

在云侧执行：

```bash
# 查看节点
kubectl get nodes -o wide

# 预期输出：
# NAME    STATUS   ROLES                       AGE   VERSION
# cloud   Ready    control-plane,etcd,master   5m    v1.28.X+k3s1
# edge    Ready    <none>                      1m    v1.28.X+k3s1
```

如果看到边侧节点（`edge`）状态为 `Ready`，说明集群部署成功！

### 步骤 5: 部署 RTOS 测试 Pod

```bash
# 在云侧执行
cd /path/to/mcs/micrun/tests/k8s

# 部署 Pod（可选：自定义配置）
export RTOS_IMAGE_NAME="localhost:5000/mica-uniproton-app:xen-0.1"
export RTOS_POD_NAME="rtos-test"
export EDGE_NODE_NAME="edge"

bash deploy-rtos-pod.sh
```

**脚本功能**：
- ✅ 检查并创建 RuntimeClass
- ✅ 验证边侧节点状态
- ✅ 创建 Pod 配置
- ✅ 部署 Pod
- ✅ 等待 Pod 就绪
- ✅ 显示 Pod 状态和日志

**预期输出**：

```
=========================================
Pod 部署成功！
=========================================

常用命令:
  查看状态: kubectl get pods rtos-test
  查看日志: kubectl logs rtos-test -f
  删除 Pod: kubectl delete pod rtos-test
  进入 Pod: kubectl exec -it rtos-test -- /bin/sh

在边侧节点验证:
  ctr task ls | grep rtos-test
  ps aux | grep containerd-shim-mica-v2
=========================================
```

### 步骤 6: 验证 Pod 运行

**在云侧**：

```bash
# 查看 Pod 状态
kubectl get pods rtos-test -o wide

# 查看详细信息
kubectl describe pod rtos-test

# 查看日志
kubectl logs rtos-test -f
```

**在边侧**：

```bash
# 查看 containerd 任务
ctr task ls | grep rtos

# 查看容器状态
ctr container ls | grep rtos

# 查看 shim 进程
ps aux | grep containerd-shim-mica-v2

# 查看 Xen domain（如果使用 Xen）
xl list
```

## 高级用法

### 自定义 Pod 配置

编辑 `deploy-rtos-pod.sh` 中的默认配置，或通过环境变量覆盖：

```bash
export RTOS_IMAGE_NAME="localhost:5000/mica-zephyr-app:xen-0.1"
export RTOS_POD_NAME="rtos-demo"
export EDGE_NODE_NAME="edge-node-1"

bash deploy-rtos-pod.sh
```

### 使用 Deployment 管理多个副本

```bash
# 创建 Deployment
cat > rtos-deployment.yaml <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rtos-deployment
spec:
  replicas: 3
  selector:
    matchLabels:
      app: rtos-test
  template:
    metadata:
      labels:
        app: rtos-test
    spec:
      runtimeClassName: micrun
      nodeName: edge
      containers:
      - name: rtos
        image: localhost:5000/mica-uniproton-app:xen-0.1
        tty: true
        stdin: true
        resources:
          limits:
            cpu: "500m"
            memory: "256Mi"
EOF

kubectl apply -f rtos-deployment.yaml

# 查看状态
kubectl get pods -l app=rtos-test
```

### 测试自动恢复

```bash
# 删除一个 Pod
kubectl delete pod -l app=rtos-test --random-

# 查看 Deployment 自动重建
kubectl get pods -l app=rtos-test -w
```

## 故障排查

### 问题 1: K3s Server 无法启动

**症状**：`systemctl status k3s` 显示失败

**排查步骤**：

```bash
# 查看详细日志
journalctl -u k3s -n 100 --no-pager

# 检查端口占用
sudo netstat -tlnp | grep 6443

# 检查防火墙
sudo iptables -L -n | grep 6443
```

### 问题 2: 边侧节点无法加入

**症状**：`kubectl get nodes` 看不到边侧节点

**排查步骤**：

```bash
# 1. 在边侧检查 K3s Agent 状态
systemctl status k3s-agent

# 2. 在边侧查看日志
journalctl -u k3s-agent -n 50 --no-pager

# 3. 测试网络连接
ping <cloud-ip>
curl -k https://<cloud-ip>:6443/healthz

# 4. 检查 token 是否正确
cat /etc/rancher/k3s/agent-token-token
```

### 问题 3: Pod 无法启动

**症状**：`kubectl get pods` 显示 `ContainerCreating` 或 `CrashLoopBackOff`

**排查步骤**：

```bash
# 1. 查看 Pod 详细信息
kubectl describe pod rtos-test

# 2. 查看 Events 部分
kubectl describe pod rtos-test | grep -A 20 Events

# 3. 查看日志
kubectl logs rtos-test --previous

# 4. 在边侧验证
ctr image ls | grep mica
ctr task ls
ps aux | grep micrun
```

### 问题 4: RuntimeClass 不生效

**症状**：Pod 没有使用 MicRun 运行时

**排查步骤**：

```bash
# 1. 检查 RuntimeClass
kubectl get runtimeclass micrun -o yaml

# 2. 在边侧检查 containerd 配置
cat /etc/containerd/config.toml | grep -A 3 micrun

# 3. 在边侧重启 containerd
systemctl restart containerd

# 4. 重新部署 Pod
kubectl delete pod rtos-test
bash deploy-rtos-pod.sh
```

## 清理环境

### 清理 Pod 和资源

```bash
# 删除 Pod
kubectl delete pod rtos-test

# 删除 Deployment（如果创建）
kubectl delete deployment rtos-deployment

# 删除 RuntimeClass
kubectl delete runtimeclass micrun
```

### 清理 K3s Agent（边侧）

```bash
ssh root@192.168.7.2
systemctl stop k3s-agent
systemctl disable k3s-agent
/usr/local/bin/k3s-agent-uninstall.sh
```

### 清理 K3s Server（云侧）

```bash
sudo /usr/local/bin/k3s-uninstall.sh

# 清理数据（可选）
sudo rm -rf /etc/rancher/k3s
sudo rm -rf /var/lib/rancher/k3s
```

## 脚本说明

### setup-k3s-cloud.sh

**功能**：在云侧安装 K3s Server

**主要步骤**：
1. 检查 root 权限
2. 检查网络配置
3. 下载并安装 K3s Server
4. 安装 kubectl
5. 获取 node token
6. 验证集群状态
7. 打印连接信息

**环境变量**：
- 无

**输出文件**：
- `/tmp/k3s-node-token.txt` - node token

### setup-k3s-edge.sh

**功能**：在边侧安装 K3s Agent

**主要步骤**：
1. 检查参数（云侧 IP 和 token）
2. 测试网络连通性
3. 测试 API Server 连接
4. 下载并安装 K3s Agent
5. 验证 K3s Agent 状态
6. 显示日志和下一步操作

**参数**：
- `$1` - 云侧节点 IP
- `$2` - node token

**环境变量**：
- `K3S_URL` - API Server URL
- `K3S_TOKEN` - node token

### deploy-rtos-pod.sh

**功能**：部署 RTOS 测试 Pod

**主要步骤**：
1. 检查 kubectl 和集群连接
2. 检查并创建 RuntimeClass
3. 验证边侧节点状态
4. 创建 Pod 配置
5. 部署 Pod
6. 等待 Pod 就绪
7. 显示 Pod 状态和日志

**环境变量**：
- `RTOS_IMAGE_NAME` - RTOS 镜像名称（默认：`localhost:5000/mica-uniproton-app:xen-0.1`）
- `RTOS_POD_NAME` - Pod 名称（默认：`rtos-test`）
- `EDGE_NODE_NAME` - 边侧节点名称（默认：`edge`）

**输出文件**：
- `/tmp/$POD_NAME.yaml` - Pod 配置文件

## 相关文档

- [Kubernetes 集成指南](../../docs/k8s-integration.md) - 详细的部署指南
- [快速入门](../../docs/quick-start.md) - MicRun 基础使用
- [测试指南](../../docs/testing-guide.md) - 测试方法和用例
- [测试计划](../../docs/k8s-test-plan.md) - 完整的测试计划

## 问题反馈

如果遇到问题或有改进建议，请：

1. 查看相关文档
2. 检查日志信息
3. 提交 Issue: [https://atomgit.com/openeuler/mcs/issues](https://atomgit.com/openeuler/mcs/issues)

---

**最后更新**：2026-01-22
**维护者**：MicRun 开发团队
