# MicRun 内部设计文档

本目录面向开发者，描述 MicRun 当前实现边界以及下一阶段重实现目标。

## 推荐阅读顺序

1. [architecture.md](./architecture.md)
   当前分层、已完成重构、剩余问题。

2. [target-architecture.md](./target-architecture.md)
   目标分层、重实现原则、迁移顺序与判断标准。

3. [state-management.md](./state-management.md)
   `StateStore`、runtime snapshot、恢复链路、legacy 回退。

4. [io-system.md](./io-system.md)
   FIFO / TTY / copier / attach 语义。

5. [pedestal-architecture.md](./pedestal-architecture.md)
   pedestal 抽象、平台 bootstrap、显式 host 绑定与后续收敛方向。

6. [sandbox-validation.md](./sandbox-validation.md)
   shim 崩溃后的 sandbox 校验与清理。

7. [logging.md](./logging.md)
   日志分层与调试方法。

## 流程图导航

### 1. 当前分层与主干入口

![MicRun architecture overview](../assets/flowcharts/architecture-overview.png)

### 2. 分层运行时

![MicRun layered runtime map](../assets/flowcharts/micrun-layered-runtime-map.png)

### 3. UniProton on Xen 主路径

![MicRun UniProton Xen overview](../assets/flowcharts/micrun-uniproton-xen-overview.png)

### 4. IO 交互路径

![MicRun IO path](../assets/flowcharts/io-system-flow.png)

### 5. IO 交互细节

![MicRun IO interaction map](../assets/flowcharts/micrun-io-interaction-map.png)

### 6. 恢复与状态校验

![MicRun sandbox restore and validation](../assets/flowcharts/sandbox-validation-flow.png)

### 7. 状态恢复决策

![MicRun state recovery map](../assets/flowcharts/micrun-state-recovery-map.png)

### 8. 配置与资源控制

![MicRun config resource map](../assets/flowcharts/micrun-config-resource-map.png)

## 当前实现边界

目前内部文档统一使用这些代码路径：

- `internal/transport/shimv2`
- `internal/application/*`
- `internal/domain/container`
- `internal/adapters/*`
- `internal/ports/*`

旧文档中出现的 `pkg/shim`、`pkg/micantainer` 仅作为历史背景，不再是当前实现路径。
