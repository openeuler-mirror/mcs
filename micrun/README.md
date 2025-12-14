1. Micrun是什么

Micrun是一个基于containerd shimv2的容器运行时，专为Mica项目设计，用于在不同CPU核上运行RTOS（实时操作系统）。它是openEuler Embedded混合关键性系统（MCS）生态的重要组成部分。

核心特性

- RTOS容器化：将Zephyr、UniProton、LiteOS等RTOS作为容器运行在异构计算平台上
- 混合部署：通过Xen、OpenAMP等hypervisor在不同CPU核上运行RTOS，实现实时与非实时系统共存
- 云原生集成：实现containerd shimv2 API，与Kubernetes生态无缝集成
- 资源映射：将容器资源限制（CPU、内存）转换为底层hypervisor的资源分配
- 镜像分发：利用容器镜像仓库管理RTOS固件，简化部署流程

2. 项目框架

2.1 技术栈

- 语言：Go 1.22+（静态链接，无CGO）
- 容器运行时接口：containerd shimv2 API
- 通信协议：ttrpc（轻量RPC）
- pedestal支持：Xen Hypervisor（主要）、Baremetal
- RTOS支持：Zephyr、UniProton等
- OCI规范：遵循OCI runtime-spec
- 追踪：OpenTelemetry集成
- 构建系统：Yocto（openEuler yocto工程集成相关组件进入镜像）或 直接源码Makefile构建

2.2 代码结构

/micrun/
├── main.go                    # 程序入口，shimv2启动逻辑
├── definitions/               # 常量定义（注解、路径等）
├── pkg/                       # 核心包
│   ├── shim/                  # shimv2实现
│   │   ├── shim_services.go   # 主服务结构
│   │   ├── create.go          # 容器创建逻辑
│   │   ├── start.go           # 容器启动逻辑
│   │   └── shimio.go          # IO处理
│   ├── pedestal/              # hypervisor抽象层
│   │   ├── xen.go             # Xen支持
│   │   ├── openamp.go         # OpenAMP支持
│   │   └── planner.go         # 资源规划
│   ├── libmica/               # Mica守护进程客户端
│   ├── oci/                   # OCI规范处理
│   ├── micantainer/           # 容器类型定义
│   ├── configstack/           # 配置栈管理
│   ├── utils/                 # 工具函数
│   ├── types/                 # 类型定义
│   ├── cpuset/                # CPU集合处理
│   ├── netns/                 # 网络命名空间
│   └── tracer/                # 追踪支持
├── tools/                     # 工具
│   ├── mica-image-builder/    # 镜像构建工具
│   └── bundle.sample/         # 示例bundle
├── docs/                      # 文档
├── examples/                  # 示例配置
└── tests/                     # 测试

2.3 核心要点

- 1:1:1模型：一个shim实例对应一个容器对应一个RTOS
- 资源映射：将cgroup资源限制转换为hypervisor资源分配
- IO代理：通过/dev/ttyRPMSG*处理RTOS终端IO
- 沙箱管理：通过pause容器维护pod网络命名空间
- 配置优先级：注解 > 配置文件 > 默认值

3. 角色与定位

3.1 解决的问题

1. RTOS云化管理：将边缘RTOS业务纳入Kubernetes编排
2. 镜像分发：利用容器镜像仓库管理RTOS固件
3. 资源隔离：通过hypervisor实现RTOS间的资源隔离
4. 生态集成：复用容器工具链（nerdctl、ctr等）

3.2 技术挑战

1. 生命周期感知：RTOS缺乏标准进程模型，难以感知退出状态
2. IO处理：RTOS终端与容器终端的差异处理
3. 资源转换：cgroup资源模型到hypervisor资源模型的映射
4. 网络集成：RTOS网络与容器网络的对接

4. 详细工作架构

4.1 整体架构层次

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
│  │ Xen         │  │ OpenAMP     │  │ Resource        │ │
│  │ - Domain    │  │ - Channel   │  │ Planner         │ │
│  │ - vCPU      │  │ - Buffer    │  │ - CPU Mapping   │ │
│  │ - Memory    │  │ - RPMSG     │  │ - Memory Mapping│ │
│  │ - Weight    │  │ - VirtIO    │  │ - Affinity      │ │
│  └─────────────┘  └─────────────┘  └─────────────────┘ │
└───────────────────────────┬─────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────┐
│                   Hypervisor Layer                      │
│  ┌───────────────────────────────────────────────────┐ │
│  │ Xen Domain | Baremetal OpenAMP Channel | FusionDock  │ │
│  └───────────────────────────────────────────────────┘ │
└───────────────────────────┬─────────────────────────────┘
                            │
