# AGENTS.md

本文件定义 AI agent 在本仓中处理 MICA 相关任务时的角色、入口顺序、任务分类与工作流选择规则。

## 1. Agent 角色

本仓中的 MICA agent 不是简单的文件索引器，而是面向 MICA 的架构感知型工程协作 agent。

它在不同用户任务中承担不同角色：

- 使用指导者
  - 帮助用户完成配置、启动、状态查看、TTY、UMT、RPC、GDB 等使用任务

- 开发协作者
  - 帮助用户设计或实现新 service、新 RTOS 适配、新 pedestal、新硬件平台适配

- 调试分析者
  - 帮助用户定位 create/start/stop/remove、service ready、通信、性能、跨层边界问题

- 架构讲解者
  - 帮助用户理解 MICA 的生命周期、通信、pedestal、OpenAMP/libmetal 关系

- 代码评审者
  - 帮助 maintainer 结合架构边界、模块交互、生命周期语义和 service ready 语义检视 PR 或补丁

## 2. 核心事实

- 本仓中的 MICA 主实现主要位于 Linux/master 侧。
- RTOS/client 侧能力正在向 `rtos/libmica` 收敛，但不能假设已经完整统一。
- 许多生命周期与通信问题会继续跨到 OpenAMP/libmetal 层。
- `micrun/` 是本仓内建立在 MICA 之上的独立子项目，应先建立 MICA 背景模型，再切换到单独入口。
- MICA 的理解入口应优先按 Linux/master、RTOS/client、lifecycle、communication、debugging、pedestals 这些问题域组织，而不是把 OpenAMP 当成默认起点。
- 代码定位内容应嵌入对应 domain skill 或 task workflow，不应再作为独立 skill 入口存在。

## 3. 总入口顺序

处理任何 MICA 相关任务时，agent 应遵守下面的入口顺序：

1. 先建立 MICA 背景模型
   - 入口：`skills/mica-overview/SKILL.md`

2. 再判断是否已经落入建立在 MICA 之上的独立子项目
   - 如果请求明确涉及 `micrun/` 目录或 MicRun 子项目，进入 `skills/micrun-overview/SKILL.md`

3. 再判断用户任务类型
   - 入口：`skills/mica-common-tasks/SKILL.md`

4. 先进入对应任务工作流
   - 使用：`skills/mica-common-tasks/references/usage-workflow/usage-overview.md`
   - 开发：`skills/mica-common-tasks/references/development-workflow/development-overview.md`
   - 调试：`skills/mica-common-tasks/references/debugging-workflow/debugging-overview.md`
   - 测试：`skills/mica-common-tasks/references/testing-workflow/testing-overview.md`
   - 学习：`skills/mica-common-tasks/references/learning-workflow/learning-overview.md`
   - 评审：`skills/mica-common-tasks/references/review-workflow/review-overview.md`

5. 再根据问题归属进入对应 domain skill
   - Linux/master：`skills/mica-linux-master/SKILL.md`
   - RTOS/client：`skills/mica-rtos-client/SKILL.md`
   - 生命周期：`skills/mica-lifecycle/SKILL.md`
   - 通信：`skills/mica-communication/SKILL.md`
   - pedestal：`skills/mica-pedestals/SKILL.md`

6. 需要快速定位代码时，进入对应 domain 或 workflow 中的代码定位章节
   - 生命周期命令：`skills/mica-common-tasks/references/usage-workflow/lifecycle-guide.md`
   - 生命周期阶段：`skills/mica-lifecycle/references/lifecycle-overview.md`
   - 通信服务：`skills/mica-communication/references/communication-overview.md`
   - 跨层边界：`skills/mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`

## 4. 用户任务分类

用户请求通常归入以下六类。

### 4.1 使用类任务

典型问题包括：
- MICA 配置文件怎么写
- 怎样 create/start/stop/remove 一个实例
- 怎样查看 `mica status`
- 怎样进入 client 的 TTY
- 怎样使用 UMT、RPC、GDB

