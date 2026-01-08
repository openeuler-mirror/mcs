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
  --annotation org.openeuler.micrun.auto_disconnect=true \
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
| `-t` | 分配伪终端（`TTY`），支持交互式操作和`Ctrl-C`等控制字符 |
| `--annotation` | 传递给 MicRun 的配置注解，格式为`key=value` |

### 5.3 使用`nerdctl`运行容器（推荐用于生产）

```bash
# 运行容器
nerdctl run -d \
  --runtime io.containerd.mica.v2 \
  -l org.openeuler.micrun.auto_disconnect=true \
  <image_name>:<tag>

# 更新容器资源限制
nerdctl update --memory 1024m <container_id>
```

**选项说明**：
| 选项 | 说明 |
|------|------|
| `-d` | 后台运行容器 |
| `--runtime` | 指定容器运行时 |
| `-l` | 添加注解（`Annotation`），注意：`nerdctl`中`-l`对应注解，而非`Docker`中的`Label` |
| `--memory` | 设置内存限制 |

---

## 步骤 6：（可选）接入`Kubernetes`集群

### 6.1 注册为`RuntimeClass`

创建`<runtimeclass_file>.yaml`：

```yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: micrun
handler: micrun
```

应用配置：

```bash
kubectl apply -f <runtimeclass_file>.yaml
```

**字段说明**：
| 字段 | 说明 |
|------|------|
| `metadata.name` | `RuntimeClass`名称，`Pod`中引用时使用 |
| `handler` | 运行时处理器名称，对应`containerd`配置中的运行时名称 |

### 6.2 配置`kubelet`

```bash
# 启动`kubelet`时配置`CPU`管理策略
kubelet --cpu-manager-policy=static

# 可选：配置 CPU 隔离
# isolcpus, nohz_full 等参数根据实际需求调整
```

**选项说明**：
| 选项 | 说明 |
|------|------|
| `--cpu-manager-policy` | `CPU`管理策略，`static`适用于需要保证`CPU`资源的`RTOS`场景 |

### 6.3 运行`RTOS Pod`

创建`<pod_file>.yaml`：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: <pod_name>
spec:
  runtimeClassName: micrun
  containers:
  - name: <container_name>
    image: <image_name>:<tag>
    resources:
      limits:
        cpu: "1000m"
        memory: "512Mi"
```

应用配置：

```bash
kubectl apply -f <pod_file>.yaml
```

---

## 常见问题

### Q：`micran`和`micrun`有什么区别？

**A**：`micran`是过去的名称，现在统一使用`micrun`。如果文档中看到`micran`，应该改为`micrun`。

### Q：为什么需要`-t`参数？

**A**：`-t`（`--tty`）为容器分配伪终端，这对于`RTOS`容器非常重要：
- 支持交互式`shell`
- 能够识别控制字符（如`Ctrl-C`）
- 支持终端窗口大小调整

### Q：镜像导入后如何查看完整名称？

**A**：运行`ctr image ls`，镜像名称格式为：
```
<registry>/<image-name>:<tag>
```
例如：`localhost:5000/mica-zephyr-app:xen-0.1`

### Q：如何调试容器启动问题？

**A**：
1. 查看 MicRun 日志：`tail -f /tmp/micrun/runtime.log`
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
