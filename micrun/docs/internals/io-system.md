# MicRun IO 系统设计文档

## 概述

本文档描述 MicRun 的 IO 系统，用于处理 RTOS 容器的双向数据传输（containerd FIFO ↔ RPMSG TTY）。

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                    containerd (ctr/nerdctl)                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │ CreateTask   │  │ Start        │  │ Attach           │  │
│  └──────┬───────┘  └──────┬───────┘  └────────┬─────────┘  │
└─────────┼──────────────────┼──────────────────┼────────────┘
          │                  │                  │
          │       FIFO (stdio)                  │
          └──────────────────┼──────────────────┘
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                    MicRun Shim                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              pkg/io                                    │ │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────────────┐    │ │
│  │  │ Session  │  │ Copier   │  │ EventBus         │    │ │
│  │  │ - FIFO管理│  │- 数据复制 │  │- 事件发布/订阅   │    │ │
│  │  │          │  │- epoll优化│  │                  │    │ │
│  │  └──────────┘  └──────────┘  └──────────────────┘    │ │
│  └────────────────────────────────────────────────────────┘ │
└──────────────────────────────┬──────────────────────────────┘
                               │
                               │ RPMSG TTY
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                      Mica 守护进程 (micad)                  │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              XL 控制台管理模块                          │
│  └────────────────────────────────────────────────────────┘ │
└──────────────────────────────┬──────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    Xen 虚拟机监控器                         │
│  ┌────────────────────────────────────────────────────────┐ │
│  │       RTOS 容器 (Zephyr/UniProton/LiteOS)             │
│  │              /dev/ttyRPMSG_<container>_0               │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## 核心组件

### 文件结构

```
pkg/io/
├── types.go       # 配置类型定义
├── copier.go      # 双向数据复制 + epoll 零 CPU 等待优化
├── session.go     # 会话管理 + Restart() 集成 (支持 attach)
├── events.go      # 事件总线 (解耦 IO 层和 shim 层)
├── binary.go      # Binary IO 支持 (binary:// 协议)
└── copier_test.go # 单元测试
```

### 数据流

```
┌──────────┐ stdin FIFO  ┌──────────┐     ┌──────────────┐
│   ctr    │────────────>│  Copier  │────>│  TTY In      │
│ (客户端) │<────────────│          │<────│  /dev/ttyRPMSG│
└──────────┘ stdout FIFO └──────────┘     └──────────────┘
```

### 核心功能

| 功能 | 实现位置 | 说明 |
|------|----------|------|
| FIFO 创建/打开 | `session.go` | 使用 `containerd/fifo` 包 |
| 双向数据复制 | `copier.go` | 两个 goroutine: stdin→TTY, TTY→stdout |
| **Epoll 零 CPU 等待** | `copier.go` | 使用 epoll 优化，空闲 CPU 使用率 ~0% |
| **EventBus 事件系统** | `events.go` | 解耦 IO 层和 shim 层的事件驱动架构 |
| NUL 字节过滤 | `copier.go` | RTOS 发送的 0x00 字节被过滤 |
| 换行压缩 | `copier.go`, `rpmsg_tty.go` | 压缩连续的 `\r\n` 序列，修复多余空行问题 |
| **回声抑制** | `copier.go` | 避免 PTY 和 RTOS 同时回声导致重复显示 |
| **Detach 序列检测** | `copier.go` | 可配置的 detach 键序列 (默认 Ctrl+P Ctrl+Q) |
| FIFO 重新打开 | `session.Restart()` | 支持多次 attach |
| "exit" 命令检测 | `copier.go` | 检测用户输入的 "exit" 命令，触发容器退出 |
| **统一 stdout/stderr** | `copier.go` | 当 stdout 和 stderr 相同时使用单个 copier |

## 设计原则

### 单一职责

每个组件只负责一件事：
- `Session` - 管理 FIFO 生命周期，支持 attach/detach
- `Copier` - 负责数据复制和 epoll 优化
- `EventBus` - 负责事件发布和订阅（解耦 IO 层和 shim 层）

### 性能优化

- **Epoll 零 CPU 等待**：使用 epoll 替代轮询，空闲时 CPU 使用率从 70% 降至 ~0%
- **响应时间 <100ms**：epoll 超时设置为 100ms，平衡响应性和 CPU 使用
- **缓冲区复用**：重用缓冲区减少内存分配

### 客户端分离

Detach 功能由客户端实现：
- nerdctl: 使用 `Ctrl+P Ctrl+Q` 进行 detach（仅 TTY 模式）
- ctr: 不支持 detach（设计限制）
- shim 不处理 detach 键序列（由客户端实现）

### 最小化依赖

只依赖必要的包：
- `github.com/containerd/fifo` - FIFO 操作
- 标准库 `io`, `syscall`, `sync` - 基础 IO

## 配置 (Config)

```go
type Config struct {
    // Container ID
    ContainerID string

    // FIFO 路径 (从 containerd 传入)
    StdinFIFO  string
    StdoutFIFO string
    StderrFIFO string

    // TTY 接口 (从 RPMSG 获取)
    TTYIn  io.WriteCloser  // stdin → TTY
    TTYOut io.Reader      // TTY → stdout
    TTYErr io.Reader      // TTY → stderr (可选)

    // 选项
    Terminal     bool  // 是否为终端模式
    EnableReopen bool  // 是否启用 FIFO 重新打开
    FilterNUL    bool  // 是否过滤 NUL 字节 (RTOS 需要)

    // 缓冲区大小
    StdinBufSize  int  // 默认 4KB
    StdoutBufSize int  // 默认 32KB
}
```

## IO 模式分类：ctr/nerdctl 命令选项组合

### 概述

MicRun shim 支持 `ctr` 和 `nerdctl` 两种客户端工具，它们支持不同的命令选项：

