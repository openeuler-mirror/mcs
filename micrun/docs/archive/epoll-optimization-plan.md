# IO Copier Epoll 优化方案

## 问题背景

当前 `copyStdoutErrUnified()` 函数使用非阻塞 `unix.Read()` 紧密轮询 TTY 文件描述符。当没有数据可读时，`unix.Read()` 立即返回 `EAGAIN`，goroutine 立即重试，导致 CPU 占用率高达 70%+。

### 现有代码问题

```go
for {
    // 紧密轮询 - 无延迟
    n, err := unix.Read(fd, buf)
    if isEAGAIN(err) {
        continue  // 立即重试，高 CPU 占用
    }
    // ... 处理数据
}
```

## 解决方案：使用 epoll 零 CPU 空闲等待

### 架构设计

```
┌─────────────────────────────────────────────────────────┐
│                    Unified Copier                         │
├─────────────────────────────────────────────────────────┤
│                                                           │
│  ┌─────────────────────────────────────────────────┐    │
│  │               epoll_wait()                       │    │
│  │  ┌──────────────────────────────────────────┐   │    │
│  │  │  Events:                                   │   │    │
│  │  │  - TTY fd (EPOLLIN | EPOLLET)             │   │    │
│  │  │  - cancelPipeR (EPOLLIN)                  │   │    │
│  │  └──────────────────────────────────────────┘   │    │
│  │                      ↓                           │    │
│  │              阻塞等待事件                         │    │
│  │                      ↓                           │    │
│  │  ┌──────────────────────────────────────────┐   │    │
│  │  │  Event received:                          │   │    │
│  │  │  - TTY has data → unix.Read()             │   │    │
│  │  │  - cancelPipe → return (exit)             │   │    │
│  │  │  - timeout → check context, retry         │   │    │
│  │  └──────────────────────────────────────────┘   │    │
│  └─────────────────────────────────────────────────┘    │
│                                                           │
│  CPU 使用率: 0% (空闲时) vs 70%+ (之前)                   │
└─────────────────────────────────────────────────────────┘
```

### 核心组件

#### 1. Copier 结构扩展

```go
type Copier struct {
    // ... 现有字段

    // epollFd is used for waiting on TTY data without busy polling.
    // Created when needed, closed in Stop(). Use -1 to indicate not created.
    epollFd int
}
```

#### 2. initEpoll() - 初始化 epoll 实例

```go
func (c *Copier) initEpoll(ttyFd int) error {
    // 创建 epoll 实例
    epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
    if err != nil {
        return fmt.Errorf("epoll_create1 failed: %w", err)
    }

    // 添加 TTY fd (边缘触发，单次通知)
    event := unix.EpollEvent{
        Events: unix.EPOLLIN | unix.EPOLLET,
        Fd:     int32(ttyFd),
    }
    unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, ttyFd, &event)

    // 添加 cancel pipe (用于唤醒)
    event = unix.EpollEvent{
        Events: unix.EPOLLIN,
        Fd:     int32(c.cancelPipeR),
    }
    unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, c.cancelPipeR, &event)

    return nil
}
```

#### 3. waitForData() - 等待数据就绪

```go
func (c *Copier) waitForData(ttyFd int) bool {
    // 首次调用时初始化 epoll
    if c.epollFd < 0 {
        c.initEpoll(ttyFd)
    }

    // 等待事件 (100ms 超时保持响应性)
    const epollTimeoutMs = 100
    events := make([]unix.EpollEvent, 4)
    n, err := unix.EpollWait(c.epollFd, events, epollTimeoutMs)

    // 处理事件...
    for i := 0; i < n; i++ {
        if events[i].Fd == int32(c.cancelPipeR) {
            return false  // Context canceled
        }
        if events[i].Fd == int32(ttyFd) {
            return true  // Data ready
        }
    }
    return true  // Timeout, try anyway
}
```

#### 4. 修改后的 copyStdoutErrUnified()

```go
func (c *Copier) copyStdoutErrUnified() {
    // 获取 TTY fd
    ttyFd := -1
    if fdObj, ok := c.ttyOut.(interface{ Fd() uintptr }); ok {
        ttyFd = int(fdObj.Fd())
    }

    for {
        select {
        case <-c.ctx.Done():
            return
        default:
        }

        // 使用 epoll 等待数据 (零 CPU 空闲等待)
        if !c.waitForData(ttyFd) {
            return
        }

        // 读取数据
        n, err := unix.Read(ttyFd, buf)
        // ... 处理数据
    }
}
```

### 资源清理

在 `Stop()` 和 `StopWithoutClosingFIFOs()` 中添加：

```go
// Clean up epoll fd
if c.epollFd >= 0 {
    unix.Close(c.epollFd)
    c.epollFd = -1
}
```

## 优势分析

| 指标 | 优化前 | 优化后 |
|------|--------|--------|
| 空闲 CPU 使用率 | 70%+ | ~0% |
| 数据延迟 | 即时 | <100ms |
| 唤醒延迟 | 0ms | <100ms |
| 系统调用 | 数百万/秒 | ~10/秒 |

## 边缘触发 vs 水平触发

使用 **边缘触发 (EPOLLET)**：
- 数据到达时仅通知一次
- 需要一次性读取所有可用数据
- 避免重复通知，更高效

水平触发备选方案：
- 每次等待都会通知
- 更简单但可能重复唤醒
- 适合少量数据场景

## 超时选择

使用 100ms 超时：
- 平衡响应性和 CPU 使用率
- 确保 context 取消检查
- 允许定期检查容器状态

## 实现文件

- `micrun/pkg/io/copier.go`
  - 添加 `epollFd` 字段
  - 新增 `initEpoll()` 方法
  - 新增 `waitForData()` 方法
  - 修改 `copyStdoutErrUnified()` 方法
  - 更新 `Stop()` 和 `StopWithoutClosingFIFOs()`

## 回退策略

如果 epoll 初始化失败：
- 记录错误日志
- 回退到原有的紧密轮询方式
- 功能不受影响，只是 CPU 使用率较高

```go
if err := c.initEpoll(ttyFd); err != nil {
    log.Errorf("[IO] Failed to init epoll, falling back to busy poll: %v", err)
    return true  // 立即尝试读取
}
```

## 测试验证

1. **功能测试**：容器正常启动，IO 正常工作
2. **性能测试**：CPU 使用率从 70% 降至 <5%
3. **压力测试**：大量数据传输无丢失
4. **退出测试**：Stop() 正确清理 epoll fd

## 未来优化

1. 考虑为 `copyStdout()` 和 `copyStderr()` 也添加 epoll
2. 动态调整超时时间
3. 统计 epoll 唤醒次数用于监控
