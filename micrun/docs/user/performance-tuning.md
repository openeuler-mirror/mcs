# MicRun 性能调优指南

## 概述

本文档提供 MicRun 性能优化的建议和配置选项。

## CPU 性能

### CPU 亲和性绑定

绑定 vCPU 到物理 CPU 可以减少缓存未命中和上下文切换。

**启用方式**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.enable_vcpus_pinning: "true"
spec:
  containers:
  - resources:
      limits:
        cpu: "2"
        cpu.cpus: "0-1"  # 绑定到 CPU 0 和 1
```

**适用场景**：
- 实时性要求高的 RTOS 应用
- 需要稳定性能的场景

**注意事项**：
- 绑定后容器无法迁移到其他 CPU
- 需要确保有足够的物理 CPU

### 共享 CPU 池 vs 独立 CPU

| 模式 | 优点 | 缺点 | 适用场景 |
|------|------|------|----------|
| `shared_cpu_pool=true` | CPU 利用率高，适合低负载 | 性能隔离较差 | 多容器低负载 |
| `shared_cpu_pool=false` | 性能隔离好 | CPU 利用率低 | 高性能要求 |

**配置**：
```ini
[Resource]
shared_cpu_pool = false  # 独立 CPU 模式
```

### CPU 调度权重

通过 OCI spec 设置 CPU shares：
```yaml
spec:
  containers:
  - resources:
      requests:
        cpu: "512"  # CPU shares (相对权重)
```

## 内存性能

### HugePage 支持

HugePage 可以减少 TLB 未命中，提高内存访问性能。

**启用方式**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.hugepage_enable: "true"
```

**配置**：
```ini
[Resource]
hugepage_support = true
```

**前提条件**：
```bash
# 检查系统 HugePage 配置
cat /proc/meminfo | grep Huge

# 预分配 HugePage
echo 1024 > /proc/sys/vm/nr_hugepages
```

### 内存预分配

静态内存分配可以避免运行时分配的开销：

**配置**：
```ini
[Resource]
static_resource_management = true
```

**注解方式**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.static_resource: "true"
```

### 内存阈值优化

调整内存阈值可以影响回收行为：

```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.min_memory_mb: "64"  # 提高预留
```

## IO 性能

### FIFO 大小调整

调整管道大小可以提高吞吐量：

```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.pipe_size: "131072"  # 128KB
```

**默认值**：系统默认（通常 64KB）

### Epoll 零 CPU 等待

MicRun 默认使用 epoll 进行 IO 等待，空闲时 CPU 使用率接近 0%。

**验证**：
```bash
# 检查 IO 等待 CPU 使用率
top -p $(pgrep containerd-shim-mica-v2)
```

### NUL 字节过滤

启用 NUL 字节过滤可以减少不必要的处理：

```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.filter_nul: "true"
```

**默认值**：已启用

## 网络性能

### 禁用网络命名空间

如果不需要网络隔离，禁用网络命名空间可以减少开销：

```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.disable_new_netns: "true"
```

## 日志性能

### 日志级别

生产环境使用 `info` 级别：

```json
{
  "log": {
    "level": "info"
  }
}
```

### 调试构建 vs 发布构建

| 构建类型 | 日志输出 | 性能 |
|----------|----------|------|
| Debug (`-tags debug`) | 双输出（FIFO + 文件） | 较低 |
| Release | 仅 FIFO 输出 | 较高 |

生产环境使用 Release 构建：
```bash
cd micrun
make build BUILD_TYPE=release
```

## 资源限制建议

### 典型场景配置

#### 场景 1：低延迟 RTOS

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    org.openeuler.micrun.runtime.enable_vcpus_pinning: "true"
    org.openeuler.micrun.runtime.static_resource: "true"
    org.openeuler.micrun.container.min_memory_mb: "64"
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos-app
    resources:
      limits:
        cpu: "2"
        cpu.cpus: "4-5"
        memory: "128Mi"
```

#### 场景 2：高密度部署

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    org.openeuler.micrun.runtime.shared_cpu_pool: "true"
    org.openeuler.micrun.container.min_memory_mb: "16"
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos-app
    resources:
      limits:
        cpu: "0.5"
        memory: "32Mi"
```

#### 场景 3：内存密集型

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    org.openeuler.micrun.runtime.hugepage_enable: "true"
    org.openeuler.micrun.container.min_memory_mb: "256"
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos-app
    resources:
      limits:
        cpu: "2"
        memory: "512Mi"
      requests:
        memory: "256Mi"
```

## 监控工具

### 查看资源使用

```bash
# 容器指标
ctr tasks metrics <container-id>

# 系统资源
free -h
lscpu
xl info

# IO 统计
iotop
```

### 性能分析

```bash
# CPU 性能
perf top -p $(pgrep containerd-shim-mica-v2)

# 内存分析
pmap $(pgrep containerd-shim-mica-v2)

# 系统调用跟踪
strace -p $(pgrep containerd-shim-mica-v2)
```

## 性能基准

### 典型性能指标

| 指标 | 值 | 说明 |
|------|-----|------|
| IO 延迟 | <100ms | epoll 超时设置 |
| 空闲 CPU | ~0% | epoll 零 CPU 等待 |
| 启动时间 | <1s | 容器启动到 Ready |
| 内存开销 | <10MB | shim 进程常驻内存 |

### 优化检查清单

- [ ] 使用 Release 构建
- [ ] 启用 CPU 绑定（低延迟场景）
- [ ] 启用 HugePage（内存密集型）
- [ ] 设置合理的内存预留
- [ ] 调整 FIFO 大小（高吞吐场景）
- [ ] 使用适当的日志级别
- [ ] 监控资源使用情况

## 相关文档

- [配置参考手册](./configuration.md) - 完整配置选项
- [资源映射设计](./resource-design.md) - 资源限制规则
- [故障排查指南](./troubleshooting.md) - 性能问题排查