| 选项 | ctr | nerdctl | 说明 |
|------|-----|---------|------|
| `-i` | ❌ 不支持 | ✅ 支持 | nerdctl 提供输入（使用 nerdctl 提供的 stdin 路径） |
| `-t` | ✅ 支持 | ✅ 支持 | 启用 TTY 终端模式 |
| `-d` | ✅ 支持 | ✅ 支持 | 后台运行（无 stdin FIFO = 后台） |

**重要**：`-i` 选项在 nerdctl 中表示"提供输入"，不带 `-i` 时 nerdctl 表示只读不提供输入。但为了兼容 ctr（ctr 没有 `-i` 选项，一定会指定输入），我们在 shim 层统一生成 stdin FIFO。

### 理论上的 8 种组合 vs 实际的 6 种模式

三个选项 `-i`, `-t`, `-d` 的理论组合是 2³ = 8 种：

| 编号 | 组合 | nerdctl 支持 | 说明 |
|------|------|-------------|------|
| 1 | 无选项 | ✅ | 前台非 TTY |
| 2 | `-i` | ✅ | 前台交互非 TTY |
| 3 | `-t` | ✅ | 前台 TTY |
| 4 | `-i -t` | ✅ | 前台交互 TTY |
| 5 | `-d` | ✅ | 后台非 TTY |
| 6 | `-i -d` | ❌ | **nerdctl 拦截**（交互式后台无意义） |
| 7 | `-t -d` | ✅ | 后台 TTY |
| 8 | `-i -t -d` | ❌ | **nerdctl 拦截**（交互式后台 TTY 无意义） |

**为什么 nerdctl 拦截 `-i -d` 组合？**

- `-i` 表示"需要交互输入"
- `-d` 表示"后台运行，立即返回"
- 两者语义冲突：后台模式通常不需要交互输入
- nerdctl 在 CLI 层拦截并报错：`"interactive mode requires -i and -t to be specified"`

### MicRun 支持的 6 种 IO 模式

| 模式 | 命令 | IsTTY | IsForeground | HasStdin | stdin来源 | attach能力 | detach能力 | 使用场景 |
|------|------|-------|--------------|----------|----------|-----------|-----------|---------|
| 1 | `-i -t` | ✅ | ✅ | ✅ | nerdctl提供 | **多次attach** | ✅ (back命令) | 交互式TTY调试，需反复attach |
| 2 | `-i` | ❌ | ✅ | ✅ | nerdctl提供 | ✅ attach | ❌ | 交互式非TTY，简单调试 |
| 3 | `-t -d` | ✅ | ❌ | ✅ | 生成标准FIFO | **多次attach** | ✅ (back命令) | 后台TTY调试，需反复attach |
| 4 | `-t` | ✅ | ✅ | ✅ | 生成标准FIFO | **多次attach** | ✅ (back命令) | 前台TTY查看输出 |
| 5 | `-d` | ❌ | ❌ | ✅ | 生成标准FIFO | ✅ attach | ❌ | 长期运行服务 |
| 6 | 无选项 | ❌ | ✅ | ✅ | 生成标准FIFO | ✅ attach | ❌ | 默认前台运行 |

### 核心判断规则

```go
// pkg/shim/iomode.go

// IsTTY: TTY 模式（影响终端配置、本地回显、detach 支持）
mode.IsTTY = r.Terminal

// IsForeground: 前台模式（有 nerdctl 提供的 stdin FIFO = 前台）
mode.IsForeground = (r.Stdin != "")

// HasStdin: 所有模式都支持输入（兼容 ctr）
mode.HasStdin = true
// - 带 -i: 使用 nerdctl 提供的 stdin (r.Stdin != "")
// - 不带 -i: 生成标准 stdin FIFO 以兼容 ctr

// SupportsAttach: 后台模式 或 TTY模式
mode.SupportsAttach = !mode.IsForeground || mode.IsTTY
// - TTY 模式支持多次 attach（detach 后可重新 attach）
// - 后台模式支持 attach

// SupportsDetach: TTY 模式
mode.SupportsDetach = mode.IsTTY
// - TTY 模式支持 Ctrl+P Ctrl+Q detach (nerdctl native mechanism)
```

### FIFO 路径生成规则

| 模式 | stdin | stdout | stderr | 说明 |
|------|-------|--------|--------|------|
| 1. `-i -t` | 使用 nerdctl 提供的路径 | 使用 nerdctl 提供的路径 | 使用 nerdctl 提供的路径 | nerdctl 提供具体路径 |
| 2. `-i` | 使用 nerdctl 提供的路径 | 使用 nerdctl 提供的路径 | 使用 nerdctl 提供的路径 | nerdctl 提供具体路径 |
| 3. `-t -d` | 生成标准 FIFO | 转换或生成 | 转换或生成 | 兼容 ctr，生成 stdin FIFO |
| 4. `-t` | 生成标准 FIFO | 转换或生成 | **合并到 stdout** | 兼容 ctr，生成 stdin FIFO |
| 5. `-d` | 生成标准 FIFO | 转换或生成 | 转换或生成 | 兼容 ctr，生成 stdin FIFO |
| 6. 无选项 | 生成标准 FIFO | 使用提供的路径 | 使用提供的路径 | 兼容 ctr，生成 stdin FIFO |

**标准 FIFO 路径格式**：
```
/run/containerd/io.containerd.runtime.v2.task/<namespace>/<container_id>/<stream>
```

### attach/detach 行为总结

**attach 支持**：
- **TTY 模式（1, 3, 4）**：支持多次 attach（detach 后可重新 attach）
- **非 TTY 模式（2, 5, 6）**：支持 attach

**detach 支持**：
- **所有 TTY 模式（1, 3, 4）**：支持 `Ctrl+P Ctrl+Q` 进行 detach（nerdctl 原生机制）
- **非 TTY 模式（2, 5, 6）**：不支持 detach（必须等待容器退出或 kill）

### 使用场景推荐

