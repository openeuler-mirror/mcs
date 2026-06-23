# Smoke Test

## 1. 文档目标

这篇文档定义 MICA 基础 smoke test 的执行方式。它假设测试环境已经准备完成，并且 agent 已能登录目标系统执行命令。

环境准备、SDK 获取、QEMU 下载、开发板登录和组件部署属于 `test-env.md`。

## 2. 适用范围

Smoke test 用于快速确认基础功能没有被破坏。这里的“基础功能”包括生命周期主链路，以及通过 `rpmsg-tty` 和 `screen /dev/ttyRPMSGX` 最小交互覆盖到的基础通信链路。

适用场景包括：

- 开发完成后的最小回归
- PR 或 patch review 后的基础验证
- bugfix 后确认生命周期主链路和基础通信链路仍可用
- 调试前确认环境基础状态

Smoke test 不等价于完整适配验证。新 RTOS、新 pedestal、新开发板或硬件平台适配还应进入 `adaptation-validation.md`。

## 3. 目标连接模型

Smoke test 不绑定 QEMU 或开发板。执行前只要求具备三类能力：

- 目标 shell：能在目标环境执行命令
- 文件访问：能确认配置、镜像、KO、依赖库等文件存在
- 日志收集：能收集 `mica` 输出、`micad` 输出和 kernel log

目标环境可以是：

- QEMU 串口或 SSH
- 开发板 SSH 或串口
- 其他能执行目标侧命令并收集日志的环境

## 4. 前置条件

执行 smoke test 前，应确认：

- `mica` 命令存在并可执行
- `micad` 存在并可执行
- MICA 配置目录存在
- 目标配置文件存在
- RTOS/client image 存在
- 配置文件中的 `ClientPath`、`Pedestal`、`PedestalConf` 等字段与实际部署文件一致
- 目标 pedestal 所需 KO 已加载或可加载
- 用户态依赖库存在
- 日志输出位置已确认

可以使用下面的检查模型：

```sh
which mica
which micad
ls /etc/mica/
mica status
dmesg | tail
```

实际命令应根据目标环境可用工具调整。

MCS QEMU rootfs 通常已经预置 `/etc/mica/qemu-zephyr-rproc.conf` 和 `/lib/firmware/zephyr.elf`。如果测试目标是验证新构建组件，应先确认这些预置文件是否已被当前构建产物替换；否则 smoke test 只能证明镜像基线可用。

## 5. RTOS/client image 要求

Smoke test 必须有一个可启动的 RTOS/client image。该 image 可以来自目标镜像预置内容、用户已有构建产物，或由环境准备阶段生成。

需要确认：

- `ClientPath` 指向的 ELF 或 image 文件存在
- image 中的 resource table 与目标 pedestal 约束匹配
- 如果配置需要 `PedestalConf`，对应文件存在
- RTOS/client 侧已经具备最小 MICA 初始化和 service 创建能力

如果没有可用 RTOS/client image，应回到 `test-env.md` 或 RTOS/client 构建文档，不应继续执行 smoke test。

## 6. 默认 pedestal 范围

未指定目标 pedestal 时，基础 smoke test 默认使用当前 CI 覆盖的 baremetal 场景。

如果用户提供 xen、jailhouse 或 hetero 的目标环境、对应 SDK、配置文件和所需组件，smoke test 可以切换到用户指定 pedestal。此时 smoke 验证结构保持一致，但配置文件、KO、启动前置条件和日志观察点需要按目标 pedestal 调整。

QEMU baremetal 所需的 dtb 生成和 QEMU 启动参数属于 `test-env.md` 的环境准备内容。

## 7. 基础 smoke 验证序列

基础 smoke test 同时覆盖生命周期主链路和基础通信链路。生命周期部分确认 `create/start/stop/rm/status` 的基本状态转换；基础通信部分通过 `mica status` 中的 `rpmsg-tty` 可见性、`/dev/ttyRPMSG*` 设备存在性，以及 `screen /dev/ttyRPMSGX` 最小输入回读来确认 RPMsg TTY 链路可用。

QEMU baremetal 自动化场景默认使用 `test-env.md` 中的 SSH 控制面脚本：

```sh
python3 .agents/skills/mica-common-tasks/references/testing-workflow/scripts/run_qemu_ssh_smoke.py --package-dir <qemu-package-dir>
```

该脚本通过 QEMU `hostfwd` 登录目标系统，执行本节的 smoke 序列，并用 `MICA_SMOKE_RESULT=PASS` 或 `MICA_SMOKE_RESULT=FAIL` 标记结果。串口只用于建立 SSH 控制面和早期启动诊断，不作为 smoke 命令的主要输入通道。当前 workflow 不维护独立的 loginless/initrd 注入 smoke 入口，避免 QEMU 参数、artifact 部署和 smoke 序列在两套脚本中重复漂移。

验证当前工作区 Linux/master 侧产物时，可在保持 RTOS image 和配置文件不变的前提下传入本地构建产物：

```sh
python3 .agents/skills/mica-common-tasks/references/testing-workflow/scripts/run_qemu_ssh_smoke.py \
    --package-dir <qemu-package-dir> \
    --micad <local-micad> \
    --mica <local-mica.py> \
    --ko <local-mcs_km.ko>
```

