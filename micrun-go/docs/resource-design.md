# Resource Design for MicRun

## 资源映射规范

[总结映射规则](resource-mapping.md)

本文部分地方用 Q&A 的方式展开, 从新手开发者的角度展开了整体设计的考量

### 1. CPU 资源配置解析与映射

MicRun 从 `linux.resources.cpu` 部分解析以下 CPU 资源配置：

1. **CPU Shares** (`cpu.shares`): 相对权重，用于 CPU 调度
   - 默认值: 1024
   - 范围: 2-262144
2. **CPU Quota/Period** (`cpu.quota`, `cpu.period`): 绝对 CPU 限制
   - Quota: 每个周期允许使用的 CPU 时间（微秒）
   - Period: 调度周期长度（微秒），通常为 100000（100ms）
   - CPU 核数限制 = quota / period
3. **CPU Set** (`cpu.cpus`): 指定可使用的 CPU 核心
   - 格式: "0-3" 或 "0,1,3"
4. **CPU Memory Nodes** (`cpu.mems`): NUMA 内存节点，暂时不处理

**CPU 资源映射关系：**
```
Container CPU Share <==(放缩比例1024:256)==> RTOS Client CPU Weight
Container Quota/Period <==(放缩比例1:100)==> RTOS Client CPU Capacity(百分比，占满单核100%)
Container cpuset <====> RTOS Client CPUS
```

### 2. 内存资源配置解析与映射

MicRun 从 `linux.resources.memory` 部分解析以下内存资源配置：

**内存资源映射关系：**
```
No in Container Memory resource                     <====> RTOS Client pedestal max memory
Container memory limit <====> RTOS Client memory limit
Container memory reservation < memory limit <====> RTOS Client memory min
```

**重要规范：**
- **Container 语境**：使用 container memory limit, minimal memory
- **libmica 语境**：使用 RTOS Client memory resource (旧代码，新代码有变动)
- `container.me.records` 记录了 libmica 语境下的资源量
- `container.me.memoryThreshold` 设计为单调递增的，因此它仅在新的 memory threshold 出现时才会正更新
- `pedestal.EssentialResource` 中并不记录 memoryThreshold，`memorymaxmb` 就是 OCI spec `mem.Limit`，`mem min` 就是 OCI spec `mem.Reservation`
- 因为该类型记录的是实际资源，因此仅在 `micaexecutor` 中记录 memory threshold，也保证了简单——只有 `memory threshold >= container memory limit`
- `memorymaxmb` 对应的是 mica create message 中的 memory

这是正确的资源配置方案，所有资源映射实现都应据此规范进行。

## 总结

### 1. MicRun 如何映射各种资源为 Xen 的资源

MicRun 将 Linux cgroup 资源配额转换为 Xen 虚拟化环境的资源分配：

**CPU 资源映射：**
- **quota/period** → **vCPU 数量(if vpu_pcpu_binding)**,  **cap 百分比**：通过 `quota/period` 计算所需 CPU 核心数，向上取整分配 vCPU，计算每个 vCPU 的 cap 百分比
- **cpu.shares** → **CPUWeight**：将 cgroup 的 shares (2-262144) 映射到 Xen 的 weight (1-65535)，默认比例 R=4
- **cpuset.cpus** → **CPUAffinity**：直接传递 CPU 亲和性设置


**核心约束规则：**
```
effective_capacity = min(
    (quota × 100) / period,   # quota/period 转换的百分比容量
    cpuset_size × 100          # cpuset 核心数 × 100%
)
```

### 2. cgroup 资源处理要点

**cpuset (Pinning) 与 quota/period 的关系：**
- **cpuset**：硬亲和性设置，容器进程只能在指定核心上运行
- **quota/period**：CFS 带宽控制，决定容器能消耗的 CPU 时间片
- **最终生效算力**：取两者的最小值，确保物理可实现性

**默认行为：**
- 无限制时：容器可使用所有可用 CPU 核心
- 只有 cpuset：容量 = cpuset_size × 100%
- 只有 quota/period：容量 = (quota × 100) / period

**权重转换：**

$$
W(S) = \max{1, \min{\frac{S}{R}, 65535}}; R=4
$$

其中 S 为 cgroup cpu.shares 值 (2-262144)

### 3. Kubernetes 资源要点

**K8s 到 cgroup 的转换：**
- **requests.cpu** → **cpu.shares**：通过 `MilliCPUToShares()` 转换
- **limits.cpu** → **cpu.quota/period**：通过 `MilliCPUToQuota()` 转换，period 默认为 100ms
- **limits.memory** → **memory.limit_in_bytes**：直接转换

**LimitRange 影响：**
- 自动填充默认的 requests/limits
- 确保 requests ≤ limits
- 防止单个容器独占资源

