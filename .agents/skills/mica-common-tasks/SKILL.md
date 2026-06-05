---
name: mica-common-tasks
description: 当需要面向任务的 MICA 工作流时使用，包括使用、开发、调试、测试、学习、评审、服务追踪和配置到代码解释。
---

# MICA 常见任务

## 概览

在 MICA 基础背景模型已经明确后，如果下一步需要根据用户的具体意图选择工作流，使用这个 skill。

这个 skill 有意保持任务工作流导向。它把使用、开发、调试、测试、学习和评审请求导入可复用的 playbook；更深入的技术解释仍然放在同级 domain skill 中。

如果请求已经明确落在 `micrun/` 子项目，而不是 MICA 主工作流，应在 `mica-overview` 背景模型已经建立后改用 `../micrun-overview/SKILL.md`。

## 使用场景

- 需要回答具体用户任务，而不是只解释架构
- 需要指导 MICA 命令、配置、TTY、UMT、RPC 或 GDB 的使用
- 需要规划新 service、新 RTOS 对接、新 pedestal 或硬件平台支持
- 需要调试生命周期、service readiness、通信、性能或跨层故障
- 需要在适配、bugfix、service 开发或评审后验证基础 MICA 功能
- 需要通过学习路径解释 MICA 概念
- 需要按 MICA 架构和行为规则评审 PR 或 patch

不适用于：
- 任务选择前的基础架构导入；使用 `mica-overview`
- 没有任务语境的单层深度解释；直接使用对应 subsystem skill
- 明确属于 `micrun/` 子项目的问题；在 `mica-overview` 背景模型建立后使用 `../micrun-overview/SKILL.md`

## 相关参考

从这里开始：
- `references/usage-workflow/usage-overview.md`
- `references/development-workflow/development-overview.md`
- `references/debugging-workflow/debugging-overview.md`
- `references/testing-workflow/testing-overview.md`
- `references/learning-workflow/learning-overview.md`
- `references/review-workflow/review-overview.md`

## 阅读指导

- 如果请求明确涉及 `micrun/` 或 MicRun 子项目，在 `mica-overview` 背景模型建立后切换到 `../micrun-overview/SKILL.md`
- 使用类问题从 `references/usage-workflow/usage-overview.md` 开始
- 开发类问题从 `references/development-workflow/development-overview.md` 开始
- 故障分析或性能问题从 `references/debugging-workflow/debugging-overview.md` 开始
- 验证和回归问题从 `references/testing-workflow/testing-overview.md` 开始
- 概念学习问题从 `references/learning-workflow/learning-overview.md` 开始
- PR 或 patch 评审从 `references/review-workflow/review-overview.md` 开始
- 当请求精确匹配 start 失败、service 开发、层级分类或配置到代码解释时，使用各目录内的聚焦 workflow 文件

## 常见误区

1. 把这些任务参考文档当成 subsystem 文档的替代品。
   - 它们是入口；归属明确后，应继续进入对应 subsystem skill。

2. 在问题尚未分类前直接进入深层 subsystem 文档。
   - 归属不清时，任务 playbook 通常能更快进入正确层级。
