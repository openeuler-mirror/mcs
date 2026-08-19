# Pedestal 架构

Pedestal 是 MicRun 当前的宿主 / hypervisor 抽象层。它负责把 Xen、Baremetal 这类平台差异收口到统一接口上。

## 1. 当前定位

Pedestal 现在承担两类职责：

1. 平台能力查询
2. hypervisor 控制操作

当前主要服务的调用方：

- `internal/transport/shimv2`
- `internal/adapters/config/oci`
- `internal/adapters/guest/libmica`

## 2. 当前结构

```text
internal/adapters/hypervisor/pedestal
    |
    +-- PedestalFacade
    |     - impl
    |     - optional capability caches
    |
    +-- Xen implementation
    +-- Baremetal placeholder
    +-- Control adapter (implements ports.HypervisorControl)
```

关键文件：

- `interface.go`
- `facade.go`
- `types.go`
- `control.go`
- `detection.go`

## 3. 当前接口分层

### 3.1 `Pedestal`

定义位置：
`interface.go`

基础能力包括：

- `Type()`
- `String()`
- `GeneratePedConf()`
- `MaxCPUNum()`
- `MemoryMB()`
- `MemLowThreshold()`
- `MemHighThreshold()`
- `HostCPUSeta()`（代码现状的既有拼写，非文档笔误）

### 3.2 可选扩展接口

Pedestal 通过可选接口暴露额外能力：

- `CPUScheduler`
- `LifecycleManager`
- `StateQuerier`
- `MemoryManager`

这些能力由 `PedestalFacade` 统一包装。

### 3.3 `HypervisorControl`

MicRun 真正跨层使用的宿主控制接口不是 `Pedestal` 本身，而是：
`hypervisor.go`

默认 adapter：
`control.go`

这层是 `transport/application/domain` 更应该依赖的边界。

## 4. 当前注入方式

### 4.1 shim bootstrap

`shimv2` 启动时会做平台 bootstrap：

1. `pedestal.DetectHost()`
2. 通过默认 `runtimeEnvironmentSource` 解析平台入口
3. 创建 `runtimeEnvironment`
4. 把 `HostProfile`、`vcpu stats provider`、`guest control`、`hypervisor control` 组装进 runtime/container dependencies

代码：

- `platform_bindings.go`
- `runtime_dependencies.go`
- `container_dependencies.go`

### 4.2 `HostProfile`

配置适配层不直接从 `pedestal.Host` 读取宿主画像，而是通过 `HostProfile` 显式传递：

- `host_profile.go`
- `runtime_setup.go`
- `resolver.go`

配置链路由此不依赖 pedestal 的“全局感知”。

同时，`shimv2` 的 `runtimeEnvironment` 不持有具体 `host facade` 指针，只保存上层真正需要的结果：

- `HostProfile`
- `vcpu stats provider`
- `max client CPU provider`
- `essential resource planner`
- `GuestControl`
- `HypervisorControl`

这意味着：

- 运行时依赖装配不需要再把完整 `PedestalFacade` 往下游传
- `service_factory` / `container_dependencies` 这类测试可以直接构造显式 `runtimeEnvironment`
- 默认平台入口被压缩在 `defaultRuntimeEnvironmentSource()` 一处，而不是散落到多个调用点
- `guestmicad` / `pedestal` 不暴露包级 `Default` control 适配器
- 资源规划、可分配 client CPU 计算、hugepage 判断都通过绑定后的 `PedestalFacade` 能力进入主路径

## 5. 当前全局入口状态

pedestal 无包级 `Host` / `InitHost()` 全局入口，遵循“默认 bootstrap 显式探测、绑定结果显式化”的模式：

- `libmica` 的 Pause/Resume/xl workaround 显式接收 `HypervisorControl`
- `shimv2` 默认 bootstrap 通过 `DetectHost()` 解析 host，再构造绑定到该 host 的 `HypervisorControl` / `GuestControl` / 资源能力函数
- `pedestal/resources.go`、`pedestal/planner.go` 的主能力收敛为 `PedestalFacade` 方法

## 6. 当前平台支持

| 平台 | 状态 | 说明 |
|------|------|------|
| Xen | 主路径 | 当前生产能力核心 |
| Baremetal | 显式开关 | 需要设置 `MICRUN_ENABLE_BAREMETAL=1` 才会被 host detection 选中，默认不会自动启用 |

## 7. 剩余收敛方向

- `PedestalFacade` 的能力方法不向上游扩散：需要新能力时优先评估是否
  属于 pedestal 语义，避免 facade 重新膨胀为全能接口。
