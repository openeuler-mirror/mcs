# micrun 测试体系

本文是测试体系的权威入口：测试分层、本地验证流程、为新 BUG 写回归测试的操作手册、
build tag 矩阵和覆盖基线。贡献者在提交前必须读完第 2 节并执行 `make ci`。

相关文档：

- 场景级 QEMU/K3s 测试：`tests/README.md`
- 评审流程与修复价值判定：`docs/internals/contribution-guide.md`
- 并发模型（并发修复必读）：`docs/internals/concurrency.md`
- 任务状态机：`docs/internals/task-state-machine.md`

## 1. 测试分层

micrun 没有托管 CI。强制执行的载体是 **`make ci` 本地验证门**：贡献者提交前自行运行，
评审者可以要求出示通过记录。四层测试各守一个面：

| 层 | 位置 | 守护面 | 运行方式 |
|----|------|--------|----------|
| 单元/行为测试 | `internal/**/*_test.go`（160+ 文件，与源码约 1:1） | 状态机转移、锁与并发、RPC 响应契约、协议解析、错误分类 | `make test` / `make test-race` |
| debug 变体 | `-tags debug` 下的测试（logger 双变体等） | debug 构建的行为差异面 | `make test-debug` |
| shell 契约测试 | `tests/contracts/` | 测试基础设施自身（tests/ 下脚本的契约） | `make test`（go test 的一部分） |
| 场景级 E2E | `tests/bin/*`（QEMU smoke/lifecycle/io、K3s） | 真实 guest + Xen + containerd 全链路 | 见 `tests/README.md`，按需人工触发 |

单元层的测试风格约定：

- **stdlib `testing` 为主**，表驱动 + `t.Run` 子测试；testify 仅在个别断言密集处使用。
- **mock 一律手写 fake/stub 实现 `internal/ports` 接口**（如 `stubGuestControl`、
  `fakeTask`、`fakeIOManager`）。不引入 gomock/mockery——ports 接口本身就为可 fake 设计，
  手写 fake 同时是接口易用性的活体检验。
- **行为断言而非实现断言**：测试断言"这个输入下 RPC 返回什么/状态变成什么"，
  不断言内部调用序列。重构不该翻动测试；测试翻动通常意味着测错了层。

## 1.5 规格验收映射（spec → 测试牵引）

交付规格快照见 `docs/reference/spec.md`。八条验收标准与测试入口一一对应。
"断言锚点"列指向**实际断言代码**（纸面映射 ≠ 真实断言，此列是逐条穿透
核对的结果）：

