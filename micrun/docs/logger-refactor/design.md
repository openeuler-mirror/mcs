# MicRun 日志系统重构设计文档

## 1. 概述

本文档描述`MicRun`日志系统的重构设计，使其遵循`containerd shim v2`规范。

### 1.1 重构目标

1. 遵循`containerd shim v2`日志规范
2. 支持从配置文件读取日志配置
3. 添加格式化日志函数（带`f`后缀的函数）
4. 支持`release/debug`双模式，不同输出策略
5. 自动添加`namespace`（表示容器命名空间）和`id`（表示容器名称）字段

### 1.2 参考文档

- [containerd Runtime v2 README - Logging](https://github.com/containerd/containerd/blob/main/core/runtime/v2/README.md#logging)

## 2. 重构前后对比

### 2.1 架构对比

| 方面 | 重构前 | 重构后 |
|------|--------|--------|
| **输出目标** | 仅`stderr` | `containerd FIFO`（`release/debug`） + 文件（`debug`） |
| **配置方式** | 代码硬编码 | 配置文件 `/etc/micrun/config.json` |
| **构建模式** | 单一模式 | `release/debug`双模式分离 |
| **字段注入** | 手动添加 | 自动通过`context hook`注入 |
| **时间戳精度** | 秒 | 纳秒（匹配`containerd`） |
| **字段调整** | `timespace=...` | `timespace="..."` |
| **字段更名** | `message=...` | `msg=...` |
| **字段新增** | - | `id=... namespace=...` |
| **调用位置** | 指向`logger`包装函数 | 指向实际调用源 |
| **格式化函数** | 不支持 | 完整支持 `Xxxf` 系列 |

### 2.2 代码结构对比

**重构前**：
```
micrun/logger/
└── logger.go           # 单一文件，所有逻辑混杂
```

**重构后**：
```
micrun/logger/
├── logger.go           # 公共接口和类型定义
├── logger_release.go   # release 版本实现（!debug build tag）
└── logger_debug.go     # debug 版本实现（debug build tag）
```

### 2.3 日志格式对比

#### Containerd 输出格式

**重构前**：
```
time=01-02 15:04:05 level=info message="Container created"
```
- 时间戳格式：`01-02 15:04:05`（没有年份，毫秒精度）
- 字段名：`time`, `level`, `message`
- 字段顺序：固定

**重构后**：
```
time="2026-01-09T15:04:05.123456789Z" level=info msg="Container created" id=test-container namespace=default
```
- 时间戳格式：`RFC3339` + 纳秒精度（`123456789`）
- 字段名：`time`, `level`, `msg`, `id`，`namespace`（与 containerd 一致）
- 字段顺序：`id` 在前，`namespace` 在后
- 时间戳加引号（与 containerd 一致）

#### `debug`文件输出格式

**重构前**：
```
INFO[2026-01-09 15:04:05] /path/to/file.go:123 main.function Container created
```
- 使用`logrus`默认格式
- 调用位置指向`logger`包装函数
- 没有容器`namespace`和`id`信息

**重构后**：
```
[default][test-container][2026-01-09T15:04:05.123456789Z]INFO /path/to/file.go:123 main.create
	Container created
```
- 格式：`[namespace][id][timestamp]LOGLEVEL file:line func\n\tmessage`
- 调用位置指向实际调用源
- 包含完整的`namespace`和`id`信息
- 时间戳纳秒精度

### 2.4 API 对比

#### 初始化方式

**重构前**：
```go
// 简单的 Init 函数，配置硬编码
func Init(config *Config) error {
    if config == nil {
        return nil
    }
    // ... 手动设置 logrus 选项
}
```

**重构后**：
```go
// 配置文件支持 + 默认配置
func LoadConfig(configPath string) (*Config, error)
func Initialize(cfg *Config) error
func SilenceOutput()  // 用于 bootstrap 阶段
func RestoreOutput() error
```

#### Context 管理

```go
// 设置全局 context
func SetContainerID(id string)
func SetNamespace(ns string)
func GetContainerID() string
func GetNamespace() string
func GetDefaultNamespace() string  // 从环境变量读取

// 之后所有日志自动包含这些字段
log.Info("Container created")
// 输出: ... id=my-rtos namespace=default
```

#### 格式化函数

**重构前**：

```go
// 完整的 printf 风格格式化函数
log.Infof("Container %s created with PID %d", id, pid)
log.Debugf("Memory: %d MB, CPU: %d", mem, cpu)
log.Errorf("Failed: %v", err)

// Level 别名
log.InfoLevelf("...")  // 等价于 Infof
```

### 2.5 调用位置追踪对比

**重构前**：
```
INFO[2026-01-09 15:04:05] logger/logger.go:150 log.Info() Container created
```
- 总是指向`logger.go`的包装函数

**重构后**：
```
[default][test-container][2026-01-09T15:04:05.123Z]INFO pkg/shim/create.go:450 micrun/pkg/shim.Create
	Container created
```
- 指向实际调用源（通过栈回溯找到 logger 包之外的调用者）

### 2.6 配置对比

**重构前**：
- 代码中硬编码配置
- 没有配置文件支持
- `debug`模式通过编译时选项控制

**重构后**：
- 配置文件：`/etc/micrun/config.json`
- 默认配置内置，配置文件可选
- `debug`模式通过 build tag 自动切换

**配置文件示例**：
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

## 3. 架构设计

### 3.1 文件结构

```
micrun/logger/
├── logger.go           # 公共接口和类型定义
├── logger_release.go   # release 版本实现（!debug build tag）
└── logger_debug.go     # debug 版本实现（debug build tag）
```

### 3.2 构建标签

- `logger_release.go`: 使用 `// +build !debug` 或 `//go:build !debug`
- `logger_debug.go`: 使用 `//go:build debug`

### 3.3 配置文件

配置文件路径：`/etc/micrun/config.json`

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

## 4. 日志格式规范

### 4.1 输出到`containerd`的日志（`release` + `debug`）

**格式**：`time="<timestamp>" level=<level> msg=<message> id=<id> namespace=<namespace>`

**示例**：
```
time="2026-01-09T15:04:05.123456789Z" level=info msg="Container created" id=test-container namespace=default
```

**要求**：
- 时间戳：`RFC3339`格式，纳秒精度
- 字段名：`time`, `level`, `msg`, `id`, `namespace`
- 字段顺序：`id`在前，`namespace`在后
- 时间戳加引号

### 4.2 输出到文件的日志（仅`debug`）

**格式**：`[namespace][id][timestamp]LOGLEVEL file:line func\n\tmessage`

**示例**：
```
[default][test-container][2026-01-09T15:04:05.123456789Z]INFO pkg/shim/shimio.go:651 micrun/pkg/shim.copyStdin
	Starting stdin copy
```

**要求**：
- 前缀顺序：`namespace`, `id`, `timestamp`
- 时间戳：`RFC3339`格式，纳秒精度
- LOGLEVEL：大写（`INFO`, `DEBUG`, `WARN`, `ERROR`）
- 调用位置：指向实际源文件，非`logger`包装

## 5. 输出策略

### 5.1 `release`版本

- **输出目标**：仅输出到`containerd`
- **输出位置**：通过`shim cwd`下的`log fifo`
- **格式**：containerd 兼容格式（time="..." level=... msg=... id=... namespace=...）
- **配置**：从`/etc/micrun/config.json`读取（可选）

### 5.2 `debug`版本

- **输出目标 1**：输出到`containerd`（与`release`版本格式一致）
- **输出目标 2**：输出到文件（默认`/var/log/mica/mica-runtime.log`）
- **双输出实现**：使用两个独立的`hook`，分别写入不同的输出

## 6. 公共接口设计

### 6.1 类型定义

```go
// Config 日志配置结构
type Config struct {
    Log LogConfig `json:"log"`
}

type LogConfig struct {
    Level  string `json:"level,omitempty"`   // debug, info, warn, error
    File   string `json:"file,omitempty"`    // 文件路径
    Color  bool   `json:"color,omitempty"`   // 是否显示颜色
    Caller bool   `json:"caller,omitempty"`  // 是否显示调用栈
}

const (
    IDKey        = "id"          // 字段名：容器 ID
    NamespaceKey = "namespace"   // 字段名：命名空间
)
```

### 6.2 基础日志方法

```go
func Debug(args ...any)
func Info(args ...any)
func Warn(args ...any)
func Error(args ...any)
func Fatal(args ...any)
func Panic(args ...any)
```

### 6.3 格式化日志方法

```go
func Debugf(format string, args ...any)
func Infof(format string, args ...any)
func Warnf(format string, args ...any)
func Errorf(format string, args ...any)
func Fatalf(format string, args ...any)
func Panicf(format string, args ...any)

// Level 别名（与上述函数等价）
func DebugLevelf(format string, args ...any)
func InfoLevelf(format string, args ...any)
func WarnLevelf(format string, args ...any)
func ErrorLevelf(format string, args ...any)

// 向后兼容
func Pretty(format string, args ...any)  // 等价于 Debugf
```

### 6.4 结构化日志方法

```go
func WithField(key string, value any) *logrus.Entry
func WithFields(fields logrus.Fields) *logrus.Entry
func WithError(err error) *logrus.Entry
```

### 6.5 Context 管理方法

```go
func SetContainerID(id string)
func GetContainerID() string

func SetNamespace(ns string)
func GetNamespace() string
func GetDefaultNamespace() string  // 从 CONTAINERD_NAMESPACE 环境变量读取
```

### 6.6 初始化方法

```go
func LoadConfig(configPath string) (*Config, error)
func Initialize(cfg *Config) error
func SilenceOutput()  // 静默输出（bootstrap 阶段）
func RestoreOutput() error
```

## 7. 实现细节

### 7.1 Context Hook 自动注入

通过`logrus Hook`机制自动添加`namespace`和`id`字段：

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

### 7.2 调用位置修正

`debug`模式下，通过栈回溯找到实际调用者：

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

### 7.3 containerd 兼容格式化

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

### 7.4 `release/debug`构建分离

**logger_release.go** (`//go:build !debug`):
- 只输出到 containerd FIFO
- 简洁的格式化器
- 不包含颜色和详细调用信息

**logger_debug.go** (`//go:build debug`):
- 双输出：`containerd FIFO` + 文件
- 文件格式包含详细调用信息
- 可选颜色支持

## 8. 环境变量

| 环境变量 | 说明 | 默认值 |
|----------|------|--------|
| `CONTAINERD_NAMESPACE` | 当前容器命名空间 | `default` |

## 9. 使用示例

### 9.1 初始化

```go
import log "micrun/logger"

// 方式 1：使用默认配置
if err := log.Initialize(nil); err != nil {
    // 处理错误
}

// 方式 2：加载配置文件
cfg, err := log.LoadConfig("/path/to/config.json")
if err != nil {
    // 处理错误
}
if err := log.Initialize(cfg); err != nil {
    // 处理错误
}
```

### 9.2 设置 Context

```go
// 从命令行参数提取容器 ID
containerID := extractContainerID()
log.SetContainerID(containerID)

// 从环境变量读取 namespace
namespace := log.GetDefaultNamespace()  // 读取 CONTAINERD_NAMESPACE
log.SetNamespace(namespace)

// 后续所有日志自动包含这些字段
log.Info("Container started")
// 输出: time="..." level=info msg="Container started" id=my-rtos namespace=default
```

### 9.3 日志调用

```go
// 简单日志
log.Info("Service starting")

// 格式化日志（推荐）
log.Infof("Container %s created with PID %d", id, pid)
log.Debugf("Memory: %d MB, CPU: %d%%", mem, cpu)

// 结构化日志
log.WithField("operation", "create").Info("Creating container")
log.WithFields(logrus.Fields{
    "operation": "start",
    "duration":  "100ms",
}).Info("Container started")

// 错误日志
err := someOperation()
if err != nil {
    log.WithError(err).Error("Operation failed")
    // 或
    log.Errorf("Operation failed: %v", err)
}
```

## 10. 测试验证

### 10.1 编译验证

```bash
# release 版本
go build -mod=vendor -ldflags "-s -w"

# debug 版本
go build -mod=vendor -tags debug
```

### 10.2 功能验证

```bash
# 启动容器
ctr run --rm --runtime io.containerd.mica.v2 localhost:5000/mica-uniproton-app:xen-0.1 my-rtos

# 查看 containerd 日志
journalctl -u containerd -f | grep my-rtos

# 预期输出：
# time="1970-01-01T00:23:22.694084304Z" level=info msg="..." id=my-rtos namespace=default
```

### 10.3 文件日志验证（`debug`）

```bash
# 查看 debug 文件日志
cat /var/log/mica/mica-runtime.log

# 预期输出：
# [default][my-rtos][1970-01-01T00:23:22.694084304Z]INFO pkg/shim/shimio.go:651 ...
```

## 11. 版本历史

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-01-13 | 1.3 | 检查 |
| 2026-01-12 | 1.2 | 添加重构前后对比章节 |
| 2026-01-09 | 1.1 | 实现完成 |
| 2026-01-09 | 1.0 | 初始设计 |

## 12. 相关文档

- [使用指南](./usage.md) - 详细的使用说明
- [containerd shim v2 规范](https://github.com/containerd/containerd/blob/main/core/runtime/v2/README.md)
