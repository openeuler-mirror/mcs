# MicRun 注解参考手册

## 概述

本文档列出 MicRun 支持的所有注解（annotations），这些注解用于配置 RTOS 容器的行为。注解通过 Pod/容器的 `metadata.annotations` 字段设置，**不支持通过环境变量配置**。

## 注解前缀

| 前缀 | 说明 |
|------|------|
| `org.openeuler.micrun.` | MicRun 通用注解前缀 |
| `org.openeuler.micrun.ped.` | Hypervisor (Pedestal) 相关配置 |
| `org.openeuler.micrun.runtime.` | 运行时相关配置 |
| `org.openeuler.micrun.container.` | 容器相关配置 |

## 容器配置注解

### org.openeuler.micrun.container.os

指定 RTOS 类型。

| 值 | 说明 |
|----|------|
| `zephyr` | Zephyr RTOS |
| `uniproton` | UniProton RTOS (默认) |

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.os: "zephyr"
```

---

### org.openeuler.micrun.container.firmware_path

指定 RTOS 固件文件的路径（相对于容器 rootfs）。

| 属性 | 值 |
|------|-----|
| 类型 | 字符串 |
| 默认值 | `firmware.elf` |
| 解析位置 | `<bundle>/rootfs/<firmware_path>` |

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.firmware_path: "images/zephyr.elf"
```

**路径解析规则**：
1. 如果使用绝对路径（以 `/` 开头），会去掉前缀 `/`，然后相对于 rootfs 解析
2. 如果使用相对路径，直接相对于 rootfs 解析
3. 如果注解不存在，会尝试查找 `*.elf` 文件或使用默认值 `firmware.elf`

---

### org.openeuler.micrun.container.firmware_hash

指定 RTOS 固件的 SHA-256 哈希值，用于验证固件完整性。

**说明**：
- 支持 64 位十六进制摘要，或带 `sha256:` 前缀的摘要
- **校验时机是 task start（shim 创建任务）**，不是 `ctr container create`：
  `create` 是 containerd 的纯元数据操作，不会触达 shim，因此任何注解校验
  （本键、非法格式等）都发生在 start 阶段并使 start 失败（错误形如
  `failed to create shim task: firmware sha256 mismatch ...` / `invalid firmware sha256 length`）
- 摘要不匹配或格式非法时，容器不会启动，固件也不会被加载——这是校验可生效的最早时机

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.firmware_hash: "a1b2c3d4e5f6..."
```

---

### org.openeuler.micrun.container.min_memory_mb

指定 RTOS 容器的初始内存分配（单位：MiB）。

| 属性 | 值 |
|------|-----|
| 类型 | 整数 |
| 单位 | MiB |
| 默认值 | 32 |

**说明**：
- 这是容器的**预留内存**（memory reservation）
- 实际分配的内存不会低于此值
- 与 OCI spec 的 `memory.reservation` 同时设置时，**此注解优先**（注解在
  OCI 资源解析之后应用，遵循"注解优先级最高"的统一规则）
- 可通过运行时配置 `container_minmem` 覆盖默认值

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.min_memory_mb: "32"
```

---

### org.openeuler.micrun.container.max_vcpu_num

覆盖容器的最大 vCPU 数量。

| 属性 | 值 |
|------|-----|
| 类型 | 整数 |
| 默认值 | 从运行时配置读取 |

