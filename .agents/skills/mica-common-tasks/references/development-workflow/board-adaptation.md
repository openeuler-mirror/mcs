# 开发板适配工作流

## 1. 文档目标

这篇文档用于指导 agent 处理新硬件平台或新开发板接入 MICA 的任务，重点是资源预留、底座选择、RTOS 选择、Linux/RTOS 侧资源约定和 bring-up 验证。

## 2. 主要参考

- 外部参考：`yocto-meta-openeuler/docs/source/features/mica/mica_core/developer_guides/board_adaptation.rst`

外部参考内容应总结为本 skill 文档中的判断框架，不应只给外部路径让用户自行比对。

## 3. 适配决策

新开发板适配前需要先确认：

- 目标部署底座：baremetal、jailhouse、xen、hetero
- 目标 RTOS 是否已支持 MICA
- CPU、IRQ、shared memory 是否需要静态预留
- 资源由 DTS、kernel module 参数、cell 配置还是 Xen 动态分配提供
- Linux/master 与 RTOS/client 是否对共享内存和中断有一致约定

## 4. Linux 侧资源预留

不同底座的资源预留方式不同：

- baremetal
  - 需要确认目标 CPU 离线或预留
  - 需要确认通信共享内存和 RTOS 运行内存
  - DTS `memory-region` 中通信共享内存应位于第一项

- jailhouse
  - CPU、中断、共享内存通常由 cell 配置预留

- xen
  - CPU、IRQ、shared memory 可由 MICA/Xen 相关路径动态分配

- hetero
  - 通常通过 DTS 描述 RISC-V/MCU 侧运行内存、通信共享内存、中断寄存器和中断号

## 5. RTOS 侧资源约定

RTOS 侧需要与 Linux/master 保持一致：

- 共享内存基地址和大小
- vring 与 RPMsg buffer 使用范围
- 中断号或 event/doorbell 机制
- resource table 中的 virtio/RPMsg 描述
- RTOS 镜像入口地址和加载地址

## 6. bring-up 验证

新开发板适配至少验证：

- `/proc/cpuinfo` 中目标 CPU 状态符合预期
- `/proc/interrupts` 中 MICA 相关中断存在
- `/proc/iomem` 中共享内存和 RTOS 运行内存范围存在
- `mica create` 成功
- `mica start` 进入 `Running`
- `mica status` 能展示预期状态和服务
- TTY 能作为第一通信观察窗口
- UMT 能验证共享内存数据面

共用验证方法见：`../testing-workflow/adaptation-validation.md`
