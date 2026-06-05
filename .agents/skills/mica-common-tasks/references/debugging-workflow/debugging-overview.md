# 调试类任务工作流

## 1. 文档目标

这篇文档是 MICA 调试工作流的整体入口页，负责把失败、异常、性能下降或跨层黑盒问题分流到合适的诊断路径。

它不展开具体实现细节，只负责建立调试分流关系、入口顺序和输出要求。

## 2. 问题分类

遇到一个 MICA 故障时，建议先判断它更接近下面哪一类：

1. 生命周期问题
   - 例如 `create`、`start`、`stop`、`remove`、再次 `start` 的状态转换不符合预期

2. 服务问题
   - 例如实例已经 `Running`，但服务没有出现、没有 ready，或者 endpoint 看起来 ready 但服务仍不可用

3. 边界问题
   - 例如调用链在 MICA、OpenAMP、pedestal、RTOS/client 之间变黑盒，无法继续从当前层解释

4. libmetal / 平台抽象问题
   - 例如共享内存访问、地址转换、cache 语义、I/O region 语义异常

## 3. 日志位置确认

调试开始前应先确认日志实际输出位置。不同开发板和发行环境可能使用 busybox syslog、systemd-journald、rsyslog 或其他 syslog 配置，MICA、micad、kernel 与底层 backend 的日志不一定都在同一个文件里。

常见观察位置包括：
- `mica` 命令的直接输出
- `/var/log/messages`
- `/var/log/syslog`
- `journalctl`
- `dmesg`
- 开发板镜像或 syslog 配置指定的其他文件

注意事项：
- micad 或错误提示中写出的日志路径可能是硬编码提示，只能作为线索，不能当成完整日志来源
- 用户只提供 CLI 输出时，通常不足以判断根因，应继续要求补充 syslog、kernel log 或 journald 中对应时间段的日志
- 分析日志时要同时关注 Linux/master 侧、kernel/backend 侧以及 RTOS/client 侧输出

## 4. 调试入口顺序

处理调试类任务时，优先按下面顺序进入：

1. 日志位置确认
   - 先确认当前环境日志写到哪里，避免只依赖单一路径或 CLI 输出

2. `boundary-diagnosis.md`
   - 判断问题首先落在哪一层，以及是否已经跨到 OpenAMP / libmetal / pedestal 边界

3. 当前调试工作流目录
   - 选择 lifecycle、service、OpenAMP/libmetal 边界或 libmetal 方向

4. 对应 domain skill
   - 生命周期：`../../../mica-lifecycle/SKILL.md`
   - 通信：`../../../mica-communication/SKILL.md`
   - pedestal：`../../../mica-pedestals/SKILL.md`
   - Linux/master：`../../../mica-linux-master/SKILL.md`
   - RTOS/client：`../../../mica-rtos-client/SKILL.md`

## 5. 阅读分流

如果已经能判断问题类别，建议直接进入对应文档：

- 生命周期问题：`lifecycle-diagnosis.md`
- 通信与服务问题：`communication-diagnosis.md`
- MICA / OpenAMP / 下层边界问题：`boundary-diagnosis.md`

如果还无法判断问题归属，就先根据第一个稳定可观察症状做分流，而不要一开始同时展开多条排查链。

## 6. 输出要求

调试类回答应包含：

- 失败阶段或归属层
- 日志来源与证据完整性
- 关键证据
- 排除项
- 下一步验证动作
- 继续下钻的文档或代码位置

生命周期失败输出应包含：
- 失败阶段
- 当前状态
- 最可疑模块
- 下一跳代码或文档

服务与通信失败输出应区分：
- lifecycle running
- RPMsg device ready
- endpoint ready
- service ready
- 业务语义可用

## 7. 常见误判

### 7.1 单一子系统归因误判

很多 MICA 故障并不是单一模块 bug，而是生命周期、transport、service runtime 或平台语义共同作用的结果。

### 7.2 看到 `Running` 就停止下钻

`Running` 只说明生命周期状态进入运行态，不等于服务 ready，也不等于业务链路闭合。

### 7.3 在当前仓解释不通时仍停留在同一批文件

当调用链已经明显跨到 OpenAMP、libmetal 或 pedestal 实现层时，应继续顺着边界下钻，而不是在原层重复阅读。

### 7.4 只看单一日志位置

不同系统环境下日志位置可能不同。只看 CLI 输出、只看 `/var/log/messages`，或只按错误提示里的硬编码路径取日志，都可能漏掉关键证据。

## 8. 建议继续阅读

- `boundary-diagnosis.md`
- `lifecycle-diagnosis.md`
- `communication-diagnosis.md`