| 使用场景 | 推荐命令 | attach支持 | detach支持 | 适用场景 |
|---------|---------|-----------|-----------|---------|
| 交互式TTY调试 | `nerdctl run -i -t` | ✅ 多次 | ✅ Ctrl+P Ctrl+Q | 临时调试，需要反复 attach |
| 交互式非TTY | `nerdctl run -i` | ✅ attach | ❌ | 简单交互，不需要 detach |
| TTY后台调试 | `nerdctl run -t -d` | ✅ 多次 | ✅ Ctrl+P Ctrl+Q | 后台调试，需要反复 attach |
| TTY前台查看 | `nerdctl run -t` | ✅ 多次 | ✅ Ctrl+P Ctrl+Q | 查看输出，可随时 detach |
| 长期运行服务 | `nerdctl run -d` | ✅ attach | ❌ | 守护进程、后台服务 |
| 默认前台运行 | `nerdctl run` | ✅ attach | ❌ | 简单前台任务 |

### ctr 与 nerdctl 的兼容性设计

**设计原则**：
- **统一 shim 行为**：ctr 和 nerdctl 使用相同的 shim API 和行为
- **stdin 兼容性**：不带 `-i` 时生成标准 stdin FIFO，确保 ctr 可以正常工作
- **attach 统一**：TTY 模式统一支持多次 attach，便于调试

**ctr 命令示例**：
```bash
# ctr 没有 -i 选项，但 shim 会生成 stdin FIFO
ctr run -t localhost:5000/mica-uniproton-app:xen-0.1 test-tty
ctr run -d localhost:5000/mica-uniproton-app:xen-0.1 test-daemon
```

**nerdctl 命令示例**：
```bash
# nerdctl 有 -i 选项，会提供 stdin FIFO
nerdctl run -i -t localhost:5000/mica-uniproton-app:xen-0.1 test-interactive
nerdctl run -i localhost:5000/mica-uniproton-app:xen-0.1 test-non-tty

# nerdctl 拦截 -i -d 组合并报错
nerdctl run -i -d localhost:5000/mica-uniproton-app:xen-0.1 test
# Error: interactive mode requires -i and -t to be specified
```

## 使用方式

### Shim 中使用

```go
// pkg/shim/start.go

config := micrunio.Config{
    ContainerID:  c.id,
    StdinFIFO:    c.stdin,
    StdoutFIFO:   c.stdout,
    StderrFIFO:   c.stderr,
    TTYIn:        ttyIn,
    TTYOut:       ttyOut,
    TTYErr:       ttyErr,
    Terminal:     c.terminal,
    EnableReopen: true,   // 支持 attach
    FilterNUL:    true,   // 过滤 RTOS NUL 字节
}

// 创建并启动会话
session, err := micrunio.NewSession(config)
if err != nil {
    return err
}

// 设置中断处理器 ("exit" 命令 → 关闭 IO → 容器自然退出)
// 当用户输入 "exit" 命令时，IO 关闭会触发 waitContainerExit 停止容器
session.SetInterruptHandler(func(sig syscall.Signal) {
    log.Infof("[INTERRUPT] Exit command received for container %s", c.id)
    // 发送 IO 关闭信号，触发 waitContainerExit
    c.ioExit()
    // 停止 IO 会话 (关闭 FIFO，停止 copier)
    session.Stop()
})

// 启动会话 (创建 FIFO、打开、开始复制)
if err := session.Start(); err != nil {
    return err
}

// 存储会话以便后续清理
c.ioManager = &ioSessionWrapper{session: session}
```

### NUL 字节过滤

RTOS (如 UniProton) 通过 RPMSG TTY 发送的数据可能包含 NUL 字节 (0x00)，这些字节需要被过滤掉：

```go
// 过滤 NUL 字节
func filterNUL(dst, src []byte) []byte {
    for _, b := range src {
        if b != 0 {
            dst = append(dst, b)
        }
    }
    return dst
}
```

### 换行压缩 (Line Ending Compression)

**问题描述：**

RTOS 容器在交互式 shell 中按回车键会产生多余的空行。例如：
```
Hello, UniProton!



openEuler UniProton #
```
预期只有 1-2 个空行，实际出现了 3+ 个空行。

**根本原因：**

1. **TTY 输出处理**：TTY 的 `ONLCR` termios 标志将 `\n` 转换为 `\r\n`。当 RTOS 固件已经发送 `\r\n` 作为换行符时，这会导致 `\r\r\n`，在终端上显示为额外的空行。

2. **RTOS 固件行为**：RTOS 固件本身会输出多个连续的 `\r\n` 序列。

**解决方案：**

1. **禁用 TTY 输出处理** (`pkg/micantainer/rpmsg_tty.go`)：
   ```go
   // 禁用 OPOST 和所有输出处理标志
   // RTOS 已经发送正确的换行符 (\r\n)
   // TTY 应该透传数据，不做任何转换
   termios.Oflag &^= unix.OPOST | unix.ONLCR | unix.OCRNL | unix.OLCUC
   ```

2. **压缩连续换行符** (`pkg/io/copier.go`)：
   ```go
   // compressLineEndings 将连续的 \r\n 或 \n 序列压缩为单个 \r\n
   func compressLineEndings(data []byte) []byte {
       // 检测连续的 CRLF (\r\n) 序列并压缩为单个 CRLF
       // 检测连续的 LF (\n) 序列并转换为单个 CRLF
       // 保留单个换行符用于正常输出
   }
   ```

   在 `copyStdout()` 中集成：
   ```go
   // 压缩连续的换行符 (处理 RTOS 固件发送的多个 \r\n)
   data = compressLineEndings(data)
   ```

**验证结果：**

修复后的输出：
```
Hello, UniProton!

openEuler UniProton #
```
只有 1 个空行（符合预期），而不是修复前的 3+ 个空行。

**测试：**

运行验证测试（在远程主机 192.168.7.2 上）：
```bash
bash tests/io/test_newline_fix_verify.sh
```

### Exit 命令处理和 1:1:1 生命周期

**概述**

