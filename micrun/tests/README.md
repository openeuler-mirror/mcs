# MicRun 测试

本目录提供 MicRun 的稳定测试入口，并把重复的远端访问、QEMU 启动和日志处理
收敛到 `tests/common`。

## 公开入口

```bash
micrun/tests/bin/test-qemu-smoke
micrun/tests/bin/test-io-qemu
micrun/tests/bin/test-k3s-single-node
micrun/tests/bin/test-k3s-cloud-edge
micrun/tests/bin/test-k3s-interaction
micrun/tests/bin/test-k3s-ota
micrun/tests/run_all_tests.sh
```

`micrun/tests/run_all_tests.sh k3s` 是 K3s 类别的统一入口。无 `test_id` 时默认
包含 `K3S-008` 交互测试，即会覆盖 `RuntimeClass=micrun` Pod 创建、
`kubectl attach`、边侧 containerd task、Xen domain 和删除清理。若只想跑
某个场景，可以传入 `K3S-000` 到 `K3S-009`。`K3S-009` 是 OTA 滚动升级测试，
默认不随 K3s 类别全量执行；如需纳入默认 K3s 类别，设置
`K3S_INCLUDE_OTA=true`。

## 目录结构

| 路径 | 作用 |
|------|------|
| `common/` | 共享环境变量、日志、SSH 和 QEMU helper |
| `bin/` | 面向用户的稳定测试入口 |
| `io/` | RTOS IO 场景和 expect 交互脚本 |
| `k3s/` | K3s 单节点、云边测试和边侧准备脚本 |
| `lib/` | 旧 shell helper 的兼容层 |
| `mock_micad/` | mock micad 工具 |

## QEMU Smoke

QEMU smoke 用于验证新构建的 QEMU 产物可以通过 Xen 启动，并且 guest 中的
SSH、containerd、micad 和 Xen 基础命令可用。

```bash
export QEMU_OUTPUT_DIR="<path-to-qemu-output-test-dir>"
export QEMU_BIN="<path-to-qemu-system-aarch64>"
export QEMU_LD_LIBRARY_PATH="<path-to-qemu-lib-dir>:<path-to-qemu-lib64-dir>"
export QEMU_LOCAL_SUDO_PASSWORD="<host-sudo-password-if-needed>"

micrun/tests/bin/test-qemu-smoke
```

提交文档、日志或 skill 时不要写入真实宿主机路径、sudo 密码或 guest 密码。
将这些值保留为环境变量或 `<...>` 占位符。

网络模式：

| 变量 | 含义 |
|------|------|
| `QEMU_NET_MODE=both` | 默认值，保留 `tap0`，同时增加 usernet SSH 转发 |
| `QEMU_NET_MODE=tap` | 只使用原有 tap 网络，适合手工串口或固定 guest IP 调试 |
| `QEMU_NET_MODE=user` | 只使用 usernet，适合不依赖固定 guest IP 的 smoke |
| `QEMU_USE_USERNET=false` | 兼容旧变量，等价于 `QEMU_NET_MODE=tap` |

`tap0` 仍是本地 QEMU/K3s 标准测试网络。usernet 只是为了让自动化脚本稳定
访问 `root@127.0.0.1:<QEMU_SSH_FWD_PORT>`，不是替代 tap 的新标准。
当 `QEMU_NET_MODE=tap` 时，`test-qemu-smoke` 会改用 `TEST_REMOTE_HOST`
访问 guest；仓库默认示例目标是 `root@192.168.7.2`，可按实际环境覆盖。

## QEMU IO 回归

IO 回归会把当前工作区构建出的 `containerd-shim-mica-v2` 部署到 guest，
导入 RTOS 镜像 tar，然后运行 `ctr` 和 `nerdctl` 用户路径。

```bash
export EDGE_SSH_USER="${EDGE_SSH_USER:-root}"
export EDGE_IP="${EDGE_IP:-192.168.7.2}"
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export TEST_REMOTE_PASSWORD="<guest-root-password-if-needed>"
export TEST_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"
export QEMU_SOURCE_IMAGE_REF="localhost:5000/mica-uniproton-app:xen-0.1"
export QEMU_IMAGE_TAR="<path-to-stamped-output>/exports/local_mica-uniproton-app_xen-arm64-0.1.tar"

micrun/tests/bin/test-io-qemu
```

