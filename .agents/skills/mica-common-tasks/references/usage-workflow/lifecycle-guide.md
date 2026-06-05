# 生命周期使用指南

## 1. 文档目标

这篇文档说明 MICA 实例配置、生命周期命令和状态查看方式，包括 `mica create/start/stop/rm/status/set/gdb` 的使用。

## 2. 推荐步骤
1. 先确认准备阶段已完成，见 `prepare-guide.md`
2. 编写或选择 `[Mica]` 配置文件
3. 使用 `mica create/start/status/stop/rm` 管理实例
4. 如需在线资源调整，使用 `mica set`
5. 如需调试，使用 `mica gdb`
6. 需要代码路径时，查看本文的代码定位章节，或进入 `../../../mica-lifecycle/SKILL.md`

## 3. 命令速查

### 3.1 `mica create <conf>`

根据配置文件创建一个 client OS 实例。

```sh
mica create qemu-zephyr-rproc.conf
```

配置文件承载实例名称、RTOS 镜像、CPU、底座、调试开关等信息。

### 3.2 `mica start <name>`

启动指定实例。

```sh
mica start qemu-zephyr
```

### 3.3 `mica stop <name>`

停止指定实例。

```sh
mica stop qemu-zephyr
```

### 3.4 `mica rm <name>`

删除指定实例。

```sh
mica rm qemu-zephyr
```

### 3.5 `mica status`

查询实例状态和关联服务。

```sh
mica status
```

典型 `Offline` 输出：

```text
Name                          Assigned CPU        State               Service
qemu-zephyr                   3                   Offline
```

典型 `Running` 输出：

```text
Name                          Assigned CPU        State               Service
qemu-zephyr                   3                   Running             rpmsg-tty1(/dev/ttyRPMSG0) rpmsg-tty(/dev/ttyRPMSG1)
```

字段含义：
- `Name`：实例名称
- `Assigned CPU`：分配给 client OS 的 CPU 或 CPU 范围
- `State`：实例生命周期状态，常见为 `Offline`、`Running`
- `Service`：已关联服务及可见设备

### 3.6 `mica set <name> <resource> <value>`

在线更新实例资源。

当前主要用于 Xen 部署下更新：
- `CPU`
- `VCPU`
- `CPUWeight`
- `CPUCapacity`
- `Memory`

示例：

```sh
mica set qemu-uniproton-xen Memory 1024
mica set qemu-uniproton-xen CPUCapacity 100
```

其中：
- `Memory 1024` 表示将内存更新为 1024 MB
- `CPUCapacity 100` 表示将 CPU 算力更新为 1 个物理 CPU

### 3.7 `mica gdb <name>`

对支持调试的实例启动 GDB client。

```sh
mica gdb rpi4-uniproton-debug
```

前提：
- 配置文件中 `Debug=yes`
- RTOS 镜像支持调试后端

## 4. 典型生命周期顺序

```text
mica create <conf>
mica status
mica start <name>
mica status
mica stop <name>
mica status
mica rm <name>
```

如果实例已由 `AutoBoot=yes` 或镜像启动阶段自动创建，用户可能只需要从 `mica status` 和 `mica start <name>` 开始。

## 5. 配置文件基本格式

所有实例配置项都应放在 `[Mica]` 段中。

```ini
[Mica]
Name=qemu-zephyr
CPU=3
ClientPath=/lib/firmware/qemu-zephyr-rproc.elf
AutoBoot=yes
```

## 6. 通用配置项

### 6.1 `Name`

实例名称，必须唯一。

约束：
- 字符串
- 长度小于 32
- 所有底座支持

影响：
- `mica start <name>`、`mica stop <name>`、`mica rm <name>` 的实例匹配
- `mica status` 的 `Name` 展示

### 6.2 `CPU`

为 client OS 分配的 CPU 信息。

不同底座语义：
- baremetal：仅支持单核，范围为 `0 ~ nproc - 1`
- hetero：指定为 `riscv`
- jailhouse：实际资源由 cell 文件配置
- xen：支持多核或范围，例如 `1-3`

### 6.3 `ClientPath`

client OS 镜像路径。

约束：
- 字符串
- 长度小于 128
- 需要是绝对路径
- 当前配置语义面向 ELF 镜像

### 6.4 `AutoBoot`

是否在 `micad` 启动时自动拉起实例。

取值：
- `yes`
- `no`

默认值：`no`

### 6.5 `Pedestal`

指定部署底座。

常见取值：
- `baremetal`
- `jailhouse`
- `xen`
- `hetero`

默认值：`baremetal`

### 6.6 `PedestalConf`

指定部署底座关联的配置文件。

不同底座语义：
- baremetal：通常不涉及
- hetero：指定用于引导 RTOS 的 bin 文件
- jailhouse：指定 RTOS 所需 Non-Root Cell 配置文件
- xen：指定用于引导 RTOS 的 bin 文件

### 6.7 `Debug`

表示 client OS 二进制是否支持调试。

取值：
- `yes`
- `no`

默认值：`no`

如果设置为 `yes`，并且 RTOS 镜像支持调试后端，可以通过：

```sh
mica gdb <name>
```

启动 GDB client。

## 7. Xen 配置项

### 7.1 `VCPU`

为 client OS 分配的虚拟 CPU 数量。

约束：
- 整数
- 范围为 `1 ~ nproc`
- 仅 Xen 支持

### 7.2 `MaxVCPU`

