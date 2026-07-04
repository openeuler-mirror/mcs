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

通用基础验证按阶段进入：

- `test-env.md`
- `smoke-test.md`
- `adaptation-validation.md`

`test-env.md` 用于确认用户已有开发板、已有 QEMU、已有 SDK 或需要 agent 自建 QEMU 环境。只有确认需要自建 QEMU 且用户级缓存目录中没有可复用缓存时，才下载 MCS QEMU 镜像包。

`smoke-test.md` 用于目标环境已可登录后的基础 smoke 验证，覆盖生命周期主链路和 `rpmsg-tty` 最小交互所代表的基础通信链路，不绑定 QEMU 或开发板。

`adaptation-validation.md` 用于新 RTOS、新 pedestal、新开发板或硬件平台适配后的更完整验证，覆盖 resource table、`mica create`、`mica start`、shared memory、IRQ/notify、RPMsg、service ready、TTY 与 UMT 基础链路。

## 4. 通用验证原则

测试工作流统一承载构建后、开发后、调试复现后和评审后的验证原则。其他 workflow 只需要说明为什么进入测试工作流，不应重复展开同一套环境和 smoke 规则。

验证前先按组件归属选择构建部署入口：

- Linux/master 侧组件构建与部署：`../../../mica-linux-master/references/master-build-deploy.md`
- RTOS/client 侧 image 来源、构建入口和配置匹配关系：`../../../mica-rtos-client/references/client-build-deploy.md`

构建部署入口确认后，统一进入：

1. `test-env.md`
   - 确认环境、SDK、目标连接、部署通道、QEMU 保留或释放策略

2. `smoke-test.md`
   - 原则上，MICA 相关代码改动完成后都应执行基础 smoke，确认基础生命周期和基础通信没有被破坏

3. `adaptation-validation.md` 或专项验证
   - 新 service、新协议、新 RTOS 能力、新 pedestal 能力、特定业务功能或平台适配，应在 smoke 通过后继续补充目标功能验证

不要按用户态、内核态或 KO 简单决定验证深度。SDK 与目标环境匹配、QEMU/开发板选择、部署方式、QEMU 是否保留等细节由 `test-env.md` 统一判断。

## 5. 任务来源分流

不同 workflow 进入测试时，应只携带任务语境和验证目标：

- 开发后验证：说明改动归属、构建产物和预期新增能力，然后进入本测试工作流
- 调试复现验证：说明原始失败症状、复现环境是否需要保留，然后进入 `test-env.md` 与 `smoke-test.md`
- 评审回归验证：说明 PR 或 patch 的风险点、基础 smoke 目标、是否需要保留环境继续查证，以及残余平台覆盖风险
- 使用类验证：如果用户是在确认改动后功能是否仍可用，应从使用步骤转入本测试工作流

## 6. 失败回流

测试失败时，不应停留在测试清单本身，而应根据失败阶段回流到对应诊断入口：

- create/start/stop/remove 失败：`../debugging-workflow/lifecycle-diagnosis.md`
- Running 后服务不可用或通信异常：`../debugging-workflow/communication-diagnosis.md`
- OpenAMP/libmetal/pedestal/platform 边界不清：`../debugging-workflow/boundary-diagnosis.md`
- SDK、部署环境或目标连接不满足：`test-env.md`

## 7. 输出要求

测试类回答应包含：

- 测试目标
- 测试范围
- 关键命令或观察点
- 通过标准
- 失败后的诊断入口
