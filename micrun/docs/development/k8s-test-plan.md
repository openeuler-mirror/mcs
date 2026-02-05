# Kubernetes 云边协同测试计划

> **测试时间**：2026-01-22
> **测试目标**：验证 MicRun 在 Kubernetes 云边协同场景下的功能
> **测试环境**：
> - **云侧（Cloud）**：本地主机 (192.168.7.1)
> - **边侧（Edge）**：QEMU VM (192.168.7.2, openEuler Embedded)

## 一、环境准备状态

### 1.1 云侧环境（本地主机 192.168.7.1）

**当前状态**：❌ 环境未就绪

**已安装组件**：
- ✅ 操作系统：WSL2 Linux (6.6.87.2-microsoft-standard-WSL2)
- ✅ 网络：tap0 接口 (192.168.7.1/24)，与边侧同一网段
- ✅ Docker：已安装（用于构建镜像等）

**待安装组件**：
- ❌ K3s Server
- ❌ kubectl（Kubernetes 命令行工具）
- ❌ 镜像仓库（可选，用于 RTOS 镜像分发）

**网络配置**：
```
云侧: 192.168.7.1 (tap0)
边侧: 192.168.7.2 (eth0)
连通性: ✅ 已验证（可 ping 通）
```

### 1.2 边侧环境（192.168.7.2）

**当前状态**：⚠️ 部分就绪

**已安装组件**：
- ✅ 操作系统：openEuler Embedded (5.10.0-openeuler)
- ✅ containerd：v1.7.19.m (正在运行)
- ✅ 网络：与云侧同一网段，网络互通

**待安装组件**：
- ❌ MicRun 运行时（核心问题！）
- ❌ K3s Agent
- ❌ RTOS 测试镜像

**已知问题**：
1. **MicRun 未安装**：这是最关键的问题，需要先构建并安装 MicRun
2. **系统为嵌入式系统**：资源有限，需要调整 K3s 配置

## 二、测试计划

### 阶段 1：边侧环境准备（预计时间：2-3小时）

#### 1.1 构建并安装 MicRun

**步骤**：

```bash
# 在云侧（开发机）构建 MicRun
cd /home/sx/code/aixxxx/mcs/micrun
make build

# 传输到边侧节点
scp build/micrun root@192.168.7.2:/tmp/

# 在边侧安装
ssh root@192.168.7.2
mkdir -p /usr/local/bin
mv /tmp/micrun /usr/local/bin/
chmod +x /usr/local/bin/micrun

# 验证安装
micrun --version
```

**预期结果**：
- ✅ micrun 二进制成功安装到边侧节点
- ✅ `micrun --version` 能显示版本信息

#### 1.2 注册 MicRun 运行时到 containerd

**步骤**：

```bash
# 在边侧节点配置 containerd
cat > /etc/containerd/config.toml <<'EOF'
version = 2

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun."]
EOF

# 重启 containerd
systemctl restart containerd

# 验证配置
containerd config dump
```

**预期结果**：
- ✅ containerd 配置包含 micrun 运行时
- ✅ containerd 重启成功

#### 1.3 准备 RTOS 测试镜像

**步骤**：

```bash
# 方案 1: 如果有现成的测试镜像
# 在云侧导出镜像
docker save localhost:5000/mica-uniproton-app:xen-0.1 -o rtos-test.tar

# 传输到边侧
scp rtos-test.tar root@192.168.7.2:/tmp/

# 在边侧导入
ssh root@192.168.7.2
ctr image import /tmp/rtos-test.tar

# 验证
ctr image ls | grep mica
```

**替代方案**：
- 如果没有测试镜像，跳过此步骤，使用 Mock 测试
- 或者使用快速入门文档中的示例构建镜像

**预期结果**：
- ✅ 边侧 containerd 有可用的 RTOS 镜像
- ✅ `ctr image ls` 能看到镜像

### 阶段 2：云侧环境准备（预计时间：30分钟）

#### 2.1 安装 K3s Server

**步骤**：

```bash
# 下载 K3s 安装脚本
curl -sfL https://get.k3s.io -o install-k3s.sh

# 安装 K3s Server
sudo INSTALL_K3S_EXEC="\
  --write-kubeconfig-mode=644 \
  --tls-san 192.168.7.1 \
  --disable traefik" \
  sh ./install-k3s.sh

# 等待启动（约 30 秒）
sleep 30

# 验证 K3s 状态
sudo systemctl status k3s

# 获取 node token
sudo cat /var/lib/rancher/k3s/server/node-token
```

