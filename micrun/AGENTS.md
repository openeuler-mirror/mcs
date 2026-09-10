# MicRun Agent 指引

本文件是 `micrun/` 目录内给 Codex、OpenCode、Claude Code 等 coding agent
共享使用的工程说明。后续只维护这一套入口；当架构、测试入口或构建假设变化时，
同步更新本文件和相关文档。

## 项目定位

MicRun 是面向 RTOS workload 的 `containerd` shim v2 runtime。用户可以通过
`ctr`、`nerdctl` 和 K3s 管理 UniProton、Zephyr 等 RTOS 镜像，实际 RTOS
实例生命周期由 `micad + Xen` 管理。

最重要的功能路径是 UniProton 镜像生命周期和用户交互：

- 将 RTOS 镜像导入 containerd
- 通过 `io.containerd.mica.v2` 运行镜像
- attach/detach，并交换 shell 输入输出
- 在 K3s 中通过 `RuntimeClass=micrun` 运行同一 workload
- 稳定清理 containerd task、MicRun state 和 Xen domain

## 优先阅读

- `README.md`：项目总览和当前架构草图
- `docs/user/lifecycle-semantics.md`：容器生命周期与 IO 会话语义（**权威口径**，其他文档涉及这些语义一律以它为准）
- `docs/internals/architecture.md`：分层 runtime 架构
- `docs/internals/contribution-guide.md`：贡献与评审指南（代码风格、修改边界、提交拆分）
- `docs/quick-start.md`：构建、QEMU、镜像和 K3s workflow
- `tests/README.md`：稳定测试入口和环境变量约定
- `tests/k3s/README.md`：K3s 单节点、云边和 attach 测试
- `skills/micrun-qemu-build/SKILL.md`：面向 agent 的构建/QEMU workflow 记录
- `skills/qemu-quickstart-debug/SKILL.md`：QEMU 起机与分层排障 workflow

## 架构边界

保持 package 边界清晰：

- `main.go` 只选择 shim 名称，并把启动流程交给 bootstrap 层。
- `internal/bootstrap` 负责早期命令处理和进程启动。
- `internal/transport/shimv2` 适配 containerd shim v2 API。
- `internal/application` 负责 service graph 装配和用例服务。
- `internal/domain/container` 负责 sandbox/container state 和校验规则。
- `internal/ports` 定义 application/domain 层依赖的接口。
- `internal/adapters` 实现 config、state、IO、guest、hypervisor 等 port。
- `internal/support` 放置小型通用 helper，不承载领域职责。

不要重新引入 service locator 或隐藏全局依赖。依赖应通过显式 option、
constructor 或现有 service graph 传递。

## 构建与测试

除非命令特别说明，否则从 `micrun/` 目录执行。

**提交前必须 `make ci` 全绿**（gofmt + vet + build + 单元测试 + race 检测）。
测试分层、红-绿回归测试手册、覆盖基线见 `docs/internals/testing.md`；
并发规则见 `docs/internals/concurrency.md`；状态机见
`docs/internals/task-state-machine.md`。

```bash
make ci           # 提交前本地验证门（必跑）
go test ./...
make build
BUILD_ARCH=arm64 make build
```

本地 `make build` 产物在 `micrun/builds/`，由子项目自己的 `.gitignore` 忽略。

稳定测试入口：

```bash
tests/bin/test-qemu-smoke
tests/bin/test-qemu-lifecycle
tests/bin/test-io-qemu
tests/bin/test-k3s-cloud-edge
tests/bin/test-k3s-interaction
tests/run_all_tests.sh k3s

# Re-check one scenario on a live guest (no smoke/rebuild/import):
IMAGE_PROFILE=shell tests/bin/test-io-qemu --reuse --case 5
tests/bin/test-qemu-lifecycle --reuse --case auto-close
```

使用 `go fmt ./...` 格式化 Go 代码。默认构建模式是 vendor mode；除非任务明确
要求依赖调整，不要切换 dependency mode，也不要更新 `vendor/`。

## QEMU 与 Rootfs 规则

标准 QEMU 测试必须直接使用构建产物：

```bash
openeuler-image-qemu-aarch64-*.rootfs.cpio.gz
```

不要把它重命名为 `rootfs.cpio.gz`。标准化测试不得解包、修改或重新打包
rootfs。可以在运行中的 guest 内做运行态准备，但记录时必须明确这是 guest
runtime preparation，不能把修改后的 rootfs 当作新测试基线。

QEMU/K3s 网络示例统一使用 `EDGE_IP`、`HOST_TAP_IP`、`CLOUD_IP`、
`TEST_REMOTE_HOST`、`QEMU_OUTPUT_DIR` 等泛化变量。不要提交本机绝对路径、
密码、私钥、access token 或仅适用于某个实验环境的值。

## K3s 规则

K3s 应来自构建好的 rootfs，通常是 `/usr/bin/k3s`。标准测试路径中不要在 QEMU
guest 内安装或复制 K3s binary。

QEMU 场景优先使用云边验证：

- 本机 Docker 启动 K3s server
- QEMU guest 作为 edge agent 加入集群
- edge 使用 rootfs 内的 K3s 和系统 containerd
- RTOS Pod 通过 `RuntimeClass=micrun` 运行