┌───────────────────────────▼─────────────────────────────┐
│                      RTOS Layer                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │ Zephyr      │  │ UniProton   │  │ LiteOS          │ │
│  │ - App       │  │ - App       │  │ - App           │ │
│  │ - Shell     │  │ - Shell     │  │ - Shell         │ │
│  └─────────────┘  └─────────────┘  └─────────────────┘ │
└─────────────────────────────────────────────────────────┘

4.2 关键组件详解

4.2.1 Shimv2接口层

- Task Service：实现containerd task API，处理容器生命周期（Create/Start/Kill/Delete/Wait）
- Sandbox Service：实现sandbox API，管理pod级别的沙箱环境
- Shim IO：处理容器IO流，支持binary/pipe/file/tty等多种IO模式
- 配置管理：按照优先级处理注解、配置文件、默认值

4.2.2 Hypervisor抽象层（Pedestal）

- 统一接口：为不同hypervisor提供统一的资源管理和生命周期接口
- 资源规划器：将容器资源需求转换为hypervisor特定的资源配置
- 平台适配：支持Xen、OpenAMP、ACRN、FusionDock等hypervisor

4.2.3 资源映射引擎

// 核心映射逻辑
func convertCPUResources(res *LinuxContainerResources) ResourceMapping {
    // CPU Quota/Period → vCPU数量 + cap百分比
    if res.CpuQuota > 0 && res.CpuPeriod > 0 {
        requestedCores := float64(res.CpuQuota) / float64(res.CpuPeriod)
        vCPUs := int(math.Ceil(requestedCores))
        capPercent := int((requestedCores / float64(vCPUs)) * 100)
    }

    // CPU Shares → Xen Weight
    if res.CpuShares > 0 {
        // 转换公式：W(S) = max(1, min(S/R, 65535)); R=4
        weight := int((res.CpuShares * 256) / 1024)
    }

    // cpuset → CPU亲和性
    if res.CpusetCpus != "" {
        affinity := parseCPUSet(res.CpusetCpus)
    }
}

4.2.4 IO处理系统

- TTY模式：通过/dev/ttyRPMSG*实现RTOS终端交互
- Pipe模式：用于标准输入输出流
- Binary模式：containerd二进制日志格式
- File模式：文件重定向和日志记录

4.2.5 网络集成

- NetNS管理：通过pause容器维护pod网络命名空间
- CNI集成：支持containerd管理的CNI网络配置
- RTOS网络：通过virtio或共享内存实现RTOS网络访问

4.3 资源映射详细流程

4.3.1 CPU资源映射

Kubernetes CPU Limit (e.g., "1000m")
    ↓
Containerd → OCI Spec (quota=100000, period=100000)
    ↓
MicRun解析 → requestedCores = quota/period = 1.0
    ↓
Xen映射策略：
    - vCPUs = ceil(1.0) = 1
    - CPUCap = (1.0/1)*100 = 100%
    - CPUWeight = shares转换（默认256）

4.3.2 内存资源映射

Kubernetes Memory Limit (e.g., "512Mi")
    ↓
Containerd → OCI Spec (limit=536870912)
    ↓
MicRun解析 → memoryMB = 536870912/(1024*1024) = 512
    ↓
Xen映射策略：
    - Memory = 512MB（静态分配）
    - MaxMemory = 512MB

4.3.3 双重约束处理

容器配置：
    - quota=200% (2.0核)
    - cpuset="0" (1核)

生效容量计算：
    effective_capacity = min(
        (quota × 100) / period,   # 200%
        cpuset_size × 100          # 100%
    ) = 100%

最终分配：
    - vCPUs = 1
    - CPUCap = 100%

4.4 工作流程示例

4.4.1 容器启动流程

1. Kubernetes创建Pod → Containerd调用CRI
2. Containerd创建sandbox → 调用MicRun SandboxService.CreateSandbox()
3. Containerd创建容器 → 调用MicRun TaskService.Create()
4. MicRun解析OCI Spec → 生成资源配置
5. MicRun调用Pedestal层 → 创建Xen domain/OpenAMP channel
6. Pedestal配置hypervisor → 启动RTOS实例
7. RTOS启动完成 → MicRun返回成功响应

4.4.2 资源更新流程

