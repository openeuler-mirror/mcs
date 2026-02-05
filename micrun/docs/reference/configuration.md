# MicRun 配置参考手册

## 概述

MicRun 支持多种配置方式，按优先级从高到低：

1. **注解** (Pod/Container annotations) - 最高优先级
2. **配置文件** (INI/TOML)
3. **环境变量**
4. **默认值** - 最低优先级

## 配置文件

### 配置文件位置

| 优先级 | 配置来源 |
|--------|----------|
| 1 | `MICRUN_CONF_FILE` 环境变量指定的文件 |
| 2 | `MICRUN_CONF_DIR` 环境变量指定的目录（读取所有 .conf/.toml 文件） |
| 3 | `/etc/mica/micrun/conf.d/*.conf` (drop-in 目录) |
| 4 | `/etc/mica/micrun/config.toml` (默认配置文件) |

### 配置文件格式

支持两种格式：

| 格式 | 扩展名 | 说明 |
|------|--------|------|
| INI | `.ini`, `.conf` | 传统 INI 格式 |
| TOML | `.toml` | TOML 格式 |

### INI 配置示例

```ini
[Mica]
# 调试模式
debug = false

# 最大客户端数量
max_client_num = 8

# 默认固件路径
default_firmware_path = /usr/local/share/mica/firmware.elf

[Resource]
# 容器最大 vCPU 数
max_container_vcpus = 4

# 容器最大内存 (MiB)
max_container_mem_mb = 512

# 容器最小内存 (MiB)
min_container_mem_mb = 32

# 静态资源管理
static_resource_management = false

# 共享 CPU 池
shared_cpu_pool = true

# HugePage 支持
hugepage_support = false

[Xen]
# Mini vCPU 数量
mini_vcpu_num = 1

# Dom0 CPU 独占
exclusive_dom0_cpu = false

# Xen 镜像路径
image_path = /usr/local/share/mica/xen-image.bin

# 辅助文件路径
aux_file_path = /usr/local/share/mica/xen-aux.bin
```

### TOML 配置示例

```toml
[mica]
debug = false
max_client_num = 8
default_firmware_path = "/usr/local/share/mica/firmware.elf"

[resource]
max_container_vcpus = 4
max_container_mem_mb = 512
min_container_mem_mb = 32
static_resource_management = false
shared_cpu_pool = true
hugepage_support = false

[xen]
mini_vcpu_num = 1
exclusive_dom0_cpu = false
image_path = "/usr/local/share/mica/xen-image.bin"
aux_file_path = "/usr/local/share/mica/xen-aux.bin"
```

## 环境变量

| 环境变量 | 说明 | 默认值 |
|----------|------|--------|
| `MICRUN_CONF_FILE` | 指定配置文件路径 | - |
| `MICRUN_CONF_DIR` | 指定配置目录路径 | - |
| `CONTAINERD_NAMESPACE` | 容器命名空间 | `default` |

## 配置项详解

### Mica 节配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `debug` | 布尔 | `false` | 启用调试模式 |
| `max_client_num` | 整数 | `8` | 最大客户端数量 |
| `default_firmware_path` | 字符串 | - | 默认固件路径 |

### Resource 节配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `max_container_vcpus` | 整数 | `4` | 容器最大 vCPU 数 |
| `max_container_mem_mb` | 整数 | `512` | 容器最大内存 (MiB) |
| `min_container_mem_mb` | 整数 | `32` | 容器最小内存预留 (MiB) |
| `static_resource_management` | 布尔 | `false` | 静态资源管理（禁止动态更新） |
| `shared_cpu_pool` | 布尔 | `true` | 共享 CPU 池模式 |
| `hugepage_support` | 布尔 | `false` | 启用 HugePage 支持 |

#### Static Resource Management

启用后：
- `UpdateContainer` API 将被忽略
- 资源在容器创建时固定
- 适用于资源固定的生产环境

#### Shared CPU Pool

