# K3s 云化测试

## 测试概述

本目录包含 MicRun 与 K3s/Kubernetes 云原生环境集成的测试用例，验证 RTOS 容器在云边协同场景下的功能。

## 环境要求

### 必需组件

- **K3s 或 Kubernetes**: 1.28+ 版本
- **kubectl**: 命令行工具（需在 K3s Master 节点上可用）
- **containerd**: 带有 MicRun 运行时
- **网络**: 节点间网络互通

> **注意**: 默认测试环境（root@192.168.7.2）是单节点环境，不包含 K3s。运行 K3s 测试需要独立的 K3s 集群环境。

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

## 必要文件

边侧节点需具备：

| 文件 | 默认路径 | 说明 |
|------|----------|------|
| K3s 二进制 | `/usr/bin/k3s` | 必须来自 rootfs 构建产物，可用 `K3S_BIN` 覆盖 |
| pause 镜像 tar | `/tmp/pause-image-arm64.tar` | 可用 `K3S_PAUSE_TAR` 覆盖 |
| RTOS 镜像 tar | `/tmp/localhost_5000_mica-uniproton-app_xen-0.1.tar` | 可用 `K3S_IMAGE_TAR` 覆盖 |

这些绝对路径都是运行中边侧节点内的默认路径，不是宿主机本地路径。

测试会把镜像导入到边侧 Kubernetes 使用的 containerd。若
`K3S_EDGE_CONTAINERD_MODE=external`，脚本使用系统 `ctr -a
/run/containerd/containerd.sock -n k8s.io`；若使用 bundled 模式，则使用
K3s 自带 `ctr`。清理 containerd state 后需要重新导入。
RTOS 镜像 tar 通常带有构建时源 tag，例如
`docker.io/local/mica-uniproton-app:xen-arm64-0.1`。测试脚本会把它重新标记为
`TEST_IMAGE`，默认是 `localhost:5000/mica-uniproton-app:xen-0.1`。pause 镜像会
同时保留 `rancher/mirrored-pause:3.6` 和
`docker.io/rancher/mirrored-pause:3.6` 两种常见引用。

## 单节点模式

单节点模式在边侧节点上启动 `k3s server`，适合快速确认 bundled containerd
能调用 MicRun。
如果 rootfs 只构建了 K3s agent 子命令，请跳过单节点模式，使用云边模式验证。
脚本启动前会清理运行中的旧 K3s、containerd task 和 Xen domain，避免上一轮
残留占用内存后误判。若 Dom0 可用内存低于 `K3S_SINGLE_NODE_MIN_AVAILABLE_MB`
且没有 swap，入口会干净跳过；这种情况下仍应使用云边模式覆盖主要链路。

```bash
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export TEST_REMOTE_PASSWORD="<guest-root-password-if-needed>"
export TEST_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"
export K3S_PAUSE_TAR="/tmp/pause-image-arm64.tar"
export K3S_IMAGE_TAR="/tmp/localhost_5000_mica-uniproton-app_xen-0.1.tar"

micrun/tests/bin/test-k3s-single-node
```

验证内容：

- K3s server 启动并生成 bundled containerd
- `RuntimeClass micrun` 可用
- RTOS Pod 进入 `Running`
- `k3s ctr task ls` 能看到任务
- `xl list` 能看到对应 Xen domain

## 云边模式

云边模式在本机 Docker 中启动 K3s server，边侧节点运行 K3s agent。这条路径
更接近边缘部署，也是 K3s 与 QEMU 联调的主要测试方式。

```bash
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export TEST_REMOTE_PASSWORD="<guest-root-password-if-needed>"
export TEST_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"
export K3S_CLOUD_NETWORK_PARENT="tap0"
export K3S_CLOUD_NETWORK_SUBNET="${K3S_CLOUD_NETWORK_SUBNET:-192.168.7.0/24}"
export K3S_CLOUD_NETWORK_GATEWAY="${HOST_TAP_IP}"
export K3S_CLOUD_SERVER_IP="${CLOUD_IP}"
export K3S_EDGE_NODE_IP="${EDGE_IP}"
export K3S_EDGE_NODE_NAME="qemu-aarch64"
export K3S_BIN="/usr/bin/k3s"
export K3S_CLOUD_SERVER_IMAGE="<k3s-server-image-matching-edge-version>"
export K3S_CLOUD_KUBECTL_BIN="k3s"
export K3S_CLOUD_KUBECTL_SUBCOMMAND="kubectl"
export K3S_EDGE_CONTAINERD_MODE="external"
export K3S_CONTAINERD_ADDRESS="/run/containerd/containerd.sock"
export K3S_EDGE_CTR_BIN="ctr"
export K3S_EDGE_CTR_SUBCOMMAND=""
export K3S_KUBELET_ARGS="--kubelet-arg=cgroups-per-qos=false --kubelet-arg=enforce-node-allocatable="

micrun/tests/bin/test-k3s-cloud-edge
```

