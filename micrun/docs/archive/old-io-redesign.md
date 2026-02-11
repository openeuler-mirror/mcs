# `MicRun IO`系统重构设计文档

> **⚠️ 历史文档**
>
> 本文档是 IO 系统的设计历史记录，部分设计决策可能与当前实现不一致。请参考 [io-design.md](./io-design.md) 查看当前实际实现。
>
> **主要差异**：
> - 文档中描述的 Ctrl+C 信号处理功能**未实现**
> - "back" 命令 detach 功能**未实现**，实际使用 nerdctl 原生的 `Ctrl+P Ctrl+Q`
> - 实际退出方式：在 RTOS shell 中输入 `exit` 命令或使用 `ctr task kill`

## 概述

本文档描述了`MicRun IO`系统的重新设计，旨在实现与标准容器运行时（如`runc`）的兼容性，特别是通过`TTY`设备进行交互式`RTOS`容器管理。

## 背景

### MicRun 架构

```
┌─────────────────────────────────────────────────────────────┐
│                      containerd (ctr)                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │ CreateTask   │  │ Start        │  │ Attach           │  │
│  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘  │
└─────────┼──────────────────┼──────────────────┼────────────┘
          │                  │                  │
          │       FIFO (stdio)                  │
          └──────────────────┼──────────────────┘
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                    MicRun Shim (shim)                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │ shimio.go    │  │ ioCopy()     │  │ TTY 管理模块      │  │
│  │ - pipeIO     │  │ - stdin      │  │ - dialTTY()      │  │
│  │ - binaryIO   │  │ - stdout     │  │ - /dev/ttyRPMSG  │  │
│  │ - fileIO     │  │ - stderr     │  │                   │  │
│  └──────────────┘  └──────────────┘  └──────────────────┘  │
└──────────────────────────────┬──────────────────────────────┘
                               │
                               │ RPMSG TTY
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                      Mica 守护进程 (micad)                  │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              XL 控制台管理模块                          │  │
│  └────────────────────────────────────────────────────────┘ │
└──────────────────────────────┬──────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    Xen 虚拟机监控器                         │
│  ┌────────────────────────────────────────────────────────┐ │
│  │       RTOS 容器 (Zephyr/UniProton/LiteOS)             │  │
│  │              /dev/ttyRPMSG_<container>_0               │  │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### RTOS TTY 设备

当`RTOS`容器启动时，`Xen`会创建一个`RPMSG TTY`设备，位于：
- `/dev/ttyRPMSG_<sanitized_container_id>_0`（首选路径）
- `$MICRUN_STATE_DIR/ttyRPMSG_<sanitized_container_id>_0`（备用路径）

该`TTY`是`RTOS`容器标准输入输出的**唯一**通信通道。

## 当前实现

### IO 系统架构 (pkg/io/)

```
pkg/io/
├── types.go       # 配置类型定义
├── copier.go      # 双向数据复制 + epoll 零 CPU 等待
├── session.go     # 会话管理 + Restart() 集成
├── events.go      # 事件总线 (解耦 IO 和 shim 层)
├── binary.go      # Binary IO 支持 (binary:// 协议)
└── copier_test.go # 单元测试
```

### 数据流架构

```
┌─────────────────────────────────────────────────────────────┐
│                    MicRun Shim                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              pkg/io                                    │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────────────┐    │ │
│  │  │ Session  │  │ Copier   │  │ EventBus         │    │ │
│  │  │ - FIFO管理│  │- epoll优化│  │- 事件驱动架构    │    │ │
│  │  │ - Restart│  │- 双向复制 │  │- 解耦设计        │    │ │
│  │  └──────────┘  └──────────┘  └──────────────────┘    │ │
│  │       │             │                    │             │ │
│  │       ▼             ▼                    ▼             │ │
│  │  ┌─────────────────────────────────────────────────┐  │ │
│  │  │  TTY 管理 (rpmsg_tty.go)                       │  │ │
│  │  │  - dialTTY()      - configureTTY()             │  │ │
│  │  │  - drainTTY()     - /dev/ttyRPMSG_*            │  │ │
│  │  └─────────────────────────────────────────────────┘  │ │
│  └────────────────────────────────────────────────────────┘ │
└──────────────────────────────┬──────────────────────────────┘
                               │
                               │ RPMSG TTY
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                      Mica 守护进程 (micad)                  │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              XL 控制台管理模块                          │  │
│  └────────────────────────────────────────────────────────┘ │
└──────────────────────────────┬──────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    Xen 虚拟机监控器                         │
│  ┌────────────────────────────────────────────────────────┐ │
│  │       RTOS 容器 (Zephyr/UniProton/LiteOS)             │  │
│  │              /dev/ttyRPMSG_<container>_0               │  │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 核心特性

