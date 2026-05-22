# MicRun 文档总览

本文档集面向两类读者：

- 使用者：如何部署、运行、排障、调优 MicRun
- 开发者：当前架构、关键接口、状态恢复、IO 与 pedestal 设计

## 建议阅读路径

### 初次接触 MicRun

1. [项目 README](../README.md)
2. [快速入门](quick-start.md)
3. [配置参考](reference/configuration.md)
4. [注解参考](reference/annotations.md)

### 想理解当前实现

1. [架构设计](internals/architecture.md)
2. [目标架构](internals/target-architecture.md)
3. [状态管理](internals/state-management.md)
4. [IO 系统](internals/io-system.md)
5. [Pedestal 架构](internals/pedestal-architecture.md)
6. [API 参考](reference/api-reference.md)

### 做问题排查或回归验证

1. [故障排查](user/troubleshooting.md)
2. [性能调优](user/performance-tuning.md)
3. [测试说明](../tests/README.md)
4. [IO 测试](../tests/io/README.md)
5. [K3s 测试](../tests/k3s/README.md)

## 流程图速览

### 架构总览

![MicRun architecture overview](assets/flowcharts/architecture-overview.png)

对应文档：[架构设计](internals/architecture.md)

### 分层运行时

![MicRun layered runtime map](assets/flowcharts/micrun-layered-runtime-map.png)

对应文档：[架构设计](internals/architecture.md)

### UniProton on Xen 主路径

![MicRun UniProton Xen overview](assets/flowcharts/micrun-uniproton-xen-overview.png)

对应文档：[架构设计](internals/architecture.md)

### IO 链路

![MicRun IO path](assets/flowcharts/io-system-flow.png)

对应文档：[IO 系统](internals/io-system.md)

### IO 交互细节

![MicRun IO interaction map](assets/flowcharts/micrun-io-interaction-map.png)

对应文档：[IO 系统](internals/io-system.md)

### 恢复与校验

![MicRun sandbox restore and validation](assets/flowcharts/sandbox-validation-flow.png)

对应文档：[Sandbox 校验](internals/sandbox-validation.md)

### 状态恢复决策

![MicRun state recovery map](assets/flowcharts/micrun-state-recovery-map.png)

对应文档：[Sandbox 校验](internals/sandbox-validation.md)

### 配置与资源控制

![MicRun config resource map](assets/flowcharts/micrun-config-resource-map.png)

对应文档：[架构设计](internals/architecture.md)

## 文档分区

### 用户文档

- [快速入门](quick-start.md)
- [Kubernetes 集成](user/kubernetes.md)
- [故障排查](user/troubleshooting.md)
- [性能调优](user/performance-tuning.md)

### 参考文档

- [参考文档入口](reference/README.md)
- [注解参考](reference/annotations.md)
- [配置参考](reference/configuration.md)
- [API 参考](reference/api-reference.md)
- [资源映射](reference/resources.md)

### 内部设计文档

- [内部文档入口](internals/README.md)
- [架构设计](internals/architecture.md)
- [状态管理](internals/state-management.md)
- [IO 系统](internals/io-system.md)
- [Pedestal 架构](internals/pedestal-architecture.md)
- [Sandbox 校验](internals/sandbox-validation.md)
- [日志系统](internals/logging.md)

## 当前文档状态

截至当前代码状态，文档已统一到以下实现事实：

- 主干分层为 `shimv2 -> application -> domain -> adapters/ports`
- `StateStore` 是权威状态源，legacy `state.json` 只作兼容回退
- `HostProfile`、`ResourcePolicy`、`Dependencies` 已进入显式注入链
- `globalDeps` 服务定位器已完全移除，依赖通过 `SandboxConfig.Dependencies` 显式注入
- `GuestExecutor` 已拆分为 `GuestResourceReader` / `GuestResourceUpdater` / `GuestResourceDiff` 三个子接口
- Go 命名规范已标准化（CPU/VCPU/ID），接口方法名语义统一
- `internal/domain/container` 是当前领域实现位置，不再使用旧的 `micantainer` 路径命名
- `internal/domain/console` 承载输入语义与输出规范化状态机，`adapters/io` 负责 FIFO/TTY 搬运和 epoll
- `runtimeEnvironment` 命名类型已封装宿主平台探测结果
- task runtime ports 已按 create/start/delete/query/wait/signal/io 用例收窄，lifecycle task context 保持为 application 内部细节
- `shimv2` task manager 自身的 transport 视图也已拆成 process、metrics、task presence、events、shutdown，避免 RPC 适配层重新依赖全能 runtime 接口

仍需后续随着代码继续演进而保持同步的主题：

- `libmica` 协议进一步结构化（部分已通过 `MicaUpdateRequest` 完成）
- 资源规划从 `domain/container` 抽成更清晰的子域边界
