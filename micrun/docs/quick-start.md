# MicRun 快速入门指南

> **本文档约定**：命令中以 `<...>` 包裹的内容（如 `<build_dir>`、`<container_name>`）表示需要用户根据实际情况替换的自定义值。

## 什么是 MicRun

`MicRun`是一个基于`containerd shimv2`的容器运行时，专为`Mica`项目设计，用于在同一`SoC`的不同`CPU`核上运行`RTOS`（`Real-Time Operating System`，实时操作系统）。

### 核心价值

通过`MicRun`，你可以：
- 用`Kubernetes/KubeEdge`管理`RTOS`业务
- 用容器镜像分发`RTOS`固件
- 在单一设备上实现`Linux`和`RTOS`的混合部署
- 复用云原生工具链（`ctr`、`nerdctl`等）

---

## 背景知识

### 什么是"边侧"（边缘计算）

**边侧**（`Edge`）指的是靠近数据源头或用户的一侧，相对于"云侧"（`Cloud`）而言。

```
                    云侧 (Cloud)
                       │
                    KubeEdge
                       │
    ┌──────────────────┼──────────────────┐
    │                  │                  │
 边侧节点A          边侧节点B          边侧节点C
 (工厂车间)         (智能门店)         (车载设备)
    │                  │                  │
   RTOS               RTOS               RTOS
 (实时控制)         (数据采集)         (自动驾驶)
```

### 什么是混合关键性系统（MCS）

**混合关键性系统**（`Mixed Criticality System`，简称`MCS`）是指在同一硬件平台上同时运行不同关键性级别任务的系统。

- **非关键任务**：`Linux`系统上的常规应用（如日志、`UI`）
- **关键任务**：`RTOS`上的实时控制任务（如电机控制、安全监控）

### 为什么选择容器化方案

将`RTOS`接入`Kubernetes`有几种常见方案：

| 方案 | 优点 | 缺点 |
|------|------|------|
| `CRD`+`Operator` | 灵活定制 | 需要为每个功能写代码 |
| `KubeVirt`类型 | 成熟框架 | 无法充分利用`Mica`能力 |
| **MicRun 容器化** | 复用云原生生态 | 需要适配`OCI`规范 |
| `WASM`微运行时 | 轻量级 | 无法混合部署 |

**MicRun 选择容器化方案的原因**：
1. 复用容器镜像分发机制，简化固件管理
2. 利用`Kubernetes`生态，降低运维复杂度
3. 渐进式云化，每一步都有对应方案

---

## 快速开始

使用`MicRun`包含以下步骤：

```
┌─────────────────────────────────────────────────────────────────┐
│  1. 构建系统镜像          构建包含 MicRun 的 openEuler Embedded │
│  2. 启动系统              进入构建好的系统                      │
│  3. 构建 RTOS 镜像        用 mica-image-builder 打包固件        │
│  4. 导入镜像              将镜像导入 containerd                 │
│  5. 注册运行时            在 containerd 中注册 MicRun           │
│  6. 运行 RTOS 容器        启动并测试 RTOS 容器                  │
└─────────────────────────────────────────────────────────────────┘
```

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
| `containerd` | 1.7.27+ | 容器引擎，推荐版本 |

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

**启动示例**：

```bash
sudo <qemu_path>/qemu-system-aarch64 \
  -device virtio-net-pci,netdev=net0 \
  -netdev tap,id=net0,ifname=tap0,script=/etc/qemu-ifup \
  -initrd openeuler-image-*.cpio.gz \
  -device loader,file=Image,addr=0x45000000 \
  -machine virt,gic-version=3 \
  -machine virtualization=true \
  -cpu cortex-a53 -smp 4 -m 4096 \
  -serial mon:stdio -nographic \
  -kernel xen-qemu-aarch64 \
  -append 'root=/dev/ram0 rw debugshell mem=1024M console=ttyAMA0,115200' \
  -dtb openeuler-image-mcs-qemu-aarch64-*.qemuboot.dtb
```

**参数说明**：
| 参数 | 说明 |
|------|------|
| `-device virtio-net-pci,netdev=net0` | 添加`virtio`网卡设备 |
| `-netdev tap,id=net0,ifname=tap0,script=/etc/qemu-ifup` | 配置`TAP`网络设备 |
| `-initrd openeuler-image-*.cpio.gz` | 指定`initrd`文件 |
| `-device loader,file=Image,addr=0x45000000` | 加载内核镜像到指定地址 |
| `-machine virt,gic-version=3` | 使用`virt`机器类型，`GIC`v3 |
| `-machine virtualization=true` | 启用虚拟化支持 |
| `-cpu cortex-a53 -smp 4 -m 4096` | `CPU`类型、核心数、内存大小 |
| `-serial mon:stdio -nographic` | 串口输出到标准输出，无图形界面 |
| `-kernel xen-qemu-aarch64` | 指定`Xen hypervisor` |
| `-append '...'` | 内核启动参数 |
| `-dtb *.qemuboot.dtb` | 指定设备树文件 |

