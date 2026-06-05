# pedestal 开发工作流

## 1. 文档目标

这篇文档用于指导 agent 处理新 pedestal 或现有 pedestal 扩展任务，重点是区分 pedestal、开发板适配、RTOS 适配和 OpenAMP/libmetal 机制边界。

## 2. pedestal 职责

pedestal 负责把 MICA 抽象落到具体部署底座或平台机制上，典型职责包括：

- shared memory 布局组织
- IRQ 或 notify 路径
- resource table 获取与解释
- backend 或 pedestal hooks 实现
- stop/remove/restart 资源回收语义
- 与 OpenAMP/libmetal 的衔接

## 3. 设计检查项

开发或扩展 pedestal 前，需要确认：

- Linux/master 侧是否需要新的 backend 或 `remoteproc_ops`
- RTOS/client 侧是否需要新的 pedestal hooks
- shared memory 是否与已有通信模型兼容
- notify 是否能双向闭合
- resource table 是否能在生命周期中稳定获取
- cache、I/O region、phys/virt 转换语义是否明确
- 不同 pedestal 之间是否产生不必要耦合

## 4. 关联文档

- pedestal 领域说明：`../../../mica-pedestals/SKILL.md`
- 生命周期机制：`../../../mica-lifecycle/SKILL.md`
- 边界诊断：`../debugging-workflow/boundary-diagnosis.md`
- 边界与平台语义诊断：`../debugging-workflow/boundary-diagnosis.md`