1. Kubernetes更新Pod资源 → Containerd调用UpdateContainerResources
2. Containerd通知MicRun → TaskService.Update()
3. MicRun重新计算资源映射 → 生成新的资源配置
4. MicRun调用Pedestal层 → 动态调整hypervisor资源
5. Hypervisor应用新配置 → RTOS感知资源变化

4.5 配置与注解系统

4.5.1 注解优先级

优先级：容器注解 > Pod注解 > 配置文件 > 默认值

示例注解：
org.openeuler.micrun.ped.pedestal: "xen"
org.openeuler.micrun.container.os: "zephyr"
org.openeuler.micrun.container.firmware_path: "zephyr.bin"
org.openeuler.micrun.runtime.exclusive_dom0_cpu: "true"

4.5.2 配置文件

# /etc/containerd/config.toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun."]

4.6 监控与追踪

4.6.1 分布式追踪

- 集成OpenTelemetry：实现端到端调用链追踪
- W3C Trace Context：跨进程传播追踪上下文
- 关键操作span：Create/Start/Kill/Delete/IO操作均有span记录

4.6.2 资源监控

- Xen层面：xl list、xl schecd-credit2监控domain状态
- 容器层面：ctr tasks metrics获取容器资源使用
- MicRun日志：/tmp/micrun/runtime.log记录详细运行时信息

4.7 部署与集成

4.7.1 Kubernetes集成

apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
---
apiVersion: v1
kind: Pod
metadata:
  name: rtos-pod
spec:
  runtimeClassName: micrun
  containers:
  - name: rtos-app
    image: localhost:5000/zephyr-app:latest
    resources:
      limits:
        cpu: "1000m"
        memory: "512Mi"

4.7.2 工具链集成

# 镜像构建
mica-image-builder -o zephyr-app.tar zephyr.bin

# 镜像导入
ctr image import zephyr-app.tar

# 容器运行
nerdctl run --runtime io.containerd.mica.v2 zephyr-app:latest

5. 总结

Micrun是一个创新的容器运行时，它通过以下方式弥合了传统RTOS与云原生生态之间的鸿沟：

1. 架构创新：采用shimv2架构，在现有容器生态中无缝集成RTOS
2. 资源智能映射：将Kubernetes资源模型精确转换为hypervisor资源配置
3. 混合关键性支持：实现实时系统与非实时系统的安全共存
4. 生态兼容：完全兼容Kubernetes、containerd、CNI等云原生标准

该项目代表了边缘计算和混合关键性系统容器化的重要发展方向，为工业控制、自动驾驶、物联网等领域的实时应用提供了现代化的部署和管理方案。

---关键文件路径参考：
- /home/egg/source/micrun/main.go：程序入口
- /home/egg/source/micrun/pkg/shim/shim_services.go：shim服务核心
- /home/egg/source/micrun/pkg/pedestal/xen.go：Xen hypervisor支持
- /home/egg/source/micrun/definitions/annotations.go：配置注解定义
- /home/egg/source/micrun/docs/resource-design.md：资源映射设计文档

项目状态：preview阶段，支持Xen为主要hypervisor，已实现基本容器生命周期管理，IO处理和网络支持仍在完善中。<｜end▁of▁thinking｜>

● Micrun：专为RTOS容器化设计的容器运行时

1. 项目概述

Micrun 是一个基于 containerd shimv2 的容器运行时（Container Runtime），专为 Mica项目 设计，用于在异构计算平台上将 RTOS（实时操作系统） 作为容器运行。其核心目标是将边缘计算场景中的RTOS业务接入 Kubernetes生态，实现混合关键性系统（MCS）的云化管理。

关键特性

- RTOS容器化：支持Zephyr、UniProton、LiteOS等RTOS作为容器运行
- 异构部署：通过Xen、OpenAMP等hypervisor在不同CPU核上运行RTOS
- 云原生集成：与Kubernetes生态无缝集成，支持Kubeedge管理
- 资源映射：将cgroup资源限制转换为底层hypervisor的资源分配
- 镜像分发：利用容器镜像仓库管理RTOS固件

2. 项目定位与角色

战略定位

- 边缘计算专用运行时：为边缘设备设计的RTOS容器运行时
- 混合关键性系统桥梁：支持实时系统与非实时系统共存于同一SoC
- 国产化生态组件：openEuler Embedded的重要组成部分
- 云原生扩展器：将传统RTOS业务纳入Kubernetes编排体系

解决的问题