验证内容：

- 本机 Docker 创建 macvlan 网络并启动 K3s server
- 边侧 agent 通过 `tap0` 网络加入集群
- 边侧 containerd 加载 MicRun runtime
- RTOS Pod 被调度到指定边侧节点并进入 `Running`
- 边侧 `ctr -a <containerd-sock> -n k8s.io tasks ls`
  和 `xl list` 能找到同一容器 ID
- 测试结束后默认删除 Pod，并确认边侧 task 与 Xen domain 被清理

调试时如需保留云边测试 Pod，可设置 `K3S_E2E_KEEP_POD=true`。

## 交互模式

交互模式用于验证用户真正关心的 Kubernetes 入口，而不只是 Pod 进入
`Running`。它会创建一个新的 RTOS Pod，通过 `kubectl attach` 发送命令，
再核对边侧 containerd task、Xen domain 和删除清理。

```bash
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export TEST_REMOTE_PASSWORD="<guest-root-password-if-needed>"
export TEST_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"
export K3S_INTERACTION_MODE="auto"      # auto/cloud/local/edge
export K3S_INTERACTION_EXPECT="auto"    # auto/shell/hello

micrun/tests/bin/test-k3s-interaction
```

`auto` 模式会优先检测本机是否存在正在运行的
`$K3S_CLOUD_SERVER_CONTAINER`。如果存在，则通过云侧 `kubectl` 创建 Pod；
否则若设置了 `K3S_LOCAL_KUBECONFIG`，会通过本机 K3s/kubectl 访问已存在的
控制面；最后才回退到边侧 `k3s kubectl` 运行单节点交互测试。

本机控制面模式适合 QEMU 资源有限、不希望在 guest 内运行 K3s server 的场景：

```bash
export K3S_INTERACTION_MODE="local"
export K3S_LOCAL_KUBECONFIG="<path-to-local-kubeconfig>"
export K3S_LOCAL_KUBECTL_BIN="<path-to-k3s-or-kubectl>"
export K3S_LOCAL_KUBECTL_SUBCOMMAND="kubectl"
micrun/tests/bin/test-k3s-interaction
```

验证内容：

- 创建或更新 `RuntimeClass micrun`
- 创建带 `stdin: true`、`tty: false` 的 RTOS Pod
- 等待 Pod 进入 `Running`
- 用 `kubectl attach -i` 按行发送 `help`、`uname`
- 匹配 UniProton shell 或 hello 输出标记
- 从 Pod status 解析 containerd ID
- 在边侧 containerd 中找到同一 task
- 在边侧 `xl list` 中找到同一 Xen domain
- 删除 Pod 后确认 task 和 Xen domain 清理完成

`tty` 默认关闭是为了兼容 K3s/containerd 的 attach 参数组合，避免 CRI 同时
设置 `tty=true` 和 `stderr=true` 时返回 `tty and stderr cannot both be true`。
如需要验证交互式 TTY 行为，可显式设置 `K3S_INTERACTION_TTY=true`。

在 oEE/QEMU guest 中，kubelet 对 MicRun stopped task 的 CRI 回收可能会等待
StopContainer/StopPodSandbox 超时。交互测试会先执行 Kubernetes 正常删除；
如果 `K3S_POD_DELETE_TIMEOUT` 内仍未完成，默认启用
`K3S_INTERACTION_EDGE_DELETE_FALLBACK=true`，只清理该 Pod 在边侧 containerd
中的 task/container 和对应 Xen domain，再移除已经 Terminating 的 Pod API
对象。这个 fallback 仅作用于运行中的 guest，不会解包或修改 QEMU rootfs
产物；真实环境可设置 `K3S_INTERACTION_EDGE_DELETE_FALLBACK=false` 禁用。

如果只想保留 Pod 供人工继续 attach，可设置：

```bash
export K3S_INTERACTION_KEEP_POD=true
```

## 运行态修改范围

K3s 场景需要清理和重建运行中边侧节点的 K3s 状态：

- 停止旧的 `k3s`、`micrun-k3s-agent.service` 和残留 shim
- 按模式清理 bundled K3s state 或系统 containerd 中的旧 task
- 写入 bundled `config.toml.tmpl` 或系统 `/etc/containerd/config.toml`
- 写入 K3s 和系统 CNI 配置
- 创建或更新边侧 agent systemd service
- 导入 pause 和 RTOS 镜像 tar

这些动作都是 guest 运行态准备。标准测试不得解包、修改或重新打包已构建好的
`openeuler-image-qemu-aarch64-*.rootfs.cpio.gz` 或其他 QEMU rootfs 产物。

