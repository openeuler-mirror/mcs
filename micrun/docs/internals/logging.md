# MicRun 日志系统

## 1. 概述

`MicRun` 日志系统遵循 `containerd shim v2` 规范，支持 `release` 和 `debug` 两种构建模式。

### 1.1 重构目标

1. 遵循 `containerd shim v2` 日志规范
2. 支持从配置文件读取日志配置
3. 添加格式化日志函数（带 `f` 后缀的函数）
4. 支持 `release/debug` 双模式，不同输出策略
5. 自动添加 `namespace`（表示容器命名空间）和 `id`（表示容器名称）字段

### 1.2 参考文档

- [containerd Runtime v2 README - Logging](https://github.com/containerd/containerd/blob/main/core/runtime/v2/README.md#logging)

## 2. 架构设计

### 2.1 文件结构

```
micrun/internal/support/logger/
├── logger.go           # 公共接口和类型定义
├── logger_release.go   # release 版本实现（!debug build tag）
└── logger_debug.go     # debug 版本实现（debug build tag）
```

### 2.2 构建标签

- `logger_release.go`: 使用 `// +build !debug` 或 `//go:build !debug`
- `logger_debug.go`: 使用 `//go:build debug`

### 2.3 重构前后对比

| 方面 | 重构前 | 重构后 |
|------|--------|--------|
| **输出目标** | 仅 `stderr` | `containerd FIFO`（`release/debug`） + 文件（`debug`） |
| **配置方式** | 代码硬编码 | 配置文件 `/etc/mica/micrun/config.json` |
| **构建模式** | 单一模式 | `release/debug` 双模式分离 |
| **字段注入** | 手动添加 | 自动通过 `context hook` 注入 |
| **时间戳精度** | 秒 | 纳秒（匹配 `containerd`） |
| **字段调整** | `timespace=...` | `time="..."` |
| **字段更名** | `message=...` | `msg=...` |
| **字段新增** | - | `id=... namespace=...` |
| **调用位置** | 指向 `logger` 包装函数 | 指向实际调用源 |
| **格式化函数** | 不支持 | 完整支持 `Xxxf` 系列 |

## 3. 日志格式

### 3.0 日志流向

```mermaid
flowchart LR
    Code[MicRun runtime code] --> Hook[logger context hook]
    Hook --> CD[containerd formatter]
    CD --> FIFO[shim cwd log FIFO]
    FIFO --> Journal[containerd journal]
    Hook -->|debug build| FileFmt[debug file formatter]
    FileFmt --> RuntimeLog[/var/log/mica/mica-runtime.log]
```

### 3.1 输出到 containerd 的日志（`release` + `debug`）

**格式**：`time="<timestamp>" level=<level> msg=<message> id=<id> namespace=<namespace>`

**示例**：
```
time="2026-01-09T15:04:05.123456789Z" level=info msg="Container created" id=test-container namespace=default
```

**要求**：
- 时间戳：`RFC3339` 格式，纳秒精度
- 字段名：`time`, `level`, `msg`, `id`, `namespace`
- 字段顺序：`id` 在前，`namespace` 在后
- 时间戳加引号
- `msg` 和字段值按 logfmt 风格输出；包含空白、引号、反斜杠或换行时使用 Go 字符串转义，保证单行可解析

### 3.2 输出到文件的日志（仅 `debug`）

**格式**：`[namespace][id][timestamp]LOGLEVEL file:line func\n\tmessage`

**示例**：
```
[default][test-container][2026-01-09T15:04:05.123456789Z]INFO internal/transport/shimv2/shimio.go:651 micrun/internal/transport/shimv2.copyStdin
	Starting stdin copy
```

**要求**：
- 前缀顺序：`namespace`, `id`, `timestamp`
- 时间戳：`RFC3339` 格式，纳秒精度
- LOGLEVEL：大写（`INFO`, `DEBUG`, `WARN`, `ERROR`）
- 调用位置：指向实际源文件，非 `logger` 包装

### 3.3 面向用户、开发者和 AI 的日志约定

MicRun 的日志需要同时支持现场排障、自动化测试和 AI 维护。新增或修改日志时建议遵循：

- `msg` 保持短句，描述发生了什么；把容器、namespace、阶段、结果等上下文放到字段或稳定前缀中。
- 新增关键链路日志优先使用稳定字段，如 `component`, `event`, `action`, `result`, `reason`, `hint`。
- 正常生命周期竞态不要使用 `error` 级别。例如用户输入 `exit` 后 guest 已自然退出，强制 stop 遇到 socket reset 应作为已退出状态处理。
- `info` 可记录自动恢复成功的状态清理；`warn` 表示需要关注但系统仍能继续；`error` 表示用户或开发者需要介入。
- 避免输出宿主机私有绝对路径、密钥、token、密码等信息。文档和测试记录中使用 `<workspace>`, `<build-dir>`, `<qemu-output>` 等泛化路径。
- 错误日志尽量包含下一步排查提示，例如 `hint="check containerd journal and mica runtime log"`。

推荐的 containerd 日志形态：

```text
time="2026-01-09T15:04:05.123456789Z" level=info msg="io session started" id=demo namespace=default component=io event=session.start result=ok
time="2026-01-09T15:04:08.123456789Z" level=info msg="cleanup recovered stale state" id=demo namespace=default component=recovery event=cleanup.stale result=recovered
```

**TRACE 日志示例**（仅 debug 版本）：
```
[default][test-container][2026-01-09T15:04:05.123456789Z]DEBUG internal/adapters/io/copier.go:234 micrun/internal/adapters/io.addFdToEpoll
	[TRACE] epoll fd=5 added to interest list
```

## 4. 输出策略

### 4.1 `release` 版本

- **输出目标**：仅输出到 `containerd`
- **输出位置**：通过 `shim cwd` 下的 `log fifo`
- **格式**：containerd 兼容格式（time="..." level=... msg=... id=... namespace=...）
- **配置**：从 `/etc/mica/micrun/config.json` 读取（可选），可通过 `MICRUN_LOG_CONFIG` 覆盖

**构建**：
```bash
make release
# 或
go build
```

### 4.2 `debug` 版本

- **输出目标 1**：输出到 `containerd`（与 `release` 版本格式一致）
- **输出目标 2**：输出到文件（默认 `/var/log/mica/mica-runtime.log`）
- **双输出实现**：使用两个独立的 `hook`，分别写入不同的输出

**构建**：
```bash
make debug
# 或
go build -tags=debug
```

## 5. 配置说明

### 5.1 配置文件

配置文件路径：`/etc/mica/micrun/config.json`（可通过 `MICRUN_LOG_CONFIG` 覆盖）

```json
{
  "log": {
    "level": "info",
    "file": "/var/log/mica/mica-runtime.log",
    "color": false,
    "caller": true
  }
}
```

### 5.2 配置项

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `level` | `string` | `info` | 日志等级：`debug`, `info`, `warn`, `error` |
| `file` | `string` | `/var/log/mica/mica-runtime.log` | `debug` 版本日志文件路径 |
| `color` | `boolean` | `false` | `debug` 版本是否显示颜色 |
| `caller` | `boolean` | `true` | `debug` 版本是否显示调用栈信息 |

### 5.2 环境变量

| 环境变量 | 说明 | 默认值 |
|----------|------|--------|
| `MICRUN_LOG_CONFIG` | 覆盖日志配置文件路径 | `/etc/mica/micrun/config.json` |
| `MICRUN_LOG_FILE` | 覆盖 debug 文件日志路径 | `/var/log/mica/mica-runtime.log` |
| `MICRUN_CONTAINERD_LOG_PATH` | 覆盖 containerd 日志输出路径 | `./log` |

### 5.3 日志等级

| 等级 | 说明 | Release 版本 | Debug 版本 |
|------|------|--------------|------------|
| `trace` | 测试专用诊断信息（仅 debug 编译生效） | **不输出**（编译优化掉） | 输出带 `[TRACE]` 前缀 |
| `debug` | 详细调试信息 | 输出 | 输出 |
| `info` | 一般信息（默认） | 输出 | 输出 |
| `warn` | 警告信息 | 输出 | 输出 |
| `error` | 错误信息 | 输出 | 输出 |

**Trace 级别说明**：
- `Tracef` 仅在 debug 编译版本（`-tags debug`）中生效
- Release 版本中所有 `Tracef` 调用会被编译器优化掉，零运行时开销
- 适用场景：高频事件、测试专用细节（如 fd 值、字节级跟踪、epoll 原始事件）

## 6. API 参考

### 6.1 基本日志方法

```go
func Debug(args ...any)
func Info(args ...any)
func Warn(args ...any)
func Error(args ...any)
func Fatal(args ...any)  // 退出程序
func Panic(args ...any)  // panic
```

### 6.2 格式化日志方法

```go
func Tracef(format string, args ...any)  // 仅 debug 编译生效
func Debugf(format string, args ...any)
func Infof(format string, args ...any)
func Warnf(format string, args ...any)
func Errorf(format string, args ...any)
func Fatalf(format string, args ...any)  // 退出程序
func Panicf(format string, args ...any)  // panic

// Level 别名（与上述函数等价）
func DebugLevelf(format string, args ...any)
func InfoLevelf(format string, args ...any)
func WarnLevelf(format string, args ...any)
func ErrorLevelf(format string, args ...any)

// 向后兼容（等价于 Debugf）
func Pretty(format string, args ...any)
```

**Tracef 使用说明**：
- **Debug 编译版本**：输出到 containerd FIFO 和日志文件，带 `[TRACE]` 前缀
- **Release 编译版本**：完全无开销，编译器优化掉所有调用
- **使用场景**：
  - ✅ 测试专用诊断（fd 值、字节级跟踪）
  - ✅ 高频事件（对生产定位无帮助）
  - ❌ 不要用于功能性调试（用 Debugf）
  - ❌ 不要用于状态变化（用 Infof）

### 6.3 结构化日志方法

```go
func WithField(key string, value any) *logrus.Entry
func WithFields(fields logrus.Fields) *logrus.Entry
func WithError(err error) *logrus.Entry
```

### 6.4 容器 ID 和 Namespace 设置

```go
// 设置当前容器 ID（后续日志会自动包含此 ID）
func SetContainerID(id string)

// 获取当前容器 ID
func GetContainerID() string

// 设置当前 namespace（后续日志会自动包含此 namespace）
func SetNamespace(ns string)

// 获取当前 namespace
func GetNamespace() string

// 获取默认 namespace（从环境变量 CONTAINERD_NAMESPACE 读取，默认 "default"）
func GetDefaultNamespace() string
```

### 6.5 初始化方法

```go
// 加载配置文件
func LoadConfig(configPath string) (*Config, error)

// 初始化日志系统
func Initialize(cfg *Config) error

// 静默输出（用于 bootstrap 阶段）
func SilenceOutput()

// 恢复输出
func RestoreOutput() error
```

## 7. 使用示例

### 7.1 基本使用

```go
import log "micrun/logger"

// 简单日志
log.Info("Container created")
log.Error("Failed to start:", err)

// 格式化日志（推荐）
log.Infof("Container %s created with PID %d", id, pid)
log.Debugf("Memory: %d MB, CPU: %d%%", mem, cpu)
log.Errorf("Failed to connect: %v", err)

// 带字段的日志
log.WithField("id", "test-container").Info("Starting container")
log.WithError(err).Error("Operation failed")
```

### 7.2 设置容器上下文

```go
// 设置容器 ID
log.SetContainerID("my-container-123")

// 设置 namespace（可选，默认从 CONTAINERD_NAMESPACE 环境变量读取）
log.SetNamespace("default")

// 后续所有日志都会自动包含 id 和 namespace 字段
log.Info("Container started")
// 输出: time="..." level=info msg="Container started" id=my-container-123 namespace=default
```

### 7.3 自定义初始化

```go
// 加载自定义配置文件
cfg, err := log.LoadConfig("/path/to/custom-config.json")
if err != nil {
    log.Error("Failed to load config:", err)
}

// 使用自定义配置初始化
if err := log.Initialize(cfg); err != nil {
    log.Error("Failed to initialize logger:", err)
}
```

### 7.4 Tracef 使用示例

```go
// Tracef - 仅在 debug 版本输出
log.Tracef("epoll fd=%d added to interest list", fd)
log.Tracef("context canceled: %v", ctx.Err())
```

## 8. 实现细节

### 8.1 Context Hook 自动注入

通过 `logrus Hook` 机制自动添加 `namespace` 和 `id` 字段：

```go
type contextHook struct{}

func (h *contextHook) Fire(entry *logrus.Entry) error {
    // 获取当前 namespace 和 container ID
    ns := getCurrentNamespace()
    id := getCurrentContainerID()

    // 添加到 entry.Data
    if ns != "" && entry.Data[NamespaceKey] == nil {
        entry.Data[NamespaceKey] = ns
    }
    if id != "" && entry.Data[IDKey] == nil {
        entry.Data[IDKey] = id
    }
    return nil
}
```

### 8.2 调用位置修正

`debug` 模式下，通过栈回溯找到实际调用者：

```go
func getRealCaller() (file string, line int, fn string) {
    // 从第 4 层开始（跳过：getRealCaller, Fire, contextHook.Fire, Log）
    for i := 4; i < 15; i++ {
        pc, f, l, ok := runtime.Caller(i)
        if !ok {
            break
        }
        // 跳过 logger 包内的帧
        if !isLoggerPackage(f) {
            return f, l, runtime.FuncForPC(pc).Name()
        }
    }
    return "", 0, ""
}
```

### 8.3 containerd 兼容格式化

```go
type containerdFormatter struct{}

func (f *containerdFormatter) Format(entry *logrus.Entry) ([]byte, error) {
    // 格式: time="..." level=... msg=... id=... namespace=...
    timestamp := entry.Time.Format("2006-01-02T15:04:05.000000000Z")
    b.WriteString("time=\"")
    b.WriteString(timestamp)
    b.WriteString("\" ")

    b.WriteString("level=")
    b.WriteString(entry.Level.String())
    b.WriteString(" ")

    b.WriteString("msg=")
    // ... 添加消息

    // 字段顺序：id 在前，namespace 在后
    if id, ok := entry.Data[IDKey]; ok {
        b.WriteString(" id=")
        b.WriteString(fmt.Sprintf("%v", id))
    }
    if ns, ok := entry.Data[NamespaceKey]; ok {
        b.WriteString(" namespace=")
        b.WriteString(fmt.Sprintf("%v", ns))
    }

    return b.Bytes(), nil
}
```

## 9. 环境变量

| 环境变量 | 说明 | 默认值 |
|----------|------|--------|
| `CONTAINERD_NAMESPACE` | 当前容器命名空间 | `default` |

## 10. 故障排查

### 10.1 日志未输出

**问题**：日志没有显示在 `containerd` 日志中

**解决方案**：
1. 检查 `containerd` 的 `log fifo` 是否存在（`ls -l log`）
2. 检查日志配置文件是否正确
3. 使用 `debug` 版本查看详细日志

### 10.2 配置文件无效

**问题**：配置文件修改后没有生效

**解决方案**：
1. 检查 `JSON` 格式是否正确
2. 检查配置文件路径是否正确（默认 `/etc/mica/micrun/config.json`）
3. 检查文件权限

### 10.3 `debug` 日志文件未创建

**问题**：`debug` 版本运行时日志文件未创建

**解决方案**：
1. 检查 `/var/log/mica/` 目录是否存在
2. 检查目录权限
3. 手动创建目录：`sudo mkdir -p /var/log/mica`

## 11. 版本历史

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-02-03 | 1.4 | 新增 Trace 级别（仅 debug 编译生效，release 版本零开销） |
| 2026-01-13 | 1.3 | 检查 |
| 2026-01-12 | 1.2 | 添加重构前后对比章节 |
| 2026-01-09 | 1.1 | 实现完成 |
| 2026-01-09 | 1.0 | 初始设计 |

## 12. 相关文档

- [containerd shim v2 规范](https://github.com/containerd/containerd/blob/main/core/runtime/v2/README.md)
