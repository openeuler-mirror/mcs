# nerdctl Attach Detach 机制分析报告

> **⚠️ 文档说明**
>
> 本文档是对 nerdctl detach 机制的分析报告。**注意**：
> - "back" 命令**未实现**，文档中提及此命令仅为分析讨论
> - micrun 使用 nerdctl 原生的 `Ctrl+P Ctrl+Q` 进行 detach（仅 TTY 模式）
> - 非 TTY 模式不支持 detach

## 问题描述

在 micrun 项目中，我们尝试在非 TTY 模式下实现 "back" 命令来支持 detach 功能（退出 attach 会话但保持容器运行）。然而发现即使 shim 正确关闭了 FIFO，nerdctl attach 进程也不会退出。

本报告分析了 nerdctl attach 的实现，解释了为什么 TTY 模式支持 detach 而非 TTY 模式不支持。

## 分析结论

**nerdctl 的非 TTY attach 没有提供 detach 机制。** 这是 nerdctl 的设计限制，不是 micrun 的实现问题。

## nerdctl attach 实现分析

### 源代码位置

`nerdctl/pkg/cmd/container/attach.go`

### TTY 模式 (支持 detach)

```go
// line 93-125
if spec.Process.Terminal {
    // ... 设置终端为 raw 模式 ...
    closer := func() {
        detachC <- struct{}{}           // 发送 detach 信号
        io := task.IO()
        io.Cancel()                     // 取消 IO
    }
    in, err = consoleutil.NewDetachableStdin(con, options.DetachKeys, closer)
    // ...
}

// line 146-172
select {
case <-detachC:                         // 等待 detach 信号
    io := task.IO()
    io.Wait()                           // 等待 IO 清理
case status := <-statusC:               // 等待容器退出
    // ...
}
```

**TTY 模式支持 detach 的关键机制：**

1. **`detachC` 通道**：用于传递 detach 信号
2. **`closer` 函数**：当检测到 detach 键序列（默认 Ctrl+P Ctrl+Q）时调用
3. **`NewDetachableStdin`**：封装 stdin，监控 detach 键序列
4. **`io.Cancel()`**：取消 IO 操作，使 FIFO 读取返回

### 非 TTY 模式 (不支持 detach)

```go
// line 126-128
} else {
    opt = cio.WithStreams(options.Stdin, options.Stdout, options.Stderr)
}

// line 84
detachC := make(chan struct{})  // 空 channel，没有发送者

// line 146-172
select {
case <-detachC:      // 永远不会触发，因为 channel 为空
    // ...
case status := <-statusC:   // 只等待容器退出
    // ...
}
```

**非 TTY 模式不支持 detach 的原因：**

1. **`detachC` 是空 channel**：没有任何代码向它发送数据
2. **没有 `closer` 函数**：非 TTY 模式不设置 `closer`
3. **只等待 `statusC`**：attach 只在容器退出时返回
4. **`task.Wait()`**：调用 shim 的 Wait API，只等待容器进程退出

## 为什么有这样的设计？

### TTY 模式的使用场景

- **交互式会话**：用户直接与容器交互
- **需要 detach 能力**：用户可能想临时退出，稍后重新 attach
- **有键盘输入**：可以捕获特殊键序列（Ctrl+P Ctrl+Q）

### 非 TTY 模式的使用场景

- **脚本/自动化**：通常用于管道或日志收集
- **不需要交互**：数据流是单向的或预定义的
- **简单可靠**：只需等待容器完成，无需复杂的 detach 逻辑

## 对 micrun 的影响

### 当前实现

| 命令 | TTY 模式 | 非 TTY 模式 |
|------|----------|-------------|
| `exit` | ✓ 停止容器并退出 | ✓ 停止容器并退出 |
| `Ctrl+P Ctrl+Q` | ✓ detach，容器继续运行（nerdctl 原生机制） | ✗ 不支持（nerdctl 限制） |
| `back` 命令 | ✗ 未实现 | ✗ 未实现 |

### 设计决策

基于上述分析，micrun 采取以下设计：

1. **保留 TTY 模式的 detach 支持**：使用 nerdctl 原生的 Ctrl+P Ctrl+Q 机制
2. **移除非 TTY 模式的 back 命令**：因为 nerdctl attach 不会退出
3. **非 TTY 模式只支持 exit**：停止容器并退出

### 代码实现

`pkg/io/copier.go` 中的注释说明：

```go
// Note: Detach (like Ctrl+P Ctrl+Q) is NOT supported in non-TTY mode.
// Use TTY mode for detach support, as nerdctl/ctr non-TTY attach
// has no detach mechanism and will wait until container exits.
```

## 参考代码

### nerdctl attach.go 关键代码

```go
// line 84: detachC 通道创建
detachC := make(chan struct{})

// line 93-125: TTY 模式设置 closer
if spec.Process.Terminal {
    closer := func() {
        detachC <- struct{}{}
        io := task.IO()
        io.Cancel()
    }
    // ...
} else {
    // line 126-128: 非 TTY 模式不设置 closer
    opt = cio.WithStreams(options.Stdin, options.Stdout, options.Stderr)
}

// line 146-172: 等待逻辑
select {
case <-detachC:
    io.Wait()
case status := <-statusC:
    // 容器退出处理
}
```

## 总结

nerdctl 的非 TTY attach 不支持 detach 是一个**设计决策**，而非 bug。这个设计符合容器运行时的通用惯例：

- **Docker**：非 TTY attach 也不支持 detach
- **ctr**：与 nerdctl 相同的行为
- **kubectl**：同样只支持 TTY 模式的 detach

micrun 遵循这个惯例，只在 TTY 模式提供 detach 功能。
