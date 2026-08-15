# MicRun 可靠性架构（缺陷分类学与防御机制映射）

> 本文档回答一个问题：**为什么缺陷会逃过评审和测试进入仓库，以及用什么机制
> （而非纪律）阻止同类缺陷复发**。每个机制条目都对应至少一次真实事故；
> `scripts/lint_patterns.sh` 的每条规则都引用本文的模式编号。

## 1. 缺陷分类学（约 160 项历史缺陷的归类）

| 模式 | 典型事故 | 数量级 | 逃逸根因 | 已落地机制 |
|------|---------|--------|---------|-----------|
| P1 共享可变状态的并发窗口 | `task_manager_queries` 锁外读 containers map（fatal，shim 崩溃孤儿化全部 domain）；`Dependencies` 函数字段 Create 写/attach 读竞争 | ~40 | 裸 map/字段靠"记得加锁"纪律；`-race` 只覆盖单测负载，QEMU e2e 跑的是非 race 构建 | 访问收口到 `runtime_state.go` 单一 choke-point + lint P1 禁止裸访问；`-race` 构建 shim 的 e2e 验证（见 §3） |
| P2 错误分类靠字符串匹配 | `anyDomainAlive` 把 xl 瞬时失败当"域已消亡"删状态孤儿化存活域；Down 豁免复核用 socket-only 探测恒真，真实销毁失败被吞 | ~25 | "not found"/"does not exist" 字符串匹配在错误消息演进时静默失效；清理路径"容忍 NotFound"与"掩盖真实失败"的边界靠注释维护 | `isMissingDomainError` 隔离到既有两文件 + lint P2 禁止新调用点；新分类一律 sentinel error（`errors.Is`） |
| P3 时序假设（盲 sleep） | 用例在慢速首启 guest 上因"run 后 task 未及 RUNNING"间歇失败；auto-close 宽限期相位依赖 | ~20 | 每处自写 `sleep N` 赌时序；等待语义（轮询+退出码）没有公共原语 | 测试库强制 `guest_ec` + 轮询模式；lint P3 禁止 run 后盲 sleep |
| P4 假绿测试 | features case 3 pkill 引号转义错误（崩溃恢复用例历史上恒真）；io 套件 6 处"容器没跑也 PASS" | ~25 | 断言手写、无"测试能红"验证；`>/dev/null` 吞退出码成为惯例 | 关键回归测试全部做过红验证（修复时先证 FAIL）；假绿修复模式（正向证据断言）进入 review checklist |
| P5 语义漂移 | debug 开关三层断裂（命令行/注解全无消费方）；Pids 对未知 id 返回成功违背 containerd 契约；auto-close 文档承诺"断开后 N 秒"实现是相位随机 | ~15 | 声明（文档/注解/上游契约）到消费没有链路验证 | 注解键位与消费点一一对应的审查；契约类行为对齐 runc v2 语义并测试固化 |
| P6 修复自身引入回归 | NotFound 契约修复曾带入锁外 map 读；错误豁免修复曾带入恒真复核 | ~8 | 修复验证只测"修好了"，不测"没破坏别的"；无修复间交互审查 | 修复间交互的专项回归审查；修复 checklist 增加"我的改动触碰了哪些共享不变量" |

## 2. 已固化的门禁（机制清单）

1. **`make ci`**（既有）：fmt / vet / build / 单测（-count=1）/ -race。
2. **`make lint` 的模式门禁**（新增，`scripts/lint_patterns.sh`）：
   - P1：`internal/transport/shimv2` 内禁止 choke-point 文件之外的裸
     containers-map 访问；
   - P2：禁止新增字符串匹配式错误分类（存量隔离在 micad/control.go 与
     sandbox_loader.go，迁移到 sentinel error 后删除豁免名单）；
   - P3：shell 套件禁止 `ctr run` 后直接 `sleep`（必须 guest_ec + 轮询）。
   门禁自身经过红验证（植入违规确认每条规则都能 FAIL）。
3. **红-绿纪律的机制化**：fix: 提交的回归测试在修复前必须实际失败一次
   （contribution-guide 既有要求）；关键测试的红证明记录在提交信息。
4. **状态机集中化 + setStatus 单一写点**（既有）：非法生命周期转移在
   最终写点被拒绝（P1 的运行期兜底）。
5. **`WriteFileAtomic` + persistMu 临界区复查**（既有）：持久化半写与
   状态复活类缺陷的构造性消除。

## 3. 验证金字塔与覆盖矩阵

| 层 | 覆盖的缺陷模式 | 现状 |
|----|---------------|------|
| 单元测试（~1250 函数，-race） | P1 单测可构造的竞争、P2 分类、P5 契约 | 常态 |
| Shell 契约测试 | 测试脚本自身的语法/契约 | 常态 |
| 模式门禁 | P1/P2/P3 复发 | 常态（本次新增） |
| QEMU 三套件（lifecycle 13 / features 15 / io 15） | P3/P4/P5 的端到端 | 常态 |
| K3s 四套件（cloud-edge / interaction / OTA / single-node） | 验收 7/8 与产品路径清理 | 常态（single-node 按内存预检跳过，见下） |
| **-race 构建 shim 的 QEMU e2e** | P1 在真实负载下的残留竞争 | 交付前一次性验证 + 建议纳入发布前检查单 |
| 性能基线（benchstat + e2e 时延断言） | 性能回归无感 | 基线数据入 `docs/internals/testing.md`；见 §4 |

## 4. 性能验证面

- Go 基准（lastMicaMarker / CompressLineEndings / FilterNUL）基线记录于
  `docs/internals/testing.md`；性能敏感改动必须附带对比数据。
- 端到端关键时延（QEMU 实测，中位数）作为交付基线：Create→RUNNING、
  attach 首响应、删除收敛。数据与测量脚本模式见 testing.md §1.6。
- 已知有意的性能取舍（登记，非缺陷）：TTY 行写 300ms 节流（RTOS shell
  适配）；mica socket tx/rx 各取全额预算（名义 30s 最坏 ~65s，仅影响
  错误路径的锁持有时长）。

## 5. 长期路线（未落地项，按杠杆排序）

1. **sentinel error 全迁移**：把 P2 豁免名单里的字符串分类替换为
   `errors.Is` 可判定的类型化错误（涉及 micad 协议错误面梳理）。
2. **-race e2e 常态化**：发布前检查单纳入"race 构建 shim 跑三套件"；
   若 containerd 集成稳定可做成季度例行动作。
3. **并发测试框架化**：把"红绿验证要手工做"升级为 CI 元测试（对标注的
   回归测试自动做反向验证）。
4. **single-node 套件常态化**：当前镜像 DTB 固化 dom0 内存 1536MB，低于
   套件的 1024MB 可用内存预检而按设计跳过；待镜像侧提供更高 dom0 内存
   档位后纳入常规验证。

## 6. 修复者 checklist（PR 自查）

- [ ] 我的修复触碰了哪些共享不变量？（谁还读写这个 map/字段/事件序）
- [ ] 回归测试实际失败过一次吗？（红证明）
- [ ] 错误分类用的是 sentinel error 还是字符串？
- [ ] 测试断言了正向证据吗（容器确实运行/命令确实生效），还是只检查
      "没有坏状态"（假绿高危）？
- [ ] 时序敏感处用的是轮询等待还是盲 sleep？
- [ ] 契约行为（对 containerd/CRI 可见面）与 runc v2 语义一致吗？
