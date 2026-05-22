# MicRun 快速入门指南

> **本文档约定**：命令中以 `<...>` 包裹的内容（如 `<build_dir>`、`<container_name>`）表示需要用户根据实际情况替换的自定义值。

## 什么是 MicRun

`MicRun`是一个容器运行时，让你用容器的方式管理`RTOS`（实时操作系统）。

**核心能力**：
- 用`Kubernetes`管理`RTOS`工作负载
- 用容器镜像分发`RTOS`固件
- 同一设备上同时运行`Linux`（Host）和`RTOS`

---

## 背景知识

### 什么是"边侧"（边缘计算）

**边侧**（`Edge`）指的是靠近数据源头或用户的一侧，相对于"云侧"（`Cloud`）而言。

```
                      Cloud
                        │
                    KubeEdge
                        │
          ┌─────────────┼─────────────┐
          │             │             │
       Edge A       Edge B        Edge C
      (Factory)    (Store)      (Vehicle)
          │             │             │
        RTOS         RTOS          RTOS
      (Control)  (DataCollect) (AutoDrive)
```

### 什么是混合关键性系统（MCS）

**MCS**（Mixed Criticality System）是指在同一硬件上同时运行不同优先级任务的系统：
- **非关键任务**：`Linux`（Host）上的常规应用（日志、UI等）
- **关键任务**：`RTOS`上的实时控制（电机控制、安全监控等）

### 为什么选择容器化方案

将`RTOS`接入`Kubernetes`有几种常见方案：

| 方案 | 优点 | 缺点 |
|------|------|------|
| `CRD`+`Operator` | 灵活定制 | 需要为每个功能写代码 |
| `KubeVirt`类型 | 成熟框架 | 无法充分利用`Mica`能力 |
| **MicRun 容器化** | 复用云原生生态 | 需要适配`OCI`规范 |
| `WASM`微运行时 | 轻量级 | 无法混合部署 |

**为什么选择容器化**：
1. 复用容器镜像分发，简化固件管理
2. 复用`Kubernetes`生态，降低运维成本
3. 渐进式云化，每一步都有方案

---

## 快速开始

使用`MicRun`包含以下步骤：

| 步骤 | 说明 |
|------|------|
| 1. 构建系统镜像 | 使用 oebuild 构建包含 MicRun 的 openEuler Embedded |
| 2. 启动系统 | 启动构建好的系统镜像 |
| 3. 构建 RTOS 镜像 | 使用 mica-image-builder 打包固件 |
| 4. 导入镜像 | 将镜像导入 containerd |
| 5. 注册运行时 | 在 containerd 中注册 MicRun |
| 6. 运行容器 | 启动并测试 RTOS 容器 |

---

## 步骤 1：构建系统镜像

