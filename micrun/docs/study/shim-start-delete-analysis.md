# Shim Start/Delete 命令行为分析

## 概述

本文档分析 containerd 如何调用 shim 的 start 和 delete 子命令，以及 micrun 的实现细节。

---

## 一、Shim Start 命令

### 调用时机

Containerd 需要启动新的容器时，首先需要启动 shim 守护进程。

### 调用链

```
containerd TaskManager.Create()
    ↓
ShimManager.Start() → exec shim -id xxx start
    ↓
shim start 父进程
    ↓
New() (一次性命令模式)
    ↓
StartShim()
    ↓
fork 守护进程 (子进程)
    ↓
父进程输出 socket 地址并退出
```

### StartShim() 的实际行为

```go
func (s *shimService) StartShim(ctx context.Context, opts shimv2.StartOpts) (_ string, retErr error) {
    // 1. 验证 bundle 路径
    bundle, err := validBundle(opts.ID, bundle)

    // 2. 创建 socket 文件
    socket, err := shimv2.NewSocket(sockAddr)

    // 3. 创建子进程命令
    cmd, err := newCommand(ctx, opts, bundle)

    // 4. 将 socket 通过 ExtraFiles 传递给子进程
    sock, err := socket.File()
    cmd.ExtraFiles = append(cmd.ExtraFiles, sock)

    // 5. Fork 守护进程
    if err := cmd.Start(); err != nil {
        return "", fmt.Errorf("failed to start shim task service: %w", err)
    }

    // 6. 后台等待子进程（防止僵尸）
    go cmd.Wait()

    // 7. 返回 socket 地址给 containerd
    return sockAddr, nil
}
```

### 父子进程机制

| 特性 | 父进程 (start) | 子进程 (守护进程) |
|------|--------------|-----------------|
| flag.Arg(0) | "start" | "" (空) |
| 生命周期 | 创建完子进程后退出 | 长期运行 |
| socket | 创建并传递给子进程 | 继承并监听 |
| 与 containerd 通信 | 输出 socket 地址 | TTRPC 服务 |

### 关键技术点

1. **Socket 传递**: 通过 `cmd.ExtraFiles` 将 socket 文件描述符传递给子进程
2. **进程组隔离**: `Setpgid: true` 使子进程独立于父进程
3. **地址输出**: 父进程将 socket 地址写入 stdout，containerd 读取后连接

### 代码位置

- `pkg/shim/shim_services.go`: `StartShim()` 方法
- `pkg/shim/util.go`: `newCommand()` 创建子进程命令

---

## 二、Shim Delete 命令

### 调用时机

Shim 守护进程已经不在运行时，containerd 调用此命令清理资源。

### 调用链

```
containerd → exec shim -id xxx delete
    ↓
New() (一次性命令模式)
    ↓
Cleanup()
    ↓
cleanupContainer()
    ↓
cntr.CleanupContainer()
    ↓
micad StopContainer/DeleteContainer
    ↓
unmount rootfs
```

### Cleanup() 的实际行为

```go
func (s *shimService) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {
    // 1. 从 bundle 目录读取 OCI spec
    ociSpec, err := oci.LoadSpec(cwd)

    // 2. 调用 cleanupContainer - 这里会:
    //    - 加载 sandbox 状态 (/run/micrun/sandbox/xxx/state.json)
    //    - 通过 micad 停止 RTOS
    //    - 卸载 rootfs
    err = cleanupContainer(ctx, s.id, s.id, cwd)

    return &taskAPI.DeleteResponse{...}
}
```

### cleanupContainer 做了什么

1. **从磁盘加载 sandbox** - `loadSandbox()` 读取 `/run/micrun/sandbox/xxx/state.json`
2. **通过 micad 停止 RTOS** - `sandbox.StopContainer()` → micad API
3. **通过 micad 删除 RTOS** - `sandbox.DeleteContainer()` → micad API
4. **卸载 rootfs** - `mount.UnmountAll()`

### 为什么需要 Cleanup()

在 1:1:1 模型中，**Shim 崩溃 ≠ RTOS 停止**：

| 运行时 | 进程模型 | 崩溃后 |
|--------|----------|--------|
| runc | 容器就是进程 | 进程退出 = 容器停止 |
| micrun | Shim 和 RTOS 分离 | Shim 崩溃，RTOS 仍运行 |

因此 delete 子命令需要：
- 检查 RTOS 是否还在运行
- 如果是，通过 micad 停止它
- 清理残留资源

### 代码位置

- `pkg/shim/shim_services.go:372`: `Cleanup()` 方法
- `pkg/micantainer/container.go:132`: `CleanupContainer()` 函数

---

## 三、两种 Delete 方式对比

| 方式 | 调用时机 | 代码位置 | 状态来源 |
|------|----------|----------|----------|
| **Cleanup()** | 守护进程已退出 | shim delete 子命令 | 从磁盘加载 state.json |
| **Delete()** | 守护进程运行中 | TTRPC API | 从内存读取 s.sandbox |

两者都是幂等的，设计合理。

---

## 四、总结

### Start 命令
- **目的**: 创建并启动 shim 守护进程
- **机制**: fork+exec，通过 ExtraFiles 传递 socket
- **结果**: 返回 socket 地址，containerd 连接守护进程

### Delete 命令
- **目的**: 清理资源（当守护进程不在时）
- **机制**: 从磁盘加载状态，通过 micad 停止 RTOS
- **结果**: RTOS 停止，rootfs 卸载

### 设计合理性
- ✅ Start: 符合 shim v2 规范，父子进程分离正确
- ✅ Delete: 处理了 1:1:1 模型的特殊情况（RTOS 独立运行）
- ✅ 幂等性: 两个命令都支持重复调用