**语义说明**：该注解设置的是 vCPU **上限**（对应 Xen 域配置的
`maxvcpus`）。`xl list` 的 `VCPUs` 列显示的是**当前在线** vCPU 数
（域启动时初始为 1，容器内按需在线到上限），两者含义不同：注解
`max_vcpu_num=2` 生效后，`xl list -l` 的域配置中 `b_info.max_vcpus`
为 2，而 `VCPUs` 列显示 1 属正常现象，不代表注解未生效。

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.max_vcpu_num: "4"
```

---

### org.openeuler.micrun.container.auto_close

控制"最后一个 stdin 写端消失"后是否按超时回收容器。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `true` |
| 优先级 | `auto_close_timeout` > `auto_close` > 默认 |

**行为**：
- `true`：最后一个 stdin 写端消失（detach 键序送达 / stdin EOF / start 后无人 attach）后，
  计时开始，超时（默认 30s）内无人重新 attach 即回收容器
- `false`：禁用自动回收（除非设置了非零 `auto_close_timeout`），容器保持运行等待重连
- **无效值（非布尔字符串）不报错，静默按默认值 `true` 处理**

**重要说明**：
- ⚠️ **不要使用数字值**（如 `"60"`）。此注解是布尔值，数字值会被忽略。
- 如需设置超时时长，请使用 `auto_close_timeout` 注解。
- 计时起点是**最后一个 stdin 写端消失**，attach 期间挂起（详见 `auto_close_timeout` 节）
- `false` 只对"仅 stdin 结束 / detach"类离开方式有效，**不能阻止 attach 客户端进程死亡**
  （关终端 / 断 SSH / Ctrl+C 杀死 attach 进程）导致的秒级停止

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.auto_close: "true"
```

---

### org.openeuler.micrun.container.auto_close_timeout

> 已弃用别名：`org.openeuler.micrun.container.auto_disconnect_timeout` **不再生效**——检测到该键只打 Error 级迁移告警，**其值不会被读取**（超时按 `auto_close_timeout` 或默认 30s 计算）。迁移时必须改用 `auto_close_timeout`，否则旧配置的超时语义（如 `0` 表示禁用）会被静默替换为默认行为。

指定自动关闭的超时时间。

| 属性 | 值 |
|------|-----|
| 类型 | 持续时间字符串或整数秒 |
| 默认值 | `30s` |
| 优先级 | **最高**（覆盖 `auto_close`） |
| 适用范围 | **所有 IO 模式** |

**格式**：
- 持续时间字符串：`"60s"`, `"5m"`, `"1h"`
- 整数秒：`"60"` (等价于 60s)
- 特殊值：`"0"` 或 `"0s"` = 禁用（无超时，无限连接）

**超时机制说明**：
- 30 秒默认值计时的起点是**最后一个 stdin 写端消失**（detach、stdin EOF、无人 attach），
  不是容器启动时刻；attach 客户端保持连接期间计时挂起，**永远不会触发**
- 这是为防止测试/调试会话资源泄漏而设计的保护机制
- 如需长期运行服务，请显式设置 `auto_close=false` 或 `auto_close_timeout=0`
- 注意：`auto_close=false` 不能阻止 attach 客户端**进程死亡**（关终端/断 SSH/Ctrl+C 杀死
  attach 进程）导致的容器停止。完整语义见
  [容器生命周期与 IO 会话语义](../user/lifecycle-semantics.md)（权威口径）

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.auto_close_timeout: "60s"
```

**优先级说明**：
1. 如果设置了 `auto_close_timeout`，则无论 `auto_close` 为何值，都使用此超时
2. 如果 `auto_close_timeout` 为 `"0"`，则禁用自动关闭
3. 否则，使用 `auto_close` 的值

## Hypervisor 配置注解

### org.openeuler.micrun.ped.pedestal

指定 Hypervisor (Pedestal) 类型。

| 属性 | 值 |
|------|-----|
| 类型 | 字符串 |
| 默认值 | 主机 Hypervisor 类型 |
| 可选值 | `xen`, `baremetal` |

**说明**：
- 如果指定的类型与主机不匹配，容器创建会失败
- 通常不需要设置，自动使用主机 Hypervisor
- Baremetal 不会自动探测；需要在宿主环境显式设置 `MICRUN_ENABLE_BAREMETAL=1`

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.ped.pedestal: "xen"
```

---

### org.openeuler.micrun.ped.conf

指定 Hypervisor 配置文件的路径（相对于容器 rootfs）。

