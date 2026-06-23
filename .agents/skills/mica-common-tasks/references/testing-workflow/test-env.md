# 测试环境准备

## 1. 文档目标

这篇文档用于在自动化验证前确认目标环境、SDK、构建产物和部署通道。它只处理环境准备，不定义具体 smoke test 用例。

环境准备完成后，应进入 `smoke-test.md` 执行基础 smoke 验证。

## 2. 入口判定

开始准备环境前，agent 应先确认用户当前处于哪一种状态：

1. 已有可用开发板环境
2. 已有可用 QEMU 环境
3. 已有 openEuler Embedded SDK，但没有目标运行环境
4. 完全从零开始，需要 agent 准备 QEMU 与 SDK

只有在确认需要 agent 自建 QEMU 环境时，才下载 QEMU 镜像包。不要在用户已经有开发板、已有 QEMU 或只需要编译验证时主动下载镜像。

入口判定的输出应明确：

- 目标环境类型：开发板、已有 QEMU、自建 QEMU 或仅编译
- SDK 状态：已有且匹配、已有但未确认、缺失或需要从 QEMU 包安装
- 部署方式：SSH、串口、共享目录、临时 rootfs 副本或其他方式
- 下一阶段：复用开发板、复用 QEMU、自建 QEMU、只安装 SDK 或暂停确认

## 3. 公共约束

### 3.1 SDK 匹配原则

openEuler Embedded SDK 应与目标运行环境对应。尤其涉及 KO 时，不能随意混用其他镜像或其他日期构建出来的 SDK。

需要确认：

- SDK 环境脚本路径，例如 `environment-setup-aarch64-openeuler-linux`
- 目标环境架构，常见为 aarch64
- 目标环境内核版本和 SDK 中 `KERNEL_SRC` 的对应关系
- SDK sysroot 中是否包含 MICA 用户态构建和运行依赖，例如 `libmetal`、`libopen_amp`、`libsysfs`
- 如果目标环境是开发板，SDK 是否来自该板镜像对应构建产物
- 如果目标环境是 QEMU，SDK 是否来自该 QEMU 镜像对应构建产物

KO 构建对内核配置、符号和 kABI 更敏感。即使内核版本号相同，也不能默认 SDK 与目标环境兼容。

### 3.2 组件构建与部署分工

环境准备阶段需要完成新构建组件的部署，但具体构建事实归属 domain 文档：

- Linux/master 侧构建部署：`../../../mica-linux-master/references/master-build-deploy.md`
- RTOS/client 镜像来源与构建边界：`../../../mica-rtos-client/references/client-build-deploy.md`
- pedestal-specific 组件差异：`../../../mica-pedestals/references/pedestal-overview.md`

测试环境准备阶段需要把这些产物部署到目标环境，并确认目标 shell 能访问它们。常见部署对象包括：

- `micad`
- `mica` 命令脚本
- MICA 配置文件
- 目标 pedestal 所需 KO
- `libmetal`、`libopen_amp`、`libsysfs` 等运行依赖
- RTOS/client image 及其关联文件

### 3.3 QEMU 与 SDK 缓存

为避免不同 agent session 重复下载 QEMU 包和重复安装 SDK，MICA 测试环境使用用户级外部缓存目录。默认缓存根目录为：

```text
~/.mica
```

如果用户设置了 `MICA_CACHE_HOME`，应优先使用该环境变量指定的目录。缓存目录建议按以下结构组织：

```text
<cache-root>/
  archives/        # 原始 qemu-aarch64-hmi-mcs-ros tar 包
  packages/        # tar 包解包后的 QEMU package 目录
  <timestamp>/     # 从 package 中安装出来的 SDK
```

不要把 QEMU tar 包、解包后的 package 或 SDK 默认放入 `.agents/env/`。`.agents/env/` 只用于保存当前仓库的轻量 session 状态。

SDK 安装目录应尽量短。Yocto SDK 会重定位 native ELF 的 `PT_INTERP` 动态加载器路径，路径过长会触发 `interp size` 错误。默认 SDK 目录应使用 `<cache-root>/<timestamp>`，不要使用 `<cache-root>/sdks/<timestamp>` 这类更深层级。

使用规则：