**`QEMU`注意事项**：
- `QEMU`版本不宜过低，低版本存在影响`Xen`的`bug`
- 确保`Xen DTS`为`Domain-0`预留足够内存（建议`1536M`）
- 如需调整，在`conf/local.conf`中设置：
  ```bash
  QB_XEN_CMDLINE_EXTRA = "dom0_mem=1536M"
  ```

---

## 步骤 3：构建`RTOS`容器镜像

### 3.1 为什么需要特殊构建工具

标准`Docker`容器镜像使用`[os, arch]`二元组匹配（如`linux/amd64`）。

而`RTOS`容器需要`[board, os, arch, hypervisor]`四元组匹配：
- `board`：硬件板型（如`qemu-aarch64`）
- `os`：`RTOS`类型（如`zephyr`、`uniproton`）
- `arch`：`CPU`架构（如`arm64`、`amd64`）
- `hypervisor`：虚拟化类型（如`xen`、`openamp`）

因此需要使用专门的镜像打包工具`mica-image-builder`。

### 3.2 准备构建环境

```bash
# 进入构建产物目录
cd <build_dir>/output/micrun-files

# 初始化 Python 环境
uv init
uv venv
source .venv/bin/activate

# 安装依赖
uv pip install -r requirements.txt
```

> **替代方案**：如果没有`uv`，可以用传统方式：
> ```bash
> pip install -r requirements.txt
> python mica-image-builder.py
> ```

### 3.3 交互式构建镜像

```bash
# 启动交互式构建工具
uv run ./mica-image-builder
```

根据提示选择：
1. **`Pedestal`类型**：选择`xen`或`openamp`
2. **`OS`类型**：选择`zephyr`或`uniproton`
3. **固件文件**：选择你的`<firmware>.elf`或`<firmware>.bin`文件
4. **镜像名称**：使用默认或自定义名称

### 3.4 导出镜像

构建完成后，将镜像导出为`tarball`：

```bash
# 构建时会提示是否导出，选择导出
# 或手动导出已构建的镜像
docker save -o <image_file>.tar <image_name>:<tag>
# 例如：docker save -o my-rtos-image.tar localhost:5000/mica-zephyr-app:xen-0.1
```

---

## 步骤 4：在`containerd`中注册 MicRun

### 4.1 注册运行时

编辑`/etc/containerd/config.toml`，添加以下内容：

```toml
version = 2

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun."]
```

**配置说明**：
- `version = 2`：显示声明配置文件格式版本。
- `runtime_type`：指定运行时类型为 MicRun 的`shimv2`实现`io.containerd.mica.v2`。
- `pod_annotations`：声明`MicRun`支持的注解前缀，用于接收来自`Kubernetes`/`Pod`的配置。
> `runc`这样的容器运行时不在此配置的原因：`runc`是`containerd`的默认运行时，由系统内置配置自动处理，无需手动添加。

### 4.2 重启`containerd`

```bash
# 重启 containerd 使配置生效
systemctl restart containerd

# 验证配置（检查 micrun 运行时是否被正确加载）
containerd config dump | grep -A 5 runtimes.micrun
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
| `org.openeuler.micrun.container.auto_close_timeout` | 自动关闭超时时间（持续时间字符串或秒） | `30s`（默认30秒） |

> **说明**：`auto_close` 默认为 `true`，当用户断开连接（如关闭终端）或超时后，容器会自动停止。如果希望容器保持运行以支持多次 attach，可以设置 `auto_close=false` 或使用较长的超时时间（>60秒）。

### 5.3 使用`nerdctl`运行容器（推荐用于生产）

`nerdctl`是 Docker 兼容的 CLI 工具，与 `ctr` 相比提供更友好的用户体验。

#### 5.3.1 运行容器

```bash
# 后台运行容器
nerdctl run -d \
  --runtime io.containerd.mica.v2 \
  --network host \
  --name <container_name> \
  <image_name>:<tag>

# 创建但不启动容器
nerdctl create \
  --runtime io.containerd.mica.v2 \
  --network host \
  --name <container_name> \
  <image_name>:<tag>

