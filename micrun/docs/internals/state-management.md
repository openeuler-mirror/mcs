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

### 3.1 合并的 sandbox snapshot（唯一写入目标）

逻辑命名空间：

- `runtimeStateNamespaceSandbox`

一个文档携带整个 pod 的持久化状态：

- sandbox id / state / config / network / shim pid / 创建时间
- 每个容器的完整 config（`Config.ContainerConfigs`）
- 每个容器的运行时记录（`Containers` map：state、mounts、container path）

每次容器状态转换（`setContainerState`）就是一次该文档的原子写。
被删除的容器不在 containers map 中，迟到的持久化写出的文档天然不含
其记录——状态复活在构造上不可能发生。

### 3.2 Container snapshot（只读兼容）

逻辑命名空间：

- `runtimeStateNamespaceContainer`

合并格式之前每容器单独持久化 state/config/mounts/path，现在**不写入**，
仅在恢复时（sandbox 文档缺 `containers` 键的旧格式）作为回退读取来源，
并在容器删除时清理。

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

MicRun 当前兼容两代旧格式，读取回退顺序为：

1. 合并的 sandbox `runtime.json`（`containers` 键存在 → 容器状态直接内嵌读取）
2. 旧格式 sandbox `runtime.json`（无 `containers` 键）→ 容器状态回退读
   每容器 `runtime.json`
3. legacy `state.json`（读入后迁移到 `runtime.json`）

重建完成后的第一次 `StoreSandbox` 会把文档迁移为合并格式。新写入只走
合并的 sandbox 文档；每容器文件与 legacy 文件都只是“兼容回退来源”。

注意：合并格式写出后不支持降级回旧版本 shim（旧版本会读到过期的每容器
文件）；跨该版本降级需要清空运行时状态目录。

## 6. 显式依赖注入现状

状态链路目前优先通过显式依赖拿 `StateStore`：

- `buildContainerDependencies(...)`
- `containerDeps.StateStoreFactory`
- `stateRepositoryFromDependencies(...)`
- `LoadSandboxWithDependencies(...)`
- `CleanupContainerWithDependencies(...)`

这意味着恢复和清理链不依赖包级默认仓储。

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

- 运行态校验仍主要依赖 `micad/Xen` 查询，而不是更强的一致性模型
- sandbox 与 container snapshot 仍在 `domain/container` 大包里统一维护
