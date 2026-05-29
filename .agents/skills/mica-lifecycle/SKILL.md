---
name: mica-lifecycle
description: 当需要理解 MICA 实例生命周期阶段时使用，包括 create、start、stop、remove、status，以及判断当前故障归属哪个阶段。
---

# MICA 生命周期

## 概览

当需要先在共享生命周期骨架中定位问题，再下钻到 pedestal、OpenAMP 或 service-specific 细节时，使用这个 skill。

这个 skill 是生命周期参考文档的入口壳。深层调用流内容位于 `references/lifecycle-*.md` 文件中，OpenAMP `remoteproc` 细节位于 `references/openamp-remoteproc.md`。

## 使用场景

- 需要理解 `mica create/start/stop/rm/status` 背后的实现路径
- 需要判断故障属于 create、start、running-status、stop 还是 remove
- 需要把 MICA 生命周期阶段映射到 remoteproc ready 和 running 状态转换
- 需要识别调用链何时离开公共 lifecycle 层并进入 pedestal 或 OpenAMP-specific 行为

不适用于：
- RPMsg 建立后的纯 service communication 问题；使用 `mica-communication`
- 纯 pedestal-specific 设备或内核接口问题；使用 `mica-pedestals`
- RPMsg 建立后的纯 communication/service 问题；使用 `mica-communication`

## 相关参考

从这里开始：
- `references/lifecycle-overview.md`
- `references/lifecycle-create.md`
- `references/lifecycle-start.md`
- `references/lifecycle-stop.md`
- `references/lifecycle-remove.md`
- `references/remoteproc-state-mapping.md`
- `references/openamp-remoteproc.md`

## 阅读指导

- 如果不确定问题属于哪个阶段，先读 `lifecycle-overview.md`
- 一旦故障明确属于 create/start/stop/remove，进入对应阶段专题文档
- 当 lifecycle 文档进入 backend `.config/.start/.shutdown` 分支时，根据情况切换到 `mica-pedestals` 或 `references/openamp-remoteproc.md`

## 常见误区

1. 混淆 lifecycle 阶段归属与底层机制归属。
   - lifecycle 告诉你系统处在哪个阶段；pedestal/OpenAMP 文档说明该阶段如何实现。

2. 把 `start` 等同于完整 service readiness。
   - `start` 可以完成，但 RPMsg/services 仍然未 ready。

3. 在定位 lifecycle 阶段前直接跳到 pedestal 文档。
   - 归属不清时，应先识别阶段；否则调试会变得嘈杂且重复。