该模式只证明 Linux/master 侧替换后的 `micad`、`mica` 命令和 KO 能与镜像原有 RTOS/client image、配置文件完成基础 smoke，不覆盖 RTOS image 或配置变更。

建议顺序：

1. 降低内核打印对 shell 的干扰
2. 启动或重启 `micad`
3. 查询初始状态
4. 清理同名残留实例
5. 执行 `create -> rm` 配对验证
6. 执行 `create -> start -> stop -> start -> stop -> rm` 重启配对验证
7. 在 Running 阶段确认基础 service 可见性
8. 在 Running 阶段确认 `/dev/ttyRPMSG*` 存在
9. 如果目标环境安装了 `screen`，使用 `screen /dev/ttyRPMSGX` 做一次最小 TTY 交互验证
10. 查询最终状态

命令模型：

```sh
echo "1 4 1 7" > /proc/sys/kernel/printk
micad &
mica status
mica stop <name> || true
mica rm <name> || true
mica status
mica create <conf>
mica rm <name>
mica status
mica create <conf>
mica start <name>
mica status
screen /dev/ttyRPMSGX
# 进入 TTY 后发送回车和少量字符，确认 screen 窗口能回读到回显或基础输出
mica stop <name>
mica status
mica start <name>
mica status
mica stop <name>
mica status
mica rm <name>
mica status
```

如果目标镜像已经根据 `/etc/mica/*.conf` 自动创建实例，smoke test 应先确认实例名称和状态，再清理同名残留实例，避免把环境残留误判为 `create` 行为成功。

在当前 QEMU baremetal 预置配置中，常见实例名是 `qemu-zephyr`，配置文件是 `/etc/mica/qemu-zephyr-rproc.conf`。实际实例名必须以 `mica status` 或配置文件中的 `Name` 为准。

如果配置包含 `AutoBoot=yes`，应先确认实例是否已经自动进入启动流程，避免重复执行 `mica start <name>` 造成误判。

## 8. 基础 service 与通信观察

Smoke test 的主目标是确认基础生命周期与基础通信没有被破坏。若目标配置启用了 `rpmsg-tty`，TTY 最小交互是 smoke 的基础通信确认点，而不是额外的完整功能测试。

常见观察点：

- `mica status` 中是否显示 `rpmsg-tty`、`rpmsg-rpc`、`rpmsg-umt` 等服务
- 是否出现 `/dev/ttyRPMSG*` 或目标环境对应的 TTY 设备
- 打开 TTY 后是否能看到 RTOS shell 或基础输出

TTY 交互示例：

```sh
screen /dev/ttyRPMSG1
```

自动化 smoke 中，`screen` 检查的语义不是只确认进程能打开 TTY 设备，而是要完成最小交互。推荐流程是从 `mica status` 的 `rpmsg-tty(/dev/ttyRPMSGX)` 字段选择交互 TTY，启动 detached `screen`，发送回车和少量 marker 字符，再通过 hardcopy 或等效机制回读 screen 窗口内容。若能回读到发送的 marker 或明确的交互输出，可判定 TTY 最小交互通过。

若目标环境没有安装 `screen`，可记录为跳过；若 `mica status` 已显示 `rpmsg-tty` 服务但没有任何 `/dev/ttyRPMSG*`，或 `screen` 无法回读到任何交互输出，应判定为 smoke 失败并进入通信诊断。

TTY 的复杂交互、UMT、RPC、GDB 的完整功能验证不属于最小 smoke test，应按需要进入 communication 或 adaptation validation。

## 9. 通过标准

Smoke test 通过至少应满足：

- `micad` 能启动且未立即退出
- `mica create <conf>` 成功或实例已由镜像预置流程创建
- `create -> rm` 配对验证成功，最终不残留同名实例
- `mica start <name>` 成功
- `mica status` 显示目标实例进入 `Running`
- `mica stop <name>` 成功，状态回到 `Offline` 或目标 pedestal 对应停止状态
- `start -> stop -> start -> stop` 重启配对验证成功
- `mica rm <name>` 成功，最终状态符合预期
- Running 阶段存在 `/dev/ttyRPMSG*`，且在目标安装 `screen` 时至少一个 TTY 设备可完成最小输入和回读
- kernel log 中没有明确的 KO 加载失败、resource table 解析失败、reserved memory 失败或 RPMsg 初始化失败

`Running` 只代表生命周期进入运行态，不等于所有 service ready，也不等于业务链路完整闭合。`screen /dev/ttyRPMSGX` 最小交互通过说明基础 RPMsg TTY 通信可用，但不代表 UMT、RPC、GDB 或目标业务协议已经完成完整验证。

## 10. 失败记录

Smoke test 失败时，应记录：

- 目标环境类型
- SDK 来源和匹配性结论
- pedestal 与配置文件
- RTOS/client image 路径
- 失败命令
- 返回码
- `mica` 直接输出
- `micad` 输出
- kernel log 或 `dmesg` 片段
- `mica status` 前后状态

失败后按阶段回流：

- 环境、SDK、部署问题：`test-env.md`
- create/start/stop/remove 失败：`../debugging-workflow/lifecycle-diagnosis.md`
- Running 后服务不可见或通信异常：`../debugging-workflow/communication-diagnosis.md`
- OpenAMP/libmetal/pedestal/platform 边界不清：`../debugging-workflow/boundary-diagnosis.md`