| 特性 | 实现文件 | 说明 |
|------|----------|------|
| **Epoll 零 CPU 等待** | `copier.go` | 空闲 CPU 使用率从 70% 降至 ~0% |
| **EventBus 事件系统** | `events.go` | 解耦 IO 层和 shim 层 |
| **Session.Restart()** | `session.go` | 支持多次 attach/detach |
| **回声抑制** | `copier.go` | 避免 PTY 和 RTOS 重复回显 |
| **Binary IO 支持** | `binary.go` | 支持 binary:// 协议 |

### 当前解决的问题

| 问题 | 解决方案 | 状态 |
|------|----------|------|
| **1. Enter 无提示符** | TTY raw 模式配置 + 换行压缩 | ✅ 已解决 |
| **2. 输出不完整** | 32KB 缓冲 + epoll 优化 | ✅ 已解决 |
| **3. Ctrl+C 不工作** | 控制字符检测 + 事件转发 | ✅ 已解决 |
| **4. 后台模式问题** | Session.Restart() 集成 | ✅ 已解决 |
| **5. CPU 使用率高** | Epoll 零等待优化 | ✅ 已解决 |

## 性能优化

### Epoll 零 CPU 等待机制

**问题背景**：
原始实现使用紧密轮询等待 IO 数据，导致空闲时 CPU 使用率高达 70%+。

**解决方案**：
使用 Linux epoll 机制实现零 CPU 等待：

```go
// copier.go 中的 epoll 实现
type Copier struct {
    epollFd int           // epoll 文件描述符
    cancelPipeR int       // 取消管道读端
    cancelPipeW int       // 取消管道写端
    // ...
}

func (c *Copier) waitForData(ttyFd int) bool {
    const epollTimeoutMs = 100  // 100ms 超时

    events := make([]unix.EpollEvent, 2)
    n, err := unix.EpollWait(c.epollFd, events, epollTimeoutMs)
    if n <= 0 {
        return false
    }

    for i := 0; i < n; i++ {
        if events[i].Fd == int32(c.cancelPipeR) {
            return false  // 收到取消信号
        }
    }
    return true  // 有数据可读
}
```

**性能效果**：
| 指标 | 优化前 | 优化后 |
|------|--------|--------|
| 空闲 CPU 使用率 | 70%+ | ~0% |
| 响应延迟 | 即时 | <100ms |
| 内存开销 | 低 | +2 epoll fds |

### 回声抑制机制

**问题背景**：
PTY 本地回显 + RTOS 回显导致字符重复显示。

**解决方案**：
跟踪已发送字符，过滤 RTOS 回显：

```go
type Copier struct {
    sentChars []byte  // 跟踪已发送的字符
    // ...
}

func (c *Copier) suppressRTOSEcho(data []byte) []byte {
    // 比较接收到的数据与已发送的字符
    // 过滤掉 RTOS 的回显，只保留实际输出
}
```

## 事件驱动架构

### EventBus 设计

**目的**：解耦 IO 层和 shim 层，通过事件通信。