**资源热更新：**
- 通过 CRI UpdateContainerResources 支持
- 需要实时调整 Xen domain 配置

## 详细设计

## 1. cgroup 资源语义

我们保持对 oci spec 的语义一致性，而 runc 遵循 oci规范，因此让我们考察 runc 的 oci spec 资源语义


### 1.1 Runc 的默认行为与 VCPU 数量


VCPU number 是一个 POC (Presentaion Oriented Concept) 面向展示的一般不会有用的设计，主要是为了在RTOS内部显示确实更新资源信息。
因此，在 Linux Cgroup 语义中，是没有这个概念的:

- **默认行为（无限制）：** 如果 OCI Spec 中没有指定 CPU 限制，`runc` 不会去探测 VCPU 数量来人为设定限制。此时，容器内的进程可以使用宿主机（或 VM）上**所有**可用的 CPU 核心。
- **VCPU 的角色：** 对于 `runc` 而言，它看到的是 Linux 内核呈现的"逻辑核心"。无论是在物理机上还是在虚拟机（VM）里，内核识别到的核心数就是 `runc` 能调用的上限。
- **Shimv2 的角色：** `shimv2` (如 `io.containerd.runc.v2`) 的作用是将 containerd 的请求转化为 `runc` 命令。它本身不决定策略，只是忠实传递 K8s 计算好并写入 `config.json` (OCI Spec) 的参数。

### 1.2 Runc 对 Cpuset (Pinning) 和 Quota/Period 的处理

#### A. Cpuset (Pinning)
- **机制：** 当 OCI Spec 中指定了 `linux.resources.cpu.cpus` 时，`runc` 会将其写入 Cgroup 的 `cpuset.cpus` 文件。
- **效果：** 这是一个**硬亲和性（Hard Affinity）**设置。容器内的进程**只能**被调度器安排在指定的这些核心上运行，绝对不会漂移到其他核心。

#### B. Quota/Period (算力时间)
- **机制：** `runc` 会读取 OCI Spec 中的 `quota` 和 `period`，并写入 Cgroup：
    - **Cgroup v1:** 写入 `cpu.cfs_quota_us` 和 `cpu.cfs_period_us`
    - **Cgroup v2:** 写入 `cpu.max` (格式为 `quota period`)
- **效果：** 这是 CFS (Completely Fair Scheduler) 的带宽控制。它决定了在一段 `period` 时间内，该容器能消耗多少 CPU 时间片。

### 1.3 OCI Spec 中的字段与共存关系

#### Q: k8s 侧有 `cpulimit`, oci spec 中有 `cpu quota`, `period` 它们是否会冲突？

**不会，因为 Kubernetes 在源码中就将 cpulimit 转换为 cpu quota, perid**

- **Kubernetes 层：** 用户定义 `resources.limits.cpu` (俗称 CPU Limit)
- **转换层 (Kubelet/Containerd)：** K8s 会根据 Limit 值计算出 `quota`。通常 `period` 默认为 100ms (100000us)
    - 例如：Limit = 0.5 core → Quota = 50000, Period = 100000
- **OCI Spec 层 (Runc 看到的)：** `config.json` 文件中只有标准的 Linux Cgroup 参数字段：
    - `linux.resources.cpu.quota`
    - `linux.resources.cpu.period`
    - `linux.resources.cpu.shares` 
    - `linux.resources.cpu.cpus` (对应 cpuset)

**Runc 的对待方式：**
Runc 只认 OCI 标准字段。它看到 `quota` 和 `period` 就会去设置 CFS 带宽控制；看到 `cpus` 就会去设置 Cpuset。这两者可以在 OCI Spec 中**共存**，Runc 会同时设置它们。

### 1.4 最终生效的 CPU 算力时间由什么决定？

这是一个多层约束问题。实际生效的算力受 **CFS Bandwidth (Quota)** 和 **Cpuset (物理/逻辑核数)** 的**双重约束**。

**修正后的精确逻辑（配置上限）：**

```
理论最大算力 (Cores) = min(quota/period, Count(cpuset.cpus))
```

**如果未指定 cpuset，则：**
```
理论最大算力 (Cores) = min(quota/period, Host Total Cores)
```

### 1.5 核心问题：Cpuset 会影响实际的算力占用时间, 还是仅仅是设置了容器可运行的核区间？

**答案：非常会，而且是决定性的物理天花板。**

场景示例：

- **场景 A (无瓶颈)：**
    - Quota/Period = 2.0 (允许使用 2 核)
    - Cpuset = `0-3` (绑定到 4 个核)
    - **结果：** 容器可以跑满 2.0 个核的算力。限制因素是 **Quota**

