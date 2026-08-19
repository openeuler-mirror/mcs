# micrun 并发模型

本文是并发规则的权威文档。任何触碰锁、goroutine、状态写入的修改都必须先对照本文；
`docs/internals/contribution-guide.md` §5 的不变量 2、4 与本文互为引用。

micrun 是常驻单进程 shim，天然并发面：containerd ttrpc 并发调用（Create/Start/Kill/
Delete/Wait/Resize 可同时到达）、guest 事件流回调、IO copier goroutine、退出监视器、
恢复流程。并发错误的代价是**孤儿 Xen domain 与状态错乱**——进程崩溃或状态写坏都意味着
失去对 guest domain 的唯一管理权。

## 1. 锁清单与归属

三层锁，各自保护明确的资源。**新锁必须先在本文登记**，说明保护的资源与在层级中的位置。

### 跨层与 transport 层锁

| 锁 | 类型 | 保护对象 |
|----|------|----------|
| `deps.pathsMu` | RWMutex | `Dependencies` 的 StateStoreFactory/TTYDiscoveryRoots 换入换出（Create RPC 写 vs attach/IO 读；纯叶子锁） |
| `shimService.mu` | Mutex | shimService 共享状态（任务表等；即各 task RPC 的 `withTaskLock`） |
| `shimContainer.ioMu` | Mutex | shimContainer 的 ioManager 字段 |
| `deleteMu` | Mutex | 同容器 Stop+Delete 序列串行化 |

### Sandbox 级（`internal/domain/container/sandbox.go`）

| 锁 | 类型 | 保护对象 |
|----|------|----------|
| `lifecycleLock` | Mutex | 沙盒级生命周期互斥：Stop/Delete 与 Create/Start 的串行化 |
| `containersLock` | RWMutex | `containers` map 与 `ContainerConfigs`（容器集合的增删查） |
| `resMu` | Mutex | `resManager`（VCPU/内存池/CPUSet 资源记账） |
| `persistMu` | Mutex | 沙盒文档的"快照+落盘"临界区（防旧快照回写复活已删容器） |
| `annotLock` | RWMutex | 沙盒注解 |

`notOperational()`（`sandbox_query.go`）是生命周期状态的只读判定，与 `lifecycleLock`
配合：Stop/Delete 完成后，后续 Create/Start 在进入 guest 操作前会检查它并快速失败。

### Container 级（`internal/domain/container/container.go`）

| 锁 | 类型 | 保护对象 |
|----|------|----------|
| `stateMu` | RWMutex | 单容器 `state` 字段（状态读写入口经 `setStatus`） |
| `startMu` | Mutex | 串行化同一容器的 Start/Stop/Kill 互斥：Start 在 `ensureClientPresence + startClient` 全程持锁，Stop/Kill 进门前取同一把锁，防止 Kill 拆掉 Start 正在建的 domain、或 Start 的复查在 Kill 返回后重建出新 domain（startGuest 的失败回滚在锁内，走 `stopUnlocked` 防自死锁） |
| `exitNotifierMu` | Mutex | 退出通知器 |
| `registrationMu` | Mutex | guest 侧注册状态。有意跨 `Remove + CreateGuest` 两个慢 guest RPC（各 30s 界）全程持有，序列化同 ID 重注册、防止并发重建出第二个 domain；期间并发的 Create/Start 在此锁上排队并连带持有 lifecycleLock/startMu 等待（与 lifecycleLock 持锁跨 RPC 同类的已接受模式） |

### Service 级（`internal/application/task/service.go`）

| 锁 | 类型 | 保护对象 |
|----|------|----------|
| `lifecycleClaimsMu` | Mutex | `lifecycleClaims` 集合（RPC 级生命周期声明） |

### IO Session 级（`internal/adapters/io/session.go`）

| 锁 | 类型 | 保护对象 |
|----|------|----------|
| `s.mu` | Mutex | session 生命周期与流状态（含重启临界区） |

## 2. 锁层级顺序

获取多把锁时必须遵守以下顺序（→ 表示"持有前者时可获取后者"）：

```
lifecycleLock → containersLock → { resMu | container.stateMu(读) | annotLock }
lifecycleLock → persistMu → containersLock(读) → container.stateMu(读)
```

- `containersLock → resMu`：资源记账在容器集合临界区内读取。
- `lifecycleLock → containersLock`：生命周期操作先取沙盒级互斥，再操作容器集合。
- `containersLock → container.stateMu`：**只允许读嵌套**（StoreSandbox 快照遍历），
  不允许在持 containersLock 时执行会写状态/回调的逻辑。
- `persistMu → containersLock(读)`：持久化快照在 persistMu 临界区内完成
  （`SaveSandbox` 先取 persistMu，快照函数内取 containersLock 读锁，见 §4.3）。
  这是持久化路径**唯一**允许的获取方向：禁止在持 containersLock（尤其写锁）
  时调用 StoreSandbox / persistSandboxState，否则与上述方向构成读写死锁。