```go
// events.go
type EventType string

const (
    EventExitCommand   EventType = "exit_command"
    EventDetachDetected EventType = "detach"
    EventIOError        EventType = "io_error"
    EventTTYReady       EventType = "tty_ready"
    EventStdinClosed    EventType = "stdin_closed"
)

type EventBus struct {
    subscribers map[EventType][]chan Event
    mu          sync.RWMutex
}

func (eb *EventBus) Publish(typ EventType, data interface{}) {
    eb.mu.RLock()
    defer eb.mu.RUnlock()

    for _, ch := range eb.subscribers[typ] {
        select {
        case ch <- Event{Type: typ, Data: data}:
        default:
            // 非阻塞发送，避免阻塞 IO 流
        }
    }
}

func (eb *EventBus) Subscribe(typ EventType) <-chan Event {
    ch := make(chan Event, 10)
    eb.mu.Lock()
    eb.subscribers[typ] = append(eb.subscribers[typ], ch)
    eb.mu.Unlock()
    return ch
}
```

**事件流程**：
```
用户输入 "exit" → Copier 检测 → EventBus.Publish(ExitCommand)
                                            ↓
                          shim 层订阅者收到事件
                                            ↓
                          触发容器停止流程
```

### Attach/Detach 状态管理

**场景**：用户 attach → detach → reattach

```
1. 用户 attach: Session.Start() 创建新 copier
2. 用户 detach (Ctrl+P Ctrl+Q):
   - EventBus.Publish(DetachDetected)
   - Copier 停止写入 TTY
   - FIFO 保持打开
3. 用户 reattach:
   - Session.Restart() 重新打开 FIFO
   - 创建新的 copier
   - 继续 IO 流
```

### Binary IO 支持

**binary:// 协议**：
用于 containerd 的可插拔日志记录器：

```go
// binary.go
type BinaryIO struct {
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout io.Reader
    stderr io.Reader
}

func (b *BinaryIO) Start() error {
    // 启动外部进程处理 IO
    if err := b.cmd.Start(); err != nil {
        return err
    }
    // 通过管道与外部进程通信
}
```

**使用场景**：
- 日志聚合（如 fluentd）
- 日志旋转
- 自定义日志处理

## 设计需求

### 1. 交互式终端行为

**需求**：`ctr task start my-rtos`应进入交互式`Shell`，实现：
- 按`Enter`显示`UniProton #`提示符
- 每次`Enter`显示新的提示符行
- `help`等命令显示完整输出

**实现方式**：
- 为`TTY`使用伪终端 (`PTY`) 语义
- 确保正确的行缓冲
- 正确处理`CR/LF`转换

### 2. 完整输出缓冲

**需求**：多行命令（如`help`）必须始终显示完整输出。

**实现方式**：
- 为`stdout`复制使用缓冲`I/O`
- 确保`goroutine`不会过早退出
- 正确检测`TTY`的`EOF`

### 3. 信号处理

**需求**：`Ctrl+C`应终止容器。

**实现方式**：
- 已通过`shimio.go`中的 `detectControlChars()` 实现
- 可能需要针对RTOS`行为进行优化

### 4. 后台模式和附加

**需求**：
- `ctr task start -d my-rtos`应立即返回
- `ctr task attach my-rtos`应重新附加到容器

**实现方式**：
- 正确的`FIFO`生命周期管理
- 附加/分离的状态跟踪

## IO 协议规范

### containerd Shim v2 IO 协议

基于 [containerd/runtime/v2/README.md](https://github.com/containerd/containerd/blob/main/core/runtime/v2/README.md)：

```
CreateTaskRequest {
    string id = 1;
    bool terminal = 4;
    string stdin = 5;   // URI: "fifo:/path/to/stdin"
    string stdout = 6;  // URI: "fifo:/path/to/stdout" 或 "binary://..."
    string stderr = 7;  // URI: "fifo:/path/to/stderr" 或与 stdout 相同
}
```

### 支持的 URI 方案

| 方案 | 描述 | 使用场景 |
|------|------|----------|
| `fifo` | 命名管道（默认） | 交互式终端 |
| `binary` | 外部日志记录器 | 容器日志 |
| `file` | 直接文件输出 | 日志文件 |
| `npipe` | `Windows命名管道` | 仅`Windows` |

### 数据流

```
┌──────────┐     stdin     ┌──────────┐     ┌──────────┐
│   ctr    │──────────────>│   FIFO   │─────>│  shim    │
│ (客户端) │<──────────────│ (stdio)  │<─────│          │
└──────────┘    stdout     └──────────┘     └─────┬────┘
                                                  │
                                                  ▼
                                          ┌──────────────┐
                                          │ /dev/ttyRPMSG│
                                          │  <container> │
                                          └──────────────┘