`openeuler Embedded`基础构建过程可参考以下内容
+ [快速上手](https://embedded.pages.openeuler.org/master/getting_started/index.html)
+ [mica构建指导](https://embedded.pages.openeuler.org/master/features/mica/build.html)

### 1.1 系统要求

构建的`openEuler Embedded`镜像需要包含：

| 组件 | 版本要求 | 说明 |
|------|----------|------|
| `Kernel` | - | 支持容器功能和`K8s`功能 |
| `micrun` | - | `MicRun`运行时 |
| `micad` | - | `MCS`特性的守护进程，管理RTOS |
| `Xen` | - | 作为`MCS`底座（虚拟化层） |
| `systemd` | 推荐 | 系统和服务管理 |
| `containerd` | ≥1.7.19 | 容器引擎（构建时需≥1.7.27，运行时≥1.7.19即可） |

### 1.2 生成构建环境

```bash
# 安装/更新 oebuild
oebuild neo-generate -p qemu-aarch64 \
  -f zephyr \      # Zephyr RTOS 支持
  -f micrun \      # MicRun 运行时
  -f mcs/xen \     # mcs和xen支持
  -f systemd \     # systemd 服务管理
  -f containerd \  # containerd 容器引擎（必须）
  -d <build_dir>   # 构建目录名称，自定义（如 playmicrun）

cd <build_dir>
oebuild bitbake
```

**选项说明**：
| 选项 | 说明 |
|------|------|
| `-p` | 目标平台，如`qemu-aarch64` |
| `-f` | 添加的功能特性（`feature`） |
| `-d` | 构建输出目录 |

### 1.3 构建镜像

```bash
# 进入 oebuild bitbake 创建的容器环境
bitbake openeuler-image

# 如果只想单独构建 MicRun
bitbake micrun
```

构建完成后，`MicRun`会被自动打包进系统。

> **说明**：`MicRun`是一个无`CGO`依赖的静态链接`Go`二进制文件，构建产物可直接运行。

### 1.4（可选）添加`K3s`支持

如果需要使用`Kubernetes`集群功能：

```bash
oebuild neo-generate -p qemu-aarch64 \
  -f zephyr \
  -f micrun \
  -f mcs/xen \
  -f systemd \
  -f containerd \
  -f k3s-agent \   # 添加 K3s agent 支持
  -d <build_dir>
```

---

## 步骤 2：启动系统

### 2.1 配置 Xen

在启动系统前，需要确保`Xen`已正确配置。请参考[`Mica-Xen`指导文档](https://embedded.pages.openeuler.org/master/features/mica/instruction.html)。

### 2.2 使用`QEMU`启动（开发测试）

**标准 Xen + tap 启动路径**：

这条路径继续作为 MicRun 本地标准测试方式，适合原有 `192.168.7.0/24`
测试网络、K3s 云边联调和需要 `tap0` 的场景。rootfs 应直接使用构建输出中的
`openeuler-image-qemu-aarch64-*.rootfs.cpio.gz` 产物；测试过程不应重命名、
解包或改写该 rootfs 产物。
如果实验网络或构建目录不同，优先通过变量覆盖，不要在文档、skill 或提交记录中
写入当前机器的真实绝对路径、密码或访问密钥。

```bash
cd <build_dir>/output/test
ROOTFS="$(ls -t openeuler-image-qemu-aarch64-*.rootfs.cpio.gz | head -n1)"

sudo qemu-system-aarch64 \
  -device virtio-net-pci,netdev=net0 \
  -netdev tap,id=net0,ifname=tap0,script=/etc/qemu-ifup \
  -initrd "$ROOTFS" \
  -device loader,file=Image,addr=0x45000000 \
  -machine virt,gic-version=3 \
  -machine virtualization=true \
  -cpu cortex-a53 -smp 4 -m 4096 \
  -serial mon:stdio -nographic \
  -kernel xen-qemu-aarch64 \
  -append 'root=/dev/ram0 rw debugshell mem=1536M console=ttyAMA0,115200' \
  -dtb openeuler-image-qemu-aarch64.qemuboot.dtb
```

**可选 usernet 辅助 SSH**：

如果只是做 smoke test，或希望在不依赖 guest 固定 IP 的情况下通过宿主端口
访问 SSH，可以额外加一块 usernet 网卡。仓库里的 `tests/common/qemu.sh`
默认 `QEMU_NET_MODE=both`，含义就是保留 tap 网卡，同时增加下面的 usernet
端口转发；它不是替代 tap 的新标准路径。

```bash
sudo qemu-system-aarch64 \
  -device virtio-net-pci,netdev=net0 \
  -netdev tap,id=net0,ifname=tap0,script=/etc/qemu-ifup \
  -device virtio-net-pci,netdev=net1 \
  -netdev user,id=net1,hostfwd=tcp::10023-:22 \
  ...
```

如果明确只想使用 usernet，可以把上面 tap 网卡去掉，但这种模式不会创建
`tap0`，也不适合依赖固定边侧 IP 和 macvlan 的 K3s 云边测试。

**参数说明**：
| 参数 | 说明 |
|------|------|
| `-device virtio-net-pci,netdev=net0` | 添加`virtio`网卡设备 |
| `-netdev tap,id=net0,ifname=tap0,script=/etc/qemu-ifup` | 使用宿主 `tap0`，保持原有 QEMU/K3s 测试网络 |
| `-netdev user,id=net1,hostfwd=tcp::10023-:22` | 可选用户态网络，把客体 SSH 映射到宿主 `10023` 端口 |
| `-initrd "$ROOTFS"` | 指定构建输出的 `openeuler-image-qemu-aarch64-*.rootfs.cpio.gz` 文件 |
| `-device loader,file=Image,addr=0x45000000` | 加载内核镜像到指定地址 |
| `-machine virt,gic-version=3` / `-machine virtualization=true` | 使用`virt`机器类型、`GIC`v3，并启用虚拟化支持 |
| `-cpu cortex-a53 -smp 4 -m 4096` | `CPU`类型、核心数、内存大小 |
| `-serial mon:stdio -nographic` | 串口输出到标准输出，无图形界面 |
| `-kernel xen-qemu-aarch64` | 指定`Xen hypervisor` |
| `-append '...'` | 内核启动参数 |
| `-dtb *.qemuboot.dtb` | 指定设备树文件 |

**`QEMU`注意事项**：
- MicRun 的完整生命周期测试必须通过 Xen 启动路径执行。直接用 `-kernel Image -initrd "$ROOTFS"` 启动 Linux 只能验证 rootfs、containerd 和 shim 是否存在；此时 `/proc/xen/xenbus` 不存在，MicRun 默认会把宿主 pedestal 判定为 `unsupported`。只有明确设置 `MICRUN_ENABLE_BAREMETAL=1` 时，才会进入 baremetal pedestal 路径。
- 标准化测试不得修改已经构建好的 QEMU rootfs 产物。若某次调试需要在运行中的 guest 里临时补齐目录或网络配置，必须把它记录为临时排障动作，不能把修改后的 rootfs 当作测试基线。
- `QEMU`版本不宜过低，低版本存在影响`Xen`的`bug`
- 确保`Xen DTS`为`Domain-0`预留足够内存（建议`1536M`）
- 如需调整，在`conf/local.conf`中设置：
  ```bash
  QB_XEN_CMDLINE_EXTRA = "dom0_mem=1536M"
  ```

---

## 步骤 3：构建`RTOS`容器镜像

### 3.1 为什么需要特殊构建工具

标准容器使用`[os, arch]`二元组匹配（如`linux/amd64`），而`RTOS`容器需要四元组匹配（如`qemu-aarch64/zephyr/arm64/xen`）：

| 维度 | 说明 | 示例 |
|------|------|------|
| `board` | 硬件板型 | `qemu-aarch64` |
| `os` | RTOS 类型 | `zephyr`、`uniproton` |
| `arch` | CPU 架构 | `arm64`、`amd64` |
| `hypervisor` | 虚拟化类型 | `xen`、`baremetal` |

因此需要使用`mica-image-builder`工具来打包符合要求的镜像。

### 3.2 准备构建环境

```bash
# 进入一次构建的时间戳产物目录；micrun-files 是其子目录
cd <build_dir>/output/<timestamp>

# 初始化 Python 环境
uv init
uv venv
source .venv/bin/activate

# 安装依赖
uv pip install -r micrun-files/requirements.txt
```

> **替代方案**：如果没有`uv`，可以用传统方式：
> ```bash
> pip install -r micrun-files/requirements.txt
> python3 micrun-files/mica-image-builder.py --help
> ```

### 3.3 使用 CLI 构建 UniProton 镜像

```bash
python3 micrun-files/mica-image-builder.py \
  --pedestal xen \
  --os uniproton \
  --firmware micrun-files/uniproton.elf \
  --xen-image micrun-files/uniproton.bin \
  --image-name local/mica-uniproton-app:xen-arm64-0.1 \
  --platform linux/arm64 \
  --export ./exports
```

构建成功后，tarball 位于：

```bash
exports/local_mica-uniproton-app_xen-arm64-0.1.tar
```

说明：
- 仅本地构建和导出时不需要本地 registry；只有使用 `--push` 时才需要 registry。
- 没有 `docker buildx` 的环境会先用 legacy `docker build` 构建，再在导出的 OCI archive 中规范化单平台元数据，例如 `linux/arm64`。
- 如果使用交互式模式并且不推送，请传 `--no-push --export <dir>`，脚本会生成 `local/...` 镜像名并导出 tarball。

### 3.4 交互式构建镜像

```bash
# 启动交互式构建工具
uv run micrun-files/mica-image-builder.py --no-push --export ./exports
```

根据提示选择：
1. **`Pedestal`类型**：选择`xen`或`baremetal`
2. **`OS`类型**：选择`zephyr`或`uniproton`
3. **固件文件**：选择你的`<firmware>.elf`或`<firmware>.bin`文件
4. **镜像名称**：使用默认或自定义名称

### 3.5 手动导出镜像

如果构建时没有传 `--export`，可以手动导出已构建镜像：

```bash
# 方法1: 使用 ctr 导出（推荐，在目标系统上可用）
ctr image export <image_file>.tar <image_name>:<tag>
# 例如：ctr image export my-rtos-image.tar localhost:5000/mica-uniproton-app:xen-0.1

# 方法2: 使用 nerdctl 导出（在目标系统上可用）
nerdctl save -o <image_file>.tar <image_name>:<tag>

# 方法3: 使用 docker 导出（如果构建环境有 docker）
docker save -o <image_file>.tar <image_name>:<tag>
# 例如：docker save -o my-rtos-image.tar localhost:5000/mica-uniproton-app:xen-0.1
```

---

## 步骤 4：在`containerd`中注册 MicRun

### 4.1 注册运行时

编辑或创建`/etc/containerd/config.toml`，添加以下内容：

> **注意**：如果该文件不存在，需要手动创建。containerd 可以在没有配置文件的情况下运行（使用默认配置），但注册 MicRun 运行时需要显式配置。

```toml
version = 2

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun.*"]
  container_annotations = ["org.openeuler.micrun.*"]
```

**配置说明**：
- `version = 2`：显示声明配置文件格式版本。
- `runtime_type`：指定运行时类型为 MicRun 的`shimv2`实现`io.containerd.mica.v2`。
- `pod_annotations` / `container_annotations`：声明 `MicRun` 支持的注解通配规则，用于接收来自 `Kubernetes` / `Pod` 的配置。containerd 使用通配匹配，需写成 `org.openeuler.micrun.*`。
> `runc`这样的容器运行时不在此配置的原因：`runc`是`containerd`的默认运行时，由系统内置配置自动处理，无需手动添加。

### 4.2 重启`containerd`

```bash
# 重启 containerd 使配置生效
systemctl restart containerd

# 验证配置（检查 micrun 运行时是否被正确加载）
# 注意：如果 containerd 配置文件是新创建的，需要确保 containerd 服务正确读取了配置
containerd config dump 2>/dev/null | grep -A 5 runtimes.micrun || echo "配置未生效，请检查配置文件格式"

# 如果配置未生效，可以检查 containerd 日志
journalctl -u containerd -n 20 | grep -i config
```

---

## 步骤 5：导入并运行`RTOS`容器

### 5.1 导入镜像到`containerd`

```bash
# 从 tarball 导入镜像
ctr image import <image_file>.tar

# 查看已导入的镜像
ctr image ls

# 查看特定镜像
ctr image ls | grep mica
```

**选项说明**：
| 命令 | 选项 | 说明 |
|------|------|------|
| `ctr image import` | - | 导入镜像`tarball`到`containerd` |
| `ctr image ls` | - | 列出所有已导入的镜像 |

### 5.2 使用`ctr`运行容器（开发者工具）

```bash
# 创建容器
ctr container create \
  --runtime io.containerd.mica.v2 \
  -t \
  --annotation org.openeuler.micrun.container.auto_close=true \
  <image_name>:<tag> \
  <container_name>

# 启动容器（会进入 RTOS shell）
ctr task start <container_name>

# 在另一个终端停止容器
ctr task kill -s 9 <container_name>

# 删除容器
ctr task delete <container_name>
ctr container delete <container_name>
```

**选项说明**：
| 选项 | 说明 |
|------|------|
| `--runtime` | 指定使用的容器运行时，MicRun 使用`io.containerd.mica.v2` |
| `-t` | 分配伪终端（`TTY`），支持交互式操作 |
| `--annotation` | 传递给 MicRun 的配置注解，格式为`key=value` |

**常用注解**：
| 注解 | 说明 | 示例 |
|------|------|------|
| `org.openeuler.micrun.container.auto_close` | 是否在 IO 关闭时自动停止容器 | `true`/`false` |
| `org.openeuler.micrun.container.auto_close_timeout` | 自动关闭超时时间 | `30s`（默认），支持 `60s`、`5m` 等格式 |

> **说明**：`auto_close` 默认为 `true`，当用户断开连接（如关闭终端）或超时后，容器会自动停止。如果希望容器保持运行以支持多次 attach，可以设置 `auto_close=false` 或使用较长的超时时间（>60秒）。

### 5.3 使用`nerdctl`运行容器（推荐用于生产）

`nerdctl`是 Docker 兼容的 CLI 工具，与 `ctr` 相比提供更友好的用户体验。

#### 5.3.1 运行容器

```bash
# 后台运行容器
nerdctl run -d \
  --runtime io.containerd.mica.v2 \
  --network=none \
  --name <container_name> \
  <image_name>:<tag>

# 创建但不启动容器
nerdctl create \
  --runtime io.containerd.mica.v2 \
  --network=none \
  --name <container_name> \
  <image_name>:<tag>

# 启动已创建的容器
nerdctl start <container_name>
```

**重要说明**：
- `--network=none`：RTOS 容器通常不需要网络，这是测试验证过的配置
- 如果需要网络，可以尝试省略此参数或配置 CNI 网络插件

#### 5.3.2 管理容器

```bash
# 查看运行中的容器
nerdctl ps

# 查看所有容器（包括已停止的）
nerdctl ps -a

# 停止容器
nerdctl stop <container_name>

# 强制停止容器
nerdctl stop -t 0 <container_name>

# 删除已停止的容器
nerdctl rm <container_name>

# 强制删除运行中的容器
nerdctl rm -f <container_name>

# 删除所有已停止的容器
nerdctl container prune -f
```

#### 5.3.3 查看容器信息

```bash
# 查看容器详细信息
nerdctl inspect <container_name>

# 查看容器状态
nerdctl inspect <container_name> --format '{{.State.Status}}'

# 查看容器 PID
nerdctl inspect <container_name> --format '{{.State.Pid}}'

# 查看容器日志
nerdctl logs <container_name>

# 实时跟踪日志
nerdctl logs -f <container_name>
```

#### 5.3.4 使用注解配置容器

`nerdctl` 使用 `-l`（label）参数来传递 MicRun 的注解配置：

```bash
# 设置 auto_close 为 false，容器不会自动停止
nerdctl run -d \
  --runtime io.containerd.mica.v2 \
  --network=none \
  -l org.openeuler.micrun.container.auto_close=false \
  --name <container_name> \
  <image_name>:<tag>

# 设置自动断开超时时间（秒）
nerdctl run -d \
  --runtime io.containerd.mica.v2 \
  --network=none \
  -l org.openeuler.micrun.container.auto_close_timeout=120 \
  --name <container_name> \
  <image_name>:<tag>

# 组合多个注解
nerdctl run -d \
  --runtime io.containerd.mica.v2 \
  --network=none \
  -l org.openeuler.micrun.container.auto_close=false \
  -l org.openeuler.micrun.container.auto_close_timeout=300 \
  --name <container_name> \
  <image_name>:<tag>

# 查看容器的注解配置
nerdctl inspect <container_name> --format '{{json .Config.Labels}}' | grep micrun
```

#### 5.3.5 交互式 TTY 模式

```bash
# 创建交互式 TTY 容器（前台运行）
# 注意：这种方式需要在一个交互式终端中运行
nerdctl run -it --rm \
  --runtime io.containerd.mica.v2 \
  --network=none \
  <image_name>:<tag>

# 创建 TTY 容器（后台运行）
# 注意：使用 -d 和 -t 组合时，容器会立即返回但保持运行
nerdctl run -d -t \
  --runtime io.containerd.mica.v2 \
  --network=none \
  --name <container_name> \
  <image_name>:<tag>
```

**交互行为速记**：

把 MicRun 的交互先按这四句话理解：

1. 想让容器继续跑、自己先离开：在 TTY 会话里按 `Ctrl+P`，再按 `Ctrl+Q`。
2. 想回到正在跑的容器：执行 `nerdctl attach <container_name>`。
3. 想停止容器：TTY 会话里按 `Ctrl+C`，或在外部执行 `nerdctl stop <container_name>`；在 UniProton shell 内也可以输入 `exit` 作为兼容退出方式。
4. 使用非 TTY 或管道输入时，把它当作普通字节流；不要依赖 `Ctrl+P Ctrl+Q` 或 `Ctrl+C` 这类终端按键。

常见情况如下：

| 你想做什么 | 推荐操作 | 容器状态 |
|------------|----------|----------|
| 暂时离开交互 shell | `Ctrl+P` 后 `Ctrl+Q` | 继续运行 |
| 重新进入容器 | `nerdctl attach <container_name>` | 继续运行 |
| 在 TTY 会话内停止容器 | `Ctrl+C` | 停止 |
| 正常停止容器 | `nerdctl stop <container_name>` | 停止，之后可 `nerdctl rm` |
| 强制清理容器 | `nerdctl rm -f <container_name>` | 停止并删除 |
| 在 UniProton shell 内退出 | 输入 `exit` 并回车 | 停止 |
| 管道输入命令 | `printf 'help\n' | nerdctl run -i ...` | 按输入和 auto-close 策略运行 |

TTY detach 默认使用 `Ctrl+P Ctrl+Q`。如果通过 `--detach-keys` 配置自定义序列，
MicRun 支持 `ctrl-a` 到 `ctrl-z`，也支持 `ctrl-@`、`ctrl-[`、`ctrl-\`、`ctrl-]`、
`ctrl-^`、`ctrl-_`、`ctrl-?` 这些常见符号控制键。序列中只要有一个片段非法，整条
自定义 detach 配置就不会生效。

> `Ctrl+C` 只在 TTY 会话里表示停止容器。非 TTY 或管道输入中的 `0x03` 仍按普通输入字节处理，避免破坏脚本和二进制输入。

**使用示例**：
```bash
# 启动交互式容器
nerdctl run -it --rm --runtime io.containerd.mica.v2 <image>

# 在容器中工作...
# 按 Ctrl+P，然后按 Ctrl+Q

# 容器在后台继续运行
nerdctl ps

# 重新连接到容器
nerdctl attach <容器名>

# 或使用容器 ID
nerdctl attach <container_id>
```

#### 5.3.6 与 `ctr` 命令对照

| 操作 | ctr 命令 | nerdctl 命令 |
|------|----------|--------------|
| 创建容器 | `ctr container create` | `nerdctl create` |
| 启动容器 | `ctr task start` | `nerdctl start` |
| 创建+启动 | `ctr run` | `nerdctl run` |
| 停止容器 | `ctr task kill` | `nerdctl stop` |
| 连接容器 | `ctr task attach` | `nerdctl attach` |
| 删除容器 | `ctr container delete` | `nerdctl rm` |
| 查看容器 | `ctr container ls` | `nerdctl ps` |
| 查看镜像 | `ctr image ls` | `nerdctl images` |
| 传递注解 | `--annotation` | `-l` (label) |
| 导入镜像 | `ctr image import` | `nerdctl load` |
| 导出镜像 | `ctr image export` | `nerdctl save` |

**命名空间说明**：
- `ctr` 默认使用 `default` 命名空间
- `nerdctl` 在此 openEuler Embedded 环境中默认使用 `default` 命名空间（与标准 nerdctl 不同，标准版本默认使用 `k8s.io`）
- 可以用 `ctr -n <namespace>` 或 `nerdctl -n <namespace>` 来指定命名空间
- 使用 `ctr namespace ls` 或 `nerdctl namespace ls` 查看所有命名空间

---

## 步骤 6：（可选）接入`Kubernetes`集群

> **详细指南**：完整的 Kubernetes 云边协同部署指南，请参考 **[Kubernetes 集成指南](user/kubernetes.md)**。

本节简要介绍 Kubernetes 集成的概念。完整的部署步骤、配置示例和故障排查，请查看专门的集成文档。

### 6.1 什么是云边协同

**云边协同**（Cloud-Edge Collaboration）是指在云侧集中管理多个边侧节点，实现：
- 统一的资源调度和管理
- 应用的自动化部署和升级
- 集中的监控和日志收集
- 高可用和负载均衡

### 6.2 MicRun 在 Kubernetes 中的角色

```
  Cloud - K3s Server                  Edge - K3s Agent + MicRun
┌─────────────────────┐              ┌───────────────────────────┐
│  Kubernetes API     │◄────────────►│  containerd               │
│  Server + Scheduler │  Management  │  ┌─────────────────────┐  │
│                     │     API      │  │ MicRun Runtime      │  │
└─────────────────────┘              │  └─────────────────────┘  │
                                     │  ┌─────────────────────┐  │
                                     │  │ RTOS Container      │  │
                                     │  └─────────────────────┘  │
                                     └───────────────────────────┘
```

**关键概念**：
- **RuntimeClass**：Kubernetes 资源，声明 MicRun 为容器运行时
- **K3s**：轻量级 Kubernetes 发行版，适合边缘场景

### 6.3 快速体验

**前提条件**：
1. 已完成步骤 1-5，MicRun 在边侧节点正常运行
2. 有两台机器（或虚拟机）：一台作为云侧，一台作为边侧
3. 网络互通

**核心步骤**（详细步骤请参考 [Kubernetes 集成指南](user/kubernetes.md)）：

1. **云侧部署**：安装 K3s Server
   ```bash
   curl -sfL https://get.k3s.io | sh -
   ```

2. **边侧部署**：安装 K3s Agent
   ```bash
   export K3S_URL="https://<cloud-ip>:6443"
   export K3S_TOKEN="<node-token>"
   curl -sfL https://get.k3s.io | K3S_URL=${K3S_URL} K3S_TOKEN=${K3S_TOKEN} sh -
   ```

3. **注册 RuntimeClass**
   ```bash
   kubectl apply -f - <<EOF
   apiVersion: node.k8s.io/v1
   kind: RuntimeClass
   metadata:
     name: micrun
   handler: micrun
   EOF
   ```

4. **部署 RTOS Pod**
   ```bash
   kubectl apply -f rtos-pod.yaml
   ```

**仓库测试入口**：

```bash
export EDGE_SSH_USER="${EDGE_SSH_USER:-root}"
export EDGE_IP="${EDGE_IP:-192.168.7.2}"
export HOST_TAP_IP="${HOST_TAP_IP:-192.168.7.1}"
export CLOUD_IP="${CLOUD_IP:-192.168.7.10}"

# 边侧节点已可通过 SSH 访问，且 rootfs 包含 server-capable K3s 时，
# 可验证单节点 K3s + MicRun；只有 agent 子命令时跳过该项。
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
micrun/tests/bin/test-k3s-single-node

# 云侧在本机 Docker 中启动，边侧通过 tap0 加入集群
export TEST_REMOTE_HOST="${EDGE_SSH_USER}@${EDGE_IP}"
export K3S_BIN="/usr/bin/k3s"
export K3S_CLOUD_SERVER_IMAGE="<k3s-server-image-matching-edge-version>"
export K3S_CLOUD_KUBECTL_BIN="k3s"
export K3S_CLOUD_KUBECTL_SUBCOMMAND="kubectl"
export K3S_CLOUD_NETWORK_PARENT="tap0"
export K3S_CLOUD_NETWORK_GATEWAY="${HOST_TAP_IP}"
export K3S_CLOUD_SERVER_IP="${CLOUD_IP}"
export K3S_EDGE_NODE_IP="${EDGE_IP}"
export K3S_EDGE_CONTAINERD_MODE="external"
export K3S_CONTAINERD_ADDRESS="/run/containerd/containerd.sock"
export K3S_EDGE_CTR_BIN="ctr"
export K3S_EDGE_CTR_SUBCOMMAND=""
export K3S_KUBELET_ARGS="--kubelet-arg=cgroups-per-qos=false --kubelet-arg=enforce-node-allocatable="
micrun/tests/bin/test-k3s-cloud-edge

# 云边用例默认会删除测试 Pod，并验证边侧 task/Xen domain 清理。
# 如需保留现场调试，可临时设置 K3S_E2E_KEEP_POD=true。

# 环境已经就绪后，验证 kubectl attach、边侧 task/Xen domain 和删除清理
export K3S_INTERACTION_MODE="auto"
micrun/tests/bin/test-k3s-interaction
```

`K3S_INTERACTION_MODE=auto` 会依次选择 Docker 云侧、已有本机控制面和边侧
单节点 K3s。若本机已经启动 K3s server，可显式使用：

```bash
export K3S_INTERACTION_MODE="local"
export K3S_LOCAL_KUBECONFIG="<path-to-local-kubeconfig>"
export K3S_LOCAL_KUBECTL_BIN="<path-to-k3s-or-kubectl>"
export K3S_LOCAL_KUBECTL_SUBCOMMAND="kubectl"
micrun/tests/bin/test-k3s-interaction
```

K3s 测试必须使用 rootfs 中已经构建好的 `/usr/bin/k3s`。脚本会配置运行中的
边侧节点上的 K3s service、CNI、RuntimeClass、containerd 和镜像导入；这些动作
属于 guest 运行态测试准备，不应安装 K3s，也不应解包或改写已经构建好的 QEMU
rootfs 产物。完整变量和约束见 [K3s 测试说明](../tests/k3s/README.md)。

交互测试创建的 RTOS Pod 默认使用 `stdin: true` 和 `tty: false`，通过
`kubectl attach -i` 发送 `help`、`uname` 来验证 UniProton shell。这样可避
免 K3s/containerd 在 `tty=true` 且同时打开 stderr 时拒绝 attach。若
oEE/QEMU guest 中 Kubernetes 删除等待超时，脚本默认只清理该 Pod 的运行态
containerd task/container 和 Xen domain，并移除已经 Terminating 的 Pod API
对象；不会修改构建产物中的 rootfs。

在 oEE/QEMU guest 中，kubelet 的 Pod QoS cgroup 可能无法完整创建。仓库的
K3s 脚本默认附加：

```bash
--kubelet-arg=cgroups-per-qos=false \
  --kubelet-arg=enforce-node-allocatable= \
  --kubelet-arg=fail-cgroupv1=false
```

这能避免 RTOS Pod 刚进入 Running 就被 kubelet 因 Pod cgroup 缺失而
`Stopping container`。较新的 kubelet 可能还需要 `fail-cgroupv1=false`；
较旧版本若提示 unknown flag，则通过 `K3S_KUBELET_ARGS` 移除该参数。这是
K3s 运行态参数，不需要修改 QEMU rootfs 产物。

### 6.4 学习路径

- **新手**：建议先完成步骤 1-5，熟悉 MicRun 基本用法后再尝试 Kubernetes 集成
- **有经验用户**：直接参考 [Kubernetes 集成指南](user/kubernetes.md) 进行完整部署
- **生产环境**：需要考虑高可用、监控、安全等，详见集成指南的高级用法章节

### 6.5 常见问题

**Q：必须使用 K3s 吗？**
A：不是必须的。MicRun 面向标准 Kubernetes CRI/RuntimeClass 集成；云侧和边侧
Kubernetes/K3s 版本应保持兼容。K3s 更轻量，适合边缘场景。

**Q：可以在单节点测试吗？**
A：可以。K3s 支持单节点模式，云侧和边侧可以在同一台机器上运行（仅用于测试）。

**Q：如何监控 RTOS 容器状态？**
A：使用 `kubectl get pods` 和 `kubectl describe pod` 查看。详细日志在边侧的 `/var/log/mica/mica-runtime.log`（如果日志目录不存在，请先创建：`sudo mkdir -p /var/log/mica`）。

**Q：Pod 无法启动怎么办？**
A：参考 [Kubernetes 集成指南 - 故障排查](user/kubernetes.md#故障排查) 章节，涵盖了常见问题和解决方法。

---

## 常见问题

### Q：`micran`和`micrun`有什么区别？

**A**：`micran`是旧名称，现在统一使用`micrun`。

### Q：为什么需要`-t`参数？

**A**：`-t`为容器分配伪终端，支持交互式操作和 `exit` 命令退出。

### Q：如何退出 RTOS 容器？

**A**：在容器中输入 `exit` 命令。完全清理需要：
```bash
ctr task delete <container_name>
ctr container delete <container_name>
```

### Q：镜像导入后如何查看完整名称？

**A**：运行 `ctr image ls`，格式为 `<registry>/<image-name>:<tag>`

### Q：如何调试容器启动问题？

**A**：
1. 查看 MicRun 日志：`tail -f /var/log/mica/mica-runtime.log`（需先创建目录）
2. 查看 containerd 日志：`journalctl -u containerd -f`

### Q：遇到 `ctr: task xxx: already exists` 错误怎么办？

**A**：这表示 containerd 认为容器的 task 还在运行。解决方法：

```bash
# 方法1：强制删除 task（大多数情况有效）
ctr task delete -f <container_name>
ctr container delete <container_name>

# 方法2：彻底清理（如果方法1无效）
xl destroy <container_name> 2>/dev/null
killall -9 containerd-shim-mica-v2 2>/dev/null
ctr task delete -f <container_name> 2>/dev/null
ctr container delete <container_name> 2>/dev/null
rm -rf /run/containerd/io.containerd.runtime.v2.task/default/<container_name>
```

---

## 更多资源

- [项目仓库](https://atomgit.com/openeuler/mcs)
- [问题反馈](https://atomgit.com/openeuler/mcs/issues)
- [`Mica-Xen`指导](https://embedded.pages.openeuler.org/master/features/mica/instruction.html)

---

**项目状态**：`MicRun`目前处于`Preview`阶段，欢迎反馈问题和建议。