- **场景 B (Cpuset 成为瓶颈)：**
    - Quota/Period = 2.0 (允许使用 2 核)
    - Cpuset = `0` (只绑定到核 0)
    - **结果：** 容器死活只能跑到 1.0 个核（核 0 的 100%）。虽然 CFS 给了它 200ms 的票，但它只有一个窗口（核）去兑换，物理上无法在 100ms 周期内通过单核消费 200ms。限制因素是 **Cpuset**

### 1.6 总结

1. **Runc 行为：** 忠实执行 OCI Spec，根据 Spec 设置 Cgroup 的 `cpu.cfs_quota/period` 和 `cpuset.cpus`
2. **OCI 字段：** `cpulimit` 不存在于 Spec 中，只有 `quota` 和 `period`。它们与 `cpuset` 可以共存
3. **最终算力决定因素：** 是 **CFS Quota 计算出的逻辑核数** 与 **Cpuset 绑定的物理核数** 之间的**最小值**
4. **Cpuset 影响：** `cpuset` 设定了硬性的物理执行单元数量，它会作为"物理天花板"截断 Quota 允许的逻辑算力

## 2. containerd 资源管理

### 2.1 ctr/crictl 控制选项

```shell
# --cpu-quota=50000  -> 允许在 100000 微秒 (period) 内使用 50000 微秒，即 0.5 CPU
# --cpu-period=100000
# --memory-limit=134217728 -> 128 * 1024 * 1024 = 134217728 字节
ctr run \
  --rm \
  --with-ns "pid:" \
  --with-ns "cgroup:" \
  --cpu-quota 50000 \
  --cpu-period 100000 \
  --memory-limit 134217728 \
  docker.io/polinux/stress:latest \
  stress-test-container \
  stress --cpu 2 --vm 1 --vm-bytes 256M
```

### 2.2 资源限制测试结果

**测试场景分析：**

1. **无限制场景：** 容器可以使用所有 CPU 核心
2. **cpuset 限制场景：** 容器被限制在指定核心上，即使 quota 允许更多算力
3. **quota 限制场景：** CFS 带宽控制生效，限制总 CPU 时间
4. **双重限制场景：** 取 cpuset 和 quota 的最小值

### 2.3 weight (share) 转换

`--cpu-shares` 对应 cgroup `cpu.shares`，在 Xen 中对应 `cpu.weight`

**范围映射：**
- cpushares: 2 - 262144, default=1024
- cpu_weight: 1 - 65535, default=256

转换公式：

$$
W(S) = \max{1, \min{\frac{S}{R}, 65535}}; R=4
$$

### 2.4 热更新 (动态扩缩)

通过 CRI 和 containerd 通信时（k8s集群等），容器资源可以热更新。

### 2.5 ocispec Linux Resource 默认值

`populateDefaultUnixSpec` 函数中，`Linux.Resources` 其他默认值都是 nil。参考 runc 的行为，当 quota、period 这一对无效时，开放全部算力给容器。对于 mica，此情况意味着 `CPUCapacity=0`，全权交给调度器。

## 3. Kubernetes 资源管理

### 3.1 基本资源定义

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hot-update-example
spec:
  containers:
  - name: app
    image: nginx
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "1000m"     # 可通过 kubectl patch 热更新, **但是这对K8s版本有要求**
        memory: "1Gi"    # 可通过 kubectl patch 热更新
```

kubelet 将 PodSpec 中的 `requested cpu`、`limited memory`、`limited cpu` 传给下游，`requested memory` 不会被填充，这是由 k8s 来监控的。因此 **micran 必须要能够准确反馈内存资源信息给 k8s**。

### 3.2 资源计算

```go
// MilliCPUToShares converts the milliCPU to CFS shares.
func MilliCPUToShares(milliCPU int64) uint64 {
    if milliCPU == 0 {
        // Docker converts zero milliCPU to unset, which maps to kernel default
        // for unset: 1024. Return 2 here to really match kernel default for
        // zero milliCPU.
        return MinShares
    }
    // Conceptually (milliCPU / milliCPUToCPU) * sharesPerCPU, but factored to improve rounding.
    shares := (milliCPU * SharesPerCPU) / MilliCPUToCPU
    if shares < MinShares {
        return MinShares
    }
    if shares > MaxShares {
        return MaxShares
    }
    return uint64(shares)
}
```

### 3.3 Limit Ranges

LimitRange 是命名空间级的策略，用来约束单个对象（Pod、Container、PVC）的资源分配。

```yaml
apiVersion: v1
kind: LimitRange
metadata:
  name: cpu-resource-constraint
spec:
  limits:
  - default: # this section defines default limits
      cpu: 500m
    defaultRequest: # this section defines default requests
      cpu: 500m
    max: # max and min define the limit range
      cpu: "1"
    min:
      cpu: 100m
    type: Container