```

## 实现计划

### 阶段 1：`TTY`管理增强

**文件**：`pkg/micantainer/rpmsg_tty.go`

变更：
1. 确保`TTY`以正确的标志打开
2. 根据需要设置 raw/cbreak 模式
3. 处理`TTY`调整大小信号

### 阶段 2：`IO`复制重构

**文件**：`pkg/shim/shimio.go`

变更：
1. 重构`ioCopy()`以改进缓冲区管理
2. 添加正确的`EOF`检测
3. 确保`goroutine`生命周期管理

### 阶段 3：`FIFO`生命周期管理

**文件**：`pkg/shim/start.go`

变更：
1. 为后台模式正确设置`FIFO`
2. 正确处理`attach/detach`
3. 重连的状态跟踪

### 阶段 4：信号处理

**文件**：`pkg/shim/shimio.go`

变更：
1. 优化控制字符检测
2. 确保信号到达`RTOS`

## 详细变更

### 1. `RPMSG TTY`设置 (`rpmsg_tty.go`)

```go
// 添加 TTY termios 配置
func configureTTY(fd uintptr) error {
    var termios unix.Termios
    if err := unix.IoctlSetTermios(int(fd), unix.TCGETS, &termios); err != nil {
        return err
    }

    // 设置 raw 模式以正确处理二进制数据
    // cfmakeraw(&termios) 等效实现
    termios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK |
                   unix.ISTRIP | unix.INLCR | unix.IGNCR |
                   unix.ICRNL | unix.IXON
    termios.Oflag &^= unix.OPOST
    termios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON |
                   unix.ISIG | unix.IEXTEN
    termios.Cflag &^= unix.CSIZE | unix.PARENB
    termios.Cflag |= unix.CS8

    return unix.IoctlSetTermios(int(fd), unix.TCSETS, &termios)
}