如果使用 `TEST_REMOTE_HOST=qemu-k3s`、`root@127.0.0.1` 或 `127.0.0.1`，
入口会先调用 `test-qemu-smoke` 自动启动 QEMU，并通过 usernet SSH 转发访问
guest。只使用 tap 时，请先手动启动 QEMU，并把 `TEST_REMOTE_HOST` 指向
仓库示例地址或你的实际 guest 地址。

## K3s 测试

K3s 测试分两条稳定路径：

```bash
export EDGE_SSH_USER="${EDGE_SSH_USER:-root}"
export EDGE_IP="${EDGE_IP:-192.168.7.2}"
export CLOUD_IP="${CLOUD_IP:-192.168.7.10}"

# 单节点：边侧节点自己运行 k3s server 和 MicRun
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
micrun/tests/bin/test-k3s-single-node

# 云边：本机 Docker 运行 k3s server，QEMU/边侧节点作为 agent
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export K3S_CLOUD_NETWORK_PARENT="tap0"
export K3S_CLOUD_SERVER_IP="${CLOUD_IP}"
export K3S_EDGE_NODE_IP="${EDGE_IP}"
micrun/tests/bin/test-k3s-cloud-edge
```

K3s 交互测试可在单节点或云边环境已经就绪后单独执行：

```bash
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export K3S_INTERACTION_MODE="auto"   # auto/cloud/local/edge
micrun/tests/bin/test-k3s-interaction
```

该入口会创建 `RuntimeClass=micrun` 的 RTOS Pod，使用 `kubectl attach` 发送
`help`、`uname`，验证边侧 containerd task、Xen domain
和 Pod 删除后的资源清理。

K3s OTA 测试要求云边环境已经就绪，并且边侧 containerd 能访问 v1/v2 RTOS
镜像。默认会把 `/tmp/localhost_5000_mica-uniproton-app_xen-0.1.tar` 和
`/tmp/localhost_5000_mica-uniproton-app_xen-0.2.tar` 导入边侧 `k8s.io`
namespace，再把 Deployment 从 v1 patch 到 v2：

```bash
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export K3S_OTA_V1_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"
export K3S_OTA_V2_IMAGE="localhost:5000/mica-uniproton-app:xen-0.2"
export K3S_OTA_V2_IMAGE_TAR="/tmp/localhost_5000_mica-uniproton-app_xen-0.2.tar"
micrun/tests/bin/test-k3s-ota
```

该入口验证 Deployment rollout、新旧 container ID 切换、旧 Xen domain 清理、
新 v2 Xen domain 启动、`kubectl attach` 交互和最终清理。若 kubelet 对
MicRun stopped task 的回收超时，脚本会限定到该测试 Pod 做 edge fallback
cleanup，不会修改 QEMU rootfs 产物。

K3s 脚本会修改运行中边侧节点上的 K3s 状态，例如
`/var/lib/rancher/k3s`、`/run/k3s`、CNI 配置、agent systemd service、
RuntimeClass 和镜像导入。这些都是运行态测试准备，不是 QEMU rootfs 产物修改。

## Rootfs 边界

- 标准化测试不得解包、修改或重新打包已经构建好的 QEMU rootfs 产物。
- 如果某次排障需要在运行中的 guest 里临时补齐 `/var/lib/xen`、
  `/var/volatile/log`、`/var/log/xen` 或 `/run/xen`，只能把它记录为临时
  guest 修复，不能把修复后的 rootfs 当成测试基线。
- SSH 密钥、固定密码、网络默认配置和 Xen 运行目录应尽量来自构建配置或明确的
  guest 初始化步骤。
- 需要密码 SSH 时，设置 `TEST_REMOTE_PASSWORD` 或 `QEMU_GUEST_PASSWORD`。

## 相关文档

- [K3s 测试](./k3s/README.md)
- [IO 测试](./io/README.md)
- [Kubernetes 集成指南](../docs/user/kubernetes.md)