用户在 RTOS 容器的交互式 shell 中输入 "exit" 命令可以安全退出容器。同时遵守 1:1:1 的生命周期模型：

```
┌─────────────────────────────────────────────────────────────┐
│                        Shim 进程                             │
│  - 持续运行，响应 containerd API (State, Delete, Exec等)    │
│  - 不受用户输入 exit 或 detach 影响                           │
├─────────────────────────────────────────────────────────────┤
│                        Sandbox                              │
│  - 管理一个 RTOS 容器                                        │
│  - 容器退出时 Stop（停止 RTOS）                              │
│  - Delete API 时才 Delete（删除资源）                        │
├─────────────────────────────────────────────────────────────┤
│                        RTOS (micad)                         │
│  - 实际运行 UniProton 的 RTOS 实例                           │
│  - 通过 libmica.Start/Stop 控制                              │
└─────────────────────────────────────────────────────────────┘
```

**生命周期行为**

| 场景 | RTOS | Sandbox | Shim | 说明 |
|------|------|---------|------|------|
| 容器启动 | Start | Start | Running | 初始状态 |
| 用户输入 "exit" 命令 | Stop | Stop | **继续运行** ✓ | shim 检测到 "exit" 命令，触发 IO 关闭和容器停止 |
| 容器自然退出 | Stop | Stop | **继续运行** ✓ | waitContainerExit 停止容器，shim 继续运行 |
| `ctr task delete` | Stop | Delete | **继续运行** ✓ | 显式删除任务，shim 继续响应 API |
| `ctr container delete` | Stop | Delete | **退出** ✓ | 清理完成，shim 退出 |
| stdin 关闭 (ctr disconnect) | Stop | Stop | **继续运行** ✓ | ctr 关闭 stdin，shim 继续运行 |

**关键区别 - RTOS vs 传统容器**

- **传统容器**：容器退出时，shim 也退出（1:1 绑定）
- **RTOS 容器**：容器退出时，shim 继续运行（1:1:1 分离）
  - 无论前台还是后台模式
  - 只有显式 `ctr container delete` 时才退出

⚠️ **重要澄清**：
- **前台模式和后台模式的区别**主要在于 **ctr CLI 的行为**，而不是 shim 的生命周期
- 前台模式：ctr CLI 保持连接并等待容器退出
- 后台模式：ctr CLI 立即返回，不保持连接
- **但 shim 的生命周期行为在两种模式下是一致的**：容器停止后继续运行，直到显式删除

**设计原因**
1. Shim 需要持续响应 containerd 的 API 调用（State, Delete, Exec 等）
2. 支持多次 attach/detach 周期
3. 只有显式删除时才完全清理资源

**实现细节**

1. **"exit" 命令检测** (`pkg/io/copier.go`):

```go
// 逐字符处理输入，检测行终止符
for i := 0; i < n; i++ {
    ch := buf[i]
    c.lineBuf = append(c.lineBuf, ch)

    // 检查行终止符 (\n 或 \r)
    if ch == '\n' || ch == '\r' {
        // 检查是否为 "exit" 命令
        lineWithoutTerm := c.lineBuf[:len(c.lineBuf)-1]
        if isExitCommand(lineWithoutTerm) {
            log.Infof("[IO] 'exit' command detected, triggering exit handler")
            // 触发中断处理器
            if c.onInterrupt != nil {
                c.onInterrupt(syscall.SIGINT)
            }
            continue
        }
        // 不是 exit 命令，写入 TTY
        c.ttyIn.Write(c.lineBuf)
        c.lineBuf = c.lineBuf[:0] // 清空行缓冲
    }
}
```

2. **中断处理器** (`pkg/shim/start.go`):

```go
session.SetInterruptHandler(func(sig syscall.Signal) {
    log.Infof("[INTERRUPT] Exit command received for container %s", c.id)
    // 设置状态为 STOPPED
    s.mu.Lock()
    c.status = task.Status_STOPPED
    c.exit = 130 // 128 + SIGINT (标准 exit码)
    c.exitTime = time.Now()
    s.mu.Unlock()

    // 发送 IO 关闭信号，触发 waitContainerExit
    c.ioExit()

    // 关闭 IO 会话 (关闭 FIFO，停止 copier)
    session.Stop()
})
```

3. **容器退出处理** (`pkg/shim/shimContainer.go`):

**重要**: `sandbox.Stop()` 必须在锁外部调用，否则会阻塞 State() API 导致 "context deadline exceeded" 错误。

```go
func waitContainerExit(ctx context.Context, s *shimService, c *shimContainer) (int32, error) {
    // ... 等待 c.exitIOch 关闭 ...

    // Stop the sandbox WITHOUT holding the lock
    // This prevents blocking State() API and other operations
    if c.cType.CanBeSandbox() {
        s.mu.Lock()
        sandboxToStop := s.sandbox
        s.mu.Unlock()

        if sandboxToStop != nil {
            sandboxToStop.Stop(ctx, true)  // ← 停止 RTOS（不在锁内）
            // 不调用 Delete()，保留 sandbox 以便 shim 继续响应 API
        }
    }

    // Now acquire the lock briefly to update status
    s.mu.Lock()
    c.status = task.Status_STOPPED
    c.exit = uint32(ret)
    c.exitTime = timeStamp
    s.mu.Unlock()

    // 发送退出事件到 containerd
    s.ec <- exitEvent{...}
    return int32(ret), nil
    // ← 函数返回，但 shim 继续运行
}
```

**修复前的问题**:
- `sandbox.Stop()` 在持有 `s.mu.Lock()` 时被调用
- State() API 无法获取锁，导致 "context deadline exceeded"
- 任务状态显示为 UNKNOWN 而不是 STOPPED

**修复后的效果**:
- 锁在调用 `sandbox.Stop()` 之前释放
- State() API 可以正常获取锁并返回 STOPPED 状态
- 1:1:1 生命周期正确工作（RTOS 停止、Sandbox 停止、Shim 继续运行）

