# MicRun IO Test Suite

## 环境配置

### 必需配置

1. **远程主机访问**
   - 仓库示例主机: `root@192.168.7.2`
   - 可通过环境变量配置: `export TEST_REMOTE_HOST=user@host`

2. **测试镜像**
   - 默认镜像: `localhost:5000/mica-uniproton-app:xen-0.1`
   - 导入方式: 将镜像 tarball 传输到远程主机后使用 `ctr image import` 导入

   ```bash
   # 在本地传输镜像 tarball 到远程主机
   export EDGE_SSH_USER="${EDGE_SSH_USER:-root}"
   export EDGE_IP="${EDGE_IP:-192.168.7.2}"
   export TEST_REMOTE_HOST="${TEST_REMOTE_HOST:-${EDGE_SSH_USER}@${EDGE_IP}}"

   scp mica-uniproton-app-xen-0.1.tar.gz "${TEST_REMOTE_HOST}:/tmp/"

   # 在远程主机导入镜像
   ssh "$TEST_REMOTE_HOST"
   ctr image import /tmp/mica-uniproton-app-xen-0.1.tar.gz
   ctr image tag localhost/mica-uniproton-app:xen-0.1 localhost:5000/mica-uniproton-app:xen-0.1
   ```

3. **containerd 服务**
   - 远程主机需运行 containerd
   - micrun 运行时需部署到 `/usr/bin/containerd-shim-mica-v2`

4. **expect 工具**
   - 本地需要安装 expect: `sudo apt install expect`
   - 用于交互式 TTY 测试

### 环境配置文件

创建 `tests/io/test-env.sh`:

```bash
#!/bin/bash
# MicRun IO Test Environment Configuration
export EDGE_SSH_USER="${EDGE_SSH_USER:-root}"
export EDGE_IP="${EDGE_IP:-192.168.7.2}"
export TEST_REMOTE_HOST="${TEST_REMOTE_HOST:-${EDGE_SSH_USER}@${EDGE_IP}}"
export TEST_IMAGE="${TEST_IMAGE:-localhost:5000/mica-uniproton-app:xen-0.1}"
```

使用方法:
```bash
source tests/io/test-env.sh
./tests/io/run_all_io_tests.sh
```

### qemu 一键回归

对于已经打通 `ssh "$TEST_REMOTE_HOST"` 的 qemu 环境，可直接运行：

```bash
cd micrun/tests/io
./run_qemu_regression.sh
```

该脚本会自动：

1. 构建当前工作区的 `arm64` 版本 `micrun`
2. 部署到 qemu
3. 导入 UniProton 镜像 tar
4. 执行自适应 IO 回归

推荐配套环境变量：

```bash
export EDGE_SSH_USER="${EDGE_SSH_USER:-root}"
export EDGE_IP="${EDGE_IP:-192.168.7.2}"
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export TEST_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"
export NERDCTL_NETWORK_MODE="none"
export IMAGE_PROFILE="auto"
```

## 一键执行

```bash
cd micrun/tests/io
./run_all_io_tests.sh
```

## 测试环境

## 测试用例列表

| ID | 测试名称 | 测试命令 | 预期结果 |
|----|----------|----------|----------|
| 1 | ctr 后台模式 attach | `ctr task start -d` + `ctr task attach` | help 命令正常输出 |
| 2 | ctr 前台模式 | `ctr task start` | uname 命令正常输出 |
| 3 | nerdctl 非 TTY 模式 | `ctr task attach` | 命令正常执行 |
| 4 | 多命令执行 | 连续输入多个命令 | 所有命令都有响应 |
| 5 | 退出命令检测 | 输入 `exit` | 容器正常停止 |
| 6 | 日志清洁度 | 检查高频日志 | 无日志垃圾 |
| 7 | TTY 回声抑制 | expect 脚本测试 | 无双重回显 |

> **注**: nerdctl 相关测试需要安装 nerdctl 工具，如无 nerdctl 可跳过相关测试。

## 测试结果

### 测试日期: 2026-03-03

| # | 测试用例 | 状态 | 备注 |
|---|----------|------|------|
| 1 | ctr 后台模式 attach | ✓ PASS | help 命令输出正常 |
| 2 | ctr 前台模式 (expect) | ✓ PASS | uname 命令输出正常 |
| 3 | nerdctl 非 TTY 模式 | ✓ PASS | 命令执行正常 |
| 4 | 多命令执行 | ✓ PASS | 所有命令响应正常 |
| 5 | 退出命令检测 | ✓ PASS | 容器停止正常 |
| 6 | 日志清洁度 | ✓ PASS | 0 条垃圾日志 |
| 7 | TTY 回声抑制 | ✓ PASS | 无双重回显 |

**总结**: 7/7 通过 (100%)

## 关键修复点

本次测试验证了以下修复：

1. **ctr 后台模式 attach 修复** (ea0b52b)
   - 添加 non-TTY background attach 检测
   - stdin epoll 失败后回退到 sleep 轮询
   - 检测写入者连接后重新启用 epoll

2. **IO 代码优化** (e666b4f)
   - 移除高频日志（每次 FIFO read、epoll ready 等）
   - 使用预分配缓冲区减少内存分配
   - 日志更干净，性能更好

## 测试方法

### 自动化测试

```bash
cd micrun/tests/io
./run_all_io_tests.sh
```

### 手动测试

```bash
# 测试 ctr 后台模式
ssh "$TEST_REMOTE_HOST"
ctr container create --runtime io.containerd.mica.v2 \
  localhost:5000/mica-uniproton-app:xen-0.1 test
ctr task start -d test
ctr task attach test
# 输入 help 命令，应该看到输出
# 输入 exit 退出

# 测试 nerdctl TTY 模式（如已安装 nerdctl）
nerdctl run -it --rm --runtime io.containerd.mica.v2 \
  localhost:5000/mica-uniproton-app:xen-0.1
# 交互式会话，输入 help 命令测试
```

## 已知问题

1. 多次 attach/detach 测试需要 `auto_close=false` 注解
2. 测试间需要适当延迟以避免资源冲突

## 测试文件清单

| 文件 | 用途 |
|------|------|
| `run_all_io_tests.sh` | 主入口 - 一键执行所有 IO 测试 |
| `test_ctr_foreground.exp` | ctr 前台模式 expect 测试脚本 |
| `test_tty_echo.exp` | TTY 回声抑制测试脚本 |
| `test_nerdctl_tty.exp` | nerdctl TTY 模式测试脚本（可选） |
| `test-env.sh` | 环境配置文件 |
| `IO_TEST_SUITE.md` | 本文档 |
