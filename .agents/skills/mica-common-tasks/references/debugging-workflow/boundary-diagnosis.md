# 边界诊断

## 1. 文档目标

这篇文档专门处理边界判断问题：
- 当前故障首先落在 Linux/master、RTOS/client、OpenAMP、libmetal、pedestal 还是跨层同步失配
- 当前故障还应该继续在 MICA 本仓里追，还是已经跨到 OpenAMP / libmetal 层继续分析

它不重复 lifecycle、RPMsg 或 service 的具体机制，只解释分层判断、边界诊断与下一跳。

具体机制请直接看：
- lifecycle -> OpenAMP `remoteproc`：`../../../mica-lifecycle/references/openamp-remoteproc.md`
- virtio / RPMsg / name service / endpoint：`../../../mica-communication/references/openamp-rpmsg.md`
- `Running` 与 service ready 的区别：`communication-diagnosis.md`

## 2. 快速分层规则

边界诊断开始前，应先按 `debugging-overview.md` 的日志位置确认规则确认证据来源，避免把日志缺失误判为某一层没有输出。

遇到故障时，先根据最稳定的可观察症状做初始分层：

- `mica` 命令异常、create/status 控制行为异常：先怀疑 Linux/master 控制面
- 实例 `Running` 但服务不见或不 ready：先怀疑 RTOS/client service ready 或 RPMsg 绑定链
- endpoint、buffer、name service 异常：先怀疑 OpenAMP RPMsg 传输层
- IRQ、shared memory、cache、地址映射异常：先怀疑 libmetal、pedestal 或平台适配层
- 两侧状态各自看似成立但整体链路不闭合：先怀疑跨层同步失配

输出时不要只给层级结论，还要说明“为什么先怀疑这一层”。

## 3. 总体边界关系

在当前仓里，这三层的职责边界可以先这样理解：

- MICA
  - 负责实例生命周期编排、pedestal 选择、service 管理、共享内存池组织与调试入口分流

- OpenAMP
  - 负责 `remoteproc`、virtio、RPMsg 这些底层机制对象与协议流程

- libmetal
  - 负责地址映射、I/O region、phys/virt 转换与底层内存访问语义

因此很多问题并不是“仓里缺代码”，而是调用链已经自然进入了下一层。

## 4. 边界接缝

如果只看边界判断，最关键的几个接缝是：

### 4.1 lifecycle -> `remoteproc`

当问题已经追到：
- `remoteproc_config()`
- `remoteproc_load()`
- `remoteproc_start()`

说明分析重点已经从 MICA 生命周期编排进入 OpenAMP `remoteproc` 机制。

对应文档：
- `../../../mica-lifecycle/references/openamp-remoteproc.md`

### 4.2 backend -> libmetal 地址语义

当问题已经追到：
- backend `.mmap`
- `metal_io_region`
- `metal_io_phys_to_virt()`

说明分析重点已经从 MICA backend 进入 libmetal 的地址与 I/O 语义。

诊断重点：
- `metal_io_region` 描述的是共享内存或 I/O 区域的访问语义，不是单纯裸地址
- `metal_io_phys_to_virt()` / `metal_io_virt_to_phys()` 要能在正确 region 内闭环
- `metal_io_block_read()` / `metal_io_block_write()` 可能承载平台相关 I/O 访问语义
- cache flush / invalidate 可能决定 RPMsg/virtio 数据面是否可见

### 4.3 lifecycle / shared memory bridge -> virtio / RPMsg

当问题已经追到：
- `create_rpmsg_device()`
- `remoteproc_create_virtio()`
- `rpmsg_init_vdev()`

说明分析重点已经从生命周期桥接层进入 OpenAMP virtio / RPMsg 机制。

对应文档：
- `../../../mica-communication/references/openamp-rpmsg.md`

### 4.4 RPMsg service discovery -> MICA service binding

当问题已经追到：
- `mica_ns_bind_cb()`
- `rpmsg_ns_match()`
- `rpmsg_ns_bind_cb()`

说明问题正处在 OpenAMP RPMsg 服务发现与 MICA service 模型绑定的接缝上。

对应文档：
- `../../../mica-communication/references/openamp-rpmsg.md`

## 5. 本仓内问题判定

如果问题仍然主要表现为下面这些，优先继续看本仓代码：

- pedestal 选择是否正确
- `remoteproc_ops` 是否进入预期 backend
- 平台设备或 ioctl 是否正确调用
- `resource_table` 是否已经被正确取出或布置
- service 是否声明、注册、绑定到了正确对象
- 故障仍然能够在 MICA 层用生命周期、service 或 backend 语义解释

这时更合适的下一跳通常是：
- `../../../mica-lifecycle/references/lifecycle-overview.md`
- `../../../mica-lifecycle/references/openamp-remoteproc.md`
- `../../../mica-communication/references/openamp-rpmsg.md`
- `communication-diagnosis.md`

## 6. OpenAMP 或 libmetal 下钻判定

如果问题进一步表现为下面这些，通常说明不应继续只在本仓打转：

- `remoteproc_load()` 内部失败，但外层 lifecycle 条件已经成立
- `remoteproc_create_virtio()` 失败，但 `resource_table` 与共享内存前提看起来完整
- `rpmsg_init_vdev()` 失败，但 virtio 与共享内存布局前提看起来已成立
- endpoint 或 name service 看起来已经建立，但消息收发仍异常
- 地址表面正确，但 `phys_to_virt`、I/O 访问或 cache 语义异常

这时更合适的下一跳通常是：
- OpenAMP `remoteproc`
- OpenAMP `remoteproc_virtio`
- OpenAMP RPMsg
- libmetal 地址与 I/O 语义