| 模式 | 说明 |
|------|------|
| `true` | 所有容器共享 CPU 池，cpuset 合并 |
| `false` | 每个容器使用独立的 cpuset |

### Xen 节配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `mini_vcpu_num` | 整数 | `1` | 最小 vCPU 数量 |
| `exclusive_dom0_cpu` | 布尔 | `false` | Dom0 CPU 独占 |
| `image_path` | 字符串 | - | Xen 镜像路径 |
| `aux_file_path` | 字符串 | - | Xen 辅助文件路径 |

## 配置优先级示例

### 示例 1：注解覆盖配置文件

```yaml
# Pod 注解
metadata:
  annotations:
    org.openeuler.micrun.container.min_memory_mb: "64"  # 覆盖配置文件
```

优先级：注解 (64 MiB) > 配置文件 (32 MiB) > 默认值 (16 MiB)

### 示例 2：环境变量指定配置文件

```bash
# 使用自定义配置文件
export MICRUN_CONF_FILE=/etc/mica/micrun/custom.conf
```

优先级：`$MICRUN_CONF_FILE` > `$MICRUN_CONF_DIR` > `/etc/mica/micrun/conf.d/` > `/etc/mica/micrun/config.toml`

## Drop-in 目录

Drop-in 目录允许将配置拆分为多个文件：

```
/etc/mica/micrun/conf.d/
├── 00-base.conf      # 基础配置
├── 10-resource.conf  # 资源配置
└── 99-local.conf     # 本地覆盖配置
```

加载顺序：按文件名字母序加载，后加载的配置覆盖先加载的配置。

## 配置验证

### 检查当前配置

```bash
# 查看使用的配置文件
journalctl -u containerd | grep "micrun config"

# 查看 shim 进程的环境变量
cat /proc/$(pgrep containerd-shim-mica-v2)/environ | tr '\0' '\n' | grep MICRUN
```

### 常见配置错误

| 错误 | 原因 | 解决方案 |
|------|------|----------|
| `config file not found` | 配置文件路径错误 | 检查 `MICRUN_CONF_FILE` 环境变量 |
| `invalid config format` | 配置文件格式错误 | 检查 INI/TOML 语法 |
| `value out of range` | 配置值超出有效范围 | 检查数值是否合理 |

## 生产环境配置示例

### 高性能配置

```ini
[Mica]
debug = false
max_client_num = 16

[Resource]
max_container_vcpus = 8
max_container_mem_mb = 2048
min_container_mem_mb = 64
static_resource_management = true
shared_cpu_pool = true
hugepage_support = true

[Xen]
mini_vcpu_num = 2
exclusive_dom0_cpu = true
```

### 低资源配置

```ini
[Mica]
debug = false
max_client_num = 4

[Resource]
max_container_vcpus = 2
max_container_mem_mb = 256
min_container_mem_mb = 16
static_resource_management = false
shared_cpu_pool = true
hugepage_support = false

[Xen]
mini_vcpu_num = 1
exclusive_dom0_cpu = false
```

### 开发调试配置

```ini
[Mica]
debug = true
max_client_num = 4

[Resource]
max_container_vcpus = 2
max_container_mem_mb = 256
min_container_mem_mb = 16
static_resource_management = false
shared_cpu_pool = false
hugepage_support = false

[Xen]
mini_vcpu_num = 1
exclusive_dom0_cpu = false
```

## 日志配置

日志配置位于 `/etc/mica/micrun/config.json`，由 logger 包读取：

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

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `level` | 字符串 | `info` | 日志级别 (debug, info, warn, error) |
| `file` | 字符串 | `/var/log/mica/mica-runtime.log` | 日志文件路径 |
| `color` | 布尔 | `false` | 是否启用颜色 |
| `caller` | 布尔 | `true` | 是否显示调用位置 |

## 相关文档

- [注解参考手册](./annotations-reference.md) - 注解配置
- [资源映射设计](./resource-design.md) - 资源限制规则
- [故障排查指南](./troubleshooting.md) - 配置问题排查
