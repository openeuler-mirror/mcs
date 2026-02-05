# K3s 云化测试

## 测试概述

本目录包含 MicRun 与 K3s/Kubernetes 云原生环境集成的测试用例，验证 RTOS 容器在云边协同场景下的功能。

## 环境要求

### 必需组件

- **K3s 或 Kubernetes**: 1.28+ 版本
- **kubectl**: 命令行工具
- **containerd**: 带有 MicRun 运行时
- **网络**: 节点间网络互通

### 环境配置

编辑 `tests/test-env.sh` 配置 K3s 环境：

```bash
# K3s Master 节点（控制平面）
export K3S_MASTER_NODE="root@192.168.1.100"

# K3s Worker 节点（可选，用于多节点测试）
export K3S_WORKER_NODES="root@192.168.1.101,root@192.168.1.102"

# K3s API URL
export K3S_MASTER_URL="https://192.168.1.100:6443"
```

### 前置准备

```bash
# 1. 准备测试镜像
scp mica-uniproton-app-xen-0.1.tar.gz root@192.168.1.101:/tmp/
ssh root@192.168.1.101 "ctr image import /tmp/mica-uniproton-app-xen-0.1.tar.gz"

# 2. 验证 K3s 连接
ssh root@192.168.1.100 "kubectl get nodes"

# 3. 验证 containerd 和 MicRun
ssh root@192.168.1.101 "ctr version"
ssh root@192.168.1.101 "ls -l /usr/bin/containerd-shim-mica-v2"
```

## 测试用例

| ID | 测试名称 | 说明 | 预期结果 |
|----|----------|------|----------|
| K3S-001 | RuntimeClass 创建 | 验证 RuntimeClass 资源创建 | RuntimeClass 存在 |
| K3S-002 | Pod 启动/停止 | 验证 Pod 生命周期管理 | Pod 能正常启动和停止 |
| K3S-003 | Deployment 扩缩容 | 验证副本扩缩容功能 | 副本能正常扩容和缩容 |
| K3S-004 | Pod 日志获取 | 验证日志输出功能 | 能获取容器日志 |
| K3S-005 | 资源限制 | 验证 CPU/内存限制 | 资源限制生效 |
| K3S-006 | 多节点部署 | 验证云边协同 | Pod 能调度到不同节点 |
| K3S-007 | 故障恢复 | 验证自愈能力 | Pod 删除后自动重建 |

## 执行方式

### 运行所有测试

```bash
cd tests/k3s
./run_k3s_tests.sh
```

### 运行单个测试

```bash
cd tests/k3s
./run_k3s_tests.sh K3S-001
```

### 指定 K3s Master 节点

```bash
export K3S_MASTER_NODE="root@192.168.1.100"
./run_k3s_tests.sh
```

## 测试架构

```
┌─────────────────────────────────────────────────────────────────┐
│                      K3s Control Plane                         │
│                     (Master Node)                              │
│  - API Server                                                  │
│  - Scheduler                                                   │
│  - Controller Manager                                          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Edge Nodes                                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │   Pod 1      │  │   Pod 2      │  │   Pod 3      │         │
│  │   (RTOS)     │  │   (RTOS)     │  │   (RTOS)     │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
│         │                 │                 │                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │  MicRun      │  │  MicRun      │  │  MicRun      │         │
│  │  (Shim v2)   │  │  (Shim v2)   │  │  (Shim v2)   │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
└─────────────────────────────────────────────────────────────────┘
```

## 典型测试场景

### 场景 1: 单节点 RTOS Pod

```bash
# 创建 RuntimeClass
kubectl apply -f - <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
EOF

# 创建 Pod
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: rtos-pod
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos
    image: localhost:5000/mica-uniproton-app:xen-0.1
    tty: true
    stdin: true
EOF

# 查看状态
kubectl get pods
kubectl logs rtos-pod

# 清理
kubectl delete pod rtos-pod
```

### 场景 2: 多副本 Deployment

```bash
# 创建 Deployment
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rtos-deployment
spec:
  replicas: 3
  selector:
    matchLabels:
      app: rtos-app
  template:
    metadata:
      labels:
        app: rtos-app
    spec:
      runtimeClassName: micrun
      containers:
      - name: rtos
        image: localhost:5000/mica-uniproton-app:xen-0.1
        tty: true
        stdin: true
EOF

# 扩容
kubectl scale deployment rtos-deployment --replicas=5

# 缩容
kubectl scale deployment rtos-deployment --replicas=2

# 清理
kubectl delete deployment rtos-deployment
```

### 场景 3: 带资源限制

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: rtos-limited
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos
    image: localhost:5000/mica-uniproton-app:xen-0.1
    tty: true
    stdin: true
    resources:
      requests:
        memory: "64Mi"
        cpu: "250m"
      limits:
        memory: "128Mi"
        cpu: "500m"
EOF
```

## 故障排查

### 问题 1: Pod 一直处于 Pending 状态

**原因**: 节点没有正确配置 MicRun 运行时

**解决**:
```bash
# 检查节点状态
kubectl get nodes -o wide

# 检查 RuntimeClass
kubectl get runtimeclass micrun -o yaml

# 在 Worker 节点检查 containerd 配置
ssh root@worker "cat /etc/containerd/config.toml | grep -A 5 micrun"
```

### 问题 2: Pod 启动失败

**原因**: 镜像不存在或路径错误

**解决**:
```bash
# 检查镜像
ssh root@worker "ctr image ls | grep mica"

# 导入镜像
ctr image import mica-uniproton-app-xen-0.1.tar.gz
```

### 问题 3: 无法获取日志

**原因**: RTOS 容器没有标准输出

**解决**: 检查 shim 的日志转发配置

## 参考资料

- [K8s 集成文档](../../docs/k8s-integration.md)
- [测试框架指南](../../docs/test-framework.md)
- [RuntimeClass 设计](https://kubernetes.io/docs/concepts/containers/runtime-class/)