| # | 验收标准（摘要） | 验证入口与断言锚点 | 状态 |
|---|------------------|----------|------|
| 1 | shim v2 注册、运行时类型被 containerd 识别 | smoke 探针逐项断言（`test-qemu-smoke` 的 `smoke_probe_assert`：containerd `active`、`containerd-shim-mica-v2` 在 PATH、`aarch64`、`Domain-0`）；lifecycle case 10 反向断言未知运行时报 runtime-resolution 错误（精确匹配 `failed to resolve runtime`/`binary not installed`，不接受裸 `failed`）；名字契约单测 `bootstrap/shimcli/name_test.go`（`io.containerd.mica.v2` → `containerd-shim-mica-v2`）+ `bootstrap/runner_test.go`；全部 E2E 用例经 `--runtime io.containerd.mica.v2` 隐式验证 | ✅ |
| 2 | ctr/nerdctl 创建、启动、停止、删除 | `test-qemu-lifecycle`（ctr，13 用例；断言 `ctr tasks ls` 状态字段 + `xl list` domain 计数 + 前后残留，如 case 9 的 RUNNING→删除→domain=0、case 5 的循环残留=0+fd 容差）+ `test-io-qemu` Test 7（nerdctl create/start/stop/rm 全链 8 项计数断言，`tests/io/test_suite.sh`；需专门环境，且部署的是工作区 shim 而非 rootfs 内置 shim） | ✅ |
| 3 | mica-image-builder 制作镜像 → 导入 → 启动 | 镜像注解契约：`tests/contracts` 的 `TestMicaImageBuilderAnnotationKeysMatchShim`（builder 的 label manager 对 xen/baremetal × uniproton/zephyr 组合产出的注解键值必须与 shim 消费的 `annotations.go` 常量一致）；构建全流程：`skills/micrun-qemu-build`（人工流程，无自动化断言）；导入：lifecycle/features 的 `guest_has_image`（`ctr image ls` grep）；启动：由验收 2/6 的 RUNNING/domain 断言承接 | ✅（builder 侧为注解契约级；打包全流程仍为人工验证） |
| 4 | attach 输入 help/uname 得响应 | `test-qemu-features` case 15 + `test-qemu-lifecycle` case 12：断言**真实命令响应**（`UniProton [0-9]` 版本应答或 `command not found`，显式排除输入回显与启动 banner）+ `test-io-qemu` 的 `expect_shell_output`（`support shell commond|Available commands:`，需专门环境）+ `test-k3s-interaction`（kubectl attach，同款防回显断言） | ✅ |
| 5 | 按 RTOS 类型（UniProton/Zephyr）与固件路径选择启动 | 注解消费单测：`config/oci/annotations_test.go` 的 `TestGetOSInfoReadsAnnotation`（os=zephyr/大小写透传/缺省回落）+ `oci_configs_test.go` 的 `TestBuildContainerConfigOSAnnotationSelectsZephyr`（端到端 cfg.OS=zephyr）；非法值报错：`container_validation_test.go`（错误消息列出 uniproton/zephyr 白名单）；固件路径：`oci_configs_test.go` 的 `TestResolveFirmwarePathAnnotationOverridesFallback` + features case 11 反例（非法 firmware_path 必须失败）；E2E 正例：features case 7（注意其 os=uniproton 恰为默认值，单独不能证明注解被消费——消费证明由上述单测承担） | ✅ UniProton 场景 E2E 已覆盖；Zephyr 类型选择由单测覆盖，固件场景见下方"Zephyr 验证路径" |
| 6 | 创建时配置 CPU/内存资源生效 | case 1（注解路径：断言 `xl list` 实际 Mem=32/VCPUs=1）+ case 2（热更新：断言 `max_memkb` 65536→131072）+ case 8（OCI spec 路径：`containerd_client run` 经 Go SDK 注入 LinuxResources——与 CRI/kubelet 等价——断言 65536/1）+ case 9/10 反例（非法注解/超大内存）；注解解析契约单测：`TestApplyMemoryReservationFromAnnotation`（合法生效/非法与空白忽略）+ `TestApplyResourcesMinMemoryAnnotationOverridesSpecReservation`（注解优先于 OCI spec reservation 的方向钉死） | ✅ |
| 7 | K3s RuntimeClass + Pod 启动 | `test-k3s-cloud-edge`（Pod Running + restartCount=0 + containerd task RUNNING + `xl list` domain 四重断言；**产品路径删除清理断言**：删除后 task 消失 + domain 消失，禁 edge force 兜底）；`test-k3s-single-node`（同款四重启动断言；**无删除清理断言**——删除仅由 EXIT trap 强制清场，产品清理保证由 cloud-edge/interaction 承担） | ✅ |
| 8 | kubectl attach 交互 | `test-k3s-interaction`（K3s 通用集里对应 `interaction` 场景；attach 应答断言显式排除输入回显，hello banner 仅在输入不含 uname 时可作为放行依据；restartCount=0 防 crash-loop 伪 Running；产品路径删除清理断言 + fallback 触发即判失败） | ✅ |

**Zephyr 验证路径**（固件由用户经构建侧自行提供，测试入口已就绪）：

1. 构建固件：启用 `rtos/meta-zephyr` layer（zephyr-kernel/zephyr_toolchain/
   zephyr.bbclass）构建 Zephyr 固件，获得 `.elf`/`.bin`；
2. 制作为容器镜像：`mica-image-builder.py --pedestal xen --os zephyr
   --firmware <zephyr.elf> --xen-image <zephyr.bin> --image-name
   local/mica-zephyr-app:xen-arm64-0.1 --platform linux/arm64 --export`；
3. 以该 tar 跑场景：`QEMU_IMAGE_TAR=<zephyr-tar> TEST_IMAGE=docker.io/local/
   mica-zephyr-app:xen-arm64-0.1 ./tests/bin/test-qemu-features --case 7`
   （类型选择），交互应答可加跑 `test-io-qemu`；单测与 mock_micad 的
   Zephyr 行为（合法清单、固件路径、模拟 shell）常驻看护。

