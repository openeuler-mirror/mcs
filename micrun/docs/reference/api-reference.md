# MicRun API 与核心接口参考

本文档描述 MicRun 当前代码中的核心接口与主要调用链。重点不是列出所有导出符号，而是回答三个问题：

1. 当前有哪些稳定的内部边界
2. 这些边界分别位于哪里
3. 从 `containerd` 请求进入后，主要调用链如何流转

## 1. 总体分层

```text
containerd runtime v2 RPC
        |
        v
internal/transport/shimv2
        |
        v
internal/application
        |
        v
internal/domain/container
        |
        v
internal/ports
        |
        v
internal/adapters
```

对应代码落点：

- transport: `internal/transport/shimv2`
- application: `internal/application`
- domain: `internal/domain/container`
- ports: `internal/ports`
- adapters: `internal/adapters`

## 2. Domain 接口

### 2.1 `ContainerTraits`

定义位置：
`internal/domain/container/interfaces.go`

它描述“单个 RTOS workload 容器”对外暴露的最小能力，包括：

- 标识：`ID()`
- 注解：`GetAnnotations()`
- sandbox 归属：`Sandbox()`
- 状态：`Status()`、`State()`
- 资源：`GetMemoryLimit()`、`GetClientCPU()`
- 状态持久化：`SaveState()`
- 控制：`Signal(...)`

这层接口仍然贴近当前实现，没有刻意追求更抽象的 runtime model。

### 2.2 `SandboxTraits`

定义位置：
`internal/domain/container/interfaces.go`

它是 application / transport 层感知 sandbox 的主要入口，能力包括：

- sandbox 生命周期：`Start` / `Stop` / `Delete`
- container 生命周期：`CreateContainer` / `StartContainer` / `StopContainer` / `DeleteContainer` / `KillContainer`
- 状态与查询：`GetState` / `GetAllContainers` / `StatusContainer` / `StatsContainer`
- IO：`IOStream` / `OpenTTYs` / `WinResize` / `WaitContainerExit`
- 资源更新：`UpdateContainer`

实现主体位于：

- [sandbox.go](internal/domain/container/sandbox.go)
- [sandbox_factory.go](internal/domain/container/sandbox_factory.go)
- [sandbox_loader.go](internal/domain/container/sandbox_loader.go)
- [sandbox_lifecycle.go](internal/domain/container/sandbox_lifecycle.go)

## 3. Ports

MicRun 当前比较重要的 ports 如下。

### 3.1 `GuestControl`

定义位置：
`internal/ports/guest.go`

负责 guest 生命周期和状态交互：

- `Start`
- `Stop`
- `Remove`
- `Pause`
- `Resume`
- `Exists`
- `Status`

当前默认实现链路：

`shimv2 -> buildContainerDependencies() -> guest/micad -> libmica`

### 3.2 `HypervisorControl`

定义位置：
`internal/ports/hypervisor.go`

负责宿主 / hypervisor 的控制面：

- `Type`
- `MaxCPUNum`
- `DomainState`
- `SetVCPUCount`
- `Pause`
- `Resume`

当前默认实现来自 pedestal adapter。

### 3.3 `StateStore`

定义位置：
`internal/ports/state_store.go`

用于持久化 runtime snapshot：

- `Load`
- `Save`
- `Delete`

当前文件实现位于：
`internal/adapters/state/file/store.go`

### 3.4 `IOSessionFactory`

定义位置：
`internal/ports/io.go`

用于 application 层创建 attach/session：

- `NewSession`
- `IsValidFIFOPath`
- `GenerateFIFOPath`

### 3.5 `TaskRuntime`

定义位置：
`internal/ports/task.go`

这是 application 层看到的"runtime-facing shim 接口"，由 `shimService` 实现。
它不是 domain 接口，而是 transport/application 之间的编排边界。

`TaskRuntime` 由 6 个基础子接口组合：
- `TaskLocker`、`TaskIdentity`、`TaskStore`、`TaskFactory`
- `TaskSandboxAccess`、`TaskStatusOps`

application/task 的方法不直接要求完整 `TaskRuntime`，而是按用例接收更窄的复合接口：

- `TaskCreateRuntime`
- `TaskStartRuntime`
- `TaskDeleteRuntime`
- `TaskQueryRuntime`
- `TaskWaitRuntime`
- `TaskSignalRuntime`
- `TaskIORuntime`

`TaskLifecycleRuntime` 和 `TaskAttachRuntime` 仍分别服务于 lifecycle/attach 子应用。
metrics 采集已经回收到 `transport/shimv2` 内部，不再属于 application/task 的 runtime port。
在 `taskManager` 内部，transport 读写视图继续按用途拆分为 process、metrics、task presence、events、shutdown。这样 `Stats`、`Pids`/`Connect`、`Shutdown` 和事件发布不会共享一个全能 transport runtime 接口。

实现位置：
`internal/transport/shimv2/runtime_ports.go`

### 3.6 `GuestExecutor`

定义位置：
`internal/ports/guest_executor.go`

`GuestExecutor` 是一个复合接口，由三个子接口按 Interface Segregation Principle 组合而成：

- **`GuestResourceReader`**: 读取当前资源状态
  - `ReadResource() *ResourceSnapshot`
  - `CurrentMaxMem() uint32`
  - `MemoryThresholdMB() uint32`

- **`GuestResourceUpdater`**: 应用资源变更
  - `UpdateCPUCapacity(capacity uint32) error`
  - `UpdateCPUWeight(weight uint32) error`
  - `UpdateVCPUNum(vcpu uint32) (oldCPUs, newCPUs uint32, err error)`
  - `UpdatePCPUConstraints(cpuSet string) error`
  - `EnsureMemoryLimit(mb uint32) error`
  - `UpdateMemoryThreshold(memMiB uint32) error`
  - `UpdateMemory(memMiB uint32) error`
  - `RecordMemoryState(current, threshold uint32)`
  - `VCPUPin(cpuList []int) error`