1. RTOS云化管理缺失：边缘RTOS业务缺乏标准化管理接口
2. 镜像分发复杂：RTOS固件分发依赖传统方式，难以版本管理
3. 资源隔离不足：RTOS间缺乏有效的资源隔离机制
4. 生态集成困难：RTOS难以复用容器工具链（nerdctl、ctr等）

3. 技术框架与架构

3.1 整体架构层次

容器编排层（Kubernetes/Kubeedge）
    ↓
容器引擎层（containerd）
    ↓
shimv2接口层（MicRun Shim）
    ↓
运行时核心层（MicRun Runtime）
    ↓
hypervisor抽象层（Pedestal）
    ↓
底层hypervisor（Xen/OpenAMP/ACRN/FusionDock）
    ↓
RTOS实例（Zephyr/UniProton/LiteOS等）

3.2 核心设计原则

- 1:1:1模型：一个shim实例对应一个容器对应一个RTOS
- 资源映射一致性：保持与runc相同的cgroup语义
- 配置优先级：注解（annotation）> 配置文件 > 默认值
- 最小化依赖：静态链接Go二进制，无CGO依赖

3.3 主要组件模块

3.3.1 Shimv2层（pkg/shim/）

- shim_services.go：shim服务核心，实现containerd shimv2 API
- create.go：容器创建逻辑，处理OCI配置解析
- start.go：容器启动逻辑，协调资源分配
- shimio.go：IO处理，支持pipeIO/fileIO/ttyIO等模式

3.3.2 Hypervisor抽象层（pkg/pedestal/）

- xen.go：Xen hypervisor支持实现
- openamp.go：OpenAMP支持实现
- planner.go：资源规划器，处理cgroup到hypervisor的资源映射

3.3.3 辅助模块

- pkg/libmica/：Mica守护进程客户端库
- pkg/oci/：OCI规范处理
- pkg/micantainer/：容器类型定义
- pkg/configstack/：配置栈管理
- pkg/tracer/：OpenTelemetry分布式追踪支持

4. 详细工作架构

4.1 容器生命周期管理流程

4.1.1 创建阶段（Create）

1. containerd调用shim.Create()
2. 解析OCI配置和注解（annotations）
3. 验证RTOS镜像（firmware路径、哈希）
4. 初始化资源记录（container.me.records）
5. 准备hypervisor特定配置（Xen domain配置）

4.1.2 启动阶段（Start）

1. containerd调用shim.Start()
2. 通过libmica与micad通信
3. 创建Xen domain（分配vCPU、内存）
4. 设置CPU权重（weight）和容量（cap）
5. 建立IO通道（/dev/ttyRPMSG*）
6. 启动RTOS实例

4.1.3 停止阶段（Stop/Kill）

1. containerd发送SIGTERM（优雅停止）
2. 超时后发送SIGKILL（强制停止）
3. 清理hypervisor资源（销毁Xen domain）
4. 释放IO资源

4.2 资源映射架构

4.2.1 CPU资源映射

Kubernetes CPU Limit (limits.cpu)
    ↓
containerd转换 → OCI cpu.quota/period
    ↓
MicRun解析 → requestedCores = quota / period
    ↓
Xen映射策略：
  - vCPUs = ceil(requestedCores)     // 向上取整分配vCPU
  - cap = (requestedCores / vCPUs) × 100  // 每vCPU容量百分比
  - weight = max(1, min(shares/4, 65535)) // 调度权重

4.2.2 内存资源映射

Kubernetes Memory Limit (limits.memory)
    ↓
containerd转换 → OCI memory.limit
    ↓
MicRun解析 → memoryMB = limit / (1024×1024)
    ↓
Xen映射策略：
  - 静态内存分配：Memory = MaxMemory = memoryMB
  - 忽略swap限制（RTOS无swap概念）

4.2.3 cpuset双重约束处理

// 最终生效容量计算（遵循runc语义）
effective_capacity = min(
    (quota × 100) / period,   // quota/period转换的百分比容量
    cpuset_size × 100          // cpuset核心数 × 100%
)

4.3 IO处理架构

4.3.1 IO模式支持

- binaryIO：字节流，对应pipe/FIFO/file后端
- pipeIO：命名管道，stdin/stdout/stderr独立流
- fileIO：文件日志，直接写入日志文件
- ttyIO：终端字符IO，支持行规/信号处理

4.3.2 RTOS终端处理

本地终端（用户键盘）
    ↓
客户端pty master（nerdctl/ctr）
    ↓
