# MicRun 资源映射参考

## 概述

本文档详细说明 MicRun 如何将容器资源（来自 Kubernetes/containerd）映射到 RTOS 客户机的资源配置。

## 1. CPU 资源映射

### 1.1 核心概念

| 概念 | 说明 |
|------|------|
| **pCPU** | 物理 CPU 核心，实际的硬件计算单元 |
| **vCPU** | 虚拟 CPU，Hypervisor 呈现给客户机的 CPU 抽象 |
| **cpuset** | CPU 亲和性设置，限制进程/客户机只能在指定的 pCPU 上运行 |

### 1.2 映射关系

```
Container CPU Share (cpu.shares) <==(放缩比例 1024:256)==> RTOS Client CPU Weight
Container Quota/Period (cpu.quota/cpu.period) <==(放缩比例 1:100)==> RTOS Client CPU Capacity
Container cpuset (cpu.cpus) <====> RTOS Client CPUS (CPU 亲和性)
```

### 1.3 详细映射规则

#### CPU Shares (权重)

| 容器侧 | RTOS 侧 | 转换公式 |
|--------|---------|----------|
| `cpu.shares` (默认 1024, 范围 2-262144) | `CPUWeight` (默认 256, 范围 1-65535) | `weight = max(1, min(shares / 4, 65535))` |

#### CPU Quota/Period (绝对限制)

| 容器侧 | RTOS 侧 | 说明 |
|--------|---------|------|
| `cpu.quota` (微秒) | `CPUCapacity` (%) | 容量 = (quota × 100) / period |
| `cpu.period` (微秒，通常 100000) | - | 调度周期长度 |

**核心约束规则**：
```
effective_capacity = min(
    (quota × 100) / period,   # quota/period 转换的百分比容量
    cpuset_size × 100          # cpuset 核心数 × 100%
)
```

#### CPU Set (亲和性)

| 容器侧 | RTOS 侧 | 格式 |
|--------|---------|------|
| `cpu.cpus` ("0-3" 或 "0,1,3") | `ClientCpuSet` | 直接传递 CPU 亲和性设置 |

### 1.4 VCPU 数量策略

| 策略 | 说明 |
|------|------|
| **默认** | VCPU = 1，大多数 RTOS 客户机只需要 1 个 vCPU |
| **vcpu_pcpu_binding=true** | VCPUs : PCPUs = 1:1，每个 vCPU 绑定到物理核心 |

### 1.5 Shared CPU Pool

- **shared_cpu_pool** 选项：sandbox 内的所有容器都只能运行在指定的 CPU pool 上
- **注意**：机器上可能有多个 sandbox，它们的 CPU pool 范围可能重合

## 2. 内存资源映射

### 2.1 映射关系

```
无容器内存限制                     <====> RTOS Client pedestal max memory
Container memory limit            <====> RTOS Client memory limit
Container memory reservation      <====> RTOS Client memory min
```

### 2.2 术语说明

| 术语 | 说明 |
|------|------|
| **Container 语境** | 使用 container memory limit, minimal memory |
| **libmica 语境** | 使用 RTOS Client memory resource |
| `memorymaxmb` | 对应 OCI spec 中的 `mem.Limit` |
| `mem min` | 对应 OCI spec 中的 `mem.Reservation` |

### 2.3 内存阈值管理

- `memoryThreshold` 单调递增，只能增加不能减少
- 确保 `memory threshold >= container memory limit`
- `memorymaxmb` 对应 mica create message 中的 memory

## 3. 不支持/忽略的资源

| 资源类型 | 处理方式 | 原因 |
|---------|---------|------|
| `oom_score_adj` | 部分忽略 | RTOS 无 OOM killer 概念 |
| `hugepage_limits` | 暂时忽略 | 依赖 RTOS 支持 |
| `unified` (cgroup v2) | 完全忽略 | 不适用于 RTOS 环境 |
| `memory_swap_limit_in_bytes` | 忽略 | Xen 环境下不支持 swap |
| `cpu.mems` (NUMA) | 暂时不处理 | 未来可扩展 |

