# 测试类任务工作流

## 1. 文档目标

这篇文档是 MICA 测试类任务的入口页，用于在新 RTOS、新 pedestal、新开发板、bugfix、review 或功能改动后组织基础功能验证。

测试工作流不替代开发、调试或评审工作流。它负责把改动后的验证要求收敛成可执行检查项，并在失败时转回对应诊断文档。

## 2. 适用场景

以下任务完成后应进入测试工作流：

- 新 RTOS 对接
- 新 pedestal 或新底座接入
- 新单板或新硬件平台适配
- 新 service 开发
- lifecycle、communication、pedestal 相关 bugfix
- PR 或 patch review 后的回归验证
- 文档或配置变更影响用户可见行为

## 3. 基础验证入口

通用基础验证从下面文档开始：

- `adaptation-validation.md`

该文档覆盖 resource table、`mica create`、`mica start`、shared memory、IRQ/notify、RPMsg、service ready、TTY 与 UMT 基础链路。

## 4. 失败回流

测试失败时，不应停留在测试清单本身，而应根据失败阶段回流到对应诊断入口：

- create/start/stop/remove 失败：`../debugging-workflow/lifecycle-diagnosis.md`
- Running 后服务不可用或通信异常：`../debugging-workflow/communication-diagnosis.md`
- OpenAMP/libmetal/pedestal/platform 边界不清：`../debugging-workflow/boundary-diagnosis.md`

## 5. 输出要求

测试类回答应包含：

- 测试目标
- 测试范围
- 关键命令或观察点
- 通过标准
- 失败后的诊断入口