// 更新 dialTTY 以配置 TTY
func dialTTY(ctx context.Context, containerID string) (stdin *os.File, stdout *os.File, openedPath string, err error) {
    // ... 现有代码 ...
    in, openErr := openTTYOnce(p)
    if openErr != nil {
        // ... 重试逻辑 ...
    }

    // 配置 TTY 以正确处理行
    if err := configureTTY(in.Fd()); err != nil {
        log.Warn("failed to configure TTY: " + fmt.Sprintf("%v", err))
        // 无论如何继续 - TTY 可能使用默认设置工作
    }

    // ... 函数其余部分 ...
}
```

### 2. `IO`复制缓冲区管理 (`shimio.go`)

```go
// 增强的 ioCopy，具有正确的缓冲
func ioCopy(ctx context.Context, exitch, stdinCloser chan struct{}, tty *ttyIO, stdinPipe io.WriteCloser, stdoutPipe io.Reader, onInterrupt func(syscall.Signal, string)) {
    var wg sync.WaitGroup
    killOnce := sync.Once{}
    notifyInterrupt := func(sig syscall.Signal, reason string) {
        if onInterrupt == nil {
            return
        }
        killOnce.Do(func() {
            onInterrupt(sig, reason)
        })
    }
    control := detectControlChars()

    // 为 RTOS 输出增强的缓冲区大小
    buf := make([]byte, 32*1024) // 32KB 缓冲区

    // 带有改进缓冲的 stdout 复制 goroutine
    if tty.io.Stdout() != nil {
        wg.Add(1)
        go func() {
            log.Debug("[IO] Starting stdout copy with " + fmt.Sprintf("%d", len(buf)) + " byte buffer")
            defer wg.Done()
            defer func() {
                if c, ok := stdoutPipe.(io.Closer); ok {
                    c.Close()
                }
            }()

            for {
                select {
                case <-ctx.Done():
                    log.Debug("[IO] stdout copy canceled by context")
                    return
                default:
                }

                nr, err := stdoutPipe.Read(buf)
                if nr > 0 {
                    chunk := buf[:nr]
                    log.Debug("[IO] stdout read " + fmt.Sprintf("%d", nr) + " bytes")
                    if _, werr := tty.io.Stdout().Write(chunk); werr != nil {
                        log.Error("[IO] stdout write error: " + fmt.Sprintf("%v", werr))
                        return
                    }
                    // 刷新以确保提示符立即出现
                    if flusher, ok := tty.io.Stdout().(interface{ Flush() }); ok {
                        flusher.Flush()
                    }
                }
                if err != nil {
                    if errors.Is(err, io.EOF) {
                        log.Debug("[IO] stdout EOF reached")
                    } else if !errors.Is(err, context.Canceled) {
                        log.Debug("[IO] stdout copy error: " + fmt.Sprintf("%v", err))
                    }
                    return
                }
            }
        }()
    }

    // stdin 复制 goroutine
    if tty.io.Stdin() != nil {
        wg.Add(1)
        go func() {
            log.Debug("[IO] Starting stdin copy")
            defer wg.Done()
            defer close(stdinCloser)

            stdinBuf := make([]byte, 4096)
            for {
                select {
                case <-ctx.Done():
                    log.Debug("[IO] stdin copy canceled by context")
                    return
                default:
                }

                n, err := tty.io.Stdin().Read(stdinBuf)
                if n > 0 {
                    chunk := stdinBuf[:n]
                    log.Debug("[IO] stdin read " + fmt.Sprintf("%d", n) + " bytes: " + fmt.Sprintf("%q", chunk))
                    if sig, ok := control.detect(chunk); ok {
                        log.Info("[IO] Captured control character, interrupting")
                        notifyInterrupt(sig, "host-control")
                        return
                    }
                    if stdinPipe == nil {
                        log.Debug("[IO] stdin pipe is nil")
                        return
                    }
                    if _, werr := stdinPipe.Write(chunk); werr != nil {
                        log.Debug("[IO] stdin write error: " + fmt.Sprintf("%v", werr))
                        return
                    }
                }
                if err != nil {
                    if errors.Is(err, io.EOF) {
                        log.Debug("[IO] stdin EOF reached")
                    } else if !errors.Is(err, context.Canceled) {
                        log.Debug("[IO] stdin error: " + fmt.Sprintf("%v", err))
                    }
                    return
                }
            }
        }()
    } else {
        close(stdinCloser)
    }

    wg.Wait()
    close(exitch)
    log.Debug("[IO] All IO copies completed")
}
```

### 3. 后台模式的`FIFO`设置

FIFO 设置需要正确处理交互式和后台模式：

- **交互式模式** (`ctr task start`)：在`IO`上阻塞，直到容器退出
- **后台模式** (`ctr task start -d`)：立即返回，保持`FIFO`打开以供`attach`

### 4. Attach 实现

`Attach API`已由`containerd`提供。`shim`需要：
1. 在没有附加时保持`TTY`连接活跃
2. 允许重新连接到同一个`TTY`

## 测试用例指导

### 测试概述

`MicRun IO`系统提供了三个测试脚本，分别用于不同的测试场景：

| 测试脚本 | 类型 | 主要用途 |
|----------|------|----------|
| `test_io_regression.sh` | 自动化回归测试 | 验证 IO 系统的技术实现细节 |
| `test_io_manual.sh` | 手动测试指南 | 提供逐步的手动测试指导 |
| `test_io_system.sh` | 系统集成测试 | 端到端的完整功能验证 |

### 测试脚本位置

所有`IO`测试脚本位于 `micrun/tests/io/` 目录：
```
micrun/tests/io/
├── test_io_regression.sh   # 自动化回归测试
├── test_io_manual.sh       # 手动测试指南
└── test_io_system.sh       # 系统集成测试
```

### 1. 自动化回归测试 (`test_io_regression.sh`)

**用途**：自动化验证`IO`系统的技术实现，防止未来修改引入回归问题。

**测试用例**：

| # | 测试名称 | 测试内容 |
|---|----------|----------|
| 1 | 创建容器 | 验证容器能成功创建 |
| 2 | 启动和 TTY 设备 | 验证容器启动后 TTY 设备出现 |
| 3 | NUL 字符过滤 | 验证日志中存在 NUL 字符过滤记录 |
| 4 | 换行符过滤 | 验证日志中存在多余换行符过滤记录 |
| 5 | TTY raw 模式配置 | 验证 TTY 配置为 raw 模式（lflag=0x0） |
| 6 | 输出无 NUL 字符 | 验证 NUL 过滤功能正常工作 |
| 7 | help 命令 | 手动验证 help 命令输出 |
| 8 | 容器状态 | 验证容器正在运行 |

**使用方法**：
```bash
# 运行所有测试
cd micrun/tests/io
./test_io_regression.sh all

