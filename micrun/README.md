# MicRun

MicRun是一个基于containerd shimv2的容器运行时，专为Mica项目设计，用于在异构计算平台上将RTOS（Real-Time Operating System，实时操作系统）作为容器运行。

## 项目定位

MicRun填补了RTOS与云原生生态之间的技术鸿沟，使边缘计算场景中的RTOS业务能够接入Kubernetes编排体系。作为openEuler Embedded混合关键性系统（MCS）生态的核心组件，MicRun在保持RTOS实时性和确定性的同时，为其赋予了容器化带来的部署、管理和分发优势。

## 核心价值

- **云原生集成**：实现containerd shimv2 API，与Kubernetes生态无缝集成，支持KubeEdge管理
- **资源映射**：将Kubernetes资源限制（CPU、内存）精确转换为底层hypervisor的资源分配
- **镜像分发**：利用容器镜像仓库管理RTOS固件，简化版本管理和部署流程
- **混合部署**：通过Xen等hypervisor在不同CPU核上运行RTOS，实现实时与非实时系统共存

## 典型应用场景

- **工业控制**：实时控制任务容器化部署，支持灵活的调度和升级
- **车载系统**：混合关键性车载软件管理，满足车规级功能安全要求
- **边缘AI**：AI推理与实时控制任务共存，优化边缘设备资源利用
- **物联网网关**：多协议适配与实时数据处理，统一管理边缘业务

---

## 架构概览

```
┌─────────────────────────────────────────────────────────┐
│                 Kubernetes CRI Interface                 │
└───────────────────────────┬─────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────┐
│                  Containerd Container Engine            │
│                  (runtime: io.containerd.mica.v2)       │
└───────────────────────────┬─────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────┐
│                  MicRun Shimv2 Runtime                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │ Task Service│  │ Sandbox     │  │ Shim IO         │ │
│  │ - Create    │  │ Service     │  │ - Binary IO     │ │
│  │ - Start     │  │ - Create    │  │ - Pipe IO       │ │
│  │ - Kill      │  │ - Start     │  │ - File IO       │ │
│  │ - Delete    │  │ - Stop      │  │ - TTY IO        │ │
│  │ - Wait      │  │ - Status    │  │                 │ │
│  └─────────────┘  └─────────────┘  └─────────────────┘ │
└───────────────────────────┬─────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────┐
│                Hypervisor Abstraction Layer             │
│                     (Pedestal)                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │ Xen         │  │ Baremetal   │  │ Resource        │ │
│  │ - Domain    │  │ - Channel   │  │ Planner         │ │
│  │ - vCPU      │  │ - Buffer    │  │ - CPU Mapping   │ │
│  │ - Memory    │  │ - RPMSG     │  │ - Memory Mapping│ │
│  └─────────────┘  └─────────────┘  └─────────────────┘ │
└───────────────────────────┬─────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────┐
│                      RTOS Layer                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │ Zephyr      │  │ UniProton   │  │                     │ │
│  └─────────────┘  └─────────────┘  └─────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

---

## 技术栈

- **语言**：Go 1.22+（静态链接，无CGO）
- **容器运行时接口**：containerd shimv2 API
- **通信协议**：ttrpc（轻量RPC）
- **Hypervisor支持**：Xen（主要）、Baremetal
- **RTOS支持**：Zephyr、UniProton
- **OCI规范**：遵循OCI runtime-spec
- **构建系统**：Yocto（openEuler Embedded）

---

## 代码结构

```
/micrun/
├── main.go                    # 程序入口，shimv2启动逻辑
├── definitions/               # 常量定义（注解、路径等）
├── pkg/                       # 核心包
│   ├── shim/                  # shimv2实现
│   ├── pedestal/              # hypervisor抽象层
│   ├── libmica/               # Mica守护进程客户端
│   ├── oci/                   # OCI规范处理
│   └── ...                    # 其他辅助模块
├── scripts/                   # 脚本工具
│   └── mica-image-builder/    # 镜像构建工具
├── docs/                      # 文档
└── tests/                     # 测试
```

---

## 核心设计

- **1:1:1模型**：一个shim实例对应一个容器对应一个RTOS
- **资源映射**：将cgroup资源限制转换为hypervisor资源分配
- **IO代理**：通过/dev/ttyRPMSG*处理RTOS终端IO
- **配置优先级**：注解 > 配置文件 > 默认值

---

## 文档导航

- [快速入门](docs/quick-start.md) - 从零开始部署 MicRun
- [Kubernetes 集成](docs/user/kubernetes.md) - 云边协同部署
- [故障排查](docs/user/troubleshooting.md) - 常见问题排查
- [性能调优](docs/user/performance-tuning.md) - 性能优化指南
- [注解参考](docs/reference/annotations.md) - 容器注解列表
- [配置参考](docs/reference/configuration.md) - 运行时配置
- [API 参考](docs/reference/api-reference.md) - Containerd API
- [资源映射](docs/reference/resources.md) - CPU/内存映射规范
- [IO 系统设计](docs/internals/io-system.md) - IO 架构详解
- [状态管理](docs/internals/state-management.md) - 状态持久化机制
- [日志系统](docs/internals/logging.md) - 日志系统设计
- [Sandbox 验证](docs/internals/sandbox-validation.md) - 状态验证机制
- [测试指南](tests/README.md) - 测试方法和用例

---

## 项目状态

MicRun 目前处于 **Preview** 阶段：

- 已实现基础 shimv2 接口
- 已支持 Xen hypervisor
- 已实现 CPU/内存资源映射
- 已实现基本 IO 处理
- 网络命名空间优化中
- 分布式追踪集成中

---

## 更多资源

- [项目仓库](https://atomgit.com/openeuler/mcs)
- [问题反馈](https://atomgit.com/openeuler/mcs/issues)
- [Mica-Xen 指导](https://embedded.pages.openeuler.org/master/features/mica/instruction.html)
