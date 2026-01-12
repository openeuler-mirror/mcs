# `MicRun`日志系统使用指南

## 1. 概述

`MicRun`日志系统遵循`containerd shim v2`规范，支持`release`和`debug`两种构建模式。

## 2. 快速开始

### 2.1 默认配置

日志系统默认从`/etc/micrun/config.json`读取配置。如果文件不存在，将使用默认配置。

默认配置：
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

### 2.2 基本使用

```go
import log "micrun/logger"

// 简单日志
log.Info("Container created")
log.Error("Failed to start:", err)

// 格式化日志
log.Infof("%s is already down, not need to stop it", id)
log.Errorf("create: failed to load spec: %v", err)

// 带字段的日志
log.WithField("id", "test-container").Info("Starting container")
log.WithError(err).Error("Operation failed")
```

## 3. 配置说明

### 3.1 配置项

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `level` | `string` | `info` | 日志等级：`debug`, `info`, `warn`, `error` |
| `file` | `string` | `/var/log/mica/mica-runtime.log` | `debug`版本日志文件路径 |
| `color` | `boolean` | `false` | `debug`版本是否显示颜色 |
| `caller` | `boolean` | `true` | `debug`版本是否显示调用栈信息 |

### 3.2 日志等级

- `debug`: 详细调试信息
- `info`: 一般信息（默认）
- `warn`: 警告信息
- `error`: 错误信息

## 4. `release`版本

### 4.1 输出位置

`release`版本只输出到`containerd`的`log fifo`（位于`shim cwd`下的`log`文件）。

### 4.2 日志格式

与`containerd`原生日志格式完全一致：

```
time="2026-01-09T15:04:05.123456789Z" level=info msg="Container created" id=test-container namespace=default
```

格式说明：
- 时间戳：纳秒精度`RFC3339`格式
- 字段顺序：`id`在前，`namespace`在后
- `namespace`：从`CONTAINERD_NAMESPACE`环境变量读取（默认`default`）

### 4.3 构建

```bash
make release
# 或
go build
```

## 5. `debug`版本

### 5.1 输出位置

`debug`版本同时输出到两个位置：
1. **containerd log fifo**：与`release`版本格式一致
2. **日志文件**：`/var/log/mica/mica-runtime.log`（可配置）

### 5.2 日志格式

**输出到`containerd`**（与`release`一致）：
```
time="2026-01-09T15:04:05.123456789Z" level=info msg="Container created" id=test-container namespace=default
```

**输出到文件**：
```
[default][test-container][2026-01-09T15:04:05.123456789Z]INFO /path/to/file.go:123 main.create
	Container created
```

格式说明：
- 形式：`[namespace][id][timestamp]LOGLEVEL file:line func\n\tmessage`
- 时间戳：纳秒精度`RFC3339`格式
- 包含调用位置信息（文件:行号 函数名）

### 5.3 构建

```bash
make debug
# 或
go build -tags=debug
```

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

**使用示例**：
```go
// 格式化日志
log.Infof("Container %s created with PID %d", id, pid)
log.Debugf("Memory limit: %d MB, CPU quota: %d", mem, cpu)
log.Errorf("Failed to connect: %v", err)
```

### 6.3 带字段的日志

```go
// 添加单个字段
func WithField(key string, value any) *logrus.Entry

// 添加多个字段
func WithFields(fields logrus.Fields) *logrus.Entry

// 添加错误字段
func WithError(err error) *logrus.Entry
```

### 6.4 容器`ID`和`Namespace`设置

```go
// 设置当前容器`ID`（后续日志会自动包含此`ID`）
func SetContainerID(id string)

// 获取当前容器`ID`
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

### 7.1 基本日志

```go
import log "micrun/logger"

// 简单日志
log.Info("Service starting")
log.Debug("Detailed debug information")
log.Warn("This is a warning")
log.Error("An error occurred:", err)
```

### 7.2 带容器 ID 的日志

```go
// 设置容器 ID
log.SetContainerID("my-container-123")

// 设置 namespace（可选，默认从 CONTAINERD_NAMESPACE 环境变量读取）
log.SetNamespace("default")

// 后续所有日志都会自动包含 id 和 namespace 字段
log.Info("Container started")
// 输出: time="..." level=info msg="Container started" id=my-container-123 namespace=default
```

### 7.3 使用 WithField/WithFields

```go
// 添加单个字段
log.WithField("operation", "create").Info("Creating container")

// 添加多个字段
log.WithFields(logrus.Fields{
    "operation": "start",
    "duration":  "100ms",
}).Info("Container started")

// 添加错误
err := someOperation()
if err != nil {
    log.WithError(err).Error("Operation failed")
}
```

### 7.4 自定义初始化

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

## 8. 迁移指南

### 8.1 日志调用方式

日志系统支持三种调用方式：

**1. 基本日志（多个参数）**：
```go
log.Info("Container", id, "created")
log.Debug("Memory:", mem, "MB, CPU:", cpu)
```

**2. 格式化日志（推荐）**：
```go
log.Infof("Container %s created with PID %d", id, pid)
log.Debugf("Memory limit: %d MB, CPU quota: %d", mem, cpu)
log.Errorf("Failed to connect: %v", err)
```

**3. 结构化日志（推荐用于复杂场景）**：
```go
log.WithField("id", id).Info("Container created")
log.WithFields(logrus.Fields{
    "operation": "start",
    "duration":  "100ms",
}).Info("Container started")
log.WithError(err).Error("Operation failed")
```

### 8.2 注意事项

1. **格式化函数完整支持**：`Debugf`、`Infof`、`Warnf`、`Errorf`等函数完整支持`printf`风格的格式化
2. **推荐使用格式化函数**：对于需要拼接多个变量的日志，推荐使用`Xxxf`格式化函数，代码更简洁
3. **`id`和`namespace`自动添加**：使用`SetContainerID`和`SetNamespace`后，所有日志自动包含`id`和`namespace`字段
4. **字段顺序**：与`containerd`保持一致，`id` 在前，`namespace`在后
5. **时间戳精度**：纳秒级精度，与`containerd`原生日志完全一致

## 9. 故障排查

### 9.1 日志未输出

**问题**：日志没有显示在`containerd`日志中

**解决方案**：
1. 检查`containerd`的`log fifo`是否存在（`ls -l log`）
2. 检查日志配置文件是否正确
3. 使用`debug`版本查看详细日志

### 9.2 配置文件无效

**问题**：配置文件修改后没有生效

**解决方案**：
1. 检查`JSON`格式是否正确
2. 检查配置文件路径是否正确（默认`/etc/micrun/config.json`）
3. 检查文件权限

### 9.3 `debug`日志文件未创建

**问题**：`debug`版本运行时日志文件未创建

**解决方案**：
1. 检查`/var/log/mica/`目录是否存在
2. 检查目录权限
3. 手动创建目录：`sudo mkdir -p /var/log/mica`

## 10. 附录

### 10.1 配置文件示例

**完整配置** (`/etc/micrun/config.json`)：
```json
{
  "log": {
    "level": "debug",
    "file": "/var/log/mica/mica-runtime.log",
    "color": true,
    "caller": true
  }
}
```

**最小配置**：
```json
{
  "log": {
    "level": "info"
  }
}
```

### 10.2 相关文档

- [设计文档](./design.md)
- [containerd shim v2 规范](https://github.com/containerd/containerd/blob/main/core/runtime/v2/README.md)