4. **显式删除处理** (`pkg/shim/shim_services.go`):

```go
func (s *shimService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
    // ...
    if c.cType.CanBeSandbox() {
        if s.sandbox != nil {
            s.sandbox.Stop(ctx, true)   // 停止 RTOS
            s.sandbox.Delete(ctx)        // ← 删除 sandbox
            s.sandbox = nil
        }
    }
    // ...
    return &taskAPI.DeleteResponse{...}, nil
}
```

**测试验证**

运行生命周期测试（在远程主机 192.168.7.2 上）：

```bash
# 方法1: 从本地运行测试脚本（推荐）
bash tests/io/test_ctrl_c_lifecycle.sh

# 方法2: 手动测试步骤
# 创建容器
ctr container create --runtime io.containerd.mica.v2 localhost:5000/mica-uniproton-app:xen-0.1 test-lifecycle

# 启动容器（使用 -d 标志进行后台启动）
ctr task start -d test-lifecycle

# 等待 30 秒超时
sleep 35

# 检查 shim 是否仍在运行
ps aux | grep containerd-shim-mica-v2

# 查询任务状态
ctr task status test-lifecycle

# 显式删除
ctr task delete test-lifecycle
ctr container delete test-lifecycle
```

**重要：启动模式说明**

| 启动方式 | 命令 | Shim 行为 | 说明 |
|---------|------|----------|------|
| 前台 | `ctr task start` | **Shim 保持运行** ✓ | ctr CLI 保持连接，shim 持续响应 API |
| 后台 | `ctr task start -d` | **Shim 保持运行** ✓ | 正确的 daemon 模式 |

**测试注意事项：**
1. **推荐使用 `-d` 标志**进行后台启动，以测试 daemon shim 的持久性
2. **前台模式下 shim 也保持运行**，1:1:1 生命周期在两种模式下都适用
3. 只有显式调用 `ctr container delete` 时，shim 才会退出

测试验证：
1. ✓ 容器创建和启动
2. ✓ RTOS 启动成功
3. ✓ 30秒超时后 Shim 继续运行
4. ✓ 可以查询任务状态（State API）
5. ✓ 显式删除正确清理
6. ✓ Shim 在删除后退出

**调试技巧**

**检查 shim 是否仍在运行：**
```bash
ps aux | grep containerd-shim-mica-v2
```

**检查任务状态：**
```bash
ctr task ls
ctr task status <container-id>
```

**检查 sandbox 状态：**
```bash
# 查看 micad 日志
journalctl -u micad -f
```

**常见问题**

**Q: 输入 "exit" 命令后容器状态是什么？**
A: 容器状态变为 `STOPPED`，但 shim 继续运行。这是 1:1:1 生命周期的正确行为。

**Q: 如何完全清理容器？**
A: 使用 `ctr task delete` 和 `ctr container delete` 显式删除。这会停止 RTOS、删除 sandbox，并导致 shim 退出。

**Q: Shim 什么时候退出？**
A: Shim 只在以下情况退出：
1. 显式调用 `ctr container delete` 删除所有容器
2. 收到 SIGTERM/SIGINT 信号（containerd 重启/停止）
3. Shutdown API 被调用

**Q: 为什么不直接发送 SIGTERM 到 RTOS？**
A: RTOS (micad) 是底层管理器，不能被信号杀死。必须通过 libmica.Stop() 正确停止。

**Q: 如何退出 RTOS 容器？**
A: 在 RTOS shell 中输入 `exit` 命令并按回车，shim 会检测到该命令并安全停止容器。

## 前台模式 vs 后台模式：Shim 生命周期设计

### 问题背景

在前面的章节中，我们已经实现了 1:1:1 生命周期模型（RTOS 停止 → Sandbox 停止 → Shim 继续运行）。然而，在实际测试中发现：

- **后台模式** (`ctr task start -d`)：shim 正确保持运行
- **前台模式** (`ctr task start`)：容器停止后 shim 也会退出

这是否是一个 bug？答案是否定的。这是 containerd 的**设计行为**，而非实现缺陷。本节将详细解释这一设计决策，并提供官方文档和代码调用链作为佐证。

### containerd 的前台/后台模式设计

#### 调用流程对比

```
前台模式 (ctr task start):
┌──────────────┐
│   ctr CLI    │
└──────┬───────┘
       │ 1. CreateTask
       ▼
┌──────────────┐
│  containerd  │ ◄─── 保持 TTRPC 连接打开
└──────┬───────┘
       │ 2. Start (attach IO, 等待退出)
       ▼
┌──────────────┐
│  Shim 进程   │
└──────┬───────┘
       │ 3. 等待容器退出
       ▼
┌──────────────┐
│   RTOS 容器   │
└──────────────┘
       │
       │ 容器退出
       ▼
┌──────────────┐
│  Shim 退出   │ ◄─── containerd 清理 shim
└──────────────┘

后台模式 (ctr task start -d):
┌──────────────┐
│   ctr CLI    │
└──────┬───────┘
       │ 1. CreateTask
       ▼
┌──────────────┐
│  containerd  │ ◄─── 立即返回，关闭连接
└──────┬───────┘
       │ 2. Start (detach 模式)
       ▼
┌──────────────┐
│  Shim 进程   │ ◄─── 持续运行，响应 API
└──────┬───────┘
       │ 3. 等待容器退出
       ▼
┌──────────────┐
│   RTOS 容器   │
└──────────────┘
       │
       │ 容器退出
       ▼
┌──────────────┐
│  Shim 继续运行 │ ◄─── daemon 模式，响应后续 API
└──────────────┘
```

#### 官方文档说明

根据 containerd 官方文档和社区文章：

