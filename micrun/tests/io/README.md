# MicRun IO 测试

本目录包含 MicRun 的 IO 系统测试用例，用于验证 RTOS 容器的标准输入输出功能。

## 环境要求

### 必需配置

1. **远程主机访问**
   - 默认主机: `root@192.168.7.2`
   - 可通过环境变量配置: `export REMOTE_HOST=user@host`

2. **测试镜像**
   - 默认镜像: `localhost:5000/mica-uniproton-app:xen-0.1`
   - 镜像需要预先导入到远程主机的 containerd

3. **containerd 服务**
   - 远程主机需运行 containerd
   - micrun 运行时需部署到 `/usr/bin/containerd-shim-mica-v2`

4. **expect 工具**
   - 本地需要安装 expect: `sudo apt install expect`
   - 用于交互式 TTY 测试

## 快速开始

### 环境配置

```bash
# 配置环境变量
source tests/io/test-env.sh

# 或手动设置
export REMOTE_HOST="root@192.168.7.2"
export TEST_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"
```

### 运行所有测试

```bash
cd tests/io
./run_all_io_tests.sh
```

## 测试用例

| ID | 测试名称 | 说明 |
|----|----------|------|
| 1 | ctr 后台模式 attach | 验证 `ctr task start -d` + `ctr task attach` |
| 2 | ctr 前台模式 | 验证 `ctr task start` 交互式输出 |
| 3 | nerdctl 非 TTY 模式 | 验证非 TTY 模式下的命令执行 |
| 4 | 多命令执行 | 验证连续输入多个命令的响应 |
| 5 | 退出命令检测 | 验证 `exit` 命令触发容器停止 |
| 6 | 日志清洁度 | 验证无高频日志垃圾 |
| 7 | TTY 回声抑制 | 验证无双重回显问题 |

## 测试文件说明

| 文件 | 用途 |
|------|------|
| `run_all_io_tests.sh` | 主入口 - 一键执行所有 IO 测试 |
| `test_ctr_foreground.exp` | ctr 前台模式 expect 测试脚本 |
| `test_tty_echo.exp` | TTY 回声抑制测试脚本 |
| `test_nerdctl_tty.exp` | nerdctl TTY 模式测试脚本（可选） |
| `test-env.sh` | 环境配置文件 |
| `IO_TEST_SUITE.md` | 详细测试套件文档 |
| `UX_TEST_MANUAL.md` | 手动 UX 测试指南 |

## 手动测试

```bash
# 测试 ctr 后台模式
ssh root@192.168.7.2
ctr container create --runtime io.containerd.mica.v2 \
  localhost:5000/mica-uniproton-app:xen-0.1 test-io
ctr task start -d test-io
ctr task attach test-io
# 输入 help 命令，应该看到输出
# 输入 exit 退出

# 清理
ctr task delete test-io
ctr container delete test-io
```

## 相关文档

- [IO 系统设计](../../docs/internals/io-system.md) - IO 系统架构设计文档
- [测试指南](../README.md) - MicRun 测试总览