**注意事项**：
- `--tls-san 192.168.7.1`：添加云侧 IP 到 TLS 证书
- `--disable traefik`：嵌入式环境资源有限，禁用不必要的组件
- **保存 node token**，边侧节点加入时需要

**预期结果**：
- ✅ K3s Server 运行正常
- ✅ 获得 node token（如：`K10abc...::server:xyz...`）

#### 2.2 安装 kubectl（如果未安装）

**步骤**：

```bash
# 下载 kubectl
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"

# 安装
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# 验证
kubectl version --client
```

**预期结果**：
- ✅ kubectl 安装成功
- ✅ `kubectl get nodes` 能看到云侧节点

### 阶段 3：边侧节点加入（预计时间：20分钟）

#### 3.1 安装 K3s Agent

**步骤**：

```bash
# 在边侧节点执行
export K3S_URL="https://192.168.7.1:6443"
export K3S_TOKEN="<从云侧获取的 node token>"

# 下载并安装 K3s Agent
curl -sfL https://get.k3s.io | \
  K3S_URL=${K3S_URL} \
  K3S_TOKEN=${K3S_TOKEN} \
  sh -

# 等待启动
sleep 30

# 验证 K3s Agent 状态
systemctl status k3s-agent
```

**注意事项**：
- 嵌入式环境可能需要调整 K3s Agent 配置
- 监控资源使用情况

**预期结果**：
- ✅ K3s Agent 运行正常
- ✅ 云侧 `kubectl get nodes` 能看到边侧节点

#### 3.2 验证节点加入

**在云侧执行**：

```bash
# 查看节点
kubectl get nodes -o wide

# 预期输出：
# NAME    STATUS   ROLES                       AGE   VERSION
# cloud   Ready    control-plane,etcd,master   5m    v1.28.X+k3s1
# edge    Ready    <none>                      1m    v1.28.X+k3s1
```

**预期结果**：
- ✅ 边侧节点状态为 `Ready`
- ✅ 能看到两个节点

### 阶段 4：部署 RuntimeClass（预计时间：5分钟）

**在云侧执行**：

```bash
# 创建 RuntimeClass
cat > micrun-runtimeclass.yaml <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
EOF

kubectl apply -f micrun-runtimeclass.yaml

# 验证
kubectl get runtimeclass

# 预期输出：
# NAME     HANDLER   AGE
# micrun   micrun    10s
```

**预期结果**：
- ✅ RuntimeClass 创建成功
- ✅ `kubectl get runtimeclass` 能看到 micrun

### 阶段 5：部署测试 Pod（预计时间：10分钟）

#### 5.1 创建 Pod 配置

**在云侧执行**：

```bash
cat > rtos-test-pod.yaml <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: rtos-test
spec:
  runtimeClassName: micrun
  nodeName: edge  # 指定调度到边侧节点
  containers:
  - name: rtos-app
    image: localhost:5000/mica-uniproton-app:xen-0.1
    tty: true
    stdin: true
    resources:
      limits:
        cpu: "1000m"
        memory: "512Mi"
EOF
```

#### 5.2 部署 Pod

```bash
# 创建 Pod
kubectl apply -f rtos-test-pod.yaml

# 查看状态
kubectl get pods -o wide
kubectl describe pod rtos-test

# 查看日志
kubectl logs rtos-test --tail=50
```

**预期结果**：
- ✅ Pod 状态变为 `Running`
- ✅ Pod 调度到边侧节点
- ✅ 没有错误日志

#### 5.3 在边侧验证

**在边侧执行**：

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

**预期结果**：
- ✅ 能看到 RTOS 容器在运行
- ✅ shim 进程存在
- ✅ RTOS domain 正常运行

### 阶段 6：功能验证（预计时间：15分钟）

#### 6.1 测试 Pod 管理

```bash
# 1. 查看 Pod 详情
kubectl describe pod rtos-test

# 2. 查看 Pod 日志
kubectl logs rtos-test -f

# 3. 测试 Pod 删除和重建
kubectl delete pod rtos-test
kubectl apply -f rtos-test-pod.yaml

# 4. 验证 Pod 重建成功
kubectl get pods rtos-test
```

#### 6.2 测试 Deployment（可选）

```bash
# 创建 Deployment
cat > rtos-deployment.yaml <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rtos-deployment
spec:
  replicas: 2
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
EOF

kubectl apply -f rtos-deployment.yaml

# 验证
kubectl get pods -l app=rtos-test
```

#### 6.3 测试自动恢复（可选）

```bash
# 模拟 Pod 故障
kubectl delete pod -l app=rtos-test --random-

# 等待自动恢复
sleep 10

# 验证恢复
kubectl get pods -l app=rtos-test
```

