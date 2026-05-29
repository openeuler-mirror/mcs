---
name: mica-overview
description: 当处理使用、开发、调试、学习或评审任务前需要建立 MICA 顶层背景模型时使用，包括 Linux/master、RTOS/client、pedestal backend 与 OpenAMP/libmetal 的关系。
---

# MICA 总览

## 概览

在处理具体 MICA 任务前，使用这个 skill 作为必需的顶层背景入口。

这个 skill 应保持为背景导向的总入口。任务工作流属于 `mica-common-tasks`；更深入的追踪和 subsystem 专属行为属于 references 文档和同级 skill。

## 使用场景

- 第一次进入本仓
- 回答使用、开发、调试、学习或评审请求前，需要建立 MICA 基础模型
- 需要理解 Linux/master、RTOS/client、pedestal backends 与 OpenAMP/libmetal 的分工
- 需要选择下一步加载哪个 subsystem skill

不适用于：
- 已经明确归属到某个 subsystem 的问题

## 相关参考

从这里开始：
- `references/overview.md`
- `references/glossary.md`

外部组件源码获取规则也统一放在 `references/overview.md`。

## 阅读指导

- 优先使用这个 skill 建立所有 MICA 任务共享的背景模型
- 背景模型明确后，使用 `mica-common-tasks` 选择用户意图工作流
- 整体模型明确后，快速切换到具体 subsystem skill，不要停留在 overview 层
- OpenAMP/libmetal 是更底层机制层；当当前层无法解释行为时，再从 lifecycle、communication 或 debugging 下钻进入

## 常见误区

1. 过久停留在高层 overview 模式。
   - 架构分工明确后，应进入归属 subsystem。

2. 把 MICA 当成纯 Linux 项目。
   - 真实行为横跨 Linux/master、RTOS/client、pedestal-specific logic 和 OpenAMP/libmetal。

3. 把 OpenAMP 当成默认第一入口。
   - 大多数任务应从 lifecycle、communication、Linux/master 或 RTOS/client 开始，在机制层相关时再下钻。
