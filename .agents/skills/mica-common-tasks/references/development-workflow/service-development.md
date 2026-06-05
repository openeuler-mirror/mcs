# service 开发工作流

## 1. 文档目标

这篇文档用于新增或扩展 MICA service，也可用于从某个服务名出发，定位它在 Linux/master、RTOS/client 与 OpenAMP/RPMsg 三层的关键落点。

## 2. 推荐步骤

1. 确定服务类型与 endpoint 名称
2. 确认 Linux/master 侧 service 声明、match 规则与 bind callback
3. 确认 RTOS/client 侧 endpoint 创建、线程或 callback 模型
4. 确认 name service、endpoint ready 与 service ready 条件
5. 如需定位现有代码，参考 `../../../mica-communication/references/communication-overview.md` 的服务代码定位章节
6. 如需机制细节，继续看 `../../../mica-communication/references/openamp-rpmsg.md`

## 3. 输出要求
至少给出：
- Linux 可见位置
- RTOS 实现位置
- endpoint 名称
- 是否依赖 name service / ready 时序
- 是否需要更新 `mica status` 展示、使用文档和调试文档