| 属性 | 值 |
|------|-----|
| 类型 | 字符串 |
| 默认值 | `image.bin` (Xen) |

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.ped.conf: "images/xen-image.bin"
```

---

### org.openeuler.micrun.ped.compatibility

⚠️ **已弃用**，请使用 `org.openeuler.micrun.compatibility.*` 前缀。

兼容性选项配置（格式：`^versionX`）。

## 运行时配置注解

### org.openeuler.micrun.runtime.pause

Pause 镜像名（配置占位）。注解会被读取进 `RuntimeConfig.PauseImage`，当前无下游消费路径，
后续 pause 容器支持使用。

| 属性 | 值 |
|--------|------|
| 类型 | 字符串 |
| 默认值 | - |
| 作用域 | Pod / 容器注解 |

---

### org.openeuler.micrun.runtime.max_container_cpus

限制单个容器可用的最大 CPU 数（覆盖配置文件中的同名默认值）。

| 属性 | 值 |
|--------|------|
| 类型 | 整数 |
| 默认值 | 继承配置文件 `max_container_vcpu` |
| 作用域 | Pod / 容器注解 |

---

### org.openeuler.micrun.runtime.max_container_memory

**状态：尚未接线。** 该键（与配置文件 `container_maxmem`）解析后写入运行时配置，但当前代码没有任何消费点——**不会对容器内存形成上限约束**。实际生效的内存校验只有"容器内存限制不得超过宿主总内存"（`ValidateResourceLimits`）。

| 属性 | 值 |
|--------|------|
| 类型 | 整数 |
| 默认值 | 继承配置文件 `container_maxmem` |
| 作用域 | Pod / 容器注解 |

---

### org.openeuler.micrun.runtime.disable_new_netns

禁用创建新的网络命名空间。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |
| 状态 | **尚未接线**：常量与文档已定义，Create 路径仍无条件调用 `setupNetNS`；设置该注解目前不会生效 |

**示例**（目标行为，当前未实现）：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.disable_new_netns: "true"   # 读取但暂不生效
```

---

### org.openeuler.micrun.runtime.pipe_size

指定 IO 管道的大小（字节）。

| 属性 | 值 |
|------|-----|
| 类型 | 整数 |
| 单位 | 字节 |
| 默认值 | 系统默认 |
| 状态 | **尚未接线**：注解常量存在，但 runtime/IO 路径未读取或应用该值 |

**示例**（目标行为，当前未实现）：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.pipe_size: "65536"
```

---

### org.openeuler.micrun.runtime.debug

启用调试模式。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.debug: "true"
```

---

### org.openeuler.micrun.runtime.experimental

启用实验性功能（预留开关）。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |
| 状态 | **尚未接线**：micrun 当前不读取此注解，预留供实验特性开关使用 |

**示例**（目标行为，当前未实现）：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.experimental: "true"
```

---

### org.openeuler.micrun.runtime.exclusive_dom0_cpu

控制是否保持 Dom0 CPU 独占（Xen 专用）。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |
| 仅适用于 | Xen Hypervisor |

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.exclusive_dom0_cpu: "true"
```

---

### org.openeuler.micrun.runtime.vcpu_pcpu_binding

