# 贡献与评审指南

本文档为 micrun 的贡献者和评审者提供代码风格、修改边界和评审标准的参考。
`AGENTS.md` 是 agent 和贡献者的共享入口；本文档是其补充，提供更详细的细则。

配套文档：测试流程见 `docs/internals/testing.md`；并发规则见
`docs/internals/concurrency.md`；状态机见 `docs/internals/task-state-machine.md`。

## 1. 评审 checklist：判断一个"发现"是否值得修

评审者收到 bug 报告（包括 AI 扫描/静态分析/人工发现的）时，按以下步骤判断：

1. **触发路径可达吗？** 要求提交者给出具体的输入值和执行路径。如果只能
   说"理论上可能"而无法构造场景，标记为"已知限制"，不修。
2. **有用户可见影响吗？** 崩溃、数据损坏、资源泄漏、错误行为 → 修。
   "可能影响性能"但无量化依据 → 不修，记录。
3. **修复的复杂度合理吗？** 修复代码不应比被修复的逻辑更复杂。如果修复
   需要引入新抽象/新锁/新状态机，先评估是否有更简单的方案。
4. **有回归测试吗？** **fix: 提交必须在同一提交内包含回归测试，且该测试
   在修复前失败、修复后通过（红-绿纪律，操作手册见 testing.md §3）。**
   无法写测试的 fix 必须在 commit message 写明原因和手工验证步骤。
   纯防御性加固不需要测试，但需要在 commit message 说明加固的边界场景。
5. **触碰运行时不变量了吗？** 对照本文 §5 的六条不变量逐条检查。修复
   引入了绕过不变量的新路径（直接写 status、第二状态写者、私有互斥锁、
   裸 `go`）→ 拒绝，要求改走既有收口；确需修改不变量本身的，必须给出
   等强度替代并单独成提交。

### 1.1 严重度分级

| 级别 | 定义 | 处理 |
|------|------|------|
| P0 | 崩溃/挂死/孤儿 domain/状态损坏，主流程可触发 | 立即修，最高优先评审 |
| P1 | 错误行为/资源泄漏/错误清理，特定场景触发 | 排期修，须带回归测试 |
| P2 | 边界瑕疵、日志缺失、无量化依据的性能疑虑 | 记录，攒批处理，不单独开 fix |
| 非 BUG | 触发路径不可达、上游已保证、防御性想象 | 记入已知限制，明确告知不修 |

### 1.2 修复价值 × 修复风险矩阵

"问题小但修入核心"是本仓最需要防的提交形态——修复本身引入的未知面
往往大于问题本身：

| | 修复不动不变量 | 修复触碰不变量/锁/状态机 |
|---|---|---|
| **P0/P1** | 正常修，红-绿 | 允许，但必须：单独提交 + 等强度替代 + 全量 `make ci` + QEMU 场景回归 |
| **P2** | 可修（`harden:` 前缀） | **不修**，记录为已知限制。等它升级为 P1 再动 |
| **非 BUG** | 不修，记录 | 不修，记录 |

判断口诀：**为修一个小问题而动架构，得到的不是修复而是新 BUG 的入口**。
历史数据支持这一点：AI 扫描 35 项发现中约 3 项是真问题，绝大多数修复
建议落在"非 BUG"与"P2×触碰不变量"两格。

## 2. 代码风格细则

### 错误处理

- 用 `%w` 包装需要 `errors.Is` 检查的 error；用 `%v` 包装仅用于日志展示的 error。
- sentinel error 定义在 `internal/support/errors`，类型用 `Type*` 区分
  （NotFound / AlreadyExists / Invalid / Unavailable / NotSupported）。
- 不要吞掉有用户影响的 error。best-effort cleanup（Close/Remove）的 error
  可以用 `_ =` 丢弃，但主路径的 error 必须传播或 join。
- `context.WithoutCancel` 用于必须跑完的 teardown（domain stop/delete）；
  原始 ctx 用于应尊重客户端取消的查询。

### 并发

完整规则（锁清单、层级顺序、goroutine 契约、历史违例）见
`docs/internals/concurrency.md`，此处只列高频要点：

- 共享可变状态用 `sync.Mutex` / `sync.RWMutex` 保护。锁的粒度应覆盖
  snapshot-read-check-write 的完整临界区，避免 TOCTOU。
- 慢操作（guest RPC、文件 I/O）不要持锁执行。模式：持锁快照 → 释放锁 →
  慢操作 → 持锁写回（写回时重新检查状态是否仍有效）。
- channel close 用 `sync.Once` 或单一 owner 保护。不要在多个 goroutine
  中 close 同一个 channel。
- 固定的锁顺序：`containersLock → resMu`；`lifecycleLock → containersLock`；
  `containersLock → container.stateMu`（读嵌套，StoreSandbox 快照与
  `allContainersTerminal` 均依赖此序）。不要反序获取。
- 并发修复必须先读 concurrency.md 的历史违例案例，再跑 `make test-race`。

### 注释

- 解释 **why**，不解释 **what**。代码本身能说明"做什么"，注释应说明
  "为什么这样做"——特别是修复了什么竞态、绕过了什么限制。
