# MicRun 内部设计文档

本目录面向开发者，描述 MicRun 当前实现边界以及下一阶段重实现目标。

## 推荐阅读顺序

1. [architecture.md](./architecture.md)：当前分层、架构特性、已知边界与后续方向（含目标态差量）。
2. [state-management.md](./state-management.md)：`StateStore`、runtime snapshot、恢复链路、legacy 回退。
3. [io-system.md](./io-system.md)：FIFO / TTY / copier / attach 语义（用户可见的停止/离开结局以[生命周期语义](../user/lifecycle-semantics.md)为权威）。
4. [pedestal-architecture.md](./pedestal-architecture.md)：pedestal 抽象、平台 bootstrap、显式 host 绑定与收敛方向。
5. [sandbox-validation.md](./sandbox-validation.md)：shim 崩溃后的 sandbox 校验与清理。
6. [concurrency.md](./concurrency.md)：并发模型——锁清单与层级顺序、生命周期门控、goroutine 契约、历史违例。
7. [task-state-machine.md](./task-state-machine.md)：任务状态转移表、每条边的语义与写入契约。
8. [micad-protocol.md](./micad-protocol.md)：libmica 线缆协议——create_msg 布局、控制命令、超时与存活性判定。
9. [testing.md](./testing.md)：测试分层、make ci 本地验证门、回归测试红-绿手册、覆盖基线。
10. [logging.md](./logging.md)：日志分层与调试方法。
11. [贡献指南](contribution-guide.md)：红-绿纪律、严重度分级、提交前检查。
12. [可靠性架构](reliability.md)：缺陷模式分类与防复发机制、修复者 checklist。

## 流程图导航

图集集中在 [docs/README.md](../README.md#流程图速览)，与本目录文档的对应：

| 图示 | 主题 | 相关文档 |
|------|------|---------|
| [架构总览](../README.md#架构总览) | 当前分层与主干入口 | architecture.md |
| [分层运行时](../README.md#分层运行时) | 五层职责与数据流 | architecture.md |
| [UniProton on Xen 主路径](../README.md#uniproton-on-xen-主路径) | dom0/domU 拓扑与通信 | architecture.md |
| [IO 链路](../README.md#io-链路) | attach 客户端到 RTOS shell 的数据通路 | io-system.md |
| [IO 交互细节](../README.md#io-交互细节) | run -it / attach / detach / Ctrl-C / exit 时序 | io-system.md |
| [恢复与校验](../README.md#恢复与校验) | shim 重启后的恢复与清理分支 | sandbox-validation.md |
| [状态恢复决策](../README.md#状态恢复决策) | stale 判定的决策路径 | sandbox-validation.md |
| [配置与资源控制](../README.md#配置与资源控制) | 配置来源优先级与资源规划流 | architecture.md |

## 当前实现边界

内部文档统一使用以下代码路径：`internal/transport/shimv2`、`internal/application/*`、
`internal/domain/container`、`internal/adapters/*`、`internal/ports/*`。
