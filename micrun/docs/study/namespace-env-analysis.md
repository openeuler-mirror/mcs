# Namespace 环境变量分析

## 概述

本文档分析 containerd 如何设置 `CONTAINERD_NAMESPACE` 环境变量，以及 micrun 如何使用它。

## 分析结论

**`CONTAINERD_NAMESPACE` 环境变量一定会被 containerd 设置**，不会为空。

## Containerd 设置环境变量的流程

### 1. Shim 命令创建

containerd 在创建 shim 进程时，通过 `Command()` 函数设置环境变量（参考 containerd 源码 `pkg/shim/util.go`）：

```go
func Command(ctx context.Context, config *CommandConfig) (*exec.Cmd, error) {
    // 1. 获取 namespace，要求非空
    ns, err := namespaces.NamespaceRequired(ctx)
    if err != nil {
        return nil, err  // namespace 为空时直接返回错误
    }
    // ...
    // 2. 设置环境变量
    cmd.Env = append(...,
        fmt.Sprintf("%s=%s", namespaceEnv, ns),
    )
}
```

### 2. NamespaceRequired 验证逻辑

`NamespaceRequired()` 函数确保 namespace 必须存在且非空（参考 containerd 源码 `pkg/namespaces/context.go`）：

```go
func NamespaceRequired(ctx context.Context) (string, error) {
    namespace, ok := Namespace(ctx)
    if !ok || namespace == "" {
        return "", fmt.Errorf("namespace is required: %w", errdefs.ErrFailedPrecondition)
    }
    if err := identifiers.Validate(namespace); err != nil {
        return "", fmt.Errorf("namespace validation: %w", err)
    }
    return namespace, nil
}
```

关键点：
- 如果 namespace 不存在 (`!ok`) 或为空 (`namespace == ""`)，会返回错误
- 因此 `Command()` 只会在 namespace 有效时才创建 shim 进程
- 环境变量 `CONTAINERD_NAMESPACE` 一定被设置为非空值

## Micrun 中的使用

### Logger.GetDefaultNamespace()

micrun 的日志系统从环境变量读取 namespace（`logger/logger.go:206-214`）：

```go
func GetDefaultNamespace() string {
    // Check CONTAINERD_NAMESPACE environment variable first
    // This is set by containerd when launching the shim
    if ns := os.Getenv("CONTAINERD_NAMESPACE"); ns != "" {
        return ns
    }
    // Fallback to "default" namespace
    return "default"
}
```

### 使用场景分析

| 启动方式 | CONTAINERD_NAMESPACE | 返回值 |
|---------|---------------------|--------|
| containerd/nerdctl/ctr | 一定存在，非空 | 环境变量值 |
| 手动运行 shim | 可能不存在 | "default" (fallback) |

- **通过 containerd 启动时**：环境变量总是存在，直接返回该值
- **手动运行/测试时**：可能没有该环境变量，使用 "default" fallback

## 总结

1. **环境变量一定会被设置** - containerd 的 `NamespaceRequired()` 保证这一点
2. **fallback "default" 是防御性编程** - 只在手动运行时生效，不会影响正常使用
3. **当前实现正确** - logger 代码不需要修改
