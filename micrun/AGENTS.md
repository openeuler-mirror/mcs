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
- `docs/internals/architecture.md`：分层 runtime 架构
- `docs/quick-start.md`：构建、QEMU、镜像和 K3s workflow
- `tests/README.md`：稳定测试入口和环境变量约定
- `tests/k3s/README.md`：K3s 单节点、云边和 attach 测试
- `skills/micrun-qemu-build/SKILL.md`：面向 agent 的构建/QEMU workflow 记录

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

```bash
go test ./...
make build
BUILD_ARCH=arm64 make build
```

稳定测试入口：

```bash
tests/bin/test-qemu-smoke
tests/bin/test-io-qemu
tests/bin/test-k3s-cloud-edge
tests/bin/test-k3s-interaction
tests/run_all_tests.sh k3s
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