`tests/bin/test-k3s-cloud-edge` 默认会删除测试 Pod，并验证相关 edge
containerd task 和 Xen domain 已清理。只有在需要保留现场调试时，才临时设置
`K3S_E2E_KEEP_POD=true`。

`tests/bin/test-k3s-interaction` 是主要用户交互检查。它应通过
`kubectl attach -i` 进入 UniProton shell，并验证 edge task、Xen domain 和
清理行为。

## 编码约定

- 变更范围应收敛在任务点名的 package 或 workflow 内。
- 优先使用现有 helper 和 interface，不要轻易新增抽象。
- error 和 log 应尽量结构化；必要时说明 operation、container/sandbox ID、
  namespace 和外部依赖。
- 日志要同时便于用户和后续 agent 理解：说明哪个操作失败、涉及哪个资源、
  做了什么 cleanup 或 retry。
- 保持 stopped-task 和 recovery 语义。只通过正常路径、但留下 Xen domain
  或 containerd task 残留的修复不算完成。
- 修改 lifecycle、IO、recovery、K3s 或 cleanup 行为时，应同步新增或更新测试。

## 防御性编程原则

防御性代码（nil guard、overflow clamp、bounds check）本身不是坏东西，
但没有节制的防御会让代码膨胀、掩盖真实逻辑。遵循以下规则：

1. **必须有可达触发路径**。每个防护必须能给出具体的输入值和执行路径
   来说明它如何被触发。如果只能说"理论上可能"而无法构造场景，不加。

2. **不重复上游保证**。Go 运行时、syscall 语义、接口契约已经保证的路径，
   不再加防护。例如 `write(2)` 在 `O_NONBLOCK` 下不返回 `(n>0, EAGAIN)`，
   就不需要为这个组合加 partial-write 处理。

3. **复杂度对称**。防护代码的复杂度不应超过被防护逻辑。用 3 行 nil guard
   保护 5 行业务逻辑是合理的；加 20 行防御来保护 1 行代码不是。

4. **区分三个层次**：
   - **活跃 BUG**：有可达触发路径 + 用户可见影响 → 必须修复
   - **防御性加固**：外部/不可信边界的边界保护 → 可选，归 `harden` 提交
   - **不可达理论问题**：无法构造触发场景 → 不加代码，记录为已知限制

5. **不为工具报告而加代码**。静态分析/AI 扫描报告的"问题"如果不能给出
   可达触发路径，不据此添加防御代码。先验证，再决定。

## 修改可接受性

判定细则、严重度分级（P0/P1/P2）与"修复价值 × 修复风险"矩阵见
`docs/internals/contribution-guide.md` §1；触碰下列运行时不变量的修改按其
§5 处理（需等强度替代 + 单独提交）：任务状态机单一收口、生命周期两级门控、
guest 存在性双源策略、持久化单一文档、goroutine panic 隔离、外部命令有界。

| 可接受 | 需谨慎 | 不可接受 |
|---|---|---|
| 修复活跃 BUG（有触发场景+用户影响） | 防御性加固（仅在外部边界） | 为工具报告加防御（无可达路径） |
| 性能优化（有量化依据） | 接口行为变更（需评估兼容性） | 修改 vendor 组件内容（只允许整组件增删） |
| 死代码清理（无引用代码移除） | 新增抽象层（需证明现有接口不足） | 引入 service locator / 全局可变状态 |
| 简化逻辑（减少分支/降低复杂度） | 状态机语义变更 | 不带测试的 lifecycle/IO/cleanup 改动 |

## 提交规范

- **commit message 用英文**；PR 描述可用中文。这是本仓的约定。
- fix 提交按子系统拆分（每个 ≤30 文件），不混装多个不相关子系统。
  参见 `docs/internals/contribution-guide.md` 的评审指南。
- commit body 说"修了什么 + 为什么 + 触发场景"，不描述扫描/调试过程。
- 防御性加固归 `harden:` 前缀，与真实 bug fix（`fix:`）分开。
- vendor 改动只能整组件增删 + `modules.txt`，不改组件内文件。
- 不提交本机路径、密码、token 或仅适用于某个实验环境的值。

## 文档与 Skills

workflow 假设变化时，同步更新相关的人类文档和 agent 文档：

- `docs/quick-start.md`
- `tests/README.md`
- `tests/k3s/README.md`
- `skills/micrun-qemu-build/SKILL.md`
- `skills/qemu-quickstart-debug/SKILL.md`

使用 `<build-dir>`、`<guest-root-password-if-needed>`、
`<path-to-qemu-output-test-dir>` 等泛化占位符。不要把本机路径或凭据写入提交的
docs、skills、logs 或 examples。

## Git 工作流

遵循当前分支历史。如果一个 PR 已经把 feature、docs、skills、tests 拆成不同
提交，后续同类修改应折进对应提交，不要散落成新的后续提交。提交描述
不要描述临时 PR 修复过程，应描述评审者最终看到的功能表现。若周边
提交使用 `Signed-off-by`，继续保持一致。