验收 7/8 的验证一律基于 rootfs 内置的 shim（与用户实际拿到的一致），
不依赖手工部署二进制。功能 8（配置与注解）无独立验收条目，由
`docs/reference/annotations.md`（键位契约全集）+ features 套件
auto-close/资源注解用例兜底。

## 1.6 端到端性能基线

测量方法：guest 内 `date +%s.%N` 差值，多轮取中位数；可复现的测量与
对比机制见 `tests/perf/`（measure.sh / compare.sh / baseline.json，含
环境指纹与指标语义）。性能敏感改动应复测对比（中位漂移 >25% FAIL、
>10% WARN）。

| 指标 | 中位数 | 最大值 | 说明 |
|------|--------|--------|------|
| Create→RUNNING | 8.21s | 8.80s | ~93% 耗时在 ctr run 返回前（镜像解包+domain 构建）；RUNNING 收敛余量 <0.7s |
| attach 首响应（shell 命令应答） | 0.37s | 1.02s（1/5 轮抖动） | 回显 ~50ms；异常轮为 guest IO 单次抖动 |
| 删除收敛（kill→domain 消失） | 0.37s | 0.42s | 无异常值 |
| 资源配置生效（xl list） | 32MB/1VCPU | — | 与注解/OCI 请求一致，量化证据 |

**节点分解（stage 埋点 `support/perf`，MICRUN_PERF=1，7 轮中位）**：
create_guest(micad MCreate) **5ms**；guest_start(MStart) **2.25s**；
cpu_settings 0.15s；stop_sandbox 0.20s；dial_tty（attach 的 RPMSG 节点
等待）**3.3s**。即 Create→RUNNING 的 7.8s 中 micrun 侧约 2.4s，其余
~5.4s 在 containerd 侧（镜像/snapshot/shim spawn）；attach 链路的大头
是 TTY 拨号等待。

**Race 构建的负载验证**：`-race` 交叉编译的 shim 在 guest 内经受并发
create/attach/pause-resume/metrics/kill/delete、快速 create-delete
循环、auto-close、shim crash recovery 等负载，全程**零 DATA RACE
报告**、domain 清理零残留。发布前建议纳入检查单复跑。

## 2. 贡献者本地验证流程（提交前必做）

```bash
make ci          # fmt 检查 + go vet + 构建 + 单元测试 + race 检测
```

任何一步失败都不要提交。细分目标：

| 命令 | 内容 |
|------|------|
| `make lint` | `gofmt -l`（vendor 除外）+ `go vet ./...` |
| `make build` | 当前配置构建 shim 二进制 |
| `make test` | `go test ./... -count=1`（默认构建） |
| `make test-race` | 同上 + `-race` |
| `make test-debug` | `-tags debug` 变体（debug logger 行为） |
| `make test-all` | test + test-race + test-debug |
| `make cover` | 覆盖率 → `builds/coverage.out`，打印总覆盖率 |
| `make cover-html` | HTML 报告 → `builds/coverage.html` |

触碰生命周期/IO/恢复路径的改动，除 `make ci` 外应在 QEMU 场景里跑对应用例
（`tests/bin/test-qemu-lifecycle` 至少全量一遍），见 `tests/README.md`。

### build tag 矩阵

| tag | 影响范围 | 何时运行 |
|-----|----------|----------|
| （无） | 绝大多数测试 | `make test` 默认 |
| `debug` | `logger_debug_test.go`（`!debug` 对应 release 变体默认运行） | `make test-debug` |
| `linux` | `rpmsg_tty_integration_test.go`，运行时无 python3/PTY/micad 会自跳过 | linux 默认运行 |

历史教训：曾有单元测试挂在 `//go:build test` 后面无人运行，断言的配置段名早已废弃，
静默失败了数月（`TestApplyMicrunFiles`，已修复）。**新增测试禁止携带私有 tag**；
确需 tag 的（真实环境依赖）必须在本表登记。

## 3. 为新 BUG 写回归测试（红-绿纪律）

规则（`contribution-guide.md` §1）：**fix: 提交必须在同一提交内包含回归测试**，
且该测试在修复前失败、修复后通过。无法写测试的 fix 必须在提交说明中写明原因与手工验证步骤。

操作手册：

1. **定位触发场景**。给出具体输入值和执行路径（这也是评审四问的第一问）。
   说不出触发路径的按已知限制记录，不写代码。