- 每次准备 QEMU 或 SDK 前，先检查用户级缓存目录是否已有可复用内容
- 若缓存中已有 MCS QEMU package，且来源、包大小、关键文件和 SDK 依赖校验通过，应优先复用
- 若缓存中已有 SDK，且 `environment-setup-aarch64-openeuler-linux` 可 source，并且依赖检查通过，应优先复用
- 只有缓存不存在、缓存校验失败、目标需求与缓存不匹配，或用户明确要求更新本次测试镜像或 SDK 时，才重新下载或重新安装
- 强制更新时，不应静默覆盖旧缓存；应使用新的 timestamp 子目录或在输出中说明替换动作

缓存复用前至少确认：

- archive 或 package 来自 `qemu-aarch64-hmi-mcs-ros`，不是普通 `qemu-aarch64`
- tar 包大小和来源 URL 与 MCS 包特征一致
- package 中存在 kernel、rootfs 和 SDK installer
- SDK 安装路径满足 Yocto SDK native ELF relocation 的长度限制
- SDK sysroot 中存在 `libmetal`、`libopen_amp`、`libsysfs` 和 `metal/alloc.h`
- 若需要构建 KO，SDK 的 `KERNEL_SRC` 能用于当前目标环境

环境准备输出应写明本次使用的是缓存还是新下载内容，并给出：

- QEMU package 目录
- SDK 环境脚本路径
- 缓存校验结论

### 3.4 Session 环境记录

为了让不同 agent session 能复用同一套环境信息，测试或调试环境应优先记录到仓库内本地 session YAML：

```text
.agents/env/mica-session.yaml
```

模板位于：

```text
.agents/skills/mica-common-tasks/references/testing-workflow/session-env-template.yaml
```

使用规则：

- 每次准备环境前，先读取 `.agents/env/mica-session.yaml`
- 如果 session YAML 中已经给出 SDK、QEMU package、开发板连接信息或部署产物路径，应优先校验并复用
- 如果 session YAML 缺失某项，再按用户级缓存目录和环境分支规则补齐
- 用户明确要求更换 SDK、镜像、QEMU/开发板目标或保留策略时，应更新 session YAML
- 不要把真实开发板密码写入提交文件；如必须使用密码登录，优先在 YAML 中记录 `password_env`，由环境变量承载真实密码
- `.agents/env/mica-session*.yaml` 是本地状态文件，应保持在 `.gitignore` 中

session YAML 只记录跨 session 复用所需的最小状态：

- `target`：目标类型、架构、pedestal、连接信息、部署目录和是否保留运行环境
- `qemu`：archive、package 目录、可选 work 目录和必要启动参数
- `sdk`：SDK 目录、环境脚本、`KERNEL_SRC`、校验状态和异常备注
- `artifacts`：本次准备部署的 `micad`、`mica`、KO、RTOS image、MICA 配置和是否保留目标侧 RTOS image
- `validation`：验证模式、实例名、TTY 选择、结果和关键日志路径

## 4. 环境分支

### 4.1 已有开发板环境

用户已有开发板时，agent 应优先复用用户环境，而不是替换为 QEMU。

需要确认：

- 板型与目标架构
- 登录方式，例如 SSH、串口或其他远程 shell
- 文件部署方式，例如 SCP、NFS、TFTP 或镜像挂载
- 是否有 root 权限或等效权限
- 目标系统版本与内核版本
- 对应 SDK 的获取方式或本地路径
- 是否允许替换 `micad`、`mica`、配置文件、依赖库和 KO
- 是否允许重启 `micad`、重新加载 KO 或重启系统

开发板环境准备完成的标准是：

- agent 能进入目标 shell
- agent 能把新构建产物部署到目标环境
- SDK 已 source，并能用于构建目标侧组件
- 涉及 KO 时，SDK 与开发板运行环境匹配性已确认

### 4.2 已有 QEMU 环境

用户已有 QEMU 环境时，agent 应确认该环境是否已经启用 MCS，以及它对应的 SDK 是否可用。

需要确认：