```

**作用：**
- ResourceQuota：限制命名空间内的总资源消耗
- LimitRange：约束单个对象的资源分配，防止单个对象独占资源

## 4. CRI to Container Resource Mapping

### 4.1 LinuxContainerResources to OCI Runtime Spec

| CRI Field | OCI Runtime Spec Field | Mapping Strategy |
|-----------|------------------------|------------------|
| `cpu_period` | `s.Linux.Resources.CPU.Period` | Direct assignment (`uint64(resources.GetCpuPeriod())`) |
| `cpu_quota` | `s.Linux.Resources.CPU.Quota` | Direct assignment (`resources.GetCpuQuota()`) |
| `cpu_shares` | `s.Linux.Resources.CPU.Shares` | Direct assignment (`uint64(resources.GetCpuShares())`) |
| `memory_limit_in_bytes` | `s.Linux.Resources.Memory.Limit` | Direct assignment (`resources.GetMemoryLimitInBytes()`) |
| `memory_swap_limit_in_bytes` | `s.Linux.Resources.Memory.Swap` | Direct assignment (`resources.GetMemorySwapLimitInBytes()`) |
| `cpuset_cpus` | `s.Linux.Resources.CPU.Cpus` | Direct string assignment (`resources.GetCpusetCpus()`) |
| `cpuset_mems` | `s.Linux.Resources.CPU.Mems` | Direct string assignment (`resources.GetCpusetMems()`) |
| `hugepage_limits` | `s.Linux.Resources.HugepageLimits` | Convert to `runtimespec.LinuxHugepageLimit` struct array |
| `unified` | `s.Linux.Resources.Unified` | Direct copy of map (`resources.GetUnified()`) |
| `oom_score_adj` | `s.Process.OOMScoreAdj` | Handled separately via `WithOOMScoreAdj` |

### 4.2 Kubernetes Resources Proto Definition

```proto
// LinuxContainerResources specifies Linux specific configuration for
// resources.
message LinuxContainerResources {
    // CPU CFS (Completely Fair Scheduler) period. Default: 0 (not specified).
    int64 cpu_period = 1;
    // CPU CFS (Completely Fair Scheduler) quota. Default: 0 (not specified).
    int64 cpu_quota = 2;
    // CPU shares (relative weight vs. other containers). Default: 0 (not specified).
    int64 cpu_shares = 3;
    // Memory limit in bytes. Default: 0 (not specified).
    int64 memory_limit_in_bytes = 4;
    // OOMScoreAdj adjusts the oom-killer score. Default: 0 (not specified).
    int64 oom_score_adj = 5;
    // CpusetCpus constrains the allowed set of logical CPUs. Default: "" (not specified).
    string cpuset_cpus = 6;
    // CpusetMems constrains the allowed set of memory nodes. Default: "" (not specified).
    string cpuset_mems = 7;
    // List of HugepageLimits to limit the HugeTLB usage of container per page size. Default: nil (not specified).
    repeated HugepageLimit hugepage_limits = 8;
    // Unified resources for cgroup v2. Default: nil (not specified).
    // Each key/value in the map refers to the cgroup v2.
    // e.g. "memory.max": "6937202688" or "io.weight": "default 100".
    map<string, string> unified = 9;
    // Memory swap limit in bytes. Default 0 (not specified).
    int64 memory_swap_limit_in_bytes = 10;
}
```

## 5. Containerd Cgroup 资源配额转换为 Pedestal(Hypervisor) 资源抽象

### 5.1 资源过滤策略

**目前不支持的：**
- BlockIO
- Devices
- Unified cgroup v2 metrics mapping

**不会支持的：**
- Pids
- Rdma
- HugepageLimits  

**仅保留资源：**
仅保留 `CPU`、`Memory`，并且我们仅关注：
- **CPU**: Period, Quota, Shares, Cpus, Mems
- **Memory**: Limit, Swap

### 5.2 LinuxCPU 结构

```go
type LinuxCPU struct {
    // CPU shares (relative weight (ratio) vs. other cgroups with cpu shares).
    Shares *uint64 `json:"shares,omitempty"`
    // CPU hardcap limit (in usecs). Allowed cpu time in a given period.
    Quota *int64 `json:"quota,omitempty"`
    // CPU hardcap burst limit (in usecs). Allowed accumulated cpu time additionally for burst in a
    // given period.
    Burst *uint64 `json:"burst,omitempty"`
    // CPU period to be used for hardcapping (in usecs).
    Period *uint64 `json:"period,omitempty"`
    // How much time realtime scheduling may use (in usecs).
    RealtimeRuntime *int64 `json:"realtimeRuntime,omitempty"`
    // CPU period to be used for realtime scheduling (in usecs).
    RealtimePeriod *uint64 `json:"realtimePeriod,omitempty"`
    // CPUs to use within the cpuset. Default is to use any CPU available.
    Cpus string `json:"cpus,omitempty"`
    // List of memory nodes in the cpuset. Default is to use any available memory node.
    Mems string `json:"mems,omitempty"`
    // cgroups are configured with minimum weight, 0: default behavior, 1: SCHED_IDLE.
    Idle *int64 `json:"idle,omitempty"`
}
```

### 5.3 LinuxMemory 结构

```go
type LinuxMemory struct {
    // Memory limit (in bytes).
    Limit *int64 `json:"limit,omitempty"`
    // Memory reservation or soft_limit (in bytes).
    Reservation *int64 `json:"reservation,omitempty"`
    // Total memory limit (memory + swap).
    Swap *int64 `json:"swap,omitempty"`
    // Kernel memory limit (in bytes).
    Kernel *int64 `json:"kernel,omitempty"`
    // Kernel memory limit for tcp (in bytes)
    KernelTCP *int64 `json:"kernelTCP,omitempty"`
    // How aggressive the kernel will swap memory pages.
    Swappiness *uint64 `json:"swappiness,omitempty"`
    // DisableOOMKiller disables the OOM killer for out of memory conditions
    DisableOOMKiller *bool `json:"disableOOMKiller,omitempty"`
    // Enables hierarchical memory accounting
    UseHierarchy *bool `json:"useHierarchy,omitempty"`
    // CheckBeforeUpdate enables checking if a new memory limit is lower
    // than the current usage during update, and if so, rejecting the new
    // limit.
    CheckBeforeUpdate *bool `json:"checkBeforeUpdate,omitempty"`
}
```

## 6. 不同 Pedestal 的配额映射策略

### 6.1 Baremetal

由于实现的抽象不太好，你看到的这个版本已经去掉了 `baremetal.go`
> baremetal 拿来容器化的唯一价值就是从镜像仓直接拉取容器来运行

- CPU 核心直接分配
- 内存通过 cgroup 限制

### 6.2 Xen
- vcpus 默认值为 1，maxcpus 默认 = 0，此时会转为 `maxvcpus=vcpus`
- vcpus = 0 会被设置为 1
- 每个 Pod Sandbox 所用的 CPU 资源对应一个 Xen CPU pool
- 分配的 memory 还有一个 gap，Xen-PV 的存在使得 OS 的保留内存体积更大

## 7. 完整映射表格

| LinuxContainerResources 字段 | 默认值 | Xen Pedestal 映射策略 | Baremetal Pedestal 映射策略 | 处理优先级 |
|------------------------------|--------|---------------------|---------------------------|----------|
| **CPU.** |
| `cpu_period` | 0 | 与`cpu_quota`结合计算vCPU数量和cap | 与`cpu_quota`结合计算CPU核心分配 | 高 |
| `cpu_quota` | 0 | 转换为Xen vCPU数量+cap百分比 | 转换为整数CPU核心数 | 高 |
| `cpu_shares` | 0 | 映射为Xen调度weight(1-65535) | 映射为Linux调度nice值或RT优先级 | 中 |
| `cpuset_cpus` | "" | CPUAffinity | 直接绑定物理核 | 中 |
| `cpuset_mems` | "" | 不确定 | 不确定 | 低 |
| **Memory.** |
| `memory_limit_in_bytes` | 0 | 转换为Xen domain内存分配(MB) | 转换为物理内存分配限制 | 高 |
| `memory_swap_limit_in_bytes` | 0 | **忽略** | **忽略** | 低 |
| **其他资源** |
| `oom_score_adj` | 0 | **部分忽略** (RTOS无OOM概念) | **忽略** (RTOS无OOM概念) | 忽略 |
| `hugepage_limits` | nil | **暂时忽略** (未来可扩展) | **暂时忽略** | 忽略 |
| `unified` (cgroup v2) | nil | **完全忽略** (不适用) | **完全忽略** (不适用) | 忽略 |

## 9. LinuxContainerResources 到 Micran 资源映射完整对照表

### 9.1 CPU 资源转换

#### A. Xen Pedestal

```go
type XenCPUMapping struct {
    VCPUs       int     // 虚拟CPU数量
    CPUWeight   int     // 调度权重 (1-65535, default=256)
    CPUCap      int     // CPU使用率上限 (每vCPU的百分比)
    CPUS        string  // cpuset cpus
    CPUAffinity []int   // CPU亲和性
}

