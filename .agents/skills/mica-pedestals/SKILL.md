---
name: mica-pedestals
description: 当需要理解 pedestal-specific MICA 行为时使用，包括 backend 选择、KO 或设备接口、共享内存布局、notify 路径和 RTOS/client 侧契约。
---

# MICA Pedestal

## 概览

当已经知道问题进入 pedestal-specific 分支，并且需要理解某个部署底座如何真正实现 MICA 行为时，使用这个 skill。

这个 skill 是 pedestal-specific 参考文档的入口壳。深层实现细节位于同级 `references/ped-*.md` 文件中。

## 使用场景

- 需要区分 baremetal、hetero、jailhouse 和 xen 之间的真实实现差异
- 需要理解 create/start/stop/remove 为什么在不同 pedestal 上表现不同
- 需要识别某个 pedestal 对应的 backend 文件、KO、`/dev/*`、ioctl 集合或共享内存模型
- 需要理解 pedestal-specific notify、poll、interrupt、event-channel 或 UIO 行为
- 需要理解给定 pedestal 对 RTOS/client 侧的要求

不适用于：
- platform branching 前的纯 lifecycle 阶段归属问题；使用 `mica-lifecycle`
- 没有 pedestal-specific 差异的纯 OpenAMP/libmetal 机制问题；根据症状使用 `mica-lifecycle`、`mica-communication` 或 `mica-common-tasks/references/debugging-workflow/debugging-overview.md`

## 相关参考

从这里开始：
- `references/pedestal-overview.md`
- `references/ped-baremetal.md`
- `references/ped-hetero.md`
- `references/ped-jailhouse.md`
- `references/ped-xen.md`

## 阅读指导

- 如果还不知道哪个 pedestal 分支归属该问题，先读 `pedestal-overview.md`
- 一旦问题依赖 backend 选择、`/dev/*`、ioctl、event path、内存划分或 RTOS contract，进入具体 `ped-*.md` 文件
- lifecycle 参考文档只用于共享阶段骨架；每个阶段如何落到具体部署底座，应使用 pedestal 参考文档

## 常见误区

1. 把 pedestal 当成轻量配置开关。
   - 实际上它选择的是一组 backend family，包含不同 `remoteproc_ops`、设备、内存规则和 peer-side contract。

2. 过早离开 pedestal 文档进入通用 OpenAMP 文档。
   - 如果问题仍依赖 KO、`/dev/*`、interrupt path 或 resource-table placement 规则，应先留在 pedestal 参考文档中。

3. 假设所有 pedestal 共用同一套绝对共享内存模型。
   - Xen 和 jailhouse 尤其需要自己的通信与地址解释模型。