- QEMU 镜像来源和构建日期
- 是否为带 MCS 能力的 openEuler Embedded 镜像
- 目标 pedestal，例如 baremetal、xen、jailhouse 或 hetero
- QEMU 启动方式、内核、rootfs、dtb 和启动参数
- 登录方式，例如 SSH、串口或端口转发
- 文件部署方式，例如共享目录、SCP、virtiofs、9p 或 rootfs 解包注入
- 对应 SDK 路径
- 是否允许替换目标环境中的 MICA 组件

如果已有 QEMU 环境不是 MCS 镜像，SDK 中可能缺少 `libmetal`、`libopen_amp` 等 MICA 依赖，不应直接作为 MICA 自动验证环境。

### 4.3 已有 SDK 但没有目标环境

用户只有 SDK、没有开发板或 QEMU 时，agent 不应直接开始 smoke test。

需要确认：

- 用户是否只需要编译验证
- 是否允许 agent 自建 QEMU 环境
- SDK 对应的目标镜像或构建产物是否可获得
- 是否需要下载与 SDK 匹配的 QEMU/rootfs
- 是否涉及 KO 构建

如果用户只需要编译验证，进入 domain 构建文档即可。如果用户需要运行验证，但没有目标环境，应先决定使用用户提供环境还是进入自建 QEMU 分支。

### 4.4 从零准备 QEMU 环境

只有在用户没有可用目标环境，且确认允许 agent 自建 QEMU 环境时，才进入本路径。

默认使用 MCS 相关 QEMU 包：

```text
https://build-logs.openeuler.openatom.cn:38080/packages/master/aarch64/qemu-aarch64-hmi-mcs-ros/
```

不要把普通 `qemu-aarch64` 包作为 MICA 自动验证默认来源。普通包可能没有启用 MCS，SDK 中可能缺少 OpenAMP、libmetal 等依赖。

获取或复用最新包的步骤：

1. 先检查 `<cache-root>/archives/` 和 `<cache-root>/packages/` 中是否已有可复用 MCS QEMU 包
2. 如果缓存可用，优先复用缓存并记录 package 目录
3. 如果缓存缺失、校验失败或用户明确要求更新，再读取远端目录索引
4. 匹配 `*.tar.gz`
5. 按文件名时间戳排序
6. 选择最新 tar 包
7. 下载到 `<cache-root>/archives/`
8. 解包到 `<cache-root>/packages/` 下对应 timestamp 目录

注意：不同构建目录下可能存在同名时间戳包，例如普通 `qemu-aarch64` 和 `qemu-aarch64-hmi-mcs-ros` 都可能叫 `20260615170031.tar.gz`。不能只按文件名或 tar 内部目录名判断来源。MCS 包通常明显大于普通 QEMU 包；下载或复用本地文件时，应校验来源 URL、目录名和文件大小。

解包后应识别以下文件：

- `*toolchain-latest.sh`
- `zImage` 或 `Image`
- `vmlinux`
- `openeuler-image-qemu-aarch64-*.rootfs.cpio.gz`

可使用脚本完成发现、下载、解包和 SDK 依赖校验：

```sh
python3 .agents/skills/mica-common-tasks/references/testing-workflow/scripts/prepare_mcs_qemu_env.py --download --install-sdk
```

该脚本默认使用 `MICA_CACHE_HOME` 指定的目录；未设置时使用 `~/.mica`。archive 写入 `archives/`，package 解包到 `packages/`，SDK 安装到 `<cache-root>/<timestamp>/`。后续 session 可直接复用这些路径。

如果用户明确要求更新本次测试镜像或 SDK，可使用：

```sh
python3 .agents/skills/mica-common-tasks/references/testing-workflow/scripts/prepare_mcs_qemu_env.py --download --install-sdk --force-update
```

如需临时使用其他缓存根目录，可显式传入 `--cache-dir <cache-dir>`，但 SDK 安装路径仍必须满足 Yocto SDK native ELF relocation 的长度限制。

如已有 tar 包：

```sh
python3 .agents/skills/mica-common-tasks/references/testing-workflow/scripts/prepare_mcs_qemu_env.py --archive <qemu-aarch64-hmi-mcs-ros.tar.gz> --install-sdk
```

该脚本会拒绝过小的普通 `qemu-aarch64` 包，并检查 SDK 中是否存在 `libmetal`、`libopen_amp`、`libsysfs` 和 `metal/alloc.h`。

