# 学习类任务工作流

## 1. 任务目标

学习类任务面向纯理解 MICA 架构和实现原理的用户，目标是建立概念模型、模块关系和主链路，而不是立即修改代码或定位故障。

典型任务包括：
- MICA 的整体架构模型
- 生命周期原理
- RPMsg 通信原理
- TTY、UMT、RPC、GDB 服务模型
- pedestal 与 OpenAMP/libmetal 的关系
- Linux/master 与 RTOS/client 的职责边界

## 2. 学习入口顺序

学习类任务优先按下面顺序进入：

1. `../../../mica-overview/SKILL.md`
   - 建立整体背景

2. 侧视角入口
    - Linux/master：`../../../mica-linux-master/SKILL.md`
    - RTOS/client：`../../../mica-rtos-client/SKILL.md`

3. 机制入口
    - 生命周期：`../../../mica-lifecycle/SKILL.md`
    - 通信：`../../../mica-communication/SKILL.md`
    - pedestal：`../../../mica-pedestals/SKILL.md`

4. 边界与调试入口
   - `../debugging-workflow/debugging-overview.md`

## 3. 原理解释分流

根据用户想理解的问题，进入对应方向：

- 生命周期原理：`../../../mica-lifecycle/references/lifecycle-overview.md`
- Linux/master 侧模块交互：`../../../mica-linux-master/references/master-module-interaction.md`
- RTOS/client 侧模块交互：`../../../mica-rtos-client/references/client-module-interaction.md`
- RPMsg 通信模型：`../../../mica-communication/references/openamp-rpmsg.md`
- TTY、UMT、RPC、GDB：`../../../mica-communication/references/services/*.md`
- pedestal 模型：`../../../mica-pedestals/SKILL.md`
- OpenAMP/libmetal 边界：`../debugging-workflow/boundary-diagnosis.md`

## 4. 学习类输出要求

学习类回答应包含：

- 概念模型
- 模块关系
- 主链路
- 关键边界
- 后续阅读路径

不要把学习类问题直接写成故障排查步骤，除非用户明确提出失败症状。
