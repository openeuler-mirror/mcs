# MicRun测试可靠性指南

## 概述

本文档记录了micrun测试套件的可靠性优化措施和最佳实践。通过实施这些改进，测试的可靠性和稳定性得到了显著提升。

## 增强模块

### 1. QEMU增强 (`tests/common/qemu_enhanced.sh`)

**功能**:
- 指数退避重试机制
- QEMU进程健康检查
- 自动恢复和重启
- 资源清理验证
- 网络连接失败恢复

**关键函数**:
```bash
qemu_wait_for_ssh_enhanced     # 增强的SSH等待（指数退避）
qemu_health_check              # 综合健康检查
qemu_restart_if_hung           # 自动重启挂起的guest
qemu_cleanup_enhanced          # 增强的清理和验证
qemu_recover_connectivity      # 网络连接恢复
```

**使用示例**:
```bash
source tests/common/qemu_enhanced.sh

# 等待SSH连接（最多30次重试，指数退避）
qemu_wait_for_ssh_enhanced 30

# 执行健康检查
qemu_health_check 60

# 获取诊断信息
qemu_get_diagnostics /tmp/qemu-diag.txt
```

### 2. 远程执行增强 (`tests/common/remote_enhanced.sh`)

**功能**:
- 自动重试（指数退避）
- 超时自适应
- 批量执行
- 并行执行
- 安全文件传输

**关键函数**:
```bash
remote_retry                    # 带重试的远程命令
remote_with_timeout_retry       # 带超时和重试的命令
remote_batch                    # 批量命令执行
remote_validate                 # 带验证的命令执行
copy_to_remote_safe             # 安全的文件传输
```

**使用示例**:
```bash
source tests/common/remote_enhanced.sh

# 自动重试的远程命令
remote_retry "ctr container create ..." 3

# 带验证的命令
remote_validate "ctr image ls" "grep -q mica-uniproton"

# 批量执行
echo "cmd1" | remote_batch
```

### 3. Containerd增强 (`tests/common/containerd_enhanced.sh`)

**功能**:
- 服务状态验证
- Socket连接等待
- 增强的清理和验证
- Shim崩溃检测和恢复
- 版本兼容性检查

**关键函数**:
```bash
containerd_ensure_enhanced       # 确保containerd运行
containerd_wait_socket          # 等待socket就绪
containerd_cleanup_enhanced     # 增强的清理
containerd_verify_cleanup       # 清理验证
containerd_recover_shim         # Shim恢复
containerd_check_shim_version   # 版本检查
```

**使用示例**:
```bash
source tests/common/containerd_enhanced.sh

# 确保containerd运行
containerd_ensure_enhanced

# 验证清理
containerd_verify_cleanup

# 检查版本
containerd_check_shim_version
```

### 4. 时序控制增强 (`tests/common/timing_enhanced.sh`)

**功能**:
- 动态状态检测
- 自适应时序
- 操作时间测量
- 资源稳定等待

**关键函数**:
```bash
wait_container_running          # 等待容器运行状态
calculate_adaptive_delay        # 计算自适应延迟
attach_with_timing              # 带时序的attach操作
wait_for_stabilization          # 等待资源稳定
measure_timing                  # 测量操作时间
```

**使用示例**:
```bash
source tests/common/timing_enhanced.sh

# 动态等待容器运行
wait_container_running "test-container" 30

# 使用自适应时序
startup_time=$(container_start_with_timing "test-container")
attach_with_timing "test-container" "$startup_time"

# 获取统计信息
get_timing_stats
```

## 增强测试脚本

### test-qemu-smoke-enhanced

增强版的QEMU smoke测试，集成了所有可靠性改进：
- 指数退避的SSH连接
- 健康检查验证
- 自动恢复机制
- 详细的诊断输出

### test-micrun-e2e-enhanced

端到端测试脚本，验证完整流程：
- QEMU启动和连接
- Containerd服务
- 网络弹性
- 容器生命周期
- 清理验证

## 环境配置

### 必需变量