# 运行单个测试
./test_io_regression.sh 1  # 创建容器
./test_io_regression.sh 2  # 启动和验证 TTY
./test_io_regression.sh 5  # 验证 TTY raw 模式
```

**验证要点**：
- 检查测试输出中的 PASS/FAIL 状态
- 查看日志确认 TTY 配置正确（lflag 应为 0x0）
- 验证 NUL 字符和换行符过滤被激活

### 2. 手动测试指南 (`test_io_manual.sh`)

**用途**：提供逐步的手动测试指导，验证交互式终端行为。

**测试场景**：

| # | 测试场景 | 操作步骤 | 预期结果 |
|---|----------|----------|----------|
| 1 | `Enter`键响应 | 按`Enter`键`2-3`次 | 每次显示`openEuler UniProton #`提示符，无多余空行 |
| 2 | `help`命令 | 输入 `help` 并按`Enter` | 显示命令列表，命令项之间无多余空行 |
| 3 | `memInfo`命令 | 输入`memInfo`并按`Enter` | 显示内存信息表，表格无多余空行 |
| 4 | `Ctrl+C`终止 | 按`Ctrl+C` | 容器终止，返回`Shell`提示符 |

**使用方法**：
```bash
cd micrun/tests/io
./test_io_manual.sh

# 脚本会创建容器并提供逐步指导
# 按照提示操作进行测试
```

**验证命令**：
```bash
# 检查 TTY 配置（raw 模式）
journalctl -u containerd --since '1 minute ago' --no-pager | grep TTY

# 检查 NUL 字节过滤
journalctl -u containerd --since '1 minute ago' --no-pager | grep NUL

# 检查换行符过滤
journalctl -u containerd --since '1 minute ago' --no-pager | grep 'extra newline'
```

**预期日志输出**：
```
TTY Configuration should show:
  [TTY] RPMSG TTY configured: iflag 0x1280->0x3328, oflag 0x5->0x5, lflag 0x35387->0x0
                                                          ^^^^^^^^
                                                    lflag should be 0x0 (raw mode)

NUL Filtering should show:
  [IO] Filtered X NUL bytes from Y total

Newline Filtering should show:
  [IO] Filtered X extra newline bytes
```

### 3. 系统集成测试 (`test_io_system.sh`)

**用途**：端到端的完整功能验证，包括后台模式和 attach 功能。

**测试用例**：

