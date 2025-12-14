# MicRun 资源映射规范

## 概述

本文档详细说明 MicRun 如何将容器资源（来自 Kubernetes/containerd）映射到 RTOS 客户机的资源配置。MicRun 作为 containerd shimv2 运行时，需要将 Linux 容器的资源概念转换为适合 RTOS 运行环境的资源分配策略。

## 1. CPU 资源映射

### 1.1 核心概念

- **pCPU (Physical CPU)**：物理 CPU 核心，实际的硬件计算单元
- **vCPU (Virtual CPU)**：虚拟 CPU，Hypervisor 呈现给客户机的 CPU 抽象
- **cpuset (Affinity)**：CPU 亲和性设置，限制进程/客户机只能在指定的 pCPU 上运行

### 1.2 映射关系

```
Container CPU Share (cpu.shares) <==(放缩比例 1024:256)==> RTOS Client CPU Weight
Container Quota/Period (cpu.quota/cpu.period) <==(放缩比例 1:100)==> RTOS Client CPU Capacity (百分比，占满单核 100%)
Container cpuset (cpu.cpus) <====> RTOS Client CPUS (CPU 亲和性)
```

#### 详细说明：

1. **CPU Shares (权重)**
   - **容器侧**：`cpu.shares`，默认值 1024，范围 2-262144
   - **RTOS 侧**：`CPUWeight`，默认值 256，范围 1-65535
   - **转换公式**：`weight = max(1, min(shares / 4, 65535))`
   - **用途**：CPU 调度时的相对权重，用于公平调度

2. **CPU Quota/Period (绝对限制)**
   - **容器侧**：
     - `cpu.quota`：每个周期允许使用的 CPU 时间（微秒）
     - `cpu.period`：调度周期长度（微秒），通常为 100000（100ms）
     - CPU 核数限制 = `quota / period`
   - **RTOS 侧**：`CPUCapacity`，百分比形式，100% 表示占满一个 vCPU
   - **转换公式**：`capacity = (quota × 100) / period`
   - **约束**：最终容量受 cpuset 限制：`effective_capacity = min(capacity, cpuset_size × 100)`

3. **CPU Set (亲和性)**
   - **容器侧**：`cpu.cpus`，格式如 "0-3" 或 "0,1,3"
   - **RTOS 侧**：`ClientCpuSet`，直接传递 CPU 亲和性设置
   - **作用**：硬亲和性，限制客户机只能在指定的 pCPU 上运行

### 1.3 VCPU 数量策略

#### 默认策略：VCPU = 1
- 大多数情况下，RTOS 客户机只需要 1 个 vCPU
- 简化调度，减少 Hypervisor 开销

#### 可选策略：VCPU = Size(cpuSetUnion)
- 需要通过 runtime config 或 annotation 显式启用
- 启用开关：`vcpu_pcpu_binding=true`
- **VCPU 与 PCPU 对应关系**：
  1. 启用 vcpu_pcpu_binding：VCPUs : PCPUs = 1:1
  2. 通常情况下：
     - 对于 sandbox：VCPUs : PCPUs = 1:N，N = Size(cpuSetUnion) 或 = Sum(cpuCapacity)
     - 对于 container：VCPUs : PCPUs = 1:M，M = Size(cpuSet) 或 = cpuCapacity

#### CPU 容量为 0 的特殊情况
- 如果 `CPUCapacity = 0`，表示 pedestal (hypervisor) 不限制 CPU 用量
- 客户机可以尽可能使用分配的 CPU 资源

### 1.4 Shared CPU Pool 概念

#### 背景
- 如果一个容器设置了 cpuset，调度器不会允许它运行在 cpuset 之外的 pCPU 上
- 如果一个 sandbox 中有多个容器都设置了 cpuset，可以考虑将它们的 cpuset 并集作为一个 CPU pool

#### 实现方案
- **shared_cpu_pool** 选项：sandbox 内的所有容器都只能运行在这个 CPU pool 的 pCPU 上
- **当前状态**：仅在 MicRun 中保留的概念，未来可能实现对 pedestal CPU pool 的实际操控

#### 注意事项
- 机器上可能有多个 sandbox，它们管理的 CPU pool 范围可能重合
- 例如：sandbox1: (0,1,2)，sandbox2: (1,2,3)
- 需要谨慎处理资源分配和调度策略