**逆向获取一律禁止**。评审时看到任何与上述方向相反的嵌套获取，直接拦截。

## 3. 核心并发模式

### 3.1 快照 → 释放 → 慢操作 → 重锁复查

慢操作（guest RPC、`xl` 命令、文件 IO、socket 往返）**绝不持锁执行**。标准模式：

```
持锁:  快照需要的最小状态（id、配置副本）
释放:  退出临界区
执行:  慢操作（可失败、可超时）
重锁:  重新检查状态仍然有效（容器没被并发删除/停止），再提交结果
```

写回时不复查是历史 BUG 的高发点：操作期间对象可能已被并发 Delete，
盲目写回等于把已清理的资源复活。

### 3.2 两级生命周期门控

同一容器的互斥操作（Start vs Delete、Resize vs TTY 重启等）由两级门控串行化：

1. **RPC 级 claim**：`claimLifecycle(id)` 以 try-claim 方式声明（task service 的
   `lifecycleClaims`），已声明的直接失败返回，不阻塞等待。
2. **沙盒级互斥**：进入 domain 层后由 sandbox `lifecycleLock` + `notOperational()`
   保证 Stop/Delete 的终态性。

新增互斥需求优先复用两级门控，**不要引入第三套自己的锁**。

### 3.3 单一写者的原子持久化

状态持久化是"合并单文档"模型（见 `docs/internals/state-management.md`）：
每次状态迁移 = 一次原子文件写。`persistMu` 把"快照 + 写盘"包成临界区，防止
并发 StoreSandbox 与 DeleteContainer 之间旧快照覆盖新状态（容器"复活"BUG 的根因）。

规则：**全仓库不允许第二个运行时状态写者**，不允许绕过 `persistMu` 直接写状态文件。

## 4. goroutine 规则

1. **长寿命 goroutine 一律 `panicsafe.Go`**（exit watcher、event forwarder、IO copier）。
   裸 `go` 只允许在"失败即退出进程"的引导路径。原因：shim 是 domain 的唯一管理者，
   一个 goroutine panic 杀掉整个进程 = 孤儿化全部 domain。RPC handler 不包——ttrpc 已隔离。
2. **channel 关闭单一 owner**：只有发送方（且唯一）能 close；多处关闭用 `sync.Once`。
   接收方关闭是 UB 级错误。
3. **ctx 取消传播**：teardown 路径用 `context.WithoutCancel` 脱离父取消
   （清理必须完成，否则留孤儿 domain），业务路径正常响应取消。
4. **事件回调不得回锁调用方**：guest 事件流回调里调用会重新获取 session/service 锁的
   方法会构成锁循环（历史案例见 §6.1）。回调需要状态时，通过参数传入快照。

## 5. 外部交互超时

guest RPC（libmica socket）默认 5s 超时，create/start/stop/remove 等重操作
使用 30s 长预算；外部命令（`xl`、`xenstore-read`、`systemctl`）单次调用
默认 30s 封顶。所有外部交互不允许无界阻塞——尤其是持生命周期锁的路径。
超时后按"目标可能已动作"处理：走状态复查收敛，不假设超时=未发生。

## 6. 历史违例案例（新增修改前对号入座）

1. **IO session 自死锁**：`RestartWithSubscriber` 的订阅 hook 在
   `startSessionLockedWithHook` 已持 `s.mu` 时调用 `eventStream()` 访问器（内部再次
   `s.mu.Lock`）。教训：**访问器方法默认加锁，在持锁区间内需要的是字段直读或显式
   无锁变体**；修复时引入的 hook 要按"回调不回锁"规则审查。
2. **deleteContainer 内调 deleteShimTask**：transport 层清理函数在
   domain 锁临界区内反向调用上层删除，违反层级与锁序。教训：删除链路单向
   上层→下层，下层只做幂等清理。
3. **旧快照回写复活容器**：并发 StoreSandbox 与 DeleteContainer 交错，无 `persistMu`
   时旧快照后写覆盖删除。教训：持久化必须"快照+写"整体互斥（§3.3）。

## 7. 评审检查点（并发类修改过一遍）

- [ ] 新锁已登记 §1，嵌套获取方向符合 §2
- [ ] 慢操作不在锁内；写回前重锁复查（§3.1）
- [ ] 互斥复用两级门控，未新增私有锁（§3.2）
- [ ] 状态写入走单一咽喉：`setStatus` / `ports.CanTransitTaskStatus` /
      `ports.FinalizeTaskStopped`（见 task-state-machine.md）
- [ ] 新 goroutine 用 `panicsafe.Go`；channel close 单 owner（§4）
- [ ] 外部调用有超时（§5）
- [ ] `-race` 下有回归测试（`make test-race`，见 testing.md §3）