func convertCPUResourcesXen(res *LinuxContainerResources, availableCPUs int) XenCPUMapping {
    mapping := XenCPUMapping{
        VCPUs:     1,    // 默认1个vCPU
        CPUWeight: 256,  // Xen默认权重
        CPUCap:    0,    // 0表示不限制
    }
    
    if res.CpuQuota > 0 && res.CpuPeriod > 0 {
        // 计算需要的CPU核心数
        requestedCores := float64(res.CpuQuota) / float64(res.CpuPeriod)
        
        // 分配vCPU数量 (向上取整)
        mapping.VCPUs = int(math.Ceil(requestedCores))
        if mapping.VCPUs > availableCPUs {
            mapping.VCPUs = availableCPUs
        }
        
        // 计算cap: 如果request 1.5 cores但分配2 vCPU，则每个vCPU cap=75%
        mapping.CPUCap = int((requestedCores / float64(mapping.VCPUs)) * 100)
        if mapping.CPUCap > 100 {
            mapping.CPUCap = 100
        }
    }
    
    // CPU Shares 转换为权重
    if res.CpuShares > 0 {
        // cgroup默认1024 -> Xen默认256的比例转换
        mapping.CPUWeight = int((res.CpuShares * 256) / 1024)
        if mapping.CPUWeight < 1 {
            mapping.CPUWeight = 1
        } else if mapping.CPUWeight > 65535 {
            mapping.CPUWeight = 65535
        }
    }
    
    // CPU Set 转换
    if res.CpusetCpus != "" {
        mapping.CPUAffinity = parseCPUSetForXen(res.CpusetCpus, availableCPUs)
    }
    
    return mapping
}
```

#### B. Baremetal Pedestal

```go
type BaremetalCPUMapping struct {
    // 目标配置
    CPUCores        []int  // 分配的物理CPU核心列表
    SchedulerPolicy int    // 调度策略 (SCHED_NORMAL, SCHED_RR, SCHED_FIFO)
    Priority        int    // RT优先级 (1-99) 或 nice值 (-20到19)
    CPUAffinity     []int  // CPU亲和性掩码
}

