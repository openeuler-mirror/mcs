# 任务状态机

任务（task）生命周期的权威转移表与写入契约。代码位置：`internal/ports/task_status.go`
（转移表与判定）、`internal/transport/shimv2/shimContainer.go` 的 `setStatus`（写入咽喉）、
`ports.FinalizeTaskStopped`（终态化契约）。本文与代码同步维护：**改转移表必须同改本文，
反之亦然**。

> 本文是**实现层**转移表；用户可见的容器停止/task 记录可见性/attach-detach 结局语义
> 见[容器生命周期与 IO 会话语义](../user/lifecycle-semantics.md)（权威口径）。

## 1. 为什么状态机集中在 ports 层

历史反复出现的 BUG 类：某个操作在"无锁的 guest RPC 窗口"里被并发的查询路径或信号
路径覆盖状态——例如 Start 刚把任务置 RUNNING，并发的 Kill 分类逻辑把它写回 CREATED，
随后 Wait 永远等不到终态。逐点修补不可靠，因此：

1. **转移表只有一份**，放在 `ports`（所有层可见、零依赖）。
2. **写入只有一个咽喉**：`shimContainer.setStatus`，非法转移在这里被拒绝并告警
   （最终安全网，漏过 service 级防护的竞争写者无法腐蚀生命周期）。
3. **终态只有一条路**：`FinalizeTaskStopped` 统一完成"记录退出信息 + 置 STOPPED +
   关闭退出通道"，保证阻塞的 Wait RPC 必然解除阻塞。

## 2. 转移表

同态重写（`from == to`）一律合法（幂等空操作）。

| from \ to | CREATED | RUNNING | PAUSING | PAUSED | STOPPED |
|-----------|:-------:|:-------:|:-------:|:------:|:-------:|
| **UNKNOWN**  | ✓ | ✓ | ✓ | ✓ | ✓ |
| **CREATED**  | ✓ | ✓ | — | ✓ | ✓ |
| **RUNNING**  | — | ✓ | ✓ | ✓ | ✓ |
| **PAUSING**  | — | ✓ | — | ✓ | ✓ |
| **PAUSED**   | — | ✓ | — | ✓ | ✓ |
| **STOPPED**  | — | — | — | — | ✓（吸收态） |

## 3. 每条边的设计语义

评审时若有人想增删边，先对照这里的理由：

- **UNKNOWN → 任意**：恢复（recovery）从持久化文档回填状态，种子边。UNKNOWN 只应
  出现在恢复完成前。
- **CREATED → RUNNING**：正常 Start 完成。
- **CREATED → PAUSED**（查询收敛边）：guest 在 Start 完成前被观测到已 Suspended
  （崩溃恢复场景），查询路径把观测结果直接收敛到 PAUSED，不虚构一次 RUNNING。
- **CREATED → STOPPED**：Start 失败 / 创建即清理路径直接终态化。
- **RUNNING → PAUSING**：收到 Pause 请求，进入中间态（见下）。
- **RUNNING → PAUSED**（直接收敛边）：没有在途 Pause RPC 时，checkState 把观测到的
  Suspended guest 收敛为 PAUSED（对应"pause 已执行但未持久化"的崩溃窗口）。
- **RUNNING / PAUSING / PAUSED → STOPPED**：Kill、退出、Delete、SIGTERM 等一切终止
  路径。
- **PAUSING → PAUSED**：pause 成功提交。
- **PAUSING → RUNNING**（回滚边）：pause 协调失败，回滚到 RUNNING。PAUSING 是唯一的
  "可逆"中间态。
- **PAUSED → RUNNING**：Resume。
- **禁止 RUNNING → CREATED**（前向唯一）：containerd 语义——已启动的任务不会"未启动"。
  guest 重新注册为 Ready 不能把任务写回 CREATED，这正是老 BUG 的形态。
- **STOPPED 吸收态**：无出边。任何到达 STOPPED 之后的写入都是非法的（同态重写除外）。

## 4. PAUSING 的查询策略

PAUSING 是瞬态，表上允许查询路径把它覆盖为 PAUSED/RUNNING（收敛），但**服务层策略
是查询时保持 PAUSING 原样输出**，避免查询与在途 Pause RPC 互相打架。也就是说：
表允许的边 ≠ 服务层应当主动做的写；收敛边只给 checkState/恢复这类明确的收敛路径用。

## 5. 写入契约

### setStatus（唯一咽喉）

```
同态 → 直接返回（幂等）
非法边 → 记 Warn 日志并拒绝（安全网，不 panic）
合法边 → 写入
```

新增状态写入代码必须走 `setStatus` / service 层接口，**禁止直接赋值 status 字段**。

### FinalizeTaskStopped（终态化）

```
若未 STOPPED：
    退出时间为空才写 fallback 退出码（Kill 预写的 137 等真实退出信息必须保留）
    置 STOPPED
IOExit()（幂等；已 STOPPED 的任务也执行，保证败者路径必然关闭退出通道）
```

调用方必须已持有任务锁。**任何把任务带到 STOPPED 的新路径都必须经由
FinalizeTaskStopped**，否则 Wait RPC 可能永远阻塞——这是评审不变量
（contribution-guide §5 不变量 1）。

## 6. 测试锚点

- 转移表逐边断言：`internal/ports/task_status_test.go`
- 各操作的端到端状态断言：`internal/application/task/service_test.go`、
  `internal/application/lifecycle/service_test.go`、
  `internal/transport/shimv2/shim_container_status_test.go`
- 新增边/新终止路径：按 testing.md §3 红-绿流程，先写失败用例再改表。
