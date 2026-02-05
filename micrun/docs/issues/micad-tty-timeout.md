# micad TTY 超时问题

**日期：** 2026-01-22
**状态：** 未解决（micad 侧问题）
**相关 PR：** c6c5f4f

## 问题描述

启动 RTOS 容器时，shim 等待 RPMSG TTY 设备就绪超时。错误信息：

```
ctr: wait for rpmsg tty for <container-id>: context deadline exceeded
```

## 调查摘要

### 事件时间线

1. **micad start** (T+0s)：micad 接收启动命令并开始创建 VM
2. **xenstore probe** (T+5s)：xenstore 探测约 5 秒后成功
3. **start done** (T+5s)：micad 报告 "start done"
4. **VM 移除** (T+5s)：VM 立即被移除
5. **shim 超时** (T+30s)：dialTTY 等待 TTY 设备超时

### 关键发现

1. **micad 创建了符号链接但目标不存在**
   ```bash
   $ readlink /dev/ttyRPMSG_test_0
   /dev/pts/7
   $ ls -la /dev/pts/7
   ls: cannot access '/dev/pts/7': No such file or directory
   ```

2. **xl list 中没有 VM**
   - micad 日志显示 VM 启动完成
   - 但 `xl list` 中看不到该 VM
   - 表明 VM 启动后立即被销毁

3. **micad 日志显示正常启动**
   ```
   Jan 01 09:43:29 - Starting fresh-test(/tmp/micrun/containers/fresh-test/firmware.elf) on CPU
   Jan 01 09:43:34 - Timeout for xenstore probe. Try again.
   Jan 01 09:43:34 - Success for xenstore probe.
   Jan 01 09:43:34 - start done
   Jan 01 09:43:34 - Removing fresh-test
   ```

### 根本原因分析

问题是 **micad 侧**导致的：

1. micad 创建了 RPMSG TTY 符号链接 (`/dev/ttyRPMSG_test0 -> /dev/pts/N`)
2. 但目标 pts 设备 (`/dev/pts/N`) 从未创建
3. 当 shim 尝试打开 TTY 时，收到 `ENXIO`（设备未找到）错误
4. shim 重试直到超时（30秒）
5. micad 移除了 VM（可能是由于检测到 TTY 失败）

问题是：**为什么 micad 没有创建 pts 设备？**

可能的原因：
- micad 期望 RTOS 创建 pts 设备，但 RTOS 失败了
- 符号链接创建和 pts 设备创建之间存在竞态条件
- pts 设备创建失败被静默忽略
- micad 在检测到 TTY 初始化失败时销毁了 VM

## 临时变通方案

### 变通方案 1：手动创建 TTY（未测试）
```bash
# VM 启动后，手动创建 pts 设备并更新符号链接
# 这不是推荐的解决方案
```

### 变通方案 2：忽略 TTY 超时（不推荐）
- 允许容器在没有 TTY 的情况下启动
- 会破坏交互式 shell 和 exec 功能

## micrun 中已实现的修复

虽然根本原因在 micad 侧，但 micrun 中实现了以下修复：

1. **cleanupStaleSymlink()** - 在 TTY 打开前移除无效的符号链接
2. **修复 container.start() 错误处理** - 当 startClient 失败时正确返回错误
3. **将 TTY 超时增加到 30 秒** - 在高负载下给 micad 更多时间

详见 commit c6c5f4f。

## 需要 micad 调查的内容

要解决此问题，需要在 micad 中调查以下内容：

1. **pts 设备创建机制**
   - micad 在哪里/何时创建 `/dev/pts/N` 设备？
   - 哪个组件负责？（micad、内核还是 RTOS？）
   - 是否存在竞态条件？

2. **TTY 失败期间的 VM 生命周期**
   - 为什么 micad 在 "start done" 后立即移除 VM？
   - 这是 TTY 失败时的预期行为吗？

3. **错误处理**
   - micad 是否记录 pts 设备创建失败？
   - 是否有任何静默失败？

4. **RPMSG TTY 创建流程**
   - 记录从 VM 启动到 TTY 可用的完整流程
   - 识别所有涉及的组件（micad、xen-mcs-backend、内核、RTOS）

## 测试环境

- **平台：** QEMU aarch64 上的 openEuler Embedded
- **micad 版本：**（运行中的 PID 17840）
- **容器镜像：** localhost:5000/mica-uniproton-app:xen-0.1
- **运行时：** io.containerd.mica.v2

## 相关代码

- `pkg/micantainer/rpmsg_tty.go:dialTTY()` - TTY 等待逻辑
- `pkg/micantainer/container:start()` - 容器启动流程
- `pkg/libmica/client.go:Start()` - micad 通信

## 后续步骤

1. **联系 micad 团队**调查 pts 设备创建
2. **启用 micad 调试日志**捕获 TTY 创建详情
3. **考虑添加 micad 重试逻辑**（如果 TTY 创建不稳定）
4. **评估替代 TTY 机制**（如果当前方法不可靠）
