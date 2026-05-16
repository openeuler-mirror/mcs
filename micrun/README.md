# MicRun

MicRun 是一个基于 `containerd shim v2` 的 RTOS 运行时。它把 `Zephyr`、`UniProton` 这类 RTOS 工作负载接入 `containerd / ctr / nerdctl / k3s`，并通过 `micad + Xen` 管理实际的 RTOS 实例。

## 当前定位

MicRun 解决的是“如何把 RTOS 作为云原生工作负载管理”这个问题，当前重点是：

- 用 `containerd runtime v2` 语义管理 RTOS workload
- 用容器镜像分发 RTOS 固件
- 把 OCI / Kubernetes 资源限制映射到 Xen / guest 资源模型
- 保持 shim 崩溃后的状态恢复能力
- 保持 `ctr` / `nerdctl` / `k3s` 的基本交互和 IO 语义

当前主路径已经过本地单测、QEMU `test-io-qemu` 回归和 K3s 云边/交互测试
验证。

## 当前架构

```text
containerd / ctr / nerdctl / k3s
            |
            v
internal/transport/shimv2
  - runtime v2 TaskService 适配
  - 平台 bootstrap
  - recovery / cleanup / event forward
            |
            v
internal/application
  - runtime service graph
  - task
  - lifecycle
  - attach
  - recovery
            |
            v
internal/domain/container
  - Sandbox / Container
  - 资源规则
  - 状态恢复
  - 状态仓储边界
            |
            v
internal/ports
  - GuestControl
  - HypervisorControl
  - StateStore
  - IOSessionFactory
            |
            v
internal/adapters
  - config/oci + runtimeconfig
  - guest/micad + libmica
  - hypervisor/pedestal
  - state/file
  - io
```

当前几条关键显式依赖链：

- `HostProfile`: `shimv2 -> runtimeconfig -> oci`
- `ResourcePolicy`: `shimv2 -> oci`
- `Dependencies/StateStore`: `shimv2 -> create/recovery/cleanup -> domain/container`
- `GuestExecutor`: `domain/container -> ports.GuestExecutor -> adapters/guest/libmica`

## 代码结构

```text
micrun/
├── main.go
├── definitions/                 # 注解、路径、常量
├── internal/
│   ├── bootstrap/               # 进程入口、早期命令、shim CLI/logging 启动边界
│   ├── transport/shimv2/        # shimv2 入口与 transport 适配
│   ├── application/             # runtime 服务图、task/lifecycle/attach/recovery 用例
│   ├── domain/container/        # Sandbox/Container/资源/状态领域逻辑
│   ├── adapters/                # guest/hypervisor/config/state/io 适配器
│   ├── ports/                   # 领域与应用层依赖的抽象接口
│   └── support/                 # cpuset/netns/utils 等通用支撑
├── docs/                        # 用户、参考、内部设计文档
└── tests/                       # 单测、QEMU 回归、k3s 测试
```

## 当前状态

已完成：

- `shimv2 -> application -> domain -> adapters/ports` 主干分层
- `main -> bootstrap -> shimv2` 启动边界已显式化，早期命令与日志上下文不再堆在 `main.go`
- `application/runtime` 统一装配 task/lifecycle/attach/recovery 服务图，并校验服务图完整性和引用一致性；`lifecycle/task` 不再在包内隐式自装配依赖
- `runtime.Options` 是 application 层的单一装配入口，负责把共享 `IOSessionFactory` 与 `Clock` 传给整张服务图
- `libmica` 职责拆分
- `StateStore` 权威状态源与 legacy `state.json` 兼容迁移
- `HostProfile` / `ResourcePolicy` / `Dependencies` 的显式注入链
- `globalDeps` 服务定位器完全移除，所有依赖通过 `SandboxConfig.Dependencies` 显式注入
- Go 命名规范标准化（CPU/VCPU/ID），接口方法名语义统一
- 死代码清理：`sandboxResource` 无用方法/字段、`UpdateSandboxPoolVCPUs`、`dummySandboxConfig`
- `GuestExecutor` 接口拆分为 `GuestResourceReader` / `GuestResourceUpdater` / `GuestResourceDiff` 三个子接口
- QEMU 下的 `micrun` IO / 生命周期回归验证通过

仍在继续优化：

- `libmica` 协议仍偏文本化（部分已通过 `MicaUpdateRequest` 结构化）
- `adapters/io` 内部 session、copier、事件流仍有进一步收敛空间

## 文档入口

- [文档总览](docs/README.md)
- [快速入门](docs/quick-start.md)
- [当前架构](docs/internals/architecture.md)
- [状态管理](docs/internals/state-management.md)
- [Pedestal 设计](docs/internals/pedestal-architecture.md)
- [IO 系统](docs/internals/io-system.md)
- [API 参考](docs/reference/api-reference.md)
- [测试说明](tests/README.md)

## 测试

常用验证方式：

```bash
cd micrun
go test ./... -count=1
```

QEMU 回归：

```bash
tests/bin/test-qemu-smoke
tests/bin/test-io-qemu
```

K3s 回归已纳入统一测试项目。已有 K3s 环境时可直接跑类别入口；QEMU 云边
环境使用 rootfs 内置的 `/usr/bin/k3s`，不要在 guest 内临时安装或替换 K3s：

```bash
tests/run_all_tests.sh k3s
tests/run_all_tests.sh k3s K3S-008
tests/bin/test-k3s-cloud-edge
tests/bin/test-k3s-interaction
```

## 参考

- [项目仓库](https://atomgit.com/openeuler/mcs)
- [Mica / Xen 指导](https://embedded.pages.openeuler.org/master/features/mica/instruction.html)
