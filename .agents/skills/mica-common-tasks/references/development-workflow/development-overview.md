# 开发类任务工作流

## 1. 任务目标

开发类任务面向新增能力或适配能力，目标是先建立设计边界，再进入代码修改。

典型任务包括：
- 新增 MICA service
- 对接新的 RTOS
- 对接新的 pedestal
- 对接新的硬件平台
- 修改 shared memory、IRQ、notify、resource table、service ready 语义

## 2. 通用开发原则

开发类任务必须先回答下面几个问题：

- 新能力属于 Linux/master、RTOS/client、communication、pedestal、lifecycle 还是跨层能力
- 是否需要同时修改 Linux/master 与 RTOS/client 两侧
- 是否会影响 service ready、endpoint ready 或用户可见状态
- 是否会改变 shared memory、IRQ、notify、cache、地址映射或 resource table 语义
- 是否需要同步更新文档、测试和示例配置

## 3. 新 service 开发

新增 service 时，优先进入：

- `../../../mica-communication/SKILL.md`
- `../../../mica-linux-master/SKILL.md`
- `../../../mica-rtos-client/SKILL.md`
- `service-development.md`

设计时至少确认：
- Linux/master 侧 service 声明与 bind callback
- RTOS/client 侧 endpoint 名称、线程或 callback 模型
- RPMsg name service 与 endpoint ready 语义
- first message 是否属于 service 层激活
- `mica status` 是否需要展示该 service
- 使用与调试方式是否需要补文档

## 4. 新 RTOS 对接

对接新的 RTOS 时，优先进入：

- `../../../mica-rtos-client/SKILL.md`
- `../../../mica-pedestals/SKILL.md`
- `rtos-porting.md`
- `../debugging-workflow/boundary-diagnosis.md`

设计时至少确认：
- `mica_init()` 所需 `mica_config` 如何由 client OS 提供
- client OS 如何实现 `mica_sys_ops`
- `libmica` system 适配层如何接入 OS API
- OpenAMP/libmetal 是否已经可编译和运行
- receiver、service thread、同步原语和中断接口是否具备

## 5. 新 pedestal 或硬件平台对接

对接新的 pedestal 或硬件平台时，优先进入：

- `../../../mica-pedestals/SKILL.md`
- `../../../mica-lifecycle/SKILL.md`
- `board-adaptation.md`
- `pedestal-development.md`
- `../debugging-workflow/boundary-diagnosis.md`
- `../debugging-workflow/boundary-diagnosis.md`

设计时至少确认：
- shared memory 布局
- IRQ 与 notify 路径
- resource table 位置与生命周期
- backend 与 `remoteproc_ops` 或 pedestal hooks 的边界
- cache、I/O region、phys/virt 转换语义
- stop/remove/restart 后资源是否可重复进入

## 6. 开发类输出要求

开发类回答应包含：

- 设计边界
- 涉及模块
- 关键代码接入点
- 状态与 ready 语义
- 验证路径
- 文档同步要求