func convertCPUResourcesBaremetal(res *LinuxContainerResources, totalCPUs int) BaremetalCPUMapping {
    mapping := BaremetalCPUMapping{
        CPUCores:        []int{0}, // 默认分配CPU 0
        SchedulerPolicy: SCHED_NORMAL,
        Priority:        0,
    }
    
    // CPU Period + Quota 转换为整数核心
    if res.CpuQuota > 0 && res.CpuPeriod > 0 {
        requestedCores := float64(res.CpuQuota) / float64(res.CpuPeriod)
        
        // 只能分配整数核心
        coreCount := int(math.Ceil(requestedCores))
        if coreCount > totalCPUs {
            coreCount = totalCPUs
        }
        
        // 分配连续的CPU核心
        mapping.CPUCores = make([]int, coreCount)
        for i := 0; i < coreCount; i++ {
            mapping.CPUCores[i] = i
        }
        
        // 如果请求的是小数核心，使用实时调度+时间片分割
        if requestedCores < float64(coreCount) {
            mapping.SchedulerPolicy = SCHED_RR  // 轮转调度
            // 计算时间片比例
            ratio := requestedCores / float64(coreCount)
            mapping.Priority = int(ratio * 50) // 映射到1-50的优先级范围
        }
    }
    
    // CPU Shares 转换为调度优先级
    if res.CpuShares > 0 {
        // 1024为默认值，映射到nice值-5到15的范围
        niceValue := int(((res.CpuShares - 1024) * 20) / 1024)
        if niceValue < -20 {
            niceValue = -20
        } else if niceValue > 19 {
            niceValue = 19
        }
        mapping.Priority = niceValue
    }
    
    // CPU Set 直接映射
    if res.CpusetCpus != "" {
        mapping.CPUAffinity = parseCPUSet(res.CpusetCpus)
        // 更新实际分配的核心列表
        mapping.CPUCores = mapping.CPUAffinity
    }
    
    return mapping
}
```

### 9.2 内存资源转换

#### A. Xen Pedestal

```go
type XenMemoryMapping struct {
    Memory    int64  // 静态内存分配 (MB)
    MaxMemory int64  // 最大内存限制 (MB)  
}

