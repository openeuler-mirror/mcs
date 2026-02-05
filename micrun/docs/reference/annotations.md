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
| `liteos` | Huawei LiteOS |
| `linux` | Linux 容器 |

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
| 默认值 | `16` |

**说明**：
- 这是容器的**预留内存**（memory reservation）
- 实际分配的内存不会低于此值
- 如果 OCI spec 中设置了 `memory.reservation`，会覆盖此注解

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

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.max_vcpu_num: "4"
```

---

### org.openeuler.micrun.container.auto_close

控制容器是否在 stdin 关闭后自动退出。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `true` |
| 优先级 | `auto_close_timeout` > `auto_close` > 默认 |

**行为**：
- `true`：客户端断开后，容器会在超时后自动退出
- `false`：禁用自动关闭（除非设置了 `auto_close_timeout`）

**重要说明**：
- ⚠️ **不要使用数字值**（如 `"60"`）。此注解是布尔值，数字值会被忽略。
- 如需设置超时时长，请使用 `auto_close_timeout` 注解。
- **所有 IO 模式**（TTY/Non-TTY、前台/后台）默认都启用超时机制
- 只有显式设置 `auto_close=false` 或 `auto_close_timeout=0` 才能禁用超时

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.auto_close: "true"
```

---

### org.openeuler.micrun.container.auto_close_timeout

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
- 默认情况下，**所有容器**都会在 30 秒后自动关闭（无论 TTY/Non-TTY、前台/后台）
- 这是为防止测试/调试会话资源泄漏而设计的保护机制
- 如需长期运行服务，请显式设置 `auto_close=false` 或 `auto_close_timeout=0`

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
| 可选值 | `xen`, `openamp`, `acrn` |

**说明**：
- 如果指定的类型与主机不匹配，容器创建会失败
- 通常不需要设置，自动使用主机 Hypervisor

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

### org.openeuler.micrun.runtime.disable_new_netns

禁用创建新的网络命名空间。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |

**示例**：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.runtime.disable_new_netns: "true"
```

---

### org.openeuler.micrun.runtime.pipe_size

指定 IO 管道的大小（字节）。

| 属性 | 值 |
|------|-----|
| 类型 | 整数 |
| 单位 | 字节 |
| 默认值 | 系统默认 |

**示例**：
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

启用实验性功能。

| 属性 | 值 |
|------|-----|
| 类型 | 布尔值 |
| 默认值 | `false` |

**示例**：
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

以下注解由 MicRun 内部使用，通常不需要手动设置：

| 注解 | 说明 |
|------|------|
| `org.openeuler.micrun.pkg.oci.bundle_path` | OCI bundle 路径 |
| `org.openeuler.micrun.pkg.oci.container_type` | 容器类型 |
| `org.openeuler.micrun.config_path` | Sandbox 配置路径 |

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
    org.openeuler.micrun.runtime.disable_new_netns: "true"
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

5. **资源限制**：OCI spec 中的资源限制（`resources.limits`）与注解配置会互相影响，详见 [资源映射文档](./resource-design.md)

6. **超时机制使用注意**：
   - ⚠️ `auto_close` 是布尔值注解，**不要**使用数字（如 `auto_close=60`）
   - 如需设置超时时长，使用 `auto_close_timeout` 注解（如 `auto_close_timeout=60s`）
   - **所有 IO 模式默认启用 30 秒超时**，防止资源泄漏
   - 长期运行服务需显式禁用：`auto_close=false` 或 `auto_close_timeout=0`