> **[iximiuz - Implementing Container Runtime Shim](https://iximiuz.com/en/posts/implementing-container-runtime-shim/)**:
>
> In contrast to foreground mode, in detached mode there is no long-running foreground runc process once the container has started. In fact, there is no long-running `runc` process at all. However, this means that it is up to the caller to handle the stdio after `runc` has set it up for you.
>
> The main use-case of detached mode is for higher-level tools that want to be wrappers around `runc`. By running `runc` in detached mode, those tools have far more control over the container's `stdio` without `runc` getting in the way (most wrappers around `runc` like `cri-o` or `containerd` use detached mode for this reason).

> **[云原生实验室 - Containerd shim 原理深入解读](https://icloudnative.io/posts/shim-shiminey-shim-shiminey/)**:
>
> shim 将 Containerd 进程从容器的生命周期中分离出来，具体的做法是 runc 在创建和运行容器之后退出，并将 shim 作为容器的父进程，即使 Containerd 进程挂掉或者重启，也不会对容器造成任何影响。

#### containerd 源码调用链

根据 containerd 的源码分析，前台模式和后台模式的调用链如下：

**前台模式 (`ctr task start`)**:

```
用户执行: ctr task start <container-id>
  ↓
ctr CLI (cmd/ctr/commands/tasks/):
  → client.Task.Start(ctx, id)
    → containerd.service.Start(ctx, req)
      → shim.Start(ctx, req)  [通过 TTRPC]
        → [shim 启动容器，建立 IO]
        → [返回 StartResponse]
    → [containerd 保持 TTRPC 连接]
    → client.Wait(ctx)  // 阻塞等待容器退出
    ↓
[用户输入 "exit" 命令] ← RTOS 容器推荐的退出方式
  ↓
IO 层检测到 "exit" 命令
  → 触发中断处理器
  → 关闭 IO 会话
  → waitContainerExit 检测到 IO 关闭
  → sandbox.Stop() 停止容器
  ↓
[容器退出]
  → client.Wait() 返回
  → [ctr CLI 退出，不调用 Delete]
  ↓
[shim 继续运行，等待后续 API 调用]
```

**注意**：
- **micrun 不处理 Ctrl+C/SIGINT 信号**：RTOS 容器没有传统的信号处理机制
- **推荐退出方式**：在 RTOS shell 中输入 `exit` 命令
- **外部终止方式**：使用 `ctr task kill -s SIGTERM` 或 `SIGKILL`

**后台模式 (`ctr task start -d`)**:

```
用户执行: ctr task start -d <container-id>
  ↓
ctr CLI:
  → client.Task.Start(ctx, id)
    → containerd.service.Start(ctx, req)
      → shim.Start(ctx, req)
        → [shim 启动容器，建立 IO]
        → [返回 StartResponse]
    → [ctr CLI 立即返回]
    → [不保持连接，不调用 Wait]
  ↓
[shim 继续运行，等待 API 调用]
```

**删除任务 (`ctr task delete` 或 `ctr container delete`)**:

```
用户执行: ctr task delete <container-id>
  ↓
ctr CLI:
  → client.Task.Delete(ctx, id)
    → containerd.service.Delete(ctx, req)
      → shim.Delete(ctx, req)  [通过 TTRPC]
        → [shim 停止容器，释放资源]
      → shim.Shutdown(ctx)  [通过 TTRPC]
        → [shim 关闭 TTRPC server 并退出]
  ↓
[shim 进程结束]
```

**关键点**：
- **containerd 不主动清理**：只有显式调用 Delete API 时才清理 shim
- **前台/后台模式对 shim 来说没有区别**：两种模式下 shim 的生命周期行为一致
- **区别在于 ctr CLI**：前台模式保持连接并等待，后台模式立即返回

### 前台模式 vs 后台模式的实际差异

基于 containerd 源码分析和实际测试，**shim 的生命周期在两种模式下是一致的**：

**共同点**：
- shim 都会持续运行，响应 State、Delete、Exec 等 API 调用
- 只有显式调用 `ctr container delete` 时才会清理 shim
- stdin 关闭都不会自动清理 shim

**差异点**：
| 特性 | 前台模式 | 后台模式 |
|------|---------|---------|
| ctr CLI 连接 | 保持 gRPC 连接 | 启动后立即断开 |
| IO 流 | 实时转发到用户终端 | 通常重定向到日志/FIFO |
| ctr CLI 阻塞 | 是，等待容器退出 | 否，立即返回 |
| 用户退出方式 | 输入 "exit" 或关闭终端 | 需要 attach 才能交互 |
| shim 生命周期 | **继续运行** | **继续运行** |

**重要说明**：
- 文档早期版本提到"前台模式下 shim 会退出"是不准确的
- 对于 RTOS 容器，**1:1:1 生命周期模型在前后台模式都适用**
- 区别仅在于用户交互方式，不影响 shim 的核心行为

### 总结

1. **1:1:1 生命周期模型适用于前后台两种模式**，容器停止后 shim 继续运行
2. **前台和后台模式的区别仅在于 ctr CLI 的行为**，不影响 shim 的生命周期
3. **容器退出方式**：
   - 用户输入 "exit" 命令触发容器退出（推荐方式）
   - 使用 `ctr task kill -s SIGTERM` 或 `SIGKILL` 外部终止
   - stdin 关闭触发超时退出（**所有 IO 模式默认 30 秒超时**）
   - 所有方式都不触发 shim 清理
4. **shim 清理仅在显式删除时发生**：`ctr container delete`
5. **超时机制**：为防止资源泄漏，所有 IO 模式（TTY/Non-TTY、前台/后台）默认启用 30 秒超时。如需长期运行，需显式设置 `auto_close=false` 或 `auto_close_timeout=0` 注解
6. **RTOS 容器的推荐使用方式**：
   - 开发调试：使用 `ctr task start`（前台，便于查看输出）
   - 生产环境：使用 `ctr task start -d`（后台，符合 daemon 模式）+ `auto_close=false` 注解

### 参考文档

1. [containerd Runtime v2 README](https://github.com/containerd/containerd/blob/main/core/runtime/v2/README.md) - containerd 官方 shim v2 规范
2. [iximiuz - Implementing Container Runtime Shim](https://iximiuz.com/en/posts/implementing-container-runtime-shim/) - Shim 架构深度分析
3. [云原生实验室 - Containerd shim 原理深入解读](https://icloudnative.io/posts/shim-shiminey-shim-shiminey/) - 中文 shim 原理解析
4. [GitHub Issue #9727 - Containerd shim lifecycle discussion](https://github.com/containerd/containerd/issues/9727) - Shim 生命周期讨论

---

## EventBus 事件系统

### 概述

EventBus 是 IO 层和 shim 层之间的解耦机制。IO 层发布事件，shim 层订阅事件并做出响应。这种设计使得 IO 层不需要直接依赖 shim 层的接口。

### 架构

```
┌─────────────────────────────────────────────────────────────┐
│                        IO 层                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐    │
│  │   Copier     │  │   Session    │  │  Publisher   │    │
│  │ - 检测事件    │  │ - 管理状态    │  │ - 发布事件   │    │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘    │
└─────────┼──────────────────┼──────────────────┼────────────┘
          │                  │                  │
          │                  │                  ▼
          │                  │          ┌───────────────┐
          │                  │          │   EventBus     │
          │                  │          │ - 订阅管理     │
          │                  │          │ - 事件分发     │
          │                  │          └───────┬───────┘
          │                  │                  │
          │                  │         (订阅) │
          │                  │                  ▼
┌─────────┼──────────────────┼──────────────────┼────────────┐
│         │                  │              ┌─────┴─────────┐  │
│         ▼                  ▼              │   Subscriber  │  │
│  ┌─────────────────────────────────────────┐ │               │  │
│  │             Shim 层                     │ └───────────────┘  │
│  │  - 处理 ExitCommandDetected              │                      │
│  │  - 处理 StdinClosed                       │                      │
│  │  - 处理 IOError                           │                      │
│  └─────────────────────────────────────────┘                      │
└────────────────────────────────────────────────────────────────────┘
```

### 事件类型

| 事件类型 | 触发条件 | 订阅者行为 |
|----------|----------|-----------|
| `ExitCommandDetected` | 用户输入 "exit" 命令 | shim 停止容器 |
| `IOError` | IO 操作发生错误 | shim 记录错误并处理 |
| `TTYReady` | TTY 准备就绪 | shim 继续启动流程 |
| `StdinClosed` | stdin FIFO 被客户端关闭 | shim 检测容器状态 |
| `DetachDetected` | 用户输入 detach 序列 | shim 处理 detach |

### API 使用

**发布事件**（IO 层）：

```go
import "micrun/pkg/io"

// 发布事件
event := io.Event{
    Type:        io.ExitCommandDetected,
    ContainerID: containerID,
    Data:        nil,
}
eventBus.Publish(event)
```

**订阅事件**（shim 层）：

```go
import "micrun/pkg/io"

// 订阅特定类型的事件
ch := eventBus.Subscribe(io.StdinClosed)
go func() {
    for event := range ch {
        log.Infof("Received event: %v for container %s", event.Type, event.ContainerID)
        // 处理事件...
    }
}()
```

**清理资源**：

```go
// 关闭事件总线，所有订阅者通道都会被关闭
eventBus.Close()
```

### 设计特点

1. **解耦**：IO 层不需要知道 shim 层的存在，只需发布事件
2. **非阻塞**：发布事件不会阻塞，通道满时丢弃事件
3. **自动清理**：订阅者通过 context 生命周期自动清理
4. **类型安全**：使用 EventType 枚举确保事件类型正确

### 使用场景

1. **容器退出**：IO 层检测到 "exit" 命令 → 发布 `ExitCommandDetected` → shim 停止容器
2. **客户端断开**：IO 层检测到 stdin 关闭 → 发布 `StdinClosed` → shim 检查是否需要停止容器
3. **错误处理**：IO 层检测到错误 → 发布 `IOError` → shim 记录日志并采取恢复措施

---

## Binary:// 协议

### 概述

`binary://` 协议允许通过外部程序处理容器的 IO 流。这主要用于日志处理、数据转换等场景，特别是 nerdctl 的 detached 模式。

### URI 格式

```
binary://<binary-path>?<query-params>
```

- `binary-path`: 可执行文件的绝对或相对路径
- `query-params`: 传递给二进制程序的环境变量（格式：`key=value&key2=value2`）

### 工作原理

```
┌──────────────┐     ┌─────────────┐     ┌──────────────┐
│  Shim        │────>│ Binary Proc  │────>│  日志文件     │
│              │     │ (外部程序)   │     │  /var/log/... │
└──────────────┘     └─────────────┘     └──────────────┘
      │                                       ▲
      │                                       │
      └─────────── RTOS stdout/stderr ─────────┘
```

### 数据流

1. **RTOS → Binary**：通过 pipe 传输，Binary 处理后输出
2. **Binary → 用户**：通过 stdout 传输给用户
3. **用户 → RTOS**：通过 pipe 传输，Binary 转发到 RTOS

### 配置方式

在 Pod 配置中使用：

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    org.openeuler.micrun.container.stdout_binary: "binary:///usr/local/bin/log-processor"
    org.openeuler.micrun.container.stderr_binary: "binary:///usr/local/bin/log-processor?level=debug"
spec:
  containers:
  - name: rtos-app
    # ...
```

### 使用场景

1. **日志处理**：将 RTOS 输出重定向到日志处理程序
2. **数据转换**：将 RTOS 二进制数据转换为可读格式
3. **远程传输**：将日志实时传输到远程服务器

### 实现细节

```go
// pkg/io/binary.go

// BinaryIO 处理外部程序的 IO
type BinaryIO struct {
    cmd       *exec.Cmd
    container string
    uri       *url.URL
    // Pipes for communication...
}

// 创建 BinaryIO
binaryIO, err := io.NewBinaryIO(ctx, containerID, uri)
if err != nil {
    return err
}

// 获取 IO 接口
stdoutWriter := binaryIO.Stdout()  // 容器输出 → Binary
stdinReader := binaryIO.Stdin()   // Binary → 容器输入
```

---

## Session 状态管理

### 概述

Session 负责管理 IO 会话的生命周期，包括 FIFO 创建、打开、数据复制和关闭。Session 支持多次 attach，通过 `Restart()` 方法平滑切换 IO 流。

### 状态机

```
┌──────────┐
│  Created │  ← NewSession()
└─────┬────┘
      │
      │ Start()
      ▼
┌──────────┐
│ Started  │ ←─ FIFO 已创建，Copier 运行中
└─────┬────┘
      │
      │ Stop()
      ▼
┌──────────┐
│  Stopped │ ←─ Copier 已停止，FIFO 已关闭
└──────────┘
```

### 生命周期方法

| 方法 | 说明 | 时机 |
|------|------|------|
| `NewSession()` | 创建新会话 | shim 启动时 |
| `Start()` | 创建 FIFO，启动 Copier | 首次启动或 Restart 后 |
| `Stop()` | 停止 Copier，关闭 FIFO | 容器退出或 detach |
| `Restart()` | 平滑切换到新的 FIFO | 客户端 attach 时 |
| `Close()` | 清理所有资源 | shim 退出时 |

### Restart 机制

`Restart()` 方法支持多次 attach，其流程如下：

```
┌─────────────────────────────────────────────────────────────┐
│                  Attach 流程                                 │
│                                                                 │
│  客户端 Attach ──→ shim.Attach() ──→ session.Restart()        │
│                          │                                    │
│                          ▼                                    │
│              ┌─────────────────────────┐                      │
│              │ 1. Stop() 停止现有 Copier │                      │
│              │ 2. 保留 FIFO 句柄        │                      │
│              │ 3. Start() 重新创建 Copier │                     │
│              └─────────────────────────┘                      │
│                          │                                    │
│                          ▼                                    │
│                   新 Copier 使用新 FIFO                      │
│                                                                 │
└─────────────────────────────────────────────────────────────┘
```

### FIFO 路径管理

```go
// 生成标准 FIFO 路径
func GenerateStandardFIFOPath(namespace, containerID, stream string) string {
    return filepath.Join(
        "/run/containerd/io.containerd.runtime.v2.task",
        namespace,
        containerID,
        stream,  // "stdin", "stdout", "stderr"
    )
}
```

### 状态转换

| 当前状态 | 允许的操作 | 下一状态 |
|----------|-----------|----------|
| Created | Start() | Started |
| Started | Stop(), Restart() | Stopped, Started (Restart 后) |
| Stopped | Restart() | Started |

### 注意事项

1. **幂等性**：多次调用 `Stop()` 是安全的，只有第一次会生效
2. **资源清理**：`Close()` 会关闭所有 FIFO 并停止 Copier
3. **并发安全**：所有方法都使用 `sync.Mutex` 保护内部状态
4. **Context 取消**：Context 取消时会自动清理资源

---

### FIFO 重新打开

当客户端 attach 到已运行的容器时，会创建新的 FIFO。`Session.Restart()` 负责平滑切换：

```go
// session.go 中的 Restart() 方法
func (s *Session) Restart() error {
    // 1. 停止现有的 copier（但不关闭 FIFO）
    s.StopWithoutClosingFIFOs()

    // 2. 重新打开 FIFO 并创建新的 copier
    return s.Start()
}
```

**事件驱动支持**：
- 通过 EventBus 发布 `StdinClosed` 事件
- 当 attach 客户端断开时，IO 层等待新的客户端连接
- 新客户端连接后，调用 `Restart()` 恢复 IO 会话

## 客户端兼容性

### ctr

- ✅ `ctr task start` - 交互式启动（阻塞直到容器退出）
- ✅ `ctr task attach` - 附加到运行中的容器
- ❌ detach - 不支持（设计限制）

### nerdctl

- ✅ `nerdctl run -d` - 后台启动
- ✅ `nerdctl attach` - 附加
- ✅ `nerdctl run --detach-keys=ctrl-p,ctrl-q` - detach 支持
- ⚠️ `binary://` 协议 - 需要额外支持（用于日志处理）

### Kubernetes (CRI)

- ✅ 通过 CRI API 管理
- ✅ attach/detach 由 kubelet 处理

## 调试

### 日志标识

```
[SESSION] ...  # 会话管理日志
[IO] ...      # 数据复制日志
[EVENT] ...   # 事件总线日志
[TTY] ...     # RPMSG TTY 配置日志
```

### 常见问题

**Q: 如何退出 RTOS 容器？**
A: 在 RTOS shell 中输入 `exit` 命令并按回车。

**Q: 输入 exit 后容器状态**
A: 容器状态变为 `STOPPED`，但 shim 继续运行。要完全清理容器，需要执行 `ctr task delete` 和 `ctr container delete`。

**Q: attach 后没有输出**
A: 检查 FIFO 路径是否正确，TTY 是否已打开

**Q: 容器退出后 FIFO 没有清理**
A: 检查 `session.Stop()` 是否被调用

**Q: 交互式 shell 中有多余的空行**
A: 这是 TTY 输出处理和 RTOS 固件行为共同导致的问题。
   - 确保 `pkg/micantainer/rpmsg_tty.go` 中禁用了 `OPOST|ONLCR`
   - 确保 `pkg/io/copier.go` 中调用了 `compressLineEndings()`
   - 运行 `tests/io/test_newline_fix_verify.sh` 验证修复

## 参考文献

1. [containerd Runtime v2 README](https://github.com/containerd/containerd/blob/main/core/runtime/v2/README.md)
2. [containerd FIFO package](https://github.com/containerd/fifo)
3. [Implementing Container Runtime Shim](https://iximiuz.com/en/posts/implementing-container-runtime-shim/)