## 4. Kubernetes 资源映射

### 4.1 K8s 到 cgroup 的转换

| K8s 字段 | cgroup 字段 | 转换方式 |
|----------|-------------|----------|
| `requests.cpu` | `cpu.shares` | `MilliCPUToShares()` 转换 |
| `limits.cpu` | `cpu.quota/period` | `MilliCPUToQuota()` 转换 |
| `limits.memory` | `memory.limit_in_bytes` | 直接转换 |

### 4.2 权重转换公式

$$
W(S) = \max{1, \min{\frac{S}{R}, 65535}}; \quad R=4
$$

其中 S 为 cgroup cpu.shares 值 (2-262144)

## 5. 配置优先级

配置来源优先级（从高到低）：

1. **Annotation** - Pod/容器注解
2. **Config Files** - `/etc/mica/micrun/config.json`
3. **Environment Variables** - `MICRUN_CONF_FILE`, `MICRUN_CONF_DIR`
4. **Default Values**

### 5.1 注解配置示例

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: rtos-pod
  annotations:
    io.kubernetes.cri.cpuset-cpus: "0-3"
    org.openeuler.micrun.runtime.vcpu_pcpu_binding: "true"
spec:
  containers:
  - name: rtos-app
    image: localhost:5000/mica-rtos-app:latest
    resources:
      limits:
        cpu: "1000m"
        memory: "512Mi"
```

## 6. 高级配置选项

### 6.1 静态资源管理

| 注解 | 值 | 说明 |
|------|-----|------|
| `org.openeuler.micrun.runtime.static_resource` | `true`/`false` | 启用后资源热更新将被忽略 |

### 6.2 vCPU 固定

| 注解 | 值 | 说明 |
|------|-----|------|
| `org.openeuler.micrun.runtime.enable_vcpus_pinning` | `true`/`false` | 每个 vCPU 绑定到物理核心 |

### 6.3 共享 CPU 池

| 注解 | 值 | 说明 |
|------|-----|------|
| `org.openeuler.micrun.shared_cpu_pool` | `true`/`false` | 允许多个 RTOS 容器共享 CPU 资源 |

### 6.4 大页支持

| 注解 | 值 | 说明 |
|------|-----|------|
| `org.openeuler.micrun.runtime.hugepage_enable` | `true`/`false` | 启用大页内存支持 |

## 7. 代码实现

### 7.1 关键文件

| 文件 | 说明 |
|------|------|
| `pkg/pedestal/planner.go` | 资源解析，`linuxResourceToEssential()` |
| `pkg/micantainer/container_resources.go` | 资源映射 |
| `pkg/libmica/resource_manager.go` | 资源管理 |

### 7.2 关键函数

```go
// CPU 资源转换
func linuxResourceToEssential(spec *specs.Spec, convertShares bool) *EssentialResource

// 内存阈值管理
func (me *MicaExecutor) EnsureMemoryLimit(target uint32) error

// VCPU 数量计算
func calculateVcpuNum(cpuSet string, enableVcpuPcpuBinding bool) uint32
```

## 8. 测试验证

### 8.1 测试场景

| 场景 | 配置 | 预期结果 |
|------|------|----------|
| cpuset 更严格 | quota=200%, cpuset="0" | capacity=100% |
| quota 更严格 | quota=50%, cpuset="0-3" | capacity=50% |
| 只有 cpuset | 无 quota, cpuset="0-1" | capacity=200% |
| 只有 quota | quota=150%, 无 cpuset | capacity=150% |

### 8.2 与 runc 语义对齐

MicRun 实现了与 `runc` 相同的资源限制语义：
- **cpuset**：作为物理天花板
- **quota/period**：作为逻辑上限
- **最终限制**：取两者的最小值