**预期结果**：
- ✅ Pod 管理功能正常
- ✅ Deployment 能管理多个副本
- ✅ 自动恢复功能正常

### 阶段 7：清理环境（预计时间：5分钟）

```bash
# 在云侧清理
kubectl delete deployment rtos-deployment
kubectl delete pod rtos-test
kubectl delete runtimeclass micrun

# 在边侧停止 K3s Agent（可选）
systemctl stop k3s-agent
systemctl disable k3s-agent

# 在云侧停止 K3s Server（可选）
sudo /usr/local/bin/k3s-uninstall.sh
```

## 三、测试检查清单

### 环境准备

- [ ] 云侧安装 K3s Server
- [ ] 云侧安装 kubectl
- [ ] 云侧获取 node token
- [ ] 边侧构建并安装 MicRun
- [ ] 边侧注册 MicRun 到 containerd
- [ ] 边侧准备 RTOS 测试镜像
- [ ] 边侧安装 K3s Agent
- [ ] 验证节点加入成功

### 功能测试

- [ ] RuntimeClass 创建成功
- [ ] Pod 成功调度到边侧节点
- [ ] Pod 状态变为 Running
- [ ] 边侧能看到容器运行
- [ ] Shim 进程正常
- [ ] Pod 日志可查看
- [ ] Pod 删除成功
- [ ] Deployment 管理多个副本（可选）
- [ ] 自动恢复功能正常（可选）

### 故障排查

- [ ] 记录所有错误和警告
- [ ] 收集日志信息
- [ ] 更新文档（如果发现文档问题）

## 四、已知问题和风险

### 4.1 当前限制

1. **MicRun 未安装在边侧**：
   - **影响**：无法进行实际的 RTOS 容器测试
   - **解决方案**：先构建并安装 MicRun

2. **RTOS 测试镜像缺失**：
   - **影响**：无法运行实际的 RTOS 应用
   - **解决方案**：使用 Mock 测试或构建测试镜像

3. **嵌入式环境资源有限**：
   - **影响**：K3s 可能资源不足
   - **解决方案**：调整 K3s 配置，禁用不必要的组件

### 4.2 网络问题

- **风险**：WSL2 网络配置可能在重启后失效
- **缓解**：使用静态 IP 或固定网络配置

### 4.3 时间同步

- **风险**：虚拟机时间不同步可能导致证书问题
- **缓解**：确保 NTP 服务正常运行

## 五、测试结果记录

### 测试执行记录

| 阶段 | 状态 | 开始时间 | 结束时间 | 备注 |
|------|------|---------|---------|------|
| 环境准备 | 待执行 | - | - | MicRun 未安装 |
| 云侧部署 | 待执行 | - | - | |
| 边侧部署 | 待执行 | - | - | |
| Pod 部署 | 待执行 | - | - | |
| 功能验证 | 待执行 | - | - | |
| 清理环境 | 待执行 | - | - | |

### 问题记录

| 问题 | 严重性 | 状态 | 解决方案 |
|------|--------|------|----------|
| MicRun 未安装在边侧 | 高 | 待解决 | 构建并安装 |
| RTOS 测试镜像缺失 | 中 | 待解决 | 构建测试镜像或使用 Mock |

## 六、下一步行动

### 立即行动（优先级：高）

1. **构建 MicRun**：
   ```bash
   cd /home/sx/code/aixxxx/mcs/micrun
   make build
   ```

2. **传输并安装到边侧**：
   ```bash
   scp build/micrun root@192.168.7.2:/usr/local/bin/
   ssh root@192.168.7.2 "chmod +x /usr/local/bin/micrun"
   ```

3. **配置 containerd**：
   ```bash
   ssh root@192.168.7.2
   # 编辑 /etc/containerd/config.toml 添加 micrun 运行时
   systemctl restart containerd
   ```

### 后续行动（优先级：中）

4. 准备或构建 RTOS 测试镜像
5. 安装云侧 K3s Server
6. 安装边侧 K3s Agent
7. 执行完整的测试流程

### 文档更新（优先级：低）

8. 根据测试结果更新 [k8s-integration.md](k8s-integration.md)
9. 添加故障排查案例
10. 完善测试指南

## 七、参考资源

- [Kubernetes 集成指南](k8s-integration.md) - 完整的部署指南
- [快速入门](quick-start.md) - MicRun 基础使用
- [测试指南](testing-guide.md) - 测试方法和用例
- [K3s 官方文档](https://docs.k3s.io/)
- [containerd 官方文档](https://containerd.io/docs/)

---

**最后更新**：2026-01-22
**文档状态**：待执行
**负责人**：Claude AI Assistant
