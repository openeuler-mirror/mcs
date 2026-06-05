---
name: mica-rtos-client
description: 当需要从 RTOS/client 侧理解 MICA 时使用，包括 libmica 定位、client 侧模块交互、service runtime 分层和 client 侧职责边界。
---

# MICA RTOS Client 侧

## 概览

当问题已经下钻到 RTOS/client 侧，并且需要理解 `rtos/libmica` 负责什么时，使用这个 skill。

这个 skill 是 RTOS/client 侧参考文档的入口壳。更深入的行为解释位于 references 文档中。

## 使用场景

- 需要理解哪些职责属于 client 侧
- 需要区分历史 in-RTOS 临时实现与当前 `libmica` 收敛方向
- 需要理解用户输入、`libmica`、pedestal hooks 和 service runtime 如何衔接
- 需要分析 RTOS/client 侧 service runtime、ready 状态或对端行为
- 需要把 Linux/master 侧观察结果与 peer-side 实现对齐

不适用于：
- 纯 Linux/master 控制面问题；使用 `mica-linux-master`
- 问题尚未明确到 client 侧前的纯底层机制归属；根据症状使用 `mica-lifecycle`、`mica-communication` 或 `mica-common-tasks/references/debugging-workflow/debugging-overview.md`

## 相关参考

从这里开始：
- `references/client-side-overview.md`
- `references/client-module-interaction.md`

## 阅读指导

- 如果只知道问题在“对端”，但还不知道归属子领域，先读 `client-side-overview.md`
- 如果关键问题是 `mica_init()`、pedestal hooks、receiver 和 service threads 如何连接，读 `client-module-interaction.md`
- 具体归属明确后，再进入 lifecycle、communication 或 pedestal 参考文档

## 常见误区

1. 长期把 RTOS/client 当成黑盒。
   - 许多 MICA ready 和通信问题无法只从 Linux/master 侧解决。

2. 混淆 `libmica` 抽象与历史私有 RTOS 集成路径。
   - 解释行为时，应把当前 `rtos/libmica` 方向和历史代码区分开。