# 启动已创建的容器
nerdctl start <container_name>
```

**重要说明**：
- `--network host`：对于 RTOS 容器，通常需要使用 host 网络模式，因为嵌入式环境可能没有配置 CNI 网络插件
- 如果系统配置了 CNI 网络插件，可以省略 `--network host` 或使用其他网络模式

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

# 重启容器
nerdctl restart <container_name>

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
  --network host \
  -l org.openeuler.micrun.container.auto_close=false \
  --name <container_name> \
  <image_name>:<tag>

# 设置自动断开超时时间（秒）
nerdctl run -d \
  --runtime io.containerd.mica.v2 \
  --network host \
  -l org.openeuler.micrun.container.auto_close_timeout=120s \
  --name <container_name> \
  <image_name>:<tag>

# 组合多个注解
nerdctl run -d \
  --runtime io.containerd.mica.v2 \
  --network host \
  -l org.openeuler.micrun.container.auto_close=false \
  -l org.openeuler.micrun.container.auto_close_timeout=300s \
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
  --network host \
  <image_name>:<tag>

# 创建 TTY 容器（后台运行）
# 注意：使用 -d 和 -t 组合时，容器会立即返回但保持运行
nerdctl run -d -t \
  --runtime io.containerd.mica.v2 \
  --network host \
  --name <container_name> \
  <image_name>:<tag>
```

**退出容器**：
- 在 TTY 中输入 `exit` 命令并回车，容器会停止（推荐方式）
- 使用 `Ctrl+P` 然后 `Ctrl+Q` 可以**临时退出**容器（容器继续运行，仅 TTY 模式）
- 外部终止：使用 `ctr task kill -s SIGTERM <容器名>` 或 `SIGKILL`

**Detach 和 Attach 功能说明**：
- `Ctrl+P, Ctrl+Q` 序列可以让你临时退出容器 shell，容器在后台继续运行
- 这是类似 Docker 的行为，方便你从交互式会话中临时脱离
- 退出后容器继续运行，可以使用 `nerdctl attach <容器名>` 重新连接

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
- `nerdctl` 默认使用 `k8s.io` 命名空间
- 两者创建的容器互相不可见，需要用 `ctr -n k8s.io` 来查看 nerdctl 创建的容器

---

## 步骤 6：（可选）接入`Kubernetes`集群

> **详细指南**：完整的 Kubernetes 云边协同部署指南，请参考 **[Kubernetes 集成指南](k8s-integration.md)**。

本节简要介绍 Kubernetes 集成的概念。完整的部署步骤、配置示例和故障排查，请查看专门的集成文档。

### 6.1 什么是云边协同

**云边协同**（Cloud-Edge Collaboration）是指在云侧集中管理多个边侧节点，实现：
- 统一的资源调度和管理
- 应用的自动化部署和升级
- 集中的监控和日志收集
- 高可用和负载均衡

### 6.2 MicRun 在 Kubernetes 中的角色

```
云侧（K3s Server）          边侧（K3s Agent + MicRun）
┌──────────────┐           ┌──────────────────────────┐
│ Kubernetes   │           │  containerd              │
│ API Server   │◄──────────┤  ┌────────────────────┐  │
│              │  管理接口  │  │ MicRun Runtime     │  │
│  Scheduler   │           │  │ (Shim v2)          │  │
│              │           │  └────────────────────┘  │
└──────────────┘           │  ┌────────────────────┐  │
                           │  │ RTOS Container     │  │
                           │  │ (Zephyr/UniProton) │  │
                           │  └────────────────────┘  │
                           └──────────────────────────┘
```

**关键概念**：
- **RuntimeClass**：Kubernetes 资源，声明 MicRun 为容器运行时
- **K3s**：轻量级 Kubernetes 发行版，适合边缘场景
- **云侧**：运行 K3s Server，提供管理 API
- **边侧**：运行 K3s Agent + containerd + MicRun，实际运行 RTOS 容器

### 6.3 快速体验

**前提条件**：
1. 已完成步骤 1-5，MicRun 在边侧节点正常运行
2. 有两台机器（或虚拟机）：一台作为云侧，一台作为边侧
3. 网络互通

**核心步骤**（详细步骤请参考 [Kubernetes 集成指南](k8s-integration.md)）：

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

### 6.4 学习路径

- **新手**：建议先完成步骤 1-5，熟悉 MicRun 基本用法后再尝试 Kubernetes 集成
- **有经验用户**：直接参考 [Kubernetes 集成指南](k8s-integration.md) 进行完整部署
- **生产环境**：需要考虑高可用、监控、安全等，详见集成指南的高级用法章节

