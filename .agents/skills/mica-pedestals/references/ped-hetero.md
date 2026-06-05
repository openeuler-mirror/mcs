# ped-hetero

## 1. 文档目标

这篇文档解释 hetero pedestal 在 MICA 中的完整落地方式：它如何把 MICA 的生命周期骨架落到 Linux/master 侧当前已实现的 `riscv_rproc.c` backend、`/dev/mcs` 设备、RISC-V 专用共享内存、resource table 首页布局、RISC-V 中断通知，以及远端 client 的启动与消息收发。

它主要回答：
- hetero 当前对应哪些源码文件
- hetero 为什么在 Linux/master 侧主要落到 `riscv_rproc.c`
- hetero 用的 KO、设备节点、ioctl 是什么
- create/start/stop/remove 在 hetero 下分别做什么
- 共享内存和 resource table 如何建立
- 隐含在 lifecycle 背后的 `config/mmap/notify` 这些钩子何时被调用、做了什么
- 通知路径如何闭环
- 远端 client 侧需要满足什么契约

## 2. 相关源码
- `library/remoteproc/riscv_rproc.c`
- `mcs_km/mcs_km.c`
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`
- `library/rpmsg_device/rpmsg_vdev.c`
- `library/rpmsg_device/rpmsg_service.c`
- `rtos/libmica/src/pedestals/hetero.c`
- `rtos/libmica/src/mica_service.c`
- `rtos/libmica/src/services/tty_service.c`
- `rtos/libmica/src/services/umt_service.c`
- `rtos/libmica/src/services/rpc_service.c`

## 3. hetero 底座定位

hetero 是 MICA 中“异构远端”这一大类 pedestal。

但从当前仓内 Linux/master 侧代码来看，真正已经落地的 hetero 主分支主要就是：
- `client->ped == HETERO`
- 且 `client->ped_setup.cpu_str == "riscv"`
- 然后进入 `rproc_riscv_ops`

也就是说：
- 文档名叫 `ped-hetero`
- 但当前实现细节主要落在 `library/remoteproc/riscv_rproc.c`
- 所以本文实际上是在解释“hetero 大类下当前已实现的 riscv 子分支”

在代码里，hetero 也不是一个“薄配置”，而是一套完整 backend，只是它和 baremetal 共用 `/dev/mcs`，再通过 `IOC_SET_PED_TYPE` 在 KO 里切换到 riscv 这条实现路径。

## 4. 生命周期实现

### 4.1 create
- `create_client()` 发现：
  - `client->ped == HETERO`
  - 且 `strcmp(client->ped_setup.cpu_str, "riscv") == 0`
  - 因此选择 `rproc_riscv_ops`
- `remoteproc_init()` 进入 hetero 的 `rproc_init()`
- `rproc_init()` 打开 `/dev/mcs`
- `rproc_init()` 通过 `IOC_SET_PED_TYPE` 把当前 fd 的 pedestal 类型切到 `MCS_KM_PED_RISCV`
- 创建 `struct rproc_pdata`
- 把 `rproc->priv`、`rproc->ops` 等挂好

这一阶段的结果是：
- hetero 实例具备后续配置与启动的前提
- 当前 fd 已经不再走 baremetal 默认分支，而是明确进入 riscv 这条 KO 子路径
- 但远端还没有真正跑起来

### 4.2 start
主链是：
1. `load_client_image()`
2. `remoteproc_config()` -> hetero 的 `rproc_config()`
3. `remoteproc_load()`（理论上是后续标准路径，但当前 hetero 通常会在 `.config()` 后直接进入 ready 快路径）
4. `start_client()` -> `remoteproc_start()` -> hetero 的 `rproc_start()`
5. `create_rpmsg_device()`

也就是说，hetero 的 `start` 不只是一个 `IOC_MCUON`，而是至少包含：
- 配置 remoteproc
- 处理资源表
- 设置 `bootaddr`
- 必要时装载 `PedestalConf` 指向的 bin
- 真正拉起 RISC-V/MCU
- 再建立 RPMsg 通信设备

### 4.3 stop
`stop` 通过 `remoteproc_shutdown()` 落到 hetero 的 `rproc_shutdown()`。

它的主链是：
1. 遍历 `rproc->mems`
2. 对每个映射的 memory 做 `munmap()`
3. 从 `rproc->mems` 链表中删除
4. 释放 `mem->io` 和 `mem`
5. 通过 pipe 通知 notifier 线程退出
6. 清空 `rproc->rsc_table`、`rproc->rsc_len`、`rproc->bitmap`

这一阶段的结果是：
- 运行态映射被回收
- 用户态 notifier 线程退出
- remoteproc 资源指针被清掉

这里需要特别注意一点：
- 当前 hetero/riscv 用户态 `.shutdown()` 里没有像 baremetal 那样显式把 `reserved[0]` 写成 `CPU_OFF_FUNCID` 再经由 `.notify()` 发起停机协商
- 所以如果问题是“Linux 侧 stop 了，但远端状态并未按预期退回”，不能直接套用 baremetal 的理解，要继续联动检查 RTOS/client 侧停机语义

### 4.4 remove
`remove` 主要在 `rproc_remove()` 中完成。

它的核心逻辑是：
1. 关闭 `mcs_fd`
2. 释放 `rproc_pdata`
3. 把 `rproc->priv` 置空

这一阶段的结果是：
- hetero backend 私有状态被回收
- 与 baremetal 不同，这里没有按 `g_client_list` 全局协调 notifier 基础设施是否保留的逻辑
- notifier 线程的退出动作已经在 `.shutdown()` 里通过 pipe 完成

### 4.5 生命周期隐含钩子

有几类函数虽然不是 `mica create/start/stop/remove` 这些顶层 API 显式点名调用的，但它们实际上是 hetero lifecycle 能成立的关键钩子。

#### `rproc_config()`
调用时机：
- `mica_start()` -> `load_client_image()` -> `remoteproc_config()`

作用：
- 通过 `IOC_QUERY_MEM` 获取 riscv 这一路 reserved memory 信息
- 初始化共享内存池
- 解析 ELF header 和 resource table
- 校验 resource table 大小不能超过一个 page
- 直接把 resource table 拷贝到共享内存第一页
- 调 `remoteproc_set_rsc_table()` 挂入 remoteproc
- 通过 loader 取出 entry，写入 `rproc->bootaddr`
- 把 `rproc->state` 置为 `RPROC_READY`

这里要注意：
- hetero/riscv 不会像 baremetal 那样去 `mmap` ELF 中 `resource_table` 的原始地址
- 它在 `.config()` 里就把 resource table 拷到共享内存第一页，再以这份共享内存副本作为后续 OpenAMP 看到的资源表
- 当前 hetero 路径并不是像 baremetal 那样先拿一整块共享内存，再按 `cpu_id * SHM_POOL_SIZE` 去切 per-client 分段
- 当前实现是直接以 `IOC_QUERY_MEM` 返回的 riscv 内存首地址为基础，把第一页保留给 resource table

#### `rproc_mmap()`
调用时机：
- 后续 remoteproc / rpmsg 建立过程中会通过 `remoteproc_mmap()` 使用
- 当前 `.config()` 虽然没有像 baremetal 那样显式通过 `remoteproc_mmap()` 去安放 resource table，但 vring / shared buffer 建立仍然要依赖 `.mmap()` 这条能力

作用：
- 把物理地址映射到用户态可访问的 virtual address
- 创建 `remoteproc_mem` 与 `metal_io_region`
- 挂入 `rproc->mems`

也就是说，hetero 的 shared memory / vring / buffer 真正能被 OpenAMP 使用，不只是因为有 riscv reserved memory，而是因为 `rproc_mmap()` 把它接进了 remoteproc 的内存管理体系。

#### `rproc_notify()`
调用时机：
- 后续 rpmsg/virtio 通信阶段通过 notify 路径使用
- 当前 hetero 的 `.shutdown()` 不像 baremetal 一样显式通过 `.notify()` 驱动停机协商，但正常消息收发时仍依赖 `.notify()`

作用：
- 通过 `IOC_SENDIPI` 向 RISC-V 侧发送中断
- 驱动远端处理 vring 消息

#### 回通知链：`handle_riscv_irq()` / `mcs_poll()` / notifier thread
hetero 的“远端 -> Linux/master”通知闭环也值得单独看清楚。

主链是：
1. 远端通过 riscv 专用 IRQ 线打到 Linux 侧
2. `mcs_km.c: handle_riscv_irq()` 被触发
   - 先 mask IRQ
   - 再 clear IRQ
   - `atomic_set(&riscv_irq_ack, 1)`
   - `wake_up_interruptible(&mcs_riscv_wait_queue)`
   - 最后 unmask IRQ
3. 用户态线程在 `rproc_wait_event()` 里对 `mcs_fd` 执行 `poll()`
4. `mcs_poll()` 被调用：
   - 根据 `priv->ped_type == MCS_KM_PED_RISCV` 选择 `mcs_riscv_wait_queue`
   - `atomic_cmpxchg(&riscv_irq_ack, 1, 0)`
   - 若有事件，则返回 `POLLIN | POLLRDNORM`
5. 用户态 `poll()` 返回后，`rproc_wait_event()` 调 `remoteproc_get_notification(rproc, 0)`

也就是说，hetero 的回通知链也不是“内核直接回调 remoteproc”，而是：
- KO IRQ handler 置位并唤醒 riscv waitqueue
- `/dev/mcs` 的 `poll()` 变为可读
- 用户态 notifier 线程收到 `POLLIN`
- 再回到 remoteproc/OpenAMP 层继续推进

这条链对排查“远端已经发中断，但 Linux/master 没继续处理消息”这类问题同样关键。

## 5. 跟 `/dev/mcs` 的交互

### 5.1 设备打开和 pedestal 类型切换
`rproc_init()` 负责打开 `/dev/mcs`，然后立刻通过：
- `ioctl(mcs_fd, IOC_SET_PED_TYPE, &ped_type)`

把当前 fd 对应的 `priv->ped_type` 切到：
- `MCS_KM_PED_RISCV`

这是 hetero 路径最关键的分叉动作。

### 5.2 共享内存查询
`rproc_config()` 里通过 `IOC_QUERY_MEM` 获取 reserved memory 信息。

在 KO 里，它并不是返回 baremetal 那组内存，而是：
- 检查当前 fd 的 `ped_type`
- 若是 `MCS_KM_PED_RISCV`，返回 `riscv_mem[0]`

### 5.3 远端启动
`rproc_start()` 里通过 `IOC_MCUON` 启动远端。

虽然用户态宏名叫 `IOC_MCUON`，但 KO 内核侧分发入口仍是：
- `IOC_XPUON`

随后在 `ped_type == MCS_KM_PED_RISCV` 分支里调用：
- `boot_riscv(info.boot_addr)`

### 5.4 远端通知
`rproc_notify()` 通过 `IOC_SENDIPI` 把通知送到远端。

在 KO 里，对于 riscv 分支，实际动作是：
- `send_riscv_interrupt()`
- 往 `riscv_int_base + IPC_INT_SET` 写 `IPC_INT_RISCV_NUM`

### 5.5 设备关闭
`rproc_remove()` 会关闭 `mcs_fd`。

## 6. KO 里的具体实现

`mcs_km/mcs_km.c` 负责把 hetero/riscv 所需的内核侧能力拼出来。

### 6.1 模块初始化
`mcs_dev_init()` 会依次完成：
- `get_psci_method()`
- `init_reserved_mem()`
- `init_mcs_ipi()`
- `register_mcs_dev()`

对 hetero/riscv 来说，真正相关的是：
- `init_riscv_rsv_mem()` 初始化 riscv reserved memory
- `init_riscv_irq()` 初始化 riscv IRQ 路径
- `/dev/mcs` 字符设备注册

### 6.2 字符设备注册
`register_mcs_dev()` 负责：
- `register_chrdev()`
- `class_create()`
- `device_create()`

因此 hetero 也复用同一个：
- `/dev/mcs`

### 6.3 资源内存初始化
`init_riscv_rsv_mem()` 本质上是：
- `init_ped_rsv_mem("oe,mcs_riscv_remoteproc", riscv_mem, "mcs_riscv_mem", false)`

这里有几条和 baremetal 不同的关键点：
- hetero/riscv 看的 DTS compatible 节点是：
  - `oe,mcs_riscv_remoteproc`
- 使用的是 `riscv_mem[]`
- `support_cmdline` 为 `false`

也就是说：
- baremetal 支持通过 `rmem_base/rmem_size` 走命令行声明共享内存
- 当前 hetero/riscv 这一路并不支持用那组命令行参数替代 DTS
- 它要求通过 DTS 提供 `oe,mcs_riscv_remoteproc` 及其 `memory-region`

同时，和 baremetal 类似：
- `IOC_QUERY_MEM` 实际只返回 `riscv_mem[0]`
- 所以真正直接参与主通信的也是第一段 riscv reserved memory

### 6.4 RISC-V IRQ 初始化与回收
`init_riscv_irq()` / `remove_mcs_ipi()` 负责 hetero 的 IRQ 路径和相关中断资源。

`init_riscv_irq()` 的关键动作是：
- 在 DTS 中查找：
  - `oe,mcs_riscv_remoteproc`
- 从它的第 0 个 `reg` 解析出 IPC interrupt base
- `ioremap()` 到 `riscv_int_base`
- 从 DTS 解析 `irq`
- clear IRQ
- unmask IRQ
- `request_irq(riscv_int_irq, handle_riscv_irq, ...)`

这意味着 hetero/riscv 的中断模型和 baremetal 明显不同：
- baremetal 走的是 IPI_MCS 这类 CPU IPI 路径
- hetero/riscv 走的是一套 memory-mapped IPC interrupt controller 寄存器 + 独立 IRQ 号 的模型

### 6.5 ioctl 分发
`mcs_ioctl()` 里和 hetero/riscv 相关的重要 ioctl 有：
- `IOC_SET_PED_TYPE`
- `IOC_SENDIPI`
- `IOC_XPUON`（用户态 hetero 路径里对应 `IOC_MCUON` 宏）
- `IOC_QUERY_MEM`
- `IOC_GET_COPY_MSG_MEM`

其中要注意两点：
- 用户态 `library/remoteproc/riscv_rproc.c` 里把 `_IOW('A', 1, int)` 定义成了 `IOC_MCUON`
- KO `mcs_km.c` 里同一个 ioctl 号在内核侧名字叫 `IOC_XPUON`
- 两边编号一致，只是命名不同

各 ioctl 在 hetero 下的语义可以概括成：

#### `IOC_SET_PED_TYPE`
- 入口：`rproc_init()`
- KO 行为：
  - 校验 pedestal type 合法性
  - 把当前 fd 的 `priv->ped_type` 设置成 `MCS_KM_PED_RISCV`
- 用途：告诉 KO 这不是 baremetal 默认 fd，而是 hetero/riscv 分支
- 含义：这是后续 `poll/ioctl/mmap` 都能走对分支的前提

#### `IOC_SENDIPI`
- 入口：`rproc_notify()`
- KO 行为：
  - 进入 riscv 分支
  - 调 `send_riscv_interrupt()`
- 用途：Linux/master 向 RISC-V 远端发通知，驱动其处理 vring / message / 状态变化

#### `IOC_XPUON` / `IOC_MCUON`
- 入口：`rproc_start()`
- KO 行为：
  - 进入 riscv 分支
  - 调 `boot_riscv(info.boot_addr)`
  - 在 `boot_riscv()` 中：
    - `ioremap()` 四组固定物理寄存器地址
    - 写入 `boot_addr`
    - 写 jtag 到 mcu
    - wait / reset / unwait / unrst 一系列寄存器序列
- 用途：真正把 hetero remote RISC-V 拉起
- 额外含义：所以 hetero 启动远端，并不是 baremetal 那种 PSCI `CPU_ON`，而是走一套针对 riscv 的 SoC 寄存器启动序列

#### `IOC_QUERY_MEM`
- 入口：`rproc_config()`
- KO 行为：
  - 从 `riscv_mem[0]` 取出第一段 reserved memory 的起始地址和大小
  - 返回给用户态的是 riscv 这一路通信共享内存的基址和大小
- 用途：让用户态知道 hetero/riscv 可用的共享内存总范围
- 进一步含义：当前 hetero 用户态会直接用这块内存首页保存 resource table

#### `IOC_GET_COPY_MSG_MEM`
- 入口：当前 hetero 的主生命周期代码没有直接使用，但 KO 已实现
- KO 行为：
  - 在 riscv 分支下，根据 `instance_id` 和固定偏移从 `riscv_mem[0]` 推导 copy message memory
  - 返回区域大小 `OPENAMP_SHM_COPY_SIZE * 2`
- 用途：给 UMT 类服务使用的 copy-message 共享区提供物理地址与大小
- 含义：这说明 hetero KO 里也内置了面向 UMT 服务的辅助共享区规划

所以如果从用户关心的角度总结，hetero 下这些 ioctl 可以分成三类：
1. pedestal 类型切换：`IOC_SET_PED_TYPE`
2. 启动与通知控制：`IOC_XPUON` / `IOC_MCUON`、`IOC_SENDIPI`
3. 内存相关：`IOC_QUERY_MEM`、`IOC_GET_COPY_MSG_MEM`

### 6.6 mmap 校验
`mcs_mmap()` 在 hetero/riscv 下并不是无条件映射物理地址，而是：
- 根据 `priv->ped_type == MCS_KM_PED_RISCV`
- 检查请求映射范围是否落在 `riscv_mem[]` 中某一段 reserved memory 之内
- 只有合法才允许 `remap_pfn_range()`

这意味着：
- hetero 用户态所有通过 `/dev/mcs` 做的共享内存映射，都受 `riscv_mem[]` 边界约束
- 如果 `loadbin()`、`rproc_mmap()` 或其他映射请求落到 riscv reserved memory 外部，KO 会拒绝

### 6.7 模块退出
`mcs_dev_exit()` 会按相反顺序回收：
- `remove_mcs_ipi()`
- `unregister_mcs_dev()`
- `release_reserved_mem()`

## 7. RTOS/client 侧契约

从 `rtos/libmica/src/pedestals/hetero.c` 可以看出，hetero client 侧至少要满足：
- 共享内存基址 `shm_base_addr` 与 Linux/master 侧约定一致
- resource table 就放在 `shm_base_addr` 起始位置
- `reserved[0]` 要配合传递额外状态：
  - `0` / `CPU_ON_FUNCID`：普通消息
  - `SYSTEM_RESET`：远端要求重置 virtqueue
  - `CPU_OFF_FUNCID`：下电请求
- 能处理：
  - `virtio_notify()` 触发的 A55MP 方向中断
  - `hetero_irq_handler()` 收到的来自 Linux/master 的消息
- 能正确建立 OpenAMP virtio / RPMsg 设备
- 能在 `mica_create_all_services()` 中继续创建 TTY / UMT / RPC 等 endpoint

具体地说，RTOS/client 侧 `hetero.c` 当前做了这些事：
- `ped_hetero_init_irq()`
  - 清中断、开中断、注册 `hetero_irq_handler`
- `handle_ipi()`
  - 读取 `rsc_table->reserved[0]`
  - 普通消息就 post sem
  - `SYSTEM_RESET` 就 reset vq 并把 `reserved[0]` 清零
  - `CPU_OFF_FUNCID` 就调用 `system_poweroff()`
- `ped_hetero_init_rpmsg()`
  - `metal_init()`
  - 注册 shared memory device
  - 打开 generic metal device
  - 把 region[0] 设成 shared memory io
  - 把 region[1] 设成 resource table io
  - `platform_create_vdev()`
  - `rproc_virtio_wait_remote_ready()`
  - `rpmsg_init_vdev_with_config()`
  - `mica_set_rpdev(rpdev)`
- `ped_hetero_receive_message()`
  - `rproc_virtio_notified(g_vdev, VRING1_ID)`

而在 service 层：
- `mica_create_all_services()` 会继续起服务线程
- `tty_service.c` 通过 `rpmsg_create_ept(..., "rpmsg-tty", ...)` 建 TTY endpoint
- `umt_service.c` 通过 `rpmsg_create_ept(..., "rpmsg-umt", ...)` 建 UMT endpoint
- `rpc_service.c` 通过 `rpmsg_create_ept(..., "rpmsg-rpc", ...)` 建 RPC endpoint

如果这些契约不成立，hetero 的 `start` 可能能跑到一半，但后续服务和通信就不会稳定。

## 8. hetero 调试观察顺序

如果 hetero 有问题，建议按这个顺序看：
1. `create_client()` 是否确实进入了：
   - `client->ped == HETERO`
   - `cpu_str == "riscv"`
   - `rproc_riscv_ops`
2. `rproc_init()` 是否成功打开 `/dev/mcs` 并执行 `IOC_SET_PED_TYPE`
3. `rproc_config()` 是否拿到了 `IOC_QUERY_MEM`
4. resource table 是否成功定位、拷贝到共享内存第一页并设置进 `rproc->rsc_table`
5. `rproc->bootaddr` 是否被正确取出
6. `rproc_start()` 是否成功注册 notifier
7. `client->ped_cfg` 是否存在、路径是否正确、`loadbin()` 是否成功
8. `IOC_MCUON` 是否真正进入 `boot_riscv(boot_addr)`
9. `rproc_notify()` / `handle_riscv_irq()` / `mcs_poll()` / notifier 线程是否闭环
10. `create_rpmsg_device()` 是否成功建立 virtio/RPMsg
11. RTOS 侧 `ped_hetero_init_rpmsg()` 是否成功把 `rpdev` 建起来
12. `mica_create_all_services()` 与 `rpmsg_create_ept()` 是否成功把 TTY/UMT/RPC 服务创建出来
13. `rproc_shutdown()` 是否把 memory、pipe 和状态清干净

## 9. 建议继续阅读
- `pedestal-overview.md`
- `lifecycle-overview.md`
- `lifecycle-create.md`
- `lifecycle-start.md`
- `lifecycle-stop.md`
- `lifecycle-remove.md`
