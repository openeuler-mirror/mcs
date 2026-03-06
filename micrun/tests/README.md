# MicRun 测试指南

本目录包含 MicRun 的测试用例和测试框架。

## 目录结构

| 目录 | 说明 |
|------|------|
| `io/` | IO 系统测试，包括 attach、TTY、UX 等测试 |
| `k3s/` | K3s/Kubernetes 云边协同测试（需要 K3s 环境） |
| `lib/` | 测试工具库 |
| `mock_micad/` | Mock micad 工具，用于测试 |

> **注**: K3s 测试需要独立的 K3s 集群环境，默认测试环境（root@192.168.7.2）不包含 K3s。

## 快速开始

### 环境配置

编辑 `test-env.sh` 配置测试环境：

```bash
# 远程测试主机
export TEST_REMOTE_HOST="root@192.168.7.2"

# 测试镜像
export TEST_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"

# K3s 配置（可选）
export K3S_MASTER_NODE="root@192.168.1.100"
```

### 运行所有测试

```bash
cd tests
./run_all_tests.sh
```

### 运行特定类别测试

```bash
# IO 测试
./run_all_tests.sh io

# K3s 测试
./run_all_tests.sh k3s
```

## 测试类别

### IO 测试 (io/)

验证 RTOS 容器的 IO 特性，包括：
- 容器 attach/detach
- TTY 交互
- 标准输入输出
- UX 一致性

```bash
cd tests/io
./run_all_io_tests.sh
```

### K3s 测试 (k3s/)

验证在 Kubernetes/K3s 环境下的云边协同功能。

> **前置条件**: 需要配置 K3s 集群环境，设置 `K3S_MASTER_NODE` 环境变量。

```bash
# 配置 K3s Master 节点
export K3S_MASTER_NODE="root@192.168.1.100"

cd tests/k3s
./run_k3s_tests.sh
```

详见 [k3s/README.md](k3s/README.md)

## 测试工具

### test-utils.sh

测试工具库，提供通用函数：

```bash
source tests/lib/test_utils.sh
```

### mock_micad/

Mock micad 工具，用于独立测试 MicRun：

```bash
cd tests/mock_micad
make
python3 mocker.py
```

## 相关文档

- [快速入门](../docs/quick-start.md) - MicRun 基础使用
- [故障排查](../docs/user/troubleshooting.md) - 常见问题