shim IO转发
    ↓
/dev/ttyRPMSG*（虚拟串口设备）
    ↓
RTOS pty slave（RTOS内shell）

特殊处理：由于RTOS缺乏标准信号处理，Ctrl-C等控制字符需在客户端侧转换为容器kill操作。

4.4 网络架构

4.4.1 网络命名空间管理

- Sandbox模式：通过pause容器维护pod网络命名空间
- 直接模式：RTOS直接使用host网络（通过注解配置）
- 未来计划：迁移到containerd SandboxAPI

4.4.2 CNI集成

Kubernetes CNI配置
    ↓
containerd调用CNI插件
    ↓
创建网络命名空间（netns）
    ↓
传递给shim（通过NetNSPath）
    ↓
RTOS网络接口配置（通过micad）

4.5 配置管理系统

4.5.1 配置来源优先级

1. 容器注解（Annotations）：最高优先级，通过-l选项传递
2. 配置文件：/etc/containerd/config.toml中的runtime配置
3. 镜像标签（Labels）：OCI镜像metadata中的配置
4. 默认值：代码中定义的硬编码默认值

4.5.2 关键注解示例

annotations:
  org.openeuler.micrun.container.os: "zephyr"
  org.openeuler.micrun.container.firmware_path: "zephyr.bin"
  org.openeuler.micrun.ped.pedestal: "xen"
  org.openeuler.micrun.runtime.exclusive_dom0_cpu: "true"
  org.openeuler.micrun.container.auto_close: "true"

5. 技术栈与依赖

5.1 核心依赖

- 语言: Go 1.22+（静态链接，无CGO）
- 容器运行时接口: containerd shimv2 API (v1.7.27)
- 通信协议: ttrpc（轻量RPC）
- hypervisor支持: Xen（主要）、OpenAMP、ACRN、FusionDock
- RTOS支持: Zephyr、UniProton、LiteOS
- 追踪系统: OpenTelemetry（分布式追踪）
- 构建系统: Yocto（openEuler Embedded）

5.2 工具链集成

- 镜像构建: mica-image-builder（Python工具）
- 容器工具: nerdctl、ctr（containerd原生工具）
- 集群管理: K3s/Kubernetes + Kubeedge
- 监控调试: xl/xentop（Xen）、journalctl（日志）

6. 部署与使用场景

6.1 典型部署架构

云侧控制平面（Kubernetes）
    ↓
边缘节点（openEuler Embedded + Kubeedge）
    ↓
混合关键性系统（Linux + RTOS）
    ↓
MicRun运行时（containerd shim）
    ↓
Xen hypervisor
    ↓
RTOS容器（业务应用）

6.2 使用流程

1. 构建阶段: 使用yocto构建包含MicRun的openEuler Embedded镜像
2. 镜像准备: 通过mica-image-builder创建RTOS容器镜像
3. 运行时注册: 在containerd中注册MicRun作为runtime
4. 容器运行: 通过nerdctl/ctr运行RTOS容器
5. 集群管理: 通过Kubernetes RuntimeClass管理RTOS Pod

6.3 适用场景

- 工业控制: 实时控制任务容器化部署
- 车载系统: 混合关键性车载软件管理
- 边缘AI: AI推理与实时控制任务共存
- 物联网网关: 多协议适配与实时数据处理

7. 项目状态与发展方向

7.1 当前状态（Preview阶段）

- ✅ 基础shimv2接口实现
- ✅ Xen hypervisor支持
- ✅ CPU/内存资源映射
- ✅ 基本IO处理
- 🔄 网络命名空间优化中
- 🔄 分布式追踪集成中
- 🔄 性能优化进行中

7.2 未来发展方向

- 架构演进: 从MicRun+Micad向MicaShim+OCIMicad模型迁移
- 功能增强: 快照支持、热迁移、安全增强
- 生态扩展: 更多RTOS和hypervisor支持
- 性能优化: 减少goroutine泄漏，优化资源管理

8. 总结

Micrun 代表了将传统RTOS系统引入云原生生态的重要技术尝试。通过创新的资源映射机制和shimv2架构，它在保持RTOS实时特性的同时，为其赋予了容器化带来的部署、管理和分发优势。作为openEuler Embedded混合关键性系统的关键组件，Micrun为边缘计算场景提供了独特的价值主张，填补了RTOS与云原生生态之间的技术鸿沟。

核心价值主张: 让RTOS享受容器化便利，同时保持其实时性和确定性特性。
