# MicRun Kubernetes 云边协同指南

> **本文约定**：
> - **云侧** (Cloud)：运行 K3s server 的管理节点
> - **边侧** (Edge)：运行 K3s agent 的工作节点，实际运行 RTOS 容器

## 目录

- [架构概览](#架构概览)
- [前置准备](#前置准备)
- [云侧部署](#云侧部署)
- [边侧部署](#边侧部署)
- [运行第一个 RTOS Pod](#运行第一个-rtos-pod)
- [故障排查](#故障排查)
- [高级用法](#高级用法)

---

## 架构概览

```
┌─────────────────────────────────────────────────────────────┐
│                        云侧 (Cloud)                          │
│  ┌─────────────────────────────────────────────────────┐    │
│  │              K3s Server (Management)                 │    │
│  │  - Kubernetes API                                    │    │
│  │  - Scheduler                                         │    │
│  │  - Controller Manager                               │    │
│  └───────────────────┬─────────────────────────────────┘    │
│                      │ (API Server)                         │
└──────────────────────┼──────────────────────────────────────┘
                       │ (Network)
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                     边侧 (Edge)                              │
│  ┌─────────────────────────────────────────────────────┐    │
│  │              K3s Agent                               │    │
│  │  - kubelet                                           │    │
│  │  - containerd                                        │    │
│  │  - MicRun Runtime                                    │    │
│  └───────────────────┬─────────────────────────────────┘    │
│                      │                                        │
│  ┌───────────────────▼─────────────────────────────────┐    │
│  │              RTOS Container                          │    │
│  │  - Zephyr/UniProton App                             │    │
│  │  - Managed by MicRun                                │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────┘
```

**关键组件说明**：

| 组件 | 位置 | 作用 |
|------|------|------|
| K3s Server | 云侧 | 提供 Kubernetes API，管理集群状态 |
| K3s Agent | 边侧 | 运行 kubelet，管理容器运行时 |
| containerd | 边侧 | 容器引擎，加载 MicRun 运行时 |
| MicRun | 边侧 | Shim v2 运行时，管理 RTOS 容器 |
| RTOS App | 边侧 | 实际运行的用户应用 |

---

## 前置准备

### 硬件要求

**云侧节点**：
- CPU: 2 核心及以上
- 内存: 2GB 及以上
- 网络: 与边侧节点连通

**边侧节点**：
- CPU: 4 核心及以上（推荐用于虚拟化）
- 内存: 4GB 及以上
- 网络: 与云侧节点连通
- 虚拟化: 支持 Xen 或 OpenAMP

### 软件要求

| 软件 | 云侧 | 边侧 | 说明 |
|------|------|------|------|
| 操作系统 | Linux | openEuler Embedded | 边侧需包含 micrun, micad, xen |
| K3s | v1.28+ | v1.28+ | 云侧安装 server，边侧安装 agent |
| containerd | - | 1.7.27+ | 边侧需要，云侧 K3s 自带 |
| MicRun | - | 已安装 | 边侧需要 |

### 网络要求

- 云侧和边侧节点网络互通
- 边侧节点需要能够访问云侧的 K3s API Server（默认端口 6443）
- 确保防火墙允许必要端口通信

---

## 云侧部署

### 步骤 1: 安装 K3s Server

在云侧节点执行：

```bash
# 1. 下载 K3s 安装脚本
curl -sfL https://get.k3s.io -o install-k3s.sh

# 2. 安装 K3s Server
# INSTALL_K3S_EXEC: 配置 K3s 启动参数
# --tls-san: 添加 API Server 的 TLS SAN（使用云侧 IP 或域名）
sudo INSTALL_K3S_EXEC="\
  --write-kubeconfig-mode=644 \
  --tls-san <cloud-ip-or-domain>" \
  sh ./install-k3s.sh

# 3. 等待 K3s 启动（约 30 秒）
sleep 30

# 4. 验证 K3s 状态
sudo systemctl status k3s

# 5. 获取 Kubeconfig
sudo cat /etc/rancher/k3s/k3s.yaml
```

**重要参数说明**：

| 参数 | 说明 | 示例 |
|------|------|------|
| `--write-kubeconfig-mode=644` | 允许非 root 用户读取 kubeconfig | - |
| `--tls-san` | 添加 API Server 的 TLS SAN | `192.168.1.100` 或 `cloud.example.com` |

### 步骤 2: 获取 Node Token

K3s 会自动生成一个用于节点加入的 token：

```bash
# 查看 token（边侧节点加入时需要）
sudo cat /var/lib/rancher/k3s/server/node-token

# 输出示例：
# K10abc123def456...xyz789::server:abc123def456...
```

**保存此 token**，边侧部署时会用到。

### 步骤 3: 配置 kubectl（可选）

如果在云侧节点直接管理集群：

```bash
# 1. 复制 kubeconfig 到用户目录
mkdir -p ~/.kube
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown $USER:$USER ~/.kube/config

# 2. 验证连接
kubectl get nodes

# 3. 预期输出（此时只有云侧节点）：
# NAME    STATUS   ROLES                       AGE   VERSION
# cloud   Ready    control-plane,etcd,master   1m    v1.28.X+k3s1
```

### 步骤 4: 获取云侧节点 IP

边侧节点需要连接到云侧的 API Server：

```bash
# 获取云侧节点 IP（选择边侧可达的 IP）
ip addr show | grep "inet " | grep -v 127.0.0.1

# 示例输出：
# inet 192.168.1.100/24 brd 192.168.1.255 scope global eth0
```

---

## 边侧部署

### 步骤 1: 确保 MicRun 已安装

边侧节点需要已完成 [MicRun 快速入门](quick-start.md) 的前 5 步：

1. ✅ 构建包含 MicRun 的 openEuler Embedded 系统
2. ✅ 启动系统（QEMU 或真实硬件）
3. ✅ 构建 RTOS 镜像（使用 mica-image-builder）
4. ✅ 导入镜像到 containerd
5. ✅ 注册 MicRun 运行时

验证 MicRun 安装：

```bash
# 检查 MicRun 二进制
which micrun
ls -l $(which micrun)

# 检查 containerd 配置
cat /etc/containerd/config.toml | grep -A 3 micrun

# 预期输出：
# [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
#   runtime_type = "io.containerd.mica.v2"
#   pod_annotations = ["org.openeuler.micrun."]
```

### 步骤 2: 导入 RTOS 镜像

确保 RTOS 镜像已导入 containerd：

```bash
# 查看已导入的镜像
ctr image ls | grep mica

# 示例输出：
# localhost:5000/mica-zephyr-app:xen-0.1
```

如果没有镜像，请参考 [quick-start.md 步骤 3](quick-start.md#步骤-3构建-rtos-容器镜像)。

### 步骤 3: 安装 K3s Agent

在边侧节点执行：

```bash
# 1. 设置环境变量
export K3S_URL="https://<cloud-ip>:6443"  # 替换为云侧节点 IP
export K3S_TOKEN="<node-token>"           # 替换为步骤 2 获取的 token

# 2. 下载并安装 K3s Agent
curl -sfL https://get.k3s.io | \
  K3S_URL=${K3S_URL} \
  K3S_TOKEN=${K3S_TOKEN} \
  sh -

# 3. 等待 K3s Agent 启动（约 30 秒）
sleep 30

# 4. 验证 K3s Agent 状态
sudo systemctl status k3s-agent
```

**常见问题**：

| 问题 | 原因 | 解决方法 |
|------|------|----------|
| 连接被拒绝 | 云侧防火墙阻止 6443 端口 | 云侧执行 `sudo firewall-cmd --add-port=6443/tcp --permanent && sudo firewall-cmd --reload` |
| 证书错误 | TLS SAN 不匹配 | 云侧重启 K3s 并添加正确的 `--tls-san` |
| 找不到 containerd | K3s 自带的 containerd 与系统冲突 | 配置 K3s 使用系统 containerd，见下方配置示例 |

**配置 K3s 使用系统 containerd**：

在边侧节点创建或编辑 `/etc/systemd/system/k3s-agent.service.env`：

```bash
sudo mkdir -p /etc/systemd/system/k3s-agent.service.env
sudo cat > /etc/systemd/system/k3s-agent.service.env/10-containerd.conf <<EOF
# 禁用 K3s 自带的 containerd
K3S_SUPervisor_CONTAINERD=false
# 指定使用系统 containerd 的 socket
CONTAINERD_SOCK=/run/containerd/containerd.sock
EOF

# 重启 K3s Agent
sudo systemctl daemon-reload
sudo systemctl restart k3s-agent

# 验证配置
sudo systemctl status k3s-agent
```

### 步骤 4: 验证节点加入

回到**云侧节点**，验证边侧节点已加入集群：

```bash
# 在云侧节点执行
kubectl get nodes -o wide

# 预期输出：
# NAME    STATUS   ROLES                       AGE   VERSION
# cloud   Ready    control-plane,etcd,master   5m    v1.28.X+k3s1
# edge    Ready    <none>                      1m    v1.28.X+k3s1
```

如果看到边侧节点（如 `edge`）状态为 `Ready`，说明集群部署成功！

### 步骤 5: 注册 MicRun RuntimeClass

在**云侧节点**创建 RuntimeClass：

```bash
# 1. 创建 RuntimeClass 配置文件
cat > micrun-runtimeclass.yaml <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
EOF

# 2. 应用配置
kubectl apply -f micrun-runtimeclass.yaml

# 3. 验证 RuntimeClass
kubectl get runtimeclass

# 预期输出：
# NAME     HANDLER   AGE
# micrun   micrun    10s
```

**重要**：
- RuntimeClass 是集群级别的资源，只需在云侧创建一次
- `handler: micrun` 必须与边侧 containerd 配置中的运行时名称一致

---

## 运行第一个 RTOS Pod

### 步骤 1: 创建 Pod 配置文件

在**云侧节点**创建 Pod 配置：

```bash
cat > rtos-pod.yaml <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: rtos-demo
  annotations:
    org.openeuler.micrun.container.auto_close: "true"
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos-app
    image: localhost:5000/mica-zephyr-app:xen-0.1  # 替换为你的镜像
    command: ["/bin/sh"]
    tty: true
    stdin: true
    resources:
      limits:
        cpu: "1000m"
        memory: "512Mi"
EOF
```

**配置说明**：

| 字段 | 说明 | 示例 |
|------|------|------|
| `runtimeClassName` | 指定使用 MicRun 运行时 | `micrun` |
| `metadata.annotations` | MicRun 配置注解 | `org.openeuler.micrun.container.auto_close: "true"` |
| `image` | RTOS 镜像名称 | 必须是边侧已导入的镜像 |
| `tty: true` | 分配伪终端 | RTOS 容器通常需要 |
| `stdin: true` | 保持标准输入打开 | 支持交互式操作 |
| `resources.limits.cpu` | CPU 限制 | `1000m` = 1 核心 |
| `resources.limits.memory` | 内存限制 | `512Mi` = 512 MB |

### 步骤 2: 部署 Pod

```bash
# 1. 创建 Pod
kubectl apply -f rtos-pod.yaml

# 2. 查看 Pod 状态
kubectl get pods -o wide

# 3. 查看详细信息
kubectl describe pod rtos-demo

# 4. 查看日志（如果有输出）
kubectl logs rtos-demo
```

**预期输出**：

```bash
$ kubectl get pods -o wide
NAME        READY   STATUS    RESTARTS   AGE   IP          NODE
rtos-demo   1/1     Running   0          30s   10.42.0.2   edge
```

### 步骤 3: 验证 Pod 运行

在**边侧节点**验证容器已启动：

```bash
# 查看 containerd 任务
ctr task ls | grep rtos

# 查看容器状态
ctr container ls | grep rtos

# 查看 shim 进程
ps aux | grep containerd-shim-mica-v2

# 查看 Xen domain
sudo xl list

# 预期输出包含 RTOS 容器的 domain
```

### 步骤 4: 交互式访问（可选）

如果 RTOS 镜像支持交互式 shell：

```bash
# 方法 1: 使用 kubectl exec（推荐）
kubectl exec -it rtos-demo -- /bin/sh

# 方法 2: 在边侧直接使用 ctr
ctr task exec --exec-id <random> rtos-demo /bin/sh
```

**注意**：
- `kubectl exec` 需要 MicRun 实现 `Exec` API
- 如果不支持，可以在边侧使用 `ctr task attach` 连接

### 步骤 5: 清理 Pod

```bash
# 删除 Pod
kubectl delete pod rtos-demo

# 验证清理
kubectl get pods
ctr task ls  # 边侧执行
```

---

## 故障排查

### 问题 1: Pod 状态为 `ContainerCreating` 超过 1 分钟

**现象**：
```bash
$ kubectl get pods
NAME        READY   STATUS              RESTARTS   AGE
rtos-demo   0/1     ContainerCreating   0          2m
```

**可能原因**：
1. 边侧 containerd 没有加载 MicRun 运行时
2. RuntimeClass 配置错误
3. 镜像不存在于边侧节点

**排查步骤**：

```bash
# 1. 查看 Pod 详细信息
kubectl describe pod rtos-demo

# 2. 检查 Events 部分，查找错误信息
# 常见错误：
# - "Failed to create pod sandbox: runtime network not ready"
# - "Failed to pull image: no such image"
# - "runtime 'micrun' not found"

# 3. 在边侧验证运行时配置
cat /etc/containerd/config.toml | grep -A 5 micrun

# 4. 在边侧重启 containerd
sudo systemctl restart containerd

# 5. 在边侧查看 containerd 日志
sudo journalctl -u containerd -f
```

### 问题 2: Pod 状态为 `CrashLoopBackOff`

**现象**：
```bash
$ kubectl get pods
NAME        READY   STATUS                 RESTARTS   AGE
rtos-demo   0/1     CrashLoopBackOff       5          5m
```

**可能原因**：
1. RTOS 应用启动失败
2. 资源不足（CPU/内存）
3. MicRun shim 崩溃

**排查步骤**：

```bash
# 1. 查看 Pod 日志
kubectl logs rtos-demo --previous

# 2. 在边侧查看 MicRun 日志
tail -f /var/log/mica/mica-runtime.log

# 3. 在边侧查看 shim 进程状态
ps aux | grep containerd-shim-mica-v2

# 4. 检查资源使用
kubectl top pods
kubectl top nodes

# 5. 查看容器事件
kubectl describe pod rtos-demo | grep -A 20 Events
```

### 问题 3: 边侧节点状态为 `NotReady`

**现象**：
```bash
$ kubectl get nodes
NAME    STATUS   ROLES                       AGE   VERSION
cloud   Ready    control-plane,etcd,master   10m   v1.28.X+k3s1
edge    NotReady <none>                      5m    v1.28.X+k3s1
```

**可能原因**：
1. K3s Agent 未运行
2. 网络连接问题
3. 资源不足

**排查步骤**：

```bash
# 1. 在边侧检查 K3s Agent 状态
sudo systemctl status k3s-agent

# 2. 在边侧查看 K3s Agent 日志
sudo journalctl -u k3s-agent -f

# 3. 检查网络连接（边侧执行）
ping <cloud-ip>
curl -k https://<cloud-ip>:6443/healthz

# 4. 检查防火墙规则
sudo iptables -L -n | grep 6443

# 5. 重启 K3s Agent
sudo systemctl restart k3s-agent
```

### 问题 4: 镜像拉取失败

**现象**：
```bash
$ kubectl describe pod rtos-demo
...
Events:
  Warning  Failed     5m   kubelet  Failed to pull image "localhost:5000/mica-zephyr-app:xen-0.1": Error: no such image
```

**原因**：
- containerd 在边侧节点找不到镜像
- 镜像名称或 tag 不匹配

**解决方法**：

```bash
# 在边侧节点重新导入镜像
ctr image import <image_file>.tar

# 验证镜像名称
ctr image ls | grep mica

# 确保 Pod 配置中的 image 名称与 ctr image ls 输出一致
```

**重要**：
- Kubernetes 使用的是 `image:tag` 格式
- 确保边侧的镜像名称与 Pod 配置完全匹配
- 可以使用 `ctr image tag <source> <target>` 重命名镜像

---

## 高级用法

### 使用 Deployment 管理 RTOS 应用

Deployment 可以管理多个副本并提供自愈能力：

```bash
cat > rtos-deployment.yaml <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rtos-app-deployment
spec:
  replicas: 2  # 运行 2 个 RTOS 实例
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
      - name: rtos-app
        image: localhost:5000/mica-zephyr-app:xen-0.1
        tty: true
        stdin: true
        resources:
          limits:
            cpu: "500m"
            memory: "256Mi"
EOF

kubectl apply -f rtos-deployment.yaml

# 查看 Deployment 状态
kubectl get deployment
kubectl get pods -l app=rtos-app
```

### 配置资源限制

根据 RTOS 应用需求调整资源：

```yaml
resources:
  requests:
    cpu: "250m"       # 最小 CPU 保证
    memory: "128Mi"   # 最小内存保证
  limits:
    cpu: "1000m"      # 最大 CPU 限制
    memory: "512Mi"   # 最大内存限制
```

### 使用 ConfigMap 传递配置

如果 RTOS 应用需要配置文件：

```bash
# 1. 创建 ConfigMap
cat > rtos-config.yaml <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: rtos-config
data:
  config.txt: |
    baudrate=115200
    timeout=30
EOF

kubectl apply -f rtos-config.yaml

# 2. 在 Pod 中挂载
cat > rtos-pod-with-config.yaml <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: rtos-with-config
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos-app
    image: localhost:5000/mica-zephyr-app:xen-0.1
    volumeMounts:
    - name: config
      mountPath: /etc/rtos/config
  volumes:
  - name: config
    configMap:
      name: rtos-config
EOF

kubectl apply -f rtos-pod-with-config.yaml
```

### 监控和日志

查看 RTOS 容器状态：

```bash
# 查看 Pod 日志
kubectl logs <pod-name> --tail=100 -f

# 在边侧查看 MicRun 日志
sudo tail -f /var/log/mica/mica-runtime.log

# 在边侧查看 containerd 日志
sudo journalctl -u containerd -f

# 查看资源使用
kubectl top pods
kubectl top nodes
```

---

## 附录

### A. 完整配置示例

**云侧配置**：

```bash
# /etc/rancher/k3s/config.yaml（可选）
write-kubeconfig-mode: "0644"
tls-san:
  - "192.168.1.100"  # 云侧 IP
  - "cloud.example.com"  # 云侧域名（可选）
```

**边侧配置**：

```toml
# /etc/containerd/config.toml
version = 2

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun."]
```

### B. 常用 kubectl 命令

```bash
# 查看节点
kubectl get nodes -o wide

# 查看 Pod
kubectl get pods -o wide
kubectl get pods --all-namespaces

# 查看详细信息
kubectl describe pod <pod-name>
kubectl describe node <node-name>

# 查看日志
kubectl logs <pod-name> -f
kubectl logs <pod-name> --previous  # 查看前一个实例的日志

# 进入 Pod
kubectl exec -it <pod-name> -- /bin/sh

# 删除资源
kubectl delete pod <pod-name>
kubectl delete deployment <deployment-name>
kubectl delete -f <yaml-file>
```

### C. 网络配置

**默认 K3s 网络配置**：

- Pod 网络段：`10.42.0.0/16`
- Service 网络段：`10.43.0.0/16`

**自定义网络**：

```bash
# 云侧安装时指定网络段
sudo INSTALL_K3S_EXEC="\
  --cluster-cidr=10.100.0.0/16 \
  --service-cidr=10.101.0.0/16" \
  sh ./install-k3s.sh
```

### D. 卸载 K3s

**云侧**：

```bash
sudo /usr/local/bin/k3s-uninstall.sh
```

**边侧**：

```bash
sudo /usr/local/bin/k3s-agent-uninstall.sh
```

### E. 参考资源

- [K3s 官方文档](https://docs.k3s.io/)
- [Kubernetes 官方文档](https://kubernetes.io/docs/)
- [MicRun 快速入门](quick-start.md)
- [Mica-Xen 指导](https://embedded.pages.openeuler.org/master/features/mica/instruction.html)

---

**下一步**：
- ✅ 完成本指南后，你应该能够在云侧管理多个边侧节点的 RTOS 容器
- ✅ 尝试使用 Deployment 管理 RTOS 应用的高可用部署
- ✅ 探索 Kubernetes 的高级特性（Service、Ingress、监控等）

**问题反馈**：
- 提交 Issue: [https://atomgit.com/openeuler/mcs/issues](https://atomgit.com/openeuler/mcs/issues)
- 查看日志: `/var/log/mica/mica-runtime.log`