在线扩容时可为 client OS 分配的最大虚拟 CPU 数量。

默认值：与 `VCPU` 一致。

### 7.3 `CPUWeight`

为 client OS 分配的 CPU 算力权重。

约束：
- 整数
- 范围为 `1 ~ 65535`

默认值：`256`

### 7.4 `CPUCapacity`

为 client OS 分配的 CPU 算力百分比。

约束：
- 整数
- 范围为 `0 ~ 100 * nproc`

示例：
- `100` 表示 1 个物理 CPU 算力
- `50` 表示半个物理 CPU 算力
- `0` 表示不限制

### 7.5 `Memory`

为 client OS 分配的内存大小，单位为 MB。

### 7.6 `MaxMemory`

在线扩容时可为 client OS 分配的最大内存大小，单位为 MB。

约束：
- 大于或等于 `Memory`

默认值：等于 `Memory`

## 8. 示例配置

### 8.1 baremetal

```ini
[Mica]
Name=qemu-zephyr
CPU=3
ClientPath=/lib/firmware/qemu-zephyr-rproc.elf
AutoBoot=yes
```

语义：
- 实例名为 `qemu-zephyr`
- 在 CPU3 上加载并启动 `/lib/firmware/qemu-zephyr-rproc.elf`
- `micad` 启动时自动拉起

### 8.2 jailhouse

```ini
[Mica]
Name=qemu-zephyr-ivshmem
CPU=3
ClientPath=/lib/firmware/qemu-zephyr-ivshmem.elf
AutoBoot=no
Pedestal=jailhouse
PedestalConf=/usr/share/jailhouse/cells/qemu-arm64-zephyr-mcs-demo.cell
```

语义：
- 使用 jailhouse 部署底座
- `PedestalConf` 指向 Non-Root Cell 配置文件

### 8.3 debug

```ini
[Mica]
Name=rpi4-uniproton-debug
CPU=3
ClientPath=/lib/firmware/rpi4-uniproton-debug.elf
AutoBoot=yes
Debug=yes
```

语义：
- client OS 支持 GDB 调试
- 可使用 `mica gdb rpi4-uniproton-debug`

### 8.4 xen

```ini
[Mica]
Name=qemu-uniproton-xen
CPU=1-3
ClientPath=/lib/firmware/qemu-uniproton-xen.elf
AutoBoot=no
Pedestal=xen
PedestalConf=/lib/firmware/qemu-uniproton-xen.bin
Memory=1024
VCPU=1
CPUCapacity=50
```

语义：
- 使用 Xen 部署底座
- RTOS 运行在 CPU1、CPU2、CPU3 相关资源范围内
- 内存为 1024 MB
- CPU 算力为半个物理 CPU

### 8.5 hetero

```ini
[Mica]
Name=liteos
CPU=riscv
ClientPath=/lib/firmware/liteos.elf
AutoBoot=no
Pedestal=hetero
PedestalConf=/lib/firmware/liteos.bin
```

语义：
- 使用 hetero 部署底座
- 在 RISC-V 核上运行 RTOS
- `PedestalConf` 指向用于引导 RTOS 的 bin 文件

## 9. 代码路径影响

配置字段最终会影响：

- `mica create <conf>` 的参数解析
- `micad` create 消息构造与实例对象创建
- `mica_client` 结构体字段
- pedestal/backend 选择
- remoteproc 加载与启动参数
- service 可见状态和 GDB 调试能力

## 10. 命令代码定位

### 10.1 `mica create <conf>`

优先查看：
- `mica/micad/socket_listener.c`
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`

关注点：
- create 消息校验
- `struct create_msg`
- `struct mica_client` 填充
- `mica_create()` -> `create_client()`

### 10.2 `mica start <name>`

优先查看：
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`

关注点：
- `mica_start()`
- `load_client_image()`
- `start_client()`
- `create_rpmsg_device()`

### 10.3 `mica stop <name>`

优先查看：
- `library/mica/mica.c`

关注点：
- `mica_stop()`
- `remoteproc_stop()`
- `mica_unregister_all_services()`
- `release_rpmsg_device()`

### 10.4 `mica rm <name>`

优先查看：
- `library/mica/mica.c`

关注点：
- `mica_remove()`
- stop 是否先被触发
- `destory_client()`

### 10.5 `mica status`

优先查看：
- `mica/micad/socket_listener.c`
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`

关注点：
- `show_status()`
- `mica_status()`
- `show_client_status()`
- `mica_print_service()`

### 10.6 `mica gdb <name>`

优先查看：
- `mica/micad/socket_listener.c`
- `mica/micad/services/debug/`

### 10.7 `mica set <name> <resource> <value>`

优先查看：
- `library/mica/mica.c`
- pedestal 相关实现

关注点：
- `mica_set()`
- `ped_ops->set_resource()`
- Xen 等底座特定资源更新路径

## 11. 失败回流

- `mica create/start/stop/rm` 失败：`../debugging-workflow/lifecycle-diagnosis.md`
- `mica status` 正常但服务不可用：`../debugging-workflow/communication-diagnosis.md`
- shared memory、IRQ、notify、地址映射不清：`../debugging-workflow/boundary-diagnosis.md`
- 需要验证基础链路：`../testing-workflow/testing-overview.md`

## 12. 输出要求
至少说明：
- 配置项语义
- 进入的关键结构体字段
- 最终影响的模块/阶段
