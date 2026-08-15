# MicRun 交付规格（快照）

> 本文件是 MicRun 需求规格的仓内快照，作为功能交付与测试验收的基准。
> 验收标准与测试入口的映射见 `docs/internals/testing.md` 的"规格验收映射"。

需求名称：RTOS容器运行时 MicRun

## 需求背景

RTOS 工作负载需要以更通用的方式接入 Linux 侧容器生态，使用户能够通过 containerd、ctr、nerdctl 或 K3s 等标准工具管理 RTOS 实例。为降低 RTOS 工作负载分发、启动、交互和资源配置的使用门槛，需要提供面向 RTOS 的容器运行时 MicRun，将容器语义与 MICA 实例管理能力衔接起来。

## 需求描述

提供 RTOS 容器运行时 MicRun，使 RTOS 工作负载能够以容器镜像形式分发，并通过标准容器运行时接口完成创建、启动、停止、删除、状态查询和控制台交互。MicRun 作为 containerd shim v2 运行时插件运行在 Linux 宿主侧，通过 MICA 管理组件和虚拟化底座驱动 RTOS 实例运行，对上提供容器运行时兼容接口，对下对接 MICA 实例管理和通信能力。MicRun 支持 Yocto 构建集成，可通过 openEuler Embedded 的 oebuild 构建流程打包进系统镜像。

## 功能规格

1. **容器运行时接入能力**
   MicRun 支持以 containerd shim v2 运行时插件形式接入容器引擎，支持通过标准容器工具创建和管理 RTOS 工作负载。MicRun 支持通过运行时类型标识接入 containerd，并由 containerd 调用对应运行时插件。

2. **RTOS 工作负载生命周期管理能力**
   MicRun 支持 RTOS 工作负载的创建、启动、停止、删除和状态查询。用户可通过 ctr 或 nerdctl 等标准容器命令对 RTOS 工作负载执行基础生命周期操作。

3. **RTOS 镜像分发能力**
   MicRun 支持以容器镜像形式分发 RTOS 固件。RTOS 固件可随镜像导入容器引擎，并由 MicRun 在创建和启动工作负载时解析和使用。RTOS 镜像由配套的 mica-image-builder 脚本制作，脚本根据底座类型、RTOS 类型和固件文件打包生成符合规范的容器镜像。

4. **工作负载类型选择能力**
   MicRun 支持 RTOS 工作负载类型配置。用户可通过工作负载配置或注解指定 RTOS 类型，用于选择对应的固件和运行参数。

5. **资源配置能力**
   MicRun 支持将容器侧 CPU、内存等资源配置在创建（拉起）工作负载时传递到底层 MICA 和虚拟化执行环境，用于控制 RTOS 工作负载运行所需的基础资源。资源在工作负载拉起时生效。

6. **控制台交互能力**
   MicRun 支持运行中 RTOS 工作负载的控制台输入输出交互。用户可通过容器工具连接工作负载控制台，向 RTOS shell 输入命令并查看输出。

7. **K3s 接入能力**
   MicRun 支持作为 K3s RuntimeClass 对应的容器运行时接入 K3s，使用户能够通过 K3s Pod 方式启动和管理 RTOS 工作负载。

8. **配置与注解能力**
   MicRun 支持通过运行时配置和工作负载注解传递必要配置，包括 RTOS 类型、固件路径、运行时调试开关和底座配置等。具体配置项以交付版本支持的配置文件和注解为准。

## 验收标准

1. 在 openEuler Embedded 环境中，MicRun 能够作为 containerd shim v2 运行时插件完成注册，运行时类型能够被 containerd 识别。
2. 通过 ctr 或 nerdctl 使用 MicRun 运行时创建、启动、停止和删除 RTOS 工作负载，工作负载生命周期操作能够正常完成。
3. 使用配套的 mica-image-builder 脚本，根据指定底座类型、RTOS 类型和固件文件制作 RTOS 容器镜像，导入后可通过 MicRun 启动对应工作负载。
4. 通过容器工具连接运行中的 RTOS 工作负载控制台，能够输入 RTOS shell 命令（如 help、uname）并获取对应命令的响应输出。
5. MicRun 能够根据工作负载配置或注解选择 RTOS 类型（UniProton 或 Zephyr）和固件路径，并完成工作负载启动。
6. 在工作负载创建时配置 CPU、内存等基础资源，配置该资源限制的工作负载能够正常启动并进入运行状态。
7. 在 K3s 环境中，创建 MicRun 对应 RuntimeClass 后，能够通过 Pod 方式启动 RTOS 工作负载。
8. 在 K3s 场景下，能够通过 kubectl attach 连接 RTOS 工作负载，发送 shell 命令（如 help、uname）并获取响应输出。

## 约束说明

1. **运行前提**：MicRun 运行依赖 openEuler Embedded、containerd、MICA 管理组件、Linux 内核能力和对应虚拟化底座，需确保 MICA 与 Xen 的前置部署就绪。
2. **架构限定**：MicRun 的验证路径为 AArch64（ARM64）。
3. **底座限定**：MicRun 的验证路径为 Xen 虚拟化底座，宿主须通过 Xen 启动。
4. **工作负载类型**：当前支持的 RTOS 类型为 UniProton 和 Zephyr。
5. **K3s 场景**：使用 K3s 时，需先完成 K3s 环境部署并注册 MicRun 对应 RuntimeClass。RTOS 工作负载 Pod 需显式提供占位启动命令，并采用 stdin 开启、tty 关闭的方式以保证 attach 正常。
6. **网络支持**：RTOS 工作负载面向无网络配置场景。
7. **资源配置**：资源在工作负载拉起时生效。
8. **使用入口**：MicRun 不单独提供新的用户命令行入口，用户通过 containerd、ctr、nerdctl、kubectl 等标准工具使用 MicRun 能力。
