# 使用类任务工作流

## 1. 任务目标

使用类任务面向已经想直接操作 MICA 的用户，目标是给出可执行步骤、状态确认点和失败后的下一跳。

典型任务包括：
- 准备 MICA 使用环境
- 划分和预留 Linux / RTOS 侧资源
- 安装或启动 Linux 侧 MICA 组件
- 准备 RTOS 镜像和 resource table
- 编写或理解 MICA 配置文件
- 创建、启动、停止、删除实例
- 查看 `mica status`
- 使用 TTY、UMT、RPC、GDB

使用类任务回答“怎样操作已有能力”。如果用户要的是“改动后怎样证明功能没问题”，应转到 `../testing-workflow/testing-overview.md`。

## 2. 工作流入口

处理使用类任务时，优先按下面顺序建立上下文：

1. `../../../mica-overview/SKILL.md`
   - 建立 MICA 的基础背景模型

2. `../../../mica-linux-master/SKILL.md`
   - 理解命令入口、实例状态、服务可见面

3. `../../../mica-rtos-client/SKILL.md`
   - 理解 RTOS/client 侧镜像、resource table、`libmica` 和 service runtime

4. `../../../mica-communication/SKILL.md`
   - 理解 TTY、UMT、RPC、GDB 的 service 行为

5. `../debugging-workflow/debugging-overview.md`
   - 处理使用过程中的 ready、状态、通信异常

## 3. 使用路径

使用类任务按三段组织。

### 3.1 准备阶段

入口：`prepare-guide.md`

回答：
- Linux 侧需要准备哪些资源
- DTS 或 ko 参数如何预留共享内存
- Linux 侧需要哪些组件，如何确认 ko 和 `micad` 已运行
- RTOS 侧需要准备哪些内容，如何确认 resource table 和 MICA 初始化前提

### 3.2 生命周期阶段

入口：`lifecycle-guide.md`

回答：
- 配置文件怎么写
- `mica create/start/stop/rm/status/set/gdb` 怎么用
- `mica status` 怎么看
- 不同 pedestal 的关键配置项差异

### 3.3 通信阶段

入口：`communication-guide.md`

回答：
- 如何看 service 是否出现
- 如何使用 TTY
- 使用 UMT、RPC、GDB 前要确认什么
- 服务不可用时回流到哪个诊断入口

## 4. 常见失败回流

使用过程中失败时，按症状回流：

- 准备阶段资源、ko、`micad`、RTOS 镜像或 resource table 前提不满足
  - `prepare-guide.md`
  - `../debugging-workflow/boundary-diagnosis.md`

- `mica create`、`mica start`、`mica stop`、`mica rm` 失败
  - `../debugging-workflow/lifecycle-diagnosis.md`

- 实例 `Running` 但服务不可见、TTY 进不去、UMT/RPC/GDB 不通
  - `../debugging-workflow/communication-diagnosis.md`

- shared memory、IRQ、notify、cache、地址映射、pedestal 边界不清
  - `../debugging-workflow/boundary-diagnosis.md`

- 新 RTOS、新底座、新单板或 bugfix 后需要验证基础功能
  - `../testing-workflow/testing-overview.md`

## 5. 使用类输出要求

使用类回答不要只解释原理，应优先给出：

- 用户下一步可以执行的动作
- 预期能看到的状态或输出
- 异常时进入哪个调试工作流
- 如果用户是在改动后验证功能，转到 testing workflow，而不是只给使用步骤