- 防御性代码（nil guard / overflow clamp）应注释说明它防御的具体场景。
  如果写不出场景，说明这个防护可能不需要。

### 测试

- 并发修复（锁/原子/状态机）必须有回归测试，覆盖修复前后行为差异，
  且在 `-race` 下可复现（概率性竞态用循环放大窗口）。
- 测试命名：`TestXxx` + 场景描述（如 `TestMarkTaskRunningAcceptsAlreadyRunning`）。
- QEMU/集成测试放在 `tests/`，单元测试放在对应包的 `_test.go`。
- 新增测试不带私有 build tag（矩阵见 testing.md §2）。

## 3. 提交拆分原则

**提交前必须跑 `make ci`（fmt/vet/build/test/race 全绿），流程见 testing.md §2。**

- **按子系统拆 fix**：每个 fix 提交聚焦一个功能域（如 lifecycle、IO、
  domain），评审者可独立审阅，不用通读全局。
- **目标粒度**：每个 fix 提交 ≤30 文件。超过时考虑按子目录进一步拆分。
- **不混装**：一个提交不要同时改 lifecycle + IO + config 等不相关子系统。
  如果一个文件跨多个主题（如 `container_runtime_control.go` 同时涉及
  stop/pause/signal），归入其主要所属的子系统提交。
- **提交类型前缀**：`fix:` / `refactor:` / `harden:` / `test:` / `docs:` /
  `chore(deps):`。防御性加固用 `harden:`，与真实修复分开。

## 4. 依赖管理

- vendor 模式是默认构建模式。不要切换到 module 模式，除非任务明确要求。
- vendor 目录只能整组件增删 + `modules.txt` 更新。不能修改 vendor 内的
  任何组件文件（`.go`、`LICENSE` 等）。
- 移除依赖时，确保引用该依赖的代码已改为本地替代或移除，且中间提交可编译。
- `go.mod` / `go.sum` 的改动与 vendor 删除应在同一提交（或代码先改、
  依赖后删），保证每个中间提交可编译。

## 5. 运行时不变量（评审时按此校验，修改这些机制需给出等强度替代）

这些不变量是历次竞态修复沉淀出的结构性防线，任何绕过它们的新代码都应
在评审中被拦截：

1. **任务状态机单一收口**（完整转移表与各边语义见
   `docs/internals/task-state-machine.md`）：task status 的每次写入都经过
   `shimContainer.setStatus`，由 `ports.CanTransitTaskStatus` 拒绝非法边；
   终态化只允许走 `ports.FinalizeTaskStopped`（保留已写入的 exit info，
   原子关闭 exit channel）。不要绕开状态机直接写 status 字段。
2. **生命周期两级门控**（锁层级与历史违例见 `docs/internals/concurrency.md`）：
   会“创建/重建 guest”的路径必须持有对应门控——
   同一 task id 的 Create/Start/Delete 互斥用 `claimLifecycle`（try-lock，
   busy 返回 `ContainerNotReady`/Unavailable）；跨 task 的 pod 容器
   Create/Start 与 sandbox Stop/Delete 互斥用 sandbox `lifecycleLock` +
   `notOperational` 检查。Pause/Resume/Kill/Stop/Update 不创建 guest，
   racing teardown 时依靠 `notOperational` 与状态机优雅失败，无需门控。
3. **guest 存在性策略**（micad socket 与 Xen domain 双源）：`Exists` 只看
   socket；socket 消失即控制权丢失，残留的 Xen domain 是僵尸，由下一次
   生命周期动作销毁（`Stop`/`Remove` → `destroyLingeringDomain`，或
   Down 态注册前的 `Remove`）。恢复时 socket 全部缺失但 domain 存活的
   sandbox 保留状态（`anyDomainAlive`），交由正常生命周期收敛——micad
   不支持重新接管已运行的 domain，因此 micad 重启必然意味着 workload
   重启，这是接受的行为。
4. **持久化单一文档**：容器运行时状态内嵌于 sandbox 文档，每次状态转换
   是一次原子写。不要新增独立于该文档的第二份运行时状态写入点。
5. **长生命周期 goroutine 必须 panic 隔离**：新增后台 goroutine 用
   `panicsafe.Go`（或 `defer panicsafe.Recover`），shim 是活体 domain 的
   唯一管理者，进程崩溃比丢失单个 goroutine 更糟。
6. **外部命令必须有界**：`xl`/`xenstore-read`/`systemctl` 类调用在 caller
   无 deadline 时施加默认超时（30s），不允许无界阻塞持有生命周期锁。

## 6. 已知限制（不需要修复，记录在此避免重复报告）

以下是经过评估后接受的限制，评审时不需要据此要求修改：

- 合并格式状态文档写出后不支持降级回旧版本 shim：旧版本会读到过期的
  每容器文件；跨版本降级需清空 `/run/micrun` 运行时状态。
- `pause`/`resume` 双失败（persist 失败 + rollback 也失败）后 guest 与
  状态短暂分歧：需要双重失败，`checkStateWithContext` 对部分场景可自愈。
- auto-close 定时器在 `internalKill` 进行中不复查 `IsAttached()`：已在
  kill 前加了复查，但 kill 进行中的窗口仍存在（auto-close 语义允许）。