### 6.5 常见问题

**Q：必须使用 K3s 吗？**
A：不是必须的。MicRun 兼容标准 Kubernetes（1.28+），但 K3s 更轻量，适合边缘场景。

**Q：可以在单节点测试吗？**
A：可以。K3s 支持单节点模式，云侧和边侧可以在同一台机器上运行（仅用于测试）。

**Q：如何监控 RTOS 容器状态？**
A：使用 `kubectl get pods` 和 `kubectl describe pod` 查看。详细日志在边侧的 `/var/log/mica/mica-runtime.log`。

**Q：Pod 无法启动怎么办？**
A：参考 [Kubernetes 集成指南 - 故障排查](k8s-integration.md#故障排查) 章节，涵盖了常见问题和解决方法。

---

## 常见问题

### Q：`micran`和`micrun`有什么区别？

**A**：`micran`是过去的名称，现在统一使用`micrun`。如果文档中看到`micran`，应该改为`micrun`。

### Q：为什么需要`-t`参数？

**A**：`-t`（`--tty`）为容器分配伪终端，这对于`RTOS`容器非常重要：
- 支持交互式`shell`
- 支持终端窗口大小调整
- 支持通过输入 "exit" 命令退出容器

### Q：如何退出 RTOS 容器？

**A**：在 RTOS 交互式 shell 中输入 `exit` 命令并按回车：
- Shim 会检测到 "exit" 命令
- 触发容器停止（状态变为 STOPPED）
- **重要**：Shim 继续运行，等待后续 API 调用

要完全清理容器和 shim，需要显式执行：
```bash
ctr task delete <container_name>
ctr container delete <container_name>
```

### Q：镜像导入后如何查看完整名称？

**A**：运行`ctr image ls`，镜像名称格式为：
```
<registry>/<image-name>:<tag>
```
例如：`localhost:5000/mica-zephyr-app:xen-0.1`

### Q：如何调试容器启动问题？

**A**：
1. 查看 MicRun 日志：`tail -f /var/log/mica/mica-runtime.log`
2. 查看`containerd`日志：`journalctl -u containerd -f`
3. 使用`ctr task metrics <container_id>`查看容器状态

### Q：遇到 `ctr: task xxx: already exists` 错误怎么办？

**A**：这个错误表示 containerd 认为该容器的 task 还在运行。常见原因和解决方法：

#### 场景 1：手动使用 `xl destroy` 销毁了容器

如果您手动使用 `xl destroy` 销毁了 Xen domain，但 containerd 的 task 元数据仍然存在，需要手动清理：

```bash
# 1. 查看当前状态
ctr task ls              # 检查 task 状态
xl list                 # 检查 Xen domain 状态

# 2. 清理 containerd 状态
ctr task delete -f <container_name>
ctr container delete <container_name>

# 3. 重新启动容器
ctr container create --runtime io.containerd.mica.v2 \
  -t <image> <container_name>
ctr task start -d <container_name>
```

#### 场景 2：shim 进程异常退出

如果 shim 进程异常退出，但 task 元数据还存在：

```bash
# 1. 杀掉残留的 shim 进程
killall -9 containerd-shim-mica-v2

# 2. 清理 task 和 container
ctr task delete -f <container_name>
ctr container delete <container_name>

# 3. 清理状态文件
rm -rf /run/containerd/io.containerd.runtime.v2.task/default/<container_name>

# 4. 重新创建和启动
```

#### 场景 3：容器状态不一致

如果 Xen domain、shim 进程和 containerd task 三者状态不一致，最彻底的清理方法：

```bash
# 停止并删除所有相关资源
xl destroy <container_name> 2>/dev/null
killall -9 containerd-shim-mica-v2 2>/dev/null
ctr task delete -f <container_name> 2>/dev/null
ctr container delete <container_name> 2>/dev/null
rm -rf /run/containerd/io.containerd.runtime.v2.task/default/<container_name>

# 重新开始
ctr container create --runtime io.containerd.mica.v2 \
  -t <image> <container_name>
ctr task start -d <container_name>
```

**提示**：大多数情况下，`ctr task delete -f` 就能解决问题。如果问题持续，使用场景3的彻底清理方法。

---

## 更多资源

- [项目仓库](https://atomgit.com/openeuler/mcs)
- [问题反馈](https://atomgit.com/openeuler/mcs/issues)
- [`Mica-Xen`指导](https://embedded.pages.openeuler.org/master/features/mica/instruction.html)

---

**项目状态**：`MicRun`目前处于`Preview`阶段，欢迎反馈问题和建议。