## 5. 条件子阶段

### 5.1 SDK 安装与启用

本阶段只在 SDK 缺失、SDK 未安装，或从 QEMU 包中获取 SDK installer 后需要安装时执行。用户已经提供可 source 的匹配 SDK 时，不需要重复安装。

从 QEMU 包或用户提供路径获取 SDK installer 后，执行安装脚本并按提示选择安装目录。

SDK installer 支持非交互安装：

```sh
sh <toolchain-latest.sh> -y -d <sdk-dir>
```

安装 MCS/ROS SDK 时可能会输出内核树 prepare、`Preparing ROS2 SDK...`、SDK 文件修正等大量日志。这通常是安装脚本的一部分，不应仅因输出多就判定卡死。

每个新的 shell 会话都需要 source SDK 环境脚本：

```sh
. /path/to/sdk/environment-setup-aarch64-openeuler-linux
```

启用后至少确认：

```sh
aarch64-openeuler-linux-gcc -v
test -n "$KERNEL_SRC"
```

如果需要构建 KO，还应确认 `KERNEL_SRC` 指向有效内核构建目录。

如果 `micad` 构建报 `metal/alloc.h: No such file or directory`，通常说明当前 SDK 不是带 MCS 依赖的 SDK，或 SDK sysroot 缺少 libmetal/OpenAMP 开发头文件。此时应回到环境分支确认是否误用了普通 `qemu-aarch64` 包。

### 5.2 QEMU baremetal 环境准备

本阶段只在目标环境是 ARM64 QEMU baremetal，且需要 agent 准备或修正 QEMU 启动环境时执行。已有开发板环境、已有非 baremetal QEMU 环境或只做编译验证时，不进入本阶段。

QEMU baremetal 环境需要额外准备 dtb。部署 Client OS 前，需要在 Linux 设备树中增加 `mcs-remoteproc` 设备节点并预留内存。

仓内工具位于：

```text
tools/create_dtb.sh
```

ARM64 QEMU baremetal 场景可生成 `qemu.dtb`：

```sh
./tools/create_dtb.sh qemu-a53
```

该工具依赖 `qemu-system-aarch64` 和 `dtc`。生成的 `qemu.dtb` 对应 `2G RAM, 4 cores`。启动 QEMU 时，`-m` 和 `-smp` 必须与 dtb 一致，并通过 `maxcpus=3` 为 Client OS 预留 CPU。

参考启动模型：

```sh
sudo qemu-system-aarch64 -M virt,gic-version=3 -cpu cortex-a53 -nographic \
    -netdev user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22 \
    -device virtio-net-device,netdev=net0 \
    -m 2G -smp 4 \
    -append 'root=/dev/ram0 rw maxcpus=3 console=ttyAMA0 ramdisk_size=524288' \
    -kernel zImage \
    -initrd openeuler-image-*.cpio.gz \
    -dtb qemu.dtb
```

实测 MCS rootfs 作为 initrd 启动时，如果缺少 `root=/dev/ram0 rw` 或 `ramdisk_size` 太小，可能出现 `RAMDISK: Couldn't find valid RAM disk image` 和 `VFS: Unable to mount root fs`。`ramdisk_size=524288` 可覆盖当前约 326 MiB 的 rootfs。

QEMU 自动化默认使用 user-mode networking 和 host port forwarding，不依赖宿主机 `tap`、`qemu-ifup` 或固定 IP。推荐模型是：QEMU 使用原始 rootfs 启动，串口只用于首次 root 登录、设置临时密码和注入 SSH 公钥；随后通过 `127.0.0.1:<host-port>` SSH 登录执行 smoke test、部署构建产物和收集日志。

当前 testing workflow 只维护 SSH 控制面脚本，不再维护独立的 loginless/initrd 注入 smoke 脚本。原因是 MICA 开发验证通常需要替换 `micad`、`mica` 或 KO，并需要多次登录目标系统收集状态；这些场景由 SSH 控制面覆盖更完整。若 SSH 无法建立，应把串口作为启动和网络诊断通道，而不是切换到另一套并行 smoke 入口。

可使用脚本执行默认 SSH 控制面 smoke：