func convertMemoryResourcesXen(res *LinuxContainerResources, availableMemoryMB int64) XenMemoryMapping {
    mapping := XenMemoryMapping{}
    
    if res.MemoryLimitInBytes > 0 {
        memoryMB := res.MemoryLimitInBytes / (1024 * 1024)
        
        // 确保不超过可用内存
        if memoryMB > availableMemoryMB {
            memoryMB = availableMemoryMB
        }
        
        // Xen通常使用静态内存分配
        mapping.Memory = memoryMB
        mapping.MaxMemory = memoryMB
    } else {
        // 默认分配策略
        defaultMem := availableMemoryMB / 4  // 25%的可用内存
        if defaultMem < 128 {
            defaultMem = 128  // 最小128MB
        }
        mapping.Memory = defaultMem
        mapping.MaxMemory = defaultMem
    }
    
    // memory_swap_limit_in_bytes 被忽略
    return mapping
}
```

#### B. Baremetal Pedestal

仅供参考，如果未来计划实现 baremetal 的完整容器化的话，

```go
type BaremetalMemoryMapping struct {
    MemoryLimitBytes int64  // 内存限制 (字节)
    SwapLimitBytes   int64  // swap限制 (字节)
    UseMemCgroup     bool   // 是否使用memory cgroup
}

func convertMemoryResourcesBaremetal(res *LinuxContainerResources) BaremetalMemoryMapping {
    mapping := BaremetalMemoryMapping{
        UseMemCgroup: true,  // 可以使用Linux cgroup进行内存限制
    }
    
    if res.MemoryLimitInBytes > 0 {
        mapping.MemoryLimitBytes = res.MemoryLimitInBytes
    }
    
    if res.MemorySwapLimitInBytes > 0 {
        mapping.SwapLimitBytes = res.MemorySwapLimitInBytes
    }
    
    return mapping
}
```

## 10. 资源管理器实现

## 11. Micran 资源映射实现细节

### 11.1 核心映射关系

| Linux cgroup 参数 | Micran 映射目标 | 转换规则 |
|------------------|----------------|----------|
| `cpu.shares` | `CPUWeight` | Xen: `W(S) = max(1, min(⌊S/R⌋, 65535)); R=4`<br>其他: 直接使用 `cpu.shares` 值 |
| `cpu.quota`/`cpu.period` | `CpuCpacity` (百分比容量) | `capacity = (quota × 100) / period` |
| `cpuset.cpus` | `ClientCpuSet` + `Vcpu` | 直接传递CPU亲和性，从cpuset计算vCPU数量 |

### 11.2 cpuset 对 CPU 容量的影响

Micran 遵循与 `runc` 相同的语义：**最终生效的 CPU 算力受 cpuset 和 quota/period 的双重约束**。

计算公式：
```
effective_capacity = min(
    (quota × 100) / period,   # quota/period 转换的百分比容量
    cpuset_size × 100          # cpuset 核心数 × 100%
)
```

### 11.3 代码实现示例

```go
func linuxResourceToEssential(spec *specs.Spec, convertShares bool) *EssentialResource {
    // 1. 解析 cpuset 获取 vcpuNum
    cpus, cpuSetVCpuNum := validateCPUSet(cpu.Cpus)
    if cpus != "" && cpuSetVCpuNum > 0 {
        res.ClientCpuSet = cpus
        vcpuNum = cpuSetVCpuNum
    }

    // 2. 处理 quota/period，应用 cpuset 限制
    if cpu.Quota != nil && *cpu.Quota > 0 && cpu.Period != nil && *cpu.Period > 0 {
        rawCapacity := uint32((*cpu.Quota * 100) / int64(*cpu.Period))
        if rawCapacity > 0 {
            if vcpuNum > 0 {
                maxByCpuset := vcpuNum * 100
                if rawCapacity > maxByCpuset {
                    rawCapacity = maxByCpuset
                }
            }
            *res.CpuCpacity = rawCapacity
        }
    } else if vcpuNum > 0 {
        // 无有效 quota/period，但有 cpuset
        *res.CpuCpacity = vcpuNum * 100
    }

    // 3. 处理 shares 转换
    if cpu.Shares != nil && *cpu.Shares > 0 {
        if convertShares {
            weight := ShareToWeight(*cpu.Shares)
            res.CPUWeight = &weight
        } else {
            share := uint32(*cpu.Shares)
            res.CPUWeight = &share
        }
    }
}
```

### 11.4 测试验证

测试用例覆盖场景：
1. **cpuset 限制更严格**：quota=200% (2.0核)，cpuset="0" (1核) → capacity=100%
2. **quota 限制更严格**：quota=50% (0.5核)，cpuset="0-3" (4核) → capacity=50%
3. **只有 cpuset**：无 quota/period，cpuset="0-1" (2核) → capacity=200%
4. **只有 quota**：quota=150% (1.5核)，无 cpuset → capacity=150%

### 11.5 与 runc 的语义对齐

通过上述实现，Micran 实现了与 `runc` 相同的资源限制语义：
- **cpuset**：作为物理天花板，限制容器可用的 CPU 核心数
- **quota/period**：作为逻辑上限，限制容器可使用的 CPU 时间片
- **最终限制**：取两者的最小值，确保配置的物理可实现性

这种设计保证了从 Kubernetes/containerd 传递的资源限制在 RTOS 环境中得到正确、安全的映射。

## 12. 实验性示例

### 12.1 测试命令示例

```bash
# 测试无限制场景
nerdctl run --rm \
    cpu-workload --cpu 8 --cpu-method matrixprod -t 30s