这些问题不需要单独进入一个 libmetal 工作流；它们通常作为边界诊断或通信诊断中的平台语义分支处理。

## 7. 边界误判模型

在 MICA 这里，所谓“黑盒边界”很多时候并不是实现缺失，而是调用链已经自然跨层：

- 资源表、镜像、运行态问题继续进入 `remoteproc`
- virtio device、vring、RPMsg device 问题继续进入 OpenAMP virtio / RPMsg
- 地址映射与内存访问问题继续进入 libmetal

更准确的调试规则是：
- 先判断当前问题是否还能用 MICA 的生命周期、service 或 backend 语义解释
- 如果外层条件都已成立，再继续进入 OpenAMP / libmetal，而不是在本仓重复阅读同一层内容

跨层定位时不要把“当前文件没看到答案”误判成“系统没有实现”。当 `remoteproc`、RPMsg、共享内存、cache、IRQ 或地址映射行为无法只用 MICA 结构解释时，优先判断调用链是否已经进入 OpenAMP、libmetal 或 RTOS 历史实现层。

## 8. 跨层观察点

如果需要快速判断问题当前停在哪一层，最有价值的观察点通常是：

### 8.1 lifecycle / `remoteproc` 接缝

- 是否已经进入 `remoteproc_config()` / `remoteproc_load()` / `remoteproc_start()`
- `rproc->rsc_table` 与 `rproc->state` 是否已经形成下一阶段前提

### 8.2 virtio / RPMsg 接缝

- `create_rpmsg_device()` 是否已经成功
- `remoteproc_create_virtio()` 与 `rpmsg_init_vdev()` 是否已经成功

### 8.3 service bind 接缝

- `mica_ns_bind_cb()` 是否收到远端服务发现事件
- `rpmsg_ns_match()` / `rpmsg_ns_bind_cb()` 是否闭合到本地 service

### 8.4 notify / memory / address 接缝

- backend `.notify` 是否真的触发平台通知
- `.mmap` 与地址转换是否真的建立可访问语义

### 8.5 libmetal / I/O region 接缝

- `rproc->rsc_io` 是否能正确访问 resource table / vdev resource 区域
- `client->shbuf_io` 是否指向正确共享 buffer region
- `metal_io_phys_to_virt()` 与 `metal_io_virt_to_phys()` 是否能闭环
- `metal_io_block_read()` / `metal_io_block_write()` 是否符合平台访问语义
- cache flush / invalidate 是否覆盖发送和接收路径

## 9. 常见问题

以下问题来自开发板适配和平台接入过程。命中这些症状时，优先按本节处理。

### 9.1 共享内存 `memory-region` 顺序错误

症状：
- `mica start` 或 RPMsg 初始化异常
- Linux 与 RTOS 对共享内存理解不一致
- resource table、vring 或 RPMsg buffer 行为异常

原因：
- baremetal 的 `oe,mcs_remoteproc` 节点和 hetero 的 `oe,mcs_riscv_remoteproc` 节点中，`memory-region` 第一项必须是通信共享内存
- 第二项通常是 RTOS 运行系统内存

处理：
- 检查 DTS 中 `memory-region` 顺序
- 确认第一项是 `shared-dma-pool` 类型通信共享内存
- 确认 `/proc/iomem` 中能看到对应保留内存

### 9.2 使用 ko 参数方式但未真正预留内存

症状：
- 使用 `insmod mcs_km.ko rmem_base=... rmem_size=...`
- 后续通信共享内存或 RTOS 运行内存异常

原因：
- ko 参数方式只传入通信共享内存地址和大小
- MICA 不会自动预留内存，用户仍需自行保证该物理内存未被 Linux 或其他模块使用

处理：
- 优先使用 DTS `reserved-memory` 方式
- 如果必须用 ko 参数方式，确认通信共享内存和 RTOS 运行内存都已由平台侧可靠预留

### 9.3 libmetal / OpenAMP 库或头文件缺失

症状：
- 编译 MICA 组件或运行部署时报 `libmetal`、`openamp` 相关库或头文件未找到

原因：
- SDK 或 rootfs 未包含 MICA 依赖的 `libmetal`、`openamp` 组件
- 运行环境缺少对应动态库路径

处理：
- 编译阶段确认使用 openEuler Embedded SDK，且镜像开启 MCS 特性
- 运行阶段确认 rootfs 中存在 `libmetal`、`open_amp` 等库
- 确认动态库搜索路径正确

### 9.4 `cpu_logical_map` 符号未定义或未导出

症状：
- 编译或加载 `mcs_km.ko` 时提示 `cpu_logical_map` 符号未定义或未导出

原因：
- baremetal 驱动依赖内核 `cpu_logical_map` 发起 PSCI 操作
- 某些内核版本未导出该符号

处理：
- 使用带 MCS 适配补丁的内核
- 或在自定义内核中导出 `cpu_logical_map`
- 或确认是否可通过 `CONFIG_KPROBES` 等配置满足当前实现要求

### 9.5 RTOS 侧基础运行状态不可见

症状：
- Linux 侧已执行 `mica start`
- MICA 通信未建立，无法通过 TTY 观察 RTOS

处理：
- baremetal 场景优先尝试让 RTOS 串口驱动输出到用户可访问串口
- 串口不可用时使用内存打点，在 Linux 侧通过 `devmem` 读取 RTOS 写入值
- 先确认 RTOS 开始运行，再确认共享内存与中断，再确认 MICA/OpenAMP 初始化

## 10. 建议继续阅读

- `debugging-overview.md`
- `lifecycle-diagnosis.md`
- `communication-diagnosis.md`
- `../../../mica-lifecycle/references/openamp-remoteproc.md`
- `../../../mica-communication/references/openamp-rpmsg.md`