| # | 测试名称 | 测试内容 |
|---|----------|----------|
| 1 | 创建容器 | 使用超时注解创建测试容器 |
| 2 | 交互式终端 | Enter 键、help 命令、Ctrl+C（手动） |
| 3 | 后台模式 | 验证 `ctr task start -d` 立即返回 |
| 4 | Attach | 验证 `ctr task attach` 能重新附加到容器（手动） |
| 5 | 检查日志 | 查看 IO/TTY 调试日志条目 |
| 6 | 检查 NUL 字符 | 验证输出中无 `^@` 字符 |

**使用方法**：
```bash
cd micrun/tests/io
./test_io_system.sh all

# 或运行单个测试
./test_io_system.sh 1  # 创建容器
./test_io_system.sh 3  # 后台模式
./test_io_system.sh 4  # Attach（需手动）
```

**后台模式和 Attach 测试**：
```bash
# 测试后台模式
ctr task start -d my-rtos
# 应立即返回命令行

# 验证容器在后台运行
ctr task ls

# 附加到容器
ctr task attach my-rtos
# 应能进入容器终端，Ctrl+D 分离
```

### 测试环境配置

**调试注解**：
```bash
# 使用注解延长容器超时时间
ctr container create \
  --annotation org.openeuler.micrun.container.auto_close_timeout=120 \
  ...
```

**日志分析**：
```bash
# 查看最近的 IO/TTY 日志
tail -100 /var/log/mica/mica-runtime.log | grep -E "\[IO\]|\[TTY\]"

# 实时监控日志
tail -f /var/log/mica/mica-runtime.log | grep -E "\[IO\]|\[TTY\]"
```

**关键日志标识**：
- `[TTY] Configuring RPMSG TTY for raw mode` - TTY 配置已应用
- `[TTY] Total drained X bytes` - 已从缓冲区清除陈旧数据
- `[IO] Filtered X NUL bytes` - NUL 字节过滤激活
- `[IO] stdout EOF` - 容器干净关闭

### 验证清单

部署`IO`系统修复后，已验证：

- [x] 输出中无`^@` (NUL) 字符
- [x] 第一次按`Enter`显示提示符，无大量输出重复
- [x] 每次后续`Enter`显示新的提示符行
- [x] `help`命令输出完整且一致
- [x] `Ctrl+C`正确终止容器
- [x] 后台模式 (`-d`) 立即返回
- [x] `ctr task attach`成功重新附加到运行中的容器

### 远程测试部署

```bash
# 复制测试脚本到测试主机
scp micrun/tests/io/test_io_*.sh root@192.168.7.2:/root/

# 在测试主机上运行
ssh root@192.168.7.2
cd /root
chmod +x test_io_*.sh

# 运行系统测试
./test_io_system.sh all
```

## 参考文献

1. [containerd Runtime v2 README](https://github.com/containerd/containerd/blob/main/core/runtime/v2/README.md)
2. [Implementing Container Runtime Shim: Interactive Containers](https://iximiuz.com/en/posts/implementing-container-runtime-shim-3/)
3. [Linux PTY Tutorial](https://www.linusakesson.net/programming/tty/)
4. [containerd FIFO package](https://github.com/containerd/fifo)

## 变更日志

| 日期 | 版本 | 变更 |
|------|------|------|
| 2025-02-02 | 1.4 | 添加 Epoll 零 CPU 等待、EventBus 事件系统、Binary IO 支持文档 |
| 2025-01-13 | 1.3 | 检查 |
| 2025-01-10 | 1.2 | 翻译为中文，添加完整测试用例指导 |
| 2025-01-10 | 1.1 | 添加自动化测试脚本和验证清单 |
| 2025-01-10 | 1.0 | 初始设计文档 |

## 说明

- 本文档是动态文档 - 随着实现进展进行更新
- 测试环境：`root@192.168.7.2`带`Xen pedestal`
- 参考`runc`行为以获取预期语义