2. **最小复现**。在对应层的现有测试文件里加一个用例：构造 fake/输入，断言期望行为。
   复现要落在 BUG 所属的层：状态机问题进 `domain/container` 或 `ports`，
   RPC 契约进 `transport/shimv2`，IO 行为进 `adapters/io`。不要用 E2E 层测单元问题。
3. **红**。`go test -run 新用例 ./对应包/` 必须失败，且失败方式就是 BUG 现象。
   如果它居然通过了，说明你复现的不是这个 BUG——回去重新定位。
4. **绿**。实施修复，只让该用例及相关用例通过。顺手跑 `make test` 确认无连带破坏。
5. **并发 BUG 额外要求**：修复必须带 `-race` 下的复现（`make test-race`），
   哪怕复现是概率性的（用循环放大窗口）。锁顺序问题先读 `docs/internals/concurrency.md`。

## 4. 覆盖基线

`make cover` 总体 **67.1%**（2026-08 快照）。这不是合格线，是事实基线：
改动所在包的覆盖不应明显低于该均值，新增行为应可测。不追求覆盖率数字，
不为准入门槛硬写测试。

| 覆盖 | 包 |
|------|-----|
| ≥ 90% | validation, timex, panicsafe, lockutil, definitions, contextx, channels, exitstatus, cpuset, recovery, statekey |
| 80–90% | console, task, attach, parse, ports, bootstrap(+shimcli), runtime |
| 60–80% | lifecycle, config/oci, config/runtimeconfig, state/file |
| 50–60% | domain/container, adapters/io, fs, configstack, logger, errors, libmica |
| < 50%（薄弱区，后续项） | transport/shimv2 (54%), guest/micad (54%), netns (51%), **hypervisor/pedestal (31%)**, support/sys (7%) |

薄弱区说明：pedestal 依赖 Xen 命令行环境，可 fake 化的决策逻辑应优先补；
`support/sys` 多为系统调用薄封装，价值低，不强求。main/version 无测试，可接受。

## 5. Fuzz 与 Benchmark 指引

### Fuzz 测试

对面向不可信输入的解析器，原生 Go fuzz 是默认选择：

```bash
go test -fuzz=FuzzParseMicaStatus -fuzztime=10s ./internal/adapters/guest/libmica/
go test -fuzz=FuzzIsValidFIFOPath -fuzztime=10s ./internal/adapters/io/
go test -fuzz=FuzzGetBundleImageFile -fuzztime=10s ./internal/adapters/config/oci/
```

已有目标：
- `FuzzParseMicaStatus` / `FuzzSplitMicaStatusFields`（micad 文本协议解析器）
- `FuzzIsValidFIFOPath`（containerd 客户端请求中的 FIFO 路径）
- `FuzzGetBundleImageFile`（firmware_path 注解的路径穿越防护）

**何时新增 fuzz 目标**：新解析器接收来自 socket/注解/配置文件的任意文本或
二进制时。持续集成不跑 fuzz（仅 seed corpus 在 `go test` 中验证不 panic）。

### Benchmark

```bash
go test -bench=. -benchtime=1s ./internal/domain/console/
go test -bench=BenchmarkLastMicaMarker ./internal/adapters/guest/libmica/
```

已有目标：
- `BenchmarkLastMicaMarker`（socket 响应标记扫描，51ns/短响应）
- `BenchmarkCompressLineEndings`（stdout 换行压缩）
- `BenchmarkFilterNUL`（固件记录 NUL 过滤）

**何时新增 benchmark**：IO 热路径（copier、epoll、解析器）修改后，先跑基线
再对比，避免无感知的性能回退。

## 6. 测试的边界（什么不要测）

- 不测 vendored 代码、不测 Go 标准库语义（例：`write(2)` 在 `O_NONBLOCK` 下
  不会返回 `(n>0, EAGAIN)`——这类"上游保证"不重复测试，见 AGENTS.md 防御性编程原则）。
- 不为覆盖率数字给 getter/常量包装测试；唯一例外是**对外线缆契约**
  （如 `annotations` 包的注解键字面值表——重命名会静默破坏外部调用方）。
- E2E 层不测单元问题：QEMU 用例成本高、排障链长，只守全链路场景
  （smoke / lifecycle 13 用例 / io 交互 / K3s）。
