# client 侧概览

## 1. 文档目标

这篇文档是 RTOS/client 侧的结构入口页，负责回答三个问题：
- RTOS/client 侧有哪些主要层和运行对象
- `libmica`、历史 RTOS 私有实现、service 线程、receiver、pedestal 接口分别处在哪一类职责中
- 如果问题已经落到 RTOS/client 这一侧，下一跳应该看哪篇文档

它只做结构说明和阅读分流，不重复 lifecycle、communication、pedestal 或具体 RTOS 构建方式的实现细节。

## 2. RTOS/client 侧分层

从侧视角看，RTOS/client 侧大致可以分成下面几层：

1. 初始化入口层
   - 负责建立 RTOS/client 侧 MICA 运行环境
   - 可能来自 `rtos/libmica`，也可能来自历史 RTOS 私有集成路径

2. service 创建层
   - 负责把 TTY、UMT、RPC 等服务线程真正拉起来

3. receiver / dispatch 层
   - 负责接收底层消息并把它们分发到对应 service 或处理路径

4. service runtime 层
   - 负责 endpoint、ready 状态、线程运行与回调处理

5. proxy / wait / callback 层
   - 负责需要请求/响应配对或代理调用的运行时模型

6. pedestal 接口层
   - 负责 IRQ、notify、shared memory、resource table 等底层适配能力

## 3. 关键运行对象与入口

从 RTOS/client 侧看，最关键的对象和入口通常是：

- `mica_init()`
  - 初始化 RTOS/client 侧 MICA 基础框架

- `mica_create_all_services()`
  - 把 RTOS/client 侧服务真正拉起来

- receiver 线程
  - 负责持续接收和分发消息

- service 线程
  - 负责各自 service 的 ready、收发和运行时逻辑

- endpoint / ready 状态
  - 连接 communication 层与 service runtime 层

这些入口和对象最重要的意义是：
- `mica_init()` 不等于 service ready
- service 线程拉起不等于 endpoint ready
- endpoint ready 也不等于业务运行时语义已经闭合

## 4. `libmica` 与历史 RTOS 路径边界

在当前仓里，`rtos/libmica` 更适合被理解成：
- RTOS/client 侧的可复用 MICA 抽象层
- service 创建、receiver、pedestal 适配与部分 service runtime 的收敛位置

但不要直接假设：
- 所有 RTOS 侧能力都已经完整沉到 `libmica`
- 历史 RTOS 私有实现已经与 `libmica` 完全等价
- 每个 RTOS 的 image 都能通过同一套 `libmica` CMake 命令直接构建出来

因此阅读 RTOS/client 文档时，应始终同时保持两个判断：
- 当前行为是不是已经在 `rtos/libmica` 中收敛
- 某些能力是不是仍需要参考历史 RTOS 侧实现或 RTOS 自身构建系统

更具体地说，当目标是 UniProton、Zephyr、LiteOS 或其他具体 RTOS 时，至少要继续确认：
- 当前 `libmica` 与历史 RTOS 私有 MICA 实现相比，哪些能力已经抽取出来，哪些还没有
- 平台适配是否已经迁移到当前 `libmica` 路径下的系统适配层
- 该 RTOS 的实际构建入口是 `rtos/libmica` 独立构建、RTOS 自身构建系统、Yocto 镜像构建，还是历史专用脚本
- 在假设某个特性可用之前，是否已经核对 pedestal 与平台支持状态

具体构建入口、image 来源和配置匹配关系不在本文展开，统一进入 `client-build-deploy.md`；新 RTOS 对接的设计流程进入 `../../mica-common-tasks/references/development-workflow/rtos-porting.md`。

## 5. 本目录文档分工

当前 RTOS/client 侧 references 的分工可以先这样理解：

- `client-side-overview.md`
  - RTOS/client 侧结构总图与阅读分流

- `client-module-interaction.md`
  - `mica_init()`、pedestal hooks、receiver 与 service 编排主链

- `client-build-deploy.md`
  - RTOS/client image 来源、构建边界、配置文件匹配关系

## 6. 常见观察点

如果问题首先表现为 RTOS/client 侧症状，优先看：

- `mica_init()` 是否已经完成基础初始化
- `mica_create_all_services()` 是否已经把关键服务拉起来
- receiver 线程是否已经启动并持续工作
- service 线程、endpoint 和 ready 状态是否闭合
- 问题更像 service runtime，还是更像 pedestal 适配

## 7. 阅读分流

如果问题更偏具体机制，建议直接进入对应专题：

- RTOS/client 侧模块交互：`client-module-interaction.md`
- RTOS/client image 来源与构建边界：`client-build-deploy.md`
- 生命周期问题：`../../mica-lifecycle/references/lifecycle-overview.md`
- service ready / endpoint / service runtime 问题：`../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`
- RPMsg / service bind / service 机制：`../../mica-communication/references/openamp-rpmsg.md`
- TTY / UMT / RPC / GDB 具体 service 行为：`../../mica-communication/references/services/*.md`
- pedestal / shared memory / notify / IRQ 细节：`../../mica-pedestals/references/*.md`

## 8. 建议继续阅读

- `client-module-interaction.md`
- `client-build-deploy.md`
- `../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`
- `../../mica-communication/references/openamp-rpmsg.md`
- `../../mica-communication/references/services/*.md`
- `../../mica-pedestals/references/*.md`
