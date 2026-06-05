---
name: mica-communication
description: 当需要理解 MICA 通信行为时使用，包括通信分层、RPMsg 机制、service binding，以及 TTY、UMT、RPC、GDB 如何跨两侧建立。
---

# MICA 通信

## 概览

当实例已经接近或进入 running 状态，并且需要理解通信层地图、RPMsg 机制、service binding 或跨两侧 service 建立过程时，使用这个 skill。

这个 skill 是通信参考文档的路由入口。概览、机制和 service 专属细节位于本目录下的 `references/*.md`。

## 使用场景

- 下钻具体机制或 service 前，需要通信层地图
- 需要理解 TTY、UMT、RPC 或 GDB 如何建立在 RPMsg 之上
- 需要区分 transport establishment、service binding 与 service runtime readiness
- 需要分析 endpoint、name service 或跨侧 service 创建时序

不适用于：
- 通信建立前的纯 lifecycle 阶段归属问题；使用 `mica-lifecycle`
- 纯 pedestal-specific transport substrate 问题；使用 `mica-pedestals`
- 归属仍不清晰的纯跨域排障；使用 `mica-common-tasks/references/debugging-workflow/debugging-overview.md`

## 相关参考

从这里开始：
- `references/communication-overview.md`
- `references/transport-foundation.md`
- `references/openamp-rpmsg.md`
- `references/services/tty-service.md`
- `references/services/rpc-service.md`
- `references/services/umt-service.md`
- `references/services/gdb-service.md`

## 阅读指导

- 如果需要在下钻具体机制或 service 前建立通信层地图，先读 `communication-overview.md`
- 如果症状仍像 shared memory、notify、interrupt、vring 或 transport-base 失败，先读 `transport-foundation.md`
- 如果问题是 virtio/RPMsg 机制、name service、endpoint ready 或 buffer lifecycle，读 `references/openamp-rpmsg.md`
- 如果只有 TTY、RPC、UMT 或 GDB 某一类 service 异常，进入对应 service 专属参考
- 如果主要问题仍是排障而不是通信结构理解，使用 `mica-common-tasks/references/debugging-workflow/debugging-overview.md`

## 常见误区

1. 把通信失败当成 lifecycle 失败。
   - 实例可以已经 running，但 service 仍然未 ready。

2. 把 RPMsg 机制问题和 service binding 问题当成同一类问题。
   - endpoint/name-service 建立与 service 对象绑定相关，但并不相同。

3. 只看一侧。
   - 很多 readiness 失败实际上是 Linux/master 与 RTOS/client 之间的时序或创建不匹配。