- **`GuestResourceDiff`**: 检查是否需要资源更新
  - `NeedUpdateCPUCap(target uint32) bool`
  - `NeedUpdateMemLimit(target uint32) bool`
  - `NeedUpdateCPUSet(oldSet, newSet string) bool`
  - `NeedUpdateCPUShare(target uint32) bool`
  - `NeedUpdateVCPUs(target uint32) bool`

关联类型：

- **`ResourceSnapshot`**: guest 当前资源状态快照（`CPUCapacity`、`CPUWeight`、`ClientCPUSet`、`VCPU`、`MemoryMaxMB`）

当前默认实现：`adapters/guest/libmica.MicaExecutor`

## 4. Application 层服务

### 4.1 `task.Service`

定义位置：
[service.go](internal/application/task/service.go)

职责：

- 聚合 attach + lifecycle 能力
- 为 transport 层提供 task 语义编排入口

### 4.2 `attach.Service`

定义位置：
[service.go](internal/application/attach/service.go)

职责：

- attach / reattach
- resize 前的会话准备
- stdin close 语义
- detach 后会话状态维护

### 4.3 `lifecycle.Service`

定义位置：
[service.go](internal/application/lifecycle/service.go)

职责：

- 启动任务时的 IO 与 exit orchestration
- 退出等待与退出事件关联

### 4.4 `recovery.Service`

定义位置：
[service.go](internal/application/recovery/service.go)

职责：

- orphan cleanup
- sandbox/task reconstruction

## 5. Transport 层关键入口

### 5.1 Shim 启动

入口：
[shim_bootstrap.go](internal/transport/shimv2/shim_bootstrap.go)

关键步骤：

1. `bootstrapPlatformBindings()`
   - 返回 `runtimeEnvironment`（封装宿主平台探测结果）
2. `buildContainerDependencies(bindings)`
3. `buildRuntimeDependencies(bindings, containerDeps)`
4. 创建 one-shot 或 daemon shim service

相关代码：

- [platform_bindings.go](internal/transport/shimv2/platform_bindings.go)
- [container_dependencies.go](internal/transport/shimv2/container_dependencies.go)
- [runtime_dependencies.go](internal/transport/shimv2/runtime_dependencies.go)

### 5.2 创建链路

大致路径：

```text
Create RPC
 -> shimv2/create
 -> buildCreatePlan
 -> loadRuntimeConfig
 -> createSandboxContainer / createPodContainer
 -> oci.ParseContainerCfg / oci.SandboxConfig
 -> domain/container.CreateSandbox / CreateContainer
```

代码落点：

- [create_plan.go](internal/transport/shimv2/create_plan.go)
- [runtime_config_helpers.go](internal/transport/shimv2/runtime_config_helpers.go)
- [create_sandbox_runtime.go](internal/transport/shimv2/create_sandbox_runtime.go)
- [create_pod_runtime.go](internal/transport/shimv2/create_pod_runtime.go)

### 5.3 恢复链路

大致路径：

```text
shim daemon start
 -> application/recovery.Service
 -> shimRecoveryBackend.Restore
 -> domain/container.LoadSandboxWithDependencies
 -> StateStore runtime snapshot
 -> legacy state.json fallback
```

代码落点：

- [recovery_backend.go](internal/transport/shimv2/recovery_backend.go)
- [sandbox_loader.go](internal/domain/container/sandbox_loader.go)
- [state_repository.go](internal/domain/container/state_repository.go)

## 6. 配置与资源相关接口

### 6.1 `RuntimeConfig`

定义位置：
[runtime_setup.go](internal/adapters/config/oci/runtime_setup.go)

作用：

- 管理 MicRun 运行时默认值
- 解析 INI / annotations
- 承载 `HostProfile`

### 6.2 `HostProfile`

定义位置：
[host_profile.go](internal/adapters/config/oci/host_profile.go)

作用：

- 显式携带宿主平台画像
- 供 `RuntimeConfig` / `SandboxConfig` / 容器配置解析使用
- 避免 `oci` 链路里直接读取 `pedestal.Host`

### 6.3 `ResourcePolicy`

定义位置：
[deps.go](internal/domain/container/deps.go)

作用：

- 从 `Dependencies` 中抽取资源规划和资源校验所需能力
- 从 `shimv2` 显式传到 `oci` 配置解析链
- `PlanEssentialRes` 接收 `*specs.Spec`，资源规划边界不再使用 `any` 后做运行时类型断言

## 7. 依赖注入现状

当前所有创建和恢复链路已通过显式注入完成：

- `containerDeps`（`Dependencies` 结构体，包含 `StateStoreFactory`、`GuestExecutorFactory` 等 9 个必需字段）
- `resourcePolicy`（从 `Dependencies` 中提取的资源规划能力子集）
- `runtimeResolver`（运行时配置解析器）
- `HostProfile`（宿主平台画像）

已完全移除的旧机制：

- ~~`domain/container` 内部的 `globalDeps`~~ 已完全移除，所有依赖通过 `SandboxConfig.Dependencies` 显式注入
- ~~`SetDependencies()` / `activeDependencies()` / `resolveDependencies()`~~ 已移除
- ~~包级默认 `defaultResourcePolicy()` / `defaultStateRepository()`~~ 已移除

全局入口状态：

- `pedestal.Host` / `pedestal.InitHost()` 已删除，默认 bootstrap 使用 `pedestal.DetectHost()`
- `guestmicad` / `pedestal` 已不再提供包级默认 control 适配器