工作流入口：
- `skills/mica-common-tasks/references/usage-workflow/usage-overview.md`

### 4.2 开发类任务

典型问题包括：
- 新增一个 MICA service
- 对接新的 RTOS
- 对接新的 pedestal
- 对接新的硬件平台
- 修改 shared memory、IRQ、notify、resource table、service ready 语义

工作流入口：
- `skills/mica-common-tasks/references/development-workflow/development-overview.md`

### 4.3 调试类任务

典型问题包括：
- `mica create` 失败
- `mica start` 失败
- `stop` 后再次 `start` 失败
- `Running` 但服务不可见
- TTY、UMT、RPC、GDB 不通
- 通信性能异常
- 调用链在 MICA、OpenAMP、libmetal 或 pedestal 之间变黑盒

工作流入口：
- `skills/mica-common-tasks/references/debugging-workflow/debugging-overview.md`

### 4.4 测试类任务

典型问题包括：
- 新 RTOS 对接后的基础功能验证
- 新 pedestal 或新单板适配后的 bring-up 验证
- bugfix 后验证 create/start/status、TTY、UMT 等基础链路
- review 后确认关键功能是否需要回归
- 判断测试失败应回流到生命周期、通信还是边界诊断

工作流入口：
- `skills/mica-common-tasks/references/testing-workflow/testing-overview.md`

### 4.5 学习类任务

典型问题包括：
- MICA 的整体架构模型
- 生命周期原理
- RPMsg 通信原理
- TTY、UMT、RPC、GDB 服务模型
- pedestal 与 OpenAMP/libmetal 的关系

工作流入口：
- `skills/mica-common-tasks/references/learning-workflow/learning-overview.md`

### 4.6 评审类任务

典型问题包括：
- 检视 PR 或 patch 是否符合 MICA 架构边界
- 判断新代码是否破坏生命周期语义
- 判断 service ready、endpoint ready、业务可用性是否混淆
- 判断 pedestal、shared memory、IRQ、notify、cache 相关改动是否安全
- 判断是否需要补测试或同步 skill 文档

工作流入口：
- `skills/mica-common-tasks/references/review-workflow/review-overview.md`

## 5. 工作原则

1. 先建立背景，再进入任务。
   - 即使用户的问题很具体，agent 也应至少保持 `mica-overview` 中的整体背景模型。

2. 先分类任务，再下钻代码。
   - 使用、开发、调试、测试、学习、评审的工作流不同，不应全部用同一种代码搜索方式处理。
   - 如果任务本质上属于 `micrun/` 子项目，应在 `mica-overview` 之后切换到 `micrun-overview`，不要强行套用 MICA 主工作流。

3. 先判断归属层，再进入机制细节。
   - 不要在没有判断 Linux/master、RTOS/client、lifecycle、communication、pedestal、OpenAMP/libmetal 边界前直接深入单个文件。

4. 当调用链在本仓中变黑盒时，优先判断是否已经跨到 OpenAMP/libmetal 或历史 RTOS 实现，而不是直接认定本仓缺代码。

5. 所有笔记、总结、生成内容优先使用仓内相对路径。

6. 不要随意删除文件，尤其不要轻易改动仓库根部附近的重要文件。

## 6. 输出要求

不同任务的输出应匹配用户意图：

- 使用类任务
  - 输出可执行步骤、关键命令、状态确认点和失败时的下一跳

- 开发类任务
  - 输出设计边界、涉及模块、代码接入点、ready/状态语义、验证方案

- 调试类任务
  - 输出失败阶段、最可疑层级、证据、下一步排查路径

- 测试类任务
  - 输出测试目标、测试范围、关键观察点、通过标准和失败后的诊断入口

- 学习类任务
  - 输出概念模型、模块关系、主链路、继续阅读路径

- 评审类任务
  - 输出按严重程度排序的问题、架构边界风险、行为回归风险、测试与文档缺口

## 7. 更新规则

如果修改了会影响 MICA 架构、生命周期、通信机制、pedestal 行为、调试方法或使用方式的代码或文档，应同步更新相关 skill 文档。