```bash
# QEMU配置
export QEMU_OUTPUT_DIR="<path-to-qemu-output-test-dir>"
export QEMU_BIN="<path-to-qemu-system-aarch64>"
export QEMU_LOCAL_SUDO_PASSWORD="<host-sudo-password-if-needed>"

# 测试目标
export EDGE_SSH_USER="${EDGE_SSH_USER:-root}"
export EDGE_IP="${EDGE_IP:-192.168.7.2}"
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export TEST_REMOTE_PASSWORD="<guest-root-password-if-needed>"
export TEST_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"

# 可选调优
export TIMING_DEBUG=true            # 启用时序调试
export CONTAINERD_DEBUG=true        # 启用containerd调试
export QEMU_DEBUG=true              # 启用QEMU调试
```

提交或共享测试记录前，保留变量名和占位符，不要写入真实宿主机绝对路径、
sudo 密码、guest 密码或访问密钥。

### 镜像tar自动检测

脚本会自动扫描以下路径中的uniproton镜像tar：
- `$QEMU_OUTPUT_DIR/exports/local_mica-uniproton-app_xen-arm64-0.1.tar`
- `$QEMU_OUTPUT_DIR/micrun-files/localhost_5000_mica-uniproton-app_xen-0.1.tar`
- `$QEMU_OUTPUT_DIR/localhost_5001_mica-uniproton-app_xen-0.1.tar`

测试只会读取镜像 tar 并复制到运行中的 guest，不应修改已经构建好的 QEMU
rootfs 产物。

## 最佳实践

### 1. 使用增强函数替代原始调用

**不推荐**:
```bash
sleep 3  # 固定等待
ctr task start -d test
sleep 8  # 固定等待
ctr task attach test
```

**推荐**:
```bash
source tests/common/timing_enhanced.sh
startup_time=$(container_start_with_timing "test")
attach_with_timing "test" "$startup_time"
```

### 2. 添加重试机制

**不推荐**:
```bash
remote "ctr container create ..."
```

**推荐**:
```bash
source tests/common/remote_enhanced.sh
remote_retry "ctr container create ..." 3
```

### 3. 验证清理

**不推荐**:
```bash
cleanup_all
```

**推荐**:
```bash
source tests/common/containerd_enhanced.sh
containerd_cleanup_enhanced
containerd_verify_cleanup
```

### 4. 收集诊断信息

**失败时**:
```bash
source tests/common/qemu_enhanced.sh
source tests/common/containerd_enhanced.sh

qemu_get_diagnostics /tmp/qemu-diag.txt
containerd_get_diagnostics /tmp/containerd-diag.txt
```

## 故障排查

### QEMU无法启动

1. 检查QEMU二进制和资产
```bash
qemu_resolve_assets
```

2. 检查网络配置
```bash
qemu_local_ssh "echo ok"
```

3. 获取诊断信息
```bash
qemu_get_diagnostics /tmp/qemu-diag.txt
```

### Containerd无法启动

1. 尝试强制清理
```bash
containerd_force_cleanup
```

2. 重新启动
```bash
containerd_ensure_enhanced
```

3. 检查shim版本
```bash
containerd_check_shim_version
```

### 测试超时

1. 检查时序统计
```bash
get_timing_stats
```

2. 调整超时配置
```bash
export TIMEOUT_NORMAL=20
export TIMEOUT_LONG=45
```

3. 启用调试模式
```bash
export TIMING_DEBUG=true
```

## 性能基准

基于测试环境的典型操作时间（供参考）：

| 操作 | 典型时间 | 最大超时 |
|------|----------|----------|
| QEMU启动 | 10-20s | 60s |
| SSH连接 | 2-8s | 30s |
| Containerd启动 | 2-5s | 30s |
| 容器创建 | 1-3s | 15s |
| 容器启动 | 3-8s | 30s |
| IO响应 | 1-5s | 15s |
| 清理操作 | 2-5s | 30s |

## 贡献指南

添加新的可靠性改进时：

1. 在相应的增强模块中添加函数
2. 添加详细的使用示例
3. 更新本文档
4. 添加测试用例验证改进

## 相关文档

- [测试README](../README.md)
- [IO测试文档](../io/README.md)
- [QEMU测试指南](../README.md#qemu-artifact-smoke)
- [测试可靠性分析](../../.omc/test-reliability-analysis.md)

## 更新历史

- 2026-03-17: 初始版本，记录核心可靠性改进
