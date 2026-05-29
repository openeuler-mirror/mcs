# Master 侧概览

## 1. 文档目标

这篇文档是 Linux/master 侧的结构入口页，负责回答三个问题：
- Linux/master 侧有哪些主要层和模块
- 控制面、生命周期、服务展示分别由谁负责
- 如果问题落在 Linux/master 这一侧，下一跳应该看哪篇文档

它只做结构说明和阅读分流，不重复 lifecycle、communication 或 pedestal 的实现细节。

## 2. Linux/master 侧分层

从侧视角看，Linux/master 侧大致可以分成下面几层：

1. 控制面入口层
   - 负责接收 `mica` 命令并转发到 `micad`

2. 守护进程控制层
   - 负责 `micad` 的 socket、listener、请求分发和客户端控制流

3. 生命周期编排层
   - 负责 create / start / stop / remove / status 这些生命周期动作的公共调度

4. 通信桥接层
   - 负责把生命周期推进到 communication 所需的 RPMsg / service 基础设施

5. 服务展示与注册层
   - 负责把 Linux/master 侧可见的 service 信息组织、注册并输出

6. backend 接口层
   - 负责向 pedestal 提供 Linux/master 侧所需的底层执行入口

## 3. 核心对象

从 Linux/master 侧看，最关键的对象通常是：

- `mica_client`
  - 实例级核心对象，承载 pedestal、remoteproc、rdev、services 等状态

- `remoteproc`
  - 生命周期与远端处理器状态对象

- `rdev`
  - 通信设备对象，连接到后续 RPMsg / service 体系

- `services`
  - Linux/master 侧已经注册和可见的 service 集合

- `debug` / `rbuf_dev`
  - 调试相关对象与展示面

## 4. 模块职责

Linux/master 侧常见模块可以先按职责理解为：

- `mica/micad/`
  - 守护进程与控制面

- `library/mica/`
  - 生命周期主逻辑

- `library/remoteproc/`
  - remoteproc 与镜像加载相关实现

- `library/rpmsg_device/`
  - 生命周期向通信桥接的接缝

- `library/include/mica/`
  - 对外接口与核心结构定义

## 5. 服务可见性与展示

Linux/master 侧的服务“可见”通常依赖两个层面：
- 底层 RPMsg / endpoint 是否真的建立
- master 侧是否已经把服务信息正确收集并展示出来

对应的关键接口通常包括：
- `mica_register_service()`
- `mica_unregister_all_services()`
- `mica_print_service()`

如果服务没有在 Linux 侧显示出来，既可能是服务本身还没建好，也可能是服务已存在但没有被正确注册或展示。

## 6. 常见观察点

如果问题首先表现为 Linux/master 侧症状，优先看：

- `micad` 控制面是否正常
- `mica status` 输出是否符合预期
- 服务注册与展示链路是否闭合
- 生命周期是否已经把系统推进到通信阶段

## 7. 本目录文档分工

当前 Linux/master 侧 references 的分工可以先这样理解：

- `master-side-overview.md`
  - Linux/master 侧结构总图与阅读分流

- `master-module-interaction.md`
  - Linux/master 侧模块交互、控制命令链与控制面分流

- `master-status-management.md`
  - `mica status`、服务列表与 Linux/master 可见面的组织逻辑

## 8. 阅读分流

如果问题更偏具体机制，建议直接进入对应专题：

- `master` 侧模块交互：`master-module-interaction.md`
- 生命周期问题：`../../mica-lifecycle/references/lifecycle-overview.md`
- 状态与服务展示逻辑：`master-status-management.md`
- service 注册与绑定语义：`../../mica-communication/references/openamp-rpmsg.md`
- pedestal / backend 细节：`../../mica-pedestals/references/*.md`

## 9. 建议继续阅读

- `master-module-interaction.md`
- `master-status-management.md`
- `../../mica-lifecycle/references/lifecycle-overview.md`
- `../../mica-communication/references/openamp-rpmsg.md`