> 本注解是 [`enable_vcpus_pinning`](#orgopeneulermicrunruntimeenable_vcpus_pinning) 的兼容别名，二者设置同一开关，同时设置时无需重复。

启用 VCPU 到 PCPU 的绑定。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |

**说明**：
- 启用后，容器的 vCPU 会绑定到指定的物理 CPU
- 需要配合 OCI spec 的 `cpuset.cpus` 使用

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.vcpu_pcpu_binding: "true"
```

## Sandbox 级别注解

以下注解用于配置整个 Sandbox：

### org.openeuler.micrun.runtime.enable_vcpus_pinning

启用 Sandbox 级别的 VCPU 亲和性设置。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |
| 级别 | Sandbox |

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.enable_vcpus_pinning: "true"
```

---

### org.openeuler.micrun.runtime.static_resource

启用静态资源管理模式。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |
| 级别 | Sandbox |

**说明**：
- 静态模式下，资源更新（`UpdateContainer` API）将被忽略
- 适用于资源固定的场景

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.static_resource: "true"
```

---

### org.openeuler.micrun.runtime.hugepage_enable

> 仅适用于 Xen 底座（代码中标注 only for Xen；默认 false）。

启用 HugePage 支持。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |
| 级别 | Sandbox |

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.hugepage_enable: "true"
```

## 内部注解

以下注解由 MicRun 内部使用，通常不需要手动设置。

| 注解 | 说明 |
|------|------|
| `org.openeuler.micrun.pkg.oci.bundle_path` | OCI bundle 路径（读取 OCI spec） |
| `org.openeuler.micrun.pkg.oci.container_type` | 容器类型 |
| `org.openeuler.micrun.config_path` | Sandbox 配置路径（注解来源的配置文件会忽略 `state_dir`/`firmware_path` 等宿主机路径类键） |

## Kubernetes 使用示例

### RuntimeClass 配置

```yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
```

### Pod 配置

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: rtos-pod
  annotations:
    # 容器配置
    org.openeuler.micrun.container.os: "zephyr"
    org.openeuler.micrun.container.firmware_path: "images/zephyr.elf"
    org.openeuler.micrun.container.min_memory_mb: "32"
    org.openeuler.micrun.container.auto_close_timeout: "60s"

    # Hypervisor 配置
    org.openeuler.micrun.ped.pedestal: "xen"

    # 运行时配置
    org.openeuler.micrun.runtime.vcpu_pcpu_binding: "true"

    # Sandbox 配置
    org.openeuler.micrun.runtime.enable_vcpus_pinning: "true"
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos-app
    image: localhost:5000/zephyr-app:latest
    resources:
      limits:
        memory: "64Mi"
        cpu: "2"
      requests:
        memory: "32Mi"
```

### 使用 ctr

```bash
ctr run --runtime io.containerd.mica.v2 \
  --annotation org.openeuler.micrun.container.os=zephyr \
  --annotation org.openeuler.micrun.container.firmware_path=images/zephyr.elf \
  --annotation org.openeuler.micrun.container.auto_close=false \
  localhost:5000/zephyr-app:latest zephyr-container
```

### 使用 nerdctl

```bash
nerdctl run --runtime io.containerd.mica.v2 \
  -l org.openeuler.micrun.container.os=zephyr \
  -l org.openeuler.micrun.container.firmware_path=images/zephyr.elf \
  -l org.openeuler.micrun.container.auto_close_timeout=0 \
  localhost:5000/zephyr-app:latest
```

## 注意事项

1. **注解 vs 环境变量**：MicRun 只通过 Pod/容器的 `metadata.annotations` 读取配置，不支持通过环境变量配置。

2. **优先级**：注解配置 > 运行时配置文件 > 默认值

3. **类型转换**：布尔值使用 `"true"`/`"false"` 字符串，整数使用数字字符串

4. **路径解析**：固件和配置文件路径相对于容器 rootfs (`<bundle>/rootfs/`)

5. **资源限制**：OCI spec 中的资源限制（`resources.limits`）与注解配置会互相影响，详见 [资源映射文档](./resources.md)

6. **超时机制使用注意**：
   - ⚠️ `auto_close` 是布尔值注解，**不要**使用数字（如 `auto_close=60`）
   - 如需设置超时时长，使用 `auto_close_timeout` 注解（如 `auto_close_timeout=60s`）
   - 默认 30 秒超时从最后一个 stdin 写端消失起算（attach 期间挂起）
   - 长期运行服务需显式禁用：`auto_close=false` 或 `auto_close_timeout=0`
   - 完整的停止/回收语义与边界见
     [容器生命周期与 IO 会话语义](../user/lifecycle-semantics.md)
