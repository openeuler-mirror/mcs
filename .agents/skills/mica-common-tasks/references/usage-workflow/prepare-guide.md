# 准备指南

## 1. 文档目标

这篇文档说明使用 MICA 前需要准备的 Linux/master 侧资源、Linux 侧组件、RTOS/client 侧镜像与运行前提。

它回答“使用 MICA 前系统需要具备什么”。实例配置和 `mica` 命令见 `lifecycle-guide.md`，TTY/UMT/RPC/GDB 使用见 `communication-guide.md`。

## 2. 资源划分模型

MICA 使用前需要先明确 Linux/master 和 RTOS/client 之间的资源边界：

- client OS 运行 CPU 或虚拟 CPU
- client OS 运行内存
- Linux 与 RTOS 之间的通信共享内存
- IRQ、IPI、event channel 或 notify 路径
- RTOS ELF 中的 resource table
- pedestal 相关配置文件或引导镜像

不同 pedestal 的资源来源不同：

- baremetal
  - CPU 和内存通常需要静态预留
  - 通信中断默认可使用 MICA 约定的 IPI

- jailhouse
  - CPU、中断、共享内存通常由 cell 文件静态描述

- xen
  - CPU、中断、共享内存可由 MICA/Xen 相关路径动态分配

- hetero
  - RISC-V/MCU 侧运行内存、通信共享内存和中断通常由 DTS 描述

## 3. Linux 侧资源预留

### 3.1 CPU 预留

baremetal 场景在部署 client OS 到目标核前，需要确保目标核未被 Linux 上线。

常见方式：

```text
maxcpus=<linux-cpu-count>
```

例如总共 4 个核，只让 Linux 启动 0、1、2 号核，可使用 `maxcpus=3`，将 CPU3 留给 client OS。

也可以在运行时下线目标 CPU：

```sh
echo 0 > /sys/devices/system/cpu/cpuX/online
```

注意：
- DTS 中 CPU 节点仍需要完整描述目标核
- 否则 MICA 无法再拉起未上线的核

### 3.2 共享内存预留

推荐通过 DTS `reserved-memory` 预留通信共享内存和 RTOS 运行内存。

baremetal 示例结构：

```dts
reserved-memory {
    client_os_reserved: client_os_reserved@7a000000 {
        reg = <0x00 0x7a000000 0x00 0x4000000>;
        no-map;
    };

    client_os_dma_memory_region: client_os-dma-memory@70000000 {
        compatible = "shared-dma-pool";
        reg = <0x00 0x70000000 0x00 0x100000>;
        no-map;
    };
};

mcs-remoteproc {
    compatible = "oe,mcs_remoteproc";
    memory-region = <&client_os_dma_memory_region>,
                    <&client_os_reserved>;
};
```

关键规则：
- `memory-region` 第一项必须是通信共享内存
- 第二项通常是 RTOS 运行内存

hetero 场景使用类似规则，但节点通常是 `oe,mcs_riscv_remoteproc`，并额外描述 RISC-V/MCU 通信中断寄存器和中断号。

### 3.3 ko 参数方式

无法修改 DTS 时，可以通过 ko 参数传入通信共享内存：

```sh
insmod mcs_km.ko rmem_base=0x70000000 rmem_size=0x100000
```

注意：
- ko 参数方式仅传入通信共享内存地址和大小
- MICA 不会自动预留内存
- 用户需要自行保证通信共享内存和 RTOS 运行内存没有被 Linux 或其他模块使用

### 3.4 资源预留确认

常用观察点：

```sh
cat /proc/cpuinfo
cat /proc/interrupts
cat /proc/iomem
```

预期现象：
- `/proc/cpuinfo` 中 Linux 使用的 CPU 数符合预留预期
- `/proc/interrupts` 中能看到 MICA 相关 IPI 或平台中断
- `/proc/iomem` 中能看到通信共享内存和 RTOS 运行内存范围

## 4. Linux 侧组件准备

Linux/master 侧通常需要：

- 内核态驱动
  - baremetal / hetero：`mcs_km.ko`
  - jailhouse：`jailhouse.ko`
  - xen：`xen-mcsback.ko`

- 用户态守护进程
  - `micad`

- 命令行工具
  - `mica` / `mica.py`

- 开发支持库
  - `libmica.a`
  - `libmica.so`

Yocto 构建的 MCS 镜像通常会自动安装并启动相关服务。自行安装或调试组件时，需要确认：

```sh
ps | grep micad
systemctl status micad
lsmod
```

正常状态：
- `micad` 正常运行
- 对应 pedestal 的 ko 已插入
- `/run/mica/` 下 socket 路径可由 `mica` 命令访问

Linux/master 侧组件和控制流详见：`../../../mica-linux-master/SKILL.md`。

## 5. RTOS 侧准备

RTOS/client 侧至少需要：

- RTOS ELF 镜像
- `.resource_table` 段
- OpenAMP / libmetal 初始化能力
- 与 Linux 侧一致的 shared memory 和 IRQ/notify 约定
- MICA client runtime 初始化
- 需要使用的 service，例如 TTY、UMT、RPC、GDB

已支持 MICA 的 RTOS，需要确认 RTOS 已使能 MICA 框架。通常表现为：
- 编译出的 RTOS ELF 包含 `.resource_table`
- RTOS 启动后会进入 MICA/OpenAMP 初始化流程
- RTOS 侧会创建对应 service endpoint

新 RTOS 对接详见：`../development-workflow/rtos-porting.md`。

RTOS/client 侧模块关系详见：`../../../mica-rtos-client/SKILL.md`。

## 6. 准备阶段失败回流

- `micad` 未运行或 socket 不可访问：`../debugging-workflow/lifecycle-diagnosis.md`
- RTOS ELF 缺少 `.resource_table`：`../debugging-workflow/lifecycle-diagnosis.md`
- 共享内存、IRQ、DTS、ko 参数不确定：`../debugging-workflow/boundary-diagnosis.md`
- 准备完成后需要验证基础链路：`../testing-workflow/testing-overview.md`
