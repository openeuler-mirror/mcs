# MicRun Kubernetes 云边协同指南

> **约定**：云侧运行 K3s Server，边侧运行 K3s Agent 和 RTOS 容器。

## 架构概览

```
┌───────────────────────────────┐
│      Cloud - K3s Server       │
│  Kubernetes API / Scheduler   │
└───────────────┬───────────────┘
                │
                ▼
┌───────────────────────────────┐
│      Edge - K3s Agent         │
│  kubelet / containerd / MicRun│
│                │              │
│                ▼              │
│       RTOS Container          │
└───────────────────────────────┘
```

## 前置准备

| 组件 | 云侧 | 边侧 |
|------|------|------|
| 操作系统 | Linux | openEuler Embedded (含 micrun, micad, xen) |
| K3s | v1.28+ server | v1.28+ agent |
| containerd | K3s 自带 | 1.7.27+ |
| MicRun | - | 已安装并注册 |

边侧需完成 [MicRun 快速入门](../quick-start.md) 前 5 步。

## 云侧部署

### 安装 K3s Server

```bash
# 安装（替换 <cloud-ip> 为云侧 IP）
curl -sfL https://get.k3s.io | \
  INSTALL_K3S_EXEC="--write-kubeconfig-mode=644 --tls-san <cloud-ip>" \
  sh -

# 获取 token（边侧加入需要）
sudo cat /var/lib/rancher/k3s/server/node-token

# 验证
kubectl get nodes
```

## 边侧部署

### 配置 K3s 使用系统 containerd

```bash
sudo mkdir -p /etc/systemd/system/k3s-agent.service.env
sudo tee /etc/systemd/system/k3s-agent.service.env/10-containerd.conf <<EOF
K3S_SUPERVISOR_CONTAINERD=false
CONTAINERD_SOCK=/run/containerd/containerd.sock
EOF
```

### 安装 K3s Agent

```bash
# 替换 <cloud-ip> 和 <node-token>
export K3S_URL="https://<cloud-ip>:6443"
export K3S_TOKEN="<node-token>"

curl -sfL https://get.k3s.io | K3S_URL=${K3S_URL} K3S_TOKEN=${K3S_TOKEN} sh -

# 验证
sudo systemctl status k3s-agent
```

### 注册 RuntimeClass

在云侧执行：

```bash
kubectl apply -f - <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
EOF
```

## 运行 RTOS Pod

### 创建 Pod

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: rtos-demo
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos-app
    image: localhost:5000/mica-uniproton-app:xen-0.1
    tty: true
    stdin: true
EOF
```

### 验证

```bash
# 云侧查看 Pod
kubectl get pods -o wide

# 边侧验证
ctr task ls | grep rtos
sudo xl list
```

## 常见问题

| 问题 | 可能原因 | 解决方法 |
|------|----------|----------|
| Pod ContainerCreating 超时 | 镜像不存在 | 边侧执行 `ctr image import` 导入镜像 |
| 边侧节点 NotReady | K3s Agent 未运行或网络问题 | 检查 `systemctl status k3s-agent` |
| runtime 'micrun' not found | containerd 未加载 MicRun | 检查 `/etc/containerd/config.toml` 配置 |

## 参考资源

- [K3s 官方文档](https://docs.k3s.io/)
- [MicRun 快速入门](../quick-start.md)
- [Mica-Xen 指导](https://embedded.pages.openeuler.org/master/features/mica/instruction.html)
- [问题反馈](https://atomgit.com/openeuler/mcs/issues)