```sh
python3 .agents/skills/mica-common-tasks/references/testing-workflow/scripts/run_qemu_ssh_smoke.py --package-dir <qemu-package-dir>
```

该脚本会生成 `qemu.dtb`，启动原始 rootfs，使用 `hostfwd=tcp:127.0.0.1:2222-:22` 建立 SSH 控制通道，并在目标内执行基础 smoke。脚本只改变本次 QEMU 运行时内存中的 rootfs 状态，不回写原始 `*.rootfs.cpio.gz`。串口仍然保留为早期启动日志、SSH 注入失败和内核/init/systemd 启动失败时的诊断入口。

如果用户环境必须使用 `tap`、桥接网络或开发板固定 IP，应按用户提供的网络拓扑调整 QEMU 启动方式和登录地址。

### 5.3 QEMU 生命周期策略

QEMU 是否在 smoke test 后自动释放，应由任务目的决定，不应固定为“一次性启动、跑完即销毁”。

建议策略：

1. 一次性验证环境
   - 适用于 maintainer 只要求对 PR、patch 或某个构建产物跑基础 smoke test
   - smoke test 完成并收集结果后，可以关闭 QEMU 并释放临时工作目录

2. 调试与开发环境
   - 适用于定位问题、开发大特性、反复替换组件、需要多次 SSH 登录实验或用户希望自行登录目标系统的场景
   - agent 应保留 QEMU 进程、hostfwd 端口、工作目录和关键日志，直到用户确认可以释放

3. 半自动验证环境
   - 适用于先跑 smoke test，再根据结果决定是否继续人工实验
   - smoke 通过且用户只需要结论时释放；smoke 失败或需要补证据时保留现场

输出测试环境方案时，应明确说明：

- QEMU 是否会在任务结束后关闭
- SSH 登录地址和端口
- QEMU 工作目录和日志位置
- 释放条件，例如 smoke 完成后自动释放，或用户确认后再释放
- 如果保留环境，后续由谁负责停止 QEMU

### 5.4 目标侧部署确认

本阶段在所有需要运行 smoke test 的路径中都需要执行。部署方式由环境分支决定，可以是 SSH/SCP、串口辅助、共享目录、临时 rootfs 副本或目标环境已有包管理方式。

部署完成后，应确认目标 shell 能访问新组件，并确认 `mica.conf` 中的路径与目标环境实际文件一致。

MCS QEMU rootfs 通常已经预置 `/etc/mica/qemu-zephyr-rproc.conf`、`/lib/firmware/zephyr.elf`、`/lib/firmware/zephyr.bin`、`/usr/bin/mica`、`/usr/bin/micad`、`mcs_km.ko`、`libmetal`、`libopen_amp` 和 `libsysfs`。如果只是验证环境基线，可以先使用预置组件；如果验证当前工作区改动，则需要替换对应产物后再进入 smoke test。

若只验证 Linux/master 侧改动，可仅替换 `micad`、`mica` 命令脚本和 `mcs_km.ko`，保持 `/etc/mica/*.conf` 与 `/lib/firmware/*` RTOS/client image 不变。SSH 控制面脚本支持在 smoke 前部署这些 Linux 侧产物：

```sh
python3 .agents/skills/mica-common-tasks/references/testing-workflow/scripts/run_qemu_ssh_smoke.py \
    --package-dir <qemu-package-dir> \
    --micad <local-micad> \
    --mica <local-mica.py> \
    --ko <local-mcs_km.ko>
```

该模式用于验证 Linux/master 侧软件栈与镜像预置 RTOS/client image、配置文件之间的兼容性。不要把这个结果解释为新 RTOS image 或新配置已经验证通过。

## 6. 环境准备完成标准

进入 `smoke-test.md` 前，应满足：

- 目标环境可登录
- 文件部署通道可用
- SDK 已确认并可用于当前目标环境
- 新构建的 Linux/master 侧组件已部署
- 目标 pedestal 所需 KO 和依赖库已部署或确认存在
- RTOS/client image 已存在或其生成方式已确认
- MICA 配置文件与实际部署文件路径一致
- 日志收集方式已确认

如果任一条件不满足，应先补齐环境信息，不应直接进入 smoke test。
