# MicRun 状态管理

本文档描述 MicRun 当前的状态持久化与恢复实现。

## 1. 为什么需要状态持久化

MicRun 的核心问题不是“容器有没有状态”，而是“shim 崩溃后如何重新接管仍在运行的 RTOS 实例”。

当前模型可以简化为：

```text
1 个 shim 进程
  <-管理->
1 个 sandbox
  <-管理->
1 组 RTOS workload
```

其中：

- shim 进程由 `containerd` 管理
- RTOS 实例由 `micad + Xen` 驱动
- 两者生命周期不是完全绑定的

所以：

- shim 可以死
- RTOS 可能还在跑
- 新 shim 必须从磁盘恢复运行时视图

## 2. 当前权威状态源

当前权威状态源是 `StateStore`，不是 bundle，也不是 legacy `state.json`。

接口定义：
`internal/ports/state_store.go`

当前默认文件实现：
`internal/adapters/state/file/store.go`

Domain 侧仓储实现：
`internal/domain/container/state_repository.go`

## 3. 当前存储内容

### 3.1 Sandbox snapshot

逻辑命名空间：

- `runtimeStateNamespaceSandbox`

包含的信息：

- sandbox id
- sandbox state
- sandbox config
- network config
- shim pid
- 创建时间

### 3.2 Container snapshot

逻辑命名空间：

- `runtimeStateNamespaceContainer`

包含的信息：

- container id
- sandbox id
- container state
- container config
- mounts
- container path

## 4. 恢复链路

当前恢复链路：

```text
shim daemon start
 -> application/recovery.Service
 -> shimRecoveryBackend.Restore
 -> domain/container.LoadSandboxWithDependencies
 -> stateRepository.LoadSandbox
 -> runtime snapshot
 -> legacy state.json fallback
 -> 创建恢复后的 Sandbox / Container 视图
```

关键代码：

- `internal/application/recovery/service.go`
- `internal/transport/shimv2/recovery_backend.go`
- `internal/domain/container/sandbox_loader.go`
- `internal/domain/container/sandbox_state.go`

## 5. Legacy 兼容策略

MicRun 当前仍兼容历史 `state.json` 文件，但兼容角色已经变化：

- 新写入：只走 `StateStore`
- 旧读取：当 runtime snapshot 缺失时，才回退到 legacy 文件
- 回退成功后：会把旧格式迁移回 runtime snapshot

也就是说，legacy 文件现在是“兼容回退来源”，不是主状态源。

## 6. 显式依赖注入现状

状态链路目前优先通过显式依赖拿 `StateStore`：

- `buildContainerDependencies(...)`
- `containerDeps.StateStoreFactory`
- `stateRepositoryFromDependencies(...)`
- `LoadSandboxWithDependencies(...)`
- `CleanupContainerWithDependencies(...)`

这意味着恢复和清理链已经不必只能靠包级默认仓储。

相关代码：

- `internal/transport/shimv2/container_dependencies.go`
- `internal/domain/container/state_repository.go`
- `internal/domain/container/container.go`

## 7. 状态校验

恢复不是“只要磁盘上有状态就恢复”，当前还会做运行态校验：

1. 检查持久化记录是否存在
2. 检查上一个 shim PID 是否仍活着
3. 通过 `GuestControl.Exists(...)` 检查 guest 是否仍存在
4. 通过 `GuestControl.Status(...)` 做运行态对照

关键代码：
`internal/domain/container/sandbox_loader.go`

## 8. 常见排查点

### 8.1 shim 重启后容器“丢失”

优先排查：

- runtime snapshot 是否存在
- `GuestControl.Exists(...)` 是否返回 false
- 恢复时是否被判定为 stale sandbox 并清理

### 8.2 legacy 文件与 runtime snapshot 不一致

当前以 runtime snapshot 为准。
legacy 文件只在 snapshot 不存在时才参与恢复。

### 8.3 为什么 bundle 不能作为权威状态源

因为 bundle 只是 OCI 输入，不是运行时状态存储。
shim 恢复需要的是：

- 当前 sandbox/container 视图
- shim PID
- network / state / config 快照

这些都属于 runtime state，而不是 bundle definition。

## 9. 仍待继续优化的点

- ~~`domain/container` 仍保留 `globalDeps` 兼容入口~~ 已移除，依赖通过 `SandboxConfig.Dependencies` 显式注入
- 运行态校验仍主要依赖 `micad/Xen` 查询，而不是更强的一致性模型
- sandbox 与 container snapshot 仍在 `domain/container` 大包里统一维护