## 常用变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `TEST_REMOTE_HOST` | `${EDGE_SSH_USER}@${EDGE_IP}` | 边侧 SSH 目标 |
| `TEST_REMOTE_PASSWORD` | 空 | 边侧 SSH 密码 |
| `TEST_IMAGE` | `localhost:5000/mica-uniproton-app:xen-0.1` | Pod 使用的 RTOS 镜像名 |
| `K3S_BIN` | `/usr/bin/k3s` | 边侧 K3s 二进制，来自 rootfs 构建产物 |
| `K3S_PAUSE_TAR` | `/tmp/pause-image-arm64.tar` | pause 镜像 tar |
| `K3S_IMAGE_TAR` | `/tmp/localhost_5000_mica-uniproton-app_xen-0.1.tar` | RTOS 镜像 tar |
| `K3S_CONTAINER_COMMAND` | `/micrun-placeholder` | RTOS Pod command 占位 |
| `K3S_EDGE_CONTAINERD_MODE` | `external` | `external` 使用系统 containerd，`bundled` 使用 K3s containerd |
| `K3S_CONTAINERD_ADDRESS` | `/run/containerd/containerd.sock` | external 模式的 containerd socket |
| `K3S_EDGE_CTR_BIN` | `ctr` | 边侧导入镜像和核对 task 的 ctr 命令 |
| `K3S_EDGE_CTR_SUBCOMMAND` | 空 | 使用独立 `ctr` 时保持为空；使用 `k3s ctr` 时设为 `ctr` |
| `K3S_CLOUD_SERVER_IMAGE` | `rancher/k3s:...` | 云侧 K3s server 镜像，应与边侧 K3s 版本匹配 |
| `K3S_CLOUD_KUBECTL_BIN` | `k3s` | 云侧容器内的 kubectl 命令或 k3s 二进制 |
| `K3S_CLOUD_KUBECTL_SUBCOMMAND` | `kubectl` | 云侧使用 `k3s kubectl` 时的子命令；独立 kubectl 时设为空 |
| `K3S_CLOUD_NETWORK_PARENT` | `tap0` | Docker macvlan 父接口 |
| `K3S_CLOUD_SERVER_IP` | `${CLOUD_IP}` | 云侧 K3s server 地址 |
| `K3S_EDGE_NODE_IP` | `${EDGE_IP}` | 边侧节点地址 |
| `K3S_EDGE_NODE_NAME` | `qemu-aarch64` | Kubernetes 边侧节点名 |
| `K3S_E2E_KEEP_POD` | `false` | 云边用例是否保留测试 Pod 便于调试 |
| `K3S_INTERACTION_MODE` | `auto` | 交互测试使用 `cloud`、`local` 或 `edge` kubectl |
| `K3S_INTERACTION_EXPECT` | `auto` | 交互输出匹配 `shell`、`hello` 或自动判断 |
| `K3S_INTERACTION_NODE_SELECTOR` | 空 | 覆盖交互测试 Pod 的 `key=value` 节点选择器 |
| `K3S_INTERACTION_KEEP_POD` | `false` | 交互测试后是否保留 Pod |
| `K3S_INTERACTION_TTY` | `false` | 交互测试 Pod 是否启用 TTY |
| `K3S_INTERACTION_EDGE_DELETE_FALLBACK` | `true` | Kubernetes 删除超时时是否清理边侧运行时对象 |
| `K3S_LOCAL_KUBECONFIG` | 空 | `local` 模式使用的本机 kubeconfig |
| `K3S_LOCAL_KUBECTL_BIN` | `$K3S_LOCAL_SERVER_BIN` | `local` 模式使用的 kubectl 或 k3s 二进制 |
| `K3S_LOCAL_KUBECTL_SUBCOMMAND` | `kubectl` | 当二进制是 k3s 时附加的子命令；使用独立 kubectl 时设为空 |
| `K3S_ATTACH_INPUT` | `help`、`uname` | `kubectl attach` 发送给 RTOS shell 的输入 |
| `K3S_ATTACH_TIMEOUT` | `60` | `kubectl attach` 最大等待秒数 |
| `K3S_ATTACH_LINE_DELAY` | `3` | `kubectl attach` 每行输入之间的延迟秒数 |
| `K3S_POD_DELETE_TIMEOUT` | `60` | `kubectl delete --wait` 最大等待秒数 |
| `K3S_KUBELET_ARGS` | oEE/QEMU 兼容参数 | 覆盖脚本默认 kubelet 参数，设为空字符串表示不附加参数 |
| `K3S_DEFAULT_KUBELET_ARGS` | oEE/QEMU 兼容参数 | 修改脚本默认 kubelet 参数 |
| `K3S_AUTO_CLOSE_TIMEOUT` | `0` | Pod 注解 `org.openeuler.micrun.container.auto_close_timeout` |

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

- [Kubernetes 集成](../../docs/user/kubernetes.md)
- [RuntimeClass 设计](https://kubernetes.io/docs/concepts/containers/runtime-class/)