# 测试cpuset限制场景
nerdctl run --rm --cpuset-cpus="0-3" \
    cpu-workload --cpu 8 --cpu-method matrixprod -t 30s

# 测试quota限制场景
nerdctl run --rm --cpus="4.0" \
    cpu-workload --cpu 8 --cpu-method matrixprod -t 30s

# 测试双重限制场景
nerdctl run -d --cpus="4.0" --cpuset-cpus="2,4-10" \
    cpu-workload --cpu 6 --cpu-method matrixprod -t 30s

# 测试cpuset成为瓶颈的场景
nerdctl run -d --cpus="4.0" --cpuset-cpus="2" \
    cpu-workload --cpu 6 --cpu-method matrixprod -t 30s
```

### 12.2 测试结果分析

#### 场景1：无限制
- **配置**：无 CPU 限制
- **结果**：容器可以使用所有 CPU 核心，每个 stress-ng 进程接近 100% CPU 使用率
- **观察**：8个进程分布在所有可用核心上

#### 场景2：cpuset 限制
- **配置**：`--cpuset-cpus="0-3"`（限制在4个核心）
- **结果**：8个进程被限制在4个核心上，每个核心运行2个进程
- **观察**：每个进程 CPU 使用率约 50%，总 CPU 使用率约 400%

#### 场景3：quota 限制
- **配置**：`--cpus="4.0"`（4个CPU的配额）
- **结果**：8个进程共享4个CPU的配额
- **观察**：每个进程 CPU 使用率约 50%，总 CPU 使用率约 400%

#### 场景4：双重限制
- **配置**：`--cpus="4.0"` + `--cpuset-cpus="2,4-10"`（8个核心）
- **结果**：quota 限制为4个CPU，但可以在8个核心上运行
- **观察**：每个进程 CPU 使用率约 67%，总 CPU 使用率约 400%

#### 场景5：cpuset 成为瓶颈
- **配置**：`--cpus="4.0"` + `--cpuset-cpus="2"`（1个核心）
- **结果**：quota 允许4个CPU，但物理上只有1个核心
- **观察**：每个进程 CPU 使用率约 16.6%，总 CPU 使用率约 100%

### 12.3 MicRun 资源映射测试

#### 测试用例1：Kubernetes Pod 资源映射

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mica-test-pod
spec:
  containers:
  - name: mica-client
    image: localhost:5000/mica-zephyr-app:xen-0.1
    resources:
      requests:
        cpu: "500m"
        memory: "256Mi"
      limits:
        cpu: "1000m"
        memory: "512Mi"
```

**预期映射结果：**
- **CPU**：quota=100000, period=100000 → 1.0 CPU
- **Memory**：512Mi → 512MB Xen domain 内存
- **权重**：cpu.shares=512 → Xen weight=128

#### 测试用例2：复杂 CPU 配置

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: complex-cpu-test
spec:
  containers:
  - name: rtos-client
    image: localhost:5000/mica-rtos-app:latest
    resources:
      requests:
        cpu: "1500m"
      limits:
        cpu: "2500m"
```

**预期映射结果：**
- **Xen**：requestedCores=1.5 → vCPUs=2, cap=75%
- **Baremetal**：requestedCores=2.5 → 分配3个核心，时间片比例83%

#### 测试用例3：cpuset 配置

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: cpuset-test
  annotations:
    io.kubernetes.cri.cpuset-cpus: "2,4,6"
spec:
  containers:
  - name: pinned-client
    image: localhost:5000/mica-rtos-app:latest
    resources:
      limits:
        cpu: "2000m"
```

**预期映射结果：**
- **cpuset**：CPU亲和性设置为核心2、4、6
- **容量计算**：quota=200% (2.0核)，cpuset_size=3核 → effective_capacity=200%
- **Xen**：分配2个vCPU，每个cap=100%

### 12.5 调试与监控

#### 监控指标
1. **Xen 层面**：
   - `xl list`：查看 domain 状态和资源使用
   - `xl schecd-credit2`
   - `xentop`：实时监控 Xen 资源使用, 目前 oee 没有

2. **MicRun 层面**：
   - `/tmp/micrun/runtime.log`：运行时日志
   - 共享内存资源计数器：跟踪已分配资源
   - 资源管理器状态：可用资源统计

3. **容器层面**：
   - `ctr containers list`：容器状态
   - `ctr tasks list`：任务状态
   - `ctr tasks metrics`
   - `journalctl -xeu containerd`：containerd 日志