## 2. 内存资源映射

### 2.1 映射关系

```
No Container Memory resource                     <====> RTOS Client pedestal max memory
Container memory limit      <====> RTOS Client memory limit
Container memory reservation < memory limit <====> RTOS Client memory min
```

### 2.2 详细说明

#### 术语区分
- **Container 语境**：使用 container memory limit, minimal memory
- **libmica 语境**：使用 RTOS Client memory resource

#### 资源记录位置
1. **container.me.records**：记录 libmica 语境下的资源量
2. **container.me.memoryThreshold**：设计为单调递增的，仅在新的 memory threshold 出现时才会正向更新
3. **pedestal.EssentialResource**：不记录 memoryThreshold
   - `memorymaxmb` 对应 OCI spec 中的 `mem.Limit`
   - `mem min` 对应 OCI spec 中的 `mem.Reservation`
   - 该类型记录的是实际资源

#### 实现策略
- 仅在 `micaexecutor` 中记录 `memoryThreshold`
- 保证简单性：只有 `memory threshold >= container memory limit`
- `memorymaxmb` 对应 mica create message 中的 `memory`

### 2.3 内存阈值管理

#### 单调递增特性
- `memoryThreshold` 只能增加，不能减少
- 确保资源分配的安全性
- 避免内存碎片和分配失败

#### 更新条件
```go
if new_threshold > current_threshold {
    update_threshold(new_threshold)
}
```

## 3. 其他资源处理

### 3.1 不支持/忽略的资源

| 资源类型 | 处理方式 | 原因 |
|---------|---------|------|
| `oom_score_adj` | 部分忽略 | RTOS 无 OOM killer 概念 |
| `hugepage_limits` | 暂时忽略 | 依赖 RTOS 支持，未来可扩展 |
| `unified` (cgroup v2) | 完全忽略 | 不适用于 RTOS 环境 |
| `memory_swap_limit_in_bytes` | 忽略 | Xen 环境下不支持 swap |

### 3.2 NUMA 内存节点
- `cpu.mems`：暂时不处理
- 未来可根据 RTOS 和硬件支持情况考虑实现

## 4. 配置优先级

### 4.1 配置来源优先级
1. **Annotation** (最高优先级)
2. **Config Files**
3. **Default Values** (最低优先级)

### 4.2 注解配置示例
```yaml
annotations:
  io.kubernetes.cri.cpuset-cpus: "0-3"
  io.containerd.micrun.vcpu-pcpu-binding: "true"
  # io.containerd.micrun.shared-cpu-pool: "true" 没有这个选项，因为cpupool是一个全局的方案，
  # 应该设置 shared_cpu_pool 在 micrun config 中
```

## 5. 实现要点

### 5.1 代码位置
- **资源解析**：`pkg/pedestal/planner.go` - `linuxResourceToEssential()`
- **资源映射**：`pkg/micantainer/container_resources.go`
- **资源管理**：`pkg/libmica/resource_manager.go`

### 5.2 关键函数
```go
// CPU 资源转换
func linuxResourceToEssential(spec *specs.Spec, convertShares bool) *EssentialResource

// 内存阈值管理
func (me *MicaExecutor) EnsureMemoryLimit(target uint32) error

// VCPU 数量计算
func calculateVcpuNum(cpuSet string, enableVcpuPcpuBinding bool) uint32
```

### 5.3 测试验证

#### 测试场景
1. **cpuset 限制更严格**：quota=200% (2.0核)，cpuset="0" (1核) → capacity=100%
2. **quota 限制更严格**：quota=50% (0.5核)，cpuset="0-3" (4核) → capacity=50%
3. **只有 cpuset**：无 quota/period，cpuset="0-1" (2核) → capacity=200%
4. **只有 quota**：quota=150% (1.5核)，无 cpuset → capacity=150%

#### 与 runc 语义对齐
通过上述实现，MicRun 实现了与 `runc` 相同的资源限制语义：
- **cpuset**：作为物理天花板，限制容器可用的 CPU 核心数
- **quota/period**：作为逻辑上限，限制容器可使用的 CPU 时间片
- **最终限制**：取两者的最小值，确保配置的物理可实现性
