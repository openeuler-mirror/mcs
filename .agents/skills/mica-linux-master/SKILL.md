---
name: mica-linux-master
description: 当需要从 Linux/master 侧理解 MICA 时使用，包括控制面分层、服务展示和 master 侧职责边界。
---

# MICA Linux Master 侧

## 概览

当问题位于 Linux/master 侧，或者需要理解控制面行为在 Linux/master 侧如何实现时，使用这个 skill。

这个 skill 是 master 侧参考文档的入口壳。更深入的流程、代码追踪和状态管理内容位于 references 文档中。

## 使用场景

- 需要把 Linux/master 侧理解为分层结构
- 需要分析 create/start/status/remove 期间 Linux 侧实例状态异常
- 需要理解 master 侧如何展示或更新 service 信息
- 需要把用户可见行为映射回 Linux/master 模块和调用流

不适用于：
- 纯 RTOS/client 侧归属问题；使用 `mica-rtos-client`
- Linux/master 阶段归属已经明确后的纯 pedestal backend 机制问题；使用 `mica-pedestals`

## 相关参考

从这里开始：
- `references/master-side-overview.md`
- `references/master-module-interaction.md`
- `references/master-status-management.md`

## 阅读指导

- 如果只知道问题在 Linux/master 侧，但还不知道归属组件，先读 `master-side-overview.md`
- 如果需要追踪 Linux/master 侧模块交互和控制流，读 `master-module-interaction.md`
- 如果主要症状是可见状态、服务列表或 Linux/master 侧状态展示，读 `master-status-management.md`

## 常见误区

1. 混淆 master 侧可见性与对端实际 ready 状态。
   - Linux/master 状态展示可能滞后于通信 ready，也可能与通信 ready 不一致。

2. 把 master 侧症状直接当成本地 bug 证据。
   - 很多 master 侧异常是 pedestal、OpenAMP 或 RTOS/client 行为的下游结果。
