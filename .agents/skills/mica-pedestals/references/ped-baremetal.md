# ped-baremetal

## 1. 文档目标

这篇文档解释 baremetal pedestal 在 MICA 中的完整落地方式：它如何把 MICA 的生命周期骨架落到 Linux/master 侧的 `remoteproc` 实现、`/dev/mcs` 设备、共享内存、PSCI/IPI 通知，以及远端 client 的启动与停止。

它主要回答：
- baremetal 对应哪些源码文件
- baremetal 的 KO、设备节点、ioctl 是什么
- create/start/stop/remove 在 baremetal 下分别做什么
- 共享内存和 resource table 如何建立
- 隐含在 lifecycle 背后的 `config/mmap/notify` 这些钩子何时被调用、做了什么
- 通知路径如何闭环
- 远端 client 侧需要满足什么契约

## 2. 相关源码
- `library/remoteproc/baremetal_rproc.c`
- `mcs_km/mcs_km.c`
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`
- `library/rpmsg_device/rpmsg_vdev.c`
- `library/rpmsg_device/rpmsg_service.c`

## 3. baremetal 底座定位

baremetal 是 MICA 最基础的 pedestal：
- Linux/master 侧直接和目标 CPU / 远端 OS 打交道
- 依赖 `/dev/mcs`
- 通过 PSCI、IPI、reserved memory、poll/notifier 来完成启动、停止和通知
- resource table、共享内存和运行态状态都由 baremetal 路径自己组织

在代码里，baremetal 不是一个“薄配置”，而是一套完整 backend。

## 4. 生命周期实现

### 4.1 create
- `create_client()` 选择 `rproc_bare_metal_ops`
- `remoteproc_init()` 进入 baremetal 的 `rproc_init()`
- `rproc_init()` 打开 `/dev/mcs`
- 解析 `client->ped_setup.cpu_str`
- 把 `client->ped_setup.cpu_id` 填好
- 注册 notifier 基础设施

这一阶段的结果是：
- baremetal 实例具备后续配置与启动的前提
- 但远端还没有真正跑起来

### 4.2 start
主链是：
1. `load_client_image()`
2. `remoteproc_config()` -> baremetal 的 `rproc_config()`
3. `remoteproc_load()`（需要时）
4. `start_client()` -> `remoteproc_start()` -> baremetal 的 `rproc_start()`
5. `create_rpmsg_device()`

也就是说，baremetal 的 `start` 不只是一个 `IOC_CPUON`，而是至少包含：
- 配置 remoteproc
- 处理资源表
- 必要时通过 loader 装载镜像
- 真正拉起 CPU
- 再建立 RPMsg 通信设备

### 4.3 stop
`stop` 通过 `remoteproc_shutdown()` 落到 baremetal 的 `rproc_shutdown()`。

它的主链是：
1. 把 resource table reserved[0] 置为 `CPU_OFF_FUNCID`
2. 调 `rproc->ops->notify(rproc, 0)`
3. 遍历 `rproc->mems`
4. 对每个映射的 memory 做 `munmap()`
5. 从 `rproc->mems` 链表中删除
6. 释放 `mem->io` 和 `mem`
7. 清空 `rproc->rsc_table`、`rproc->rsc_len`、`rproc->bitmap`

这一阶段的结果是：
- 运行态状态被回退
- 通知状态被推进到 shutdown
- 远端映射的内存对象被回收
- remoteproc 资源指针被清掉

### 4.4 remove
`remove` 主要在 `rproc_remove()` 中完成。

它的核心逻辑是：
1. 在 `g_client_list` 里查找 baremetal client
2. 根据是否还有 baremetal client 决定 `notifier` 是否继续保持
3. 如果没有了，就通过 pipe 通知 notifier 线程退出
4. 关闭 `mcs_fd` 和 pipe

这一阶段的结果是：
- 最后一个相关实例被移除后，baremetal 的后台通知基础设施也会被回收

### 4.5 生命周期隐含钩子

有几类函数虽然不是 `mica create/start/stop/remove` 这些顶层 API 显式点名调用的，但它们实际上是 baremetal lifecycle 能成立的关键钩子。

#### `rproc_config()`
调用时机：
- `mica_start()` -> `load_client_image()` -> `remoteproc_config()`

作用：
- 通过 `IOC_QUERY_MEM` 获取整块 reserved memory 信息
- 再按 `client->ped_setup.cpu_id * SHM_POOL_SIZE` 从这整块共享内存里切出当前 client 自己的通信分段
- 初始化共享内存池
- 解析 image header 和 resource table
- 直接按 ELF 中 `resource_table` 的原始地址执行 `remoteproc_mmap()`
- 处理远端已在线时的 resource table 恢复与状态协商

这里有个和其他 pedestal 很不一样的点：
- baremetal 不会在 `.config()` 里把 resource table 先拷贝到共享内存第一页
- 它要求 ELF 中 `resource_table` 所在地址本身就能被 `/dev/mcs` `mmap`
- 因此 baremetal 下真正的约束不是“resource table 是否能塞进第一页”，而是“resource table 的原始地址是否落在 `mcs_km` 允许映射的保留内存范围内”

#### `rproc_mmap()`
调用时机：
- `rproc_config()` 里通过 `remoteproc_mmap()` 映射 resource table
- 后续 remoteproc / rpmsg 建立过程中也可能复用这条映射能力

作用：
- 把物理地址映射到用户态可访问的 virtual address
- 创建 `remoteproc_mem` 与 `metal_io_region`
- 挂入 `rproc->mems`

也就是说，baremetal 的 shared memory / resource table 真正能被访问，不只是因为有 reserved memory，而是因为 `rproc_mmap()` 把它接进了 remoteproc 的内存管理体系。

#### `rproc_notify()`
调用时机：
- `rproc_config()` 里远端已在线、需要 `SYSTEM_RESET` 协商时会调一次
- `rproc_shutdown()` 里通知远端下电时会调一次
- 后续 rpmsg/virtio 通信阶段也会通过 notify 路径继续使用

作用：
- 通过 `IOC_SENDIPI` 向目标 CPU 发送 IPI
- 驱动远端处理状态变化或 vring 消息

#### 回通知链：`handle_clientos_ipi()` / `mcs_poll()` / notifier thread
baremetal 的“远端 -> Linux/master”通知闭环也值得单独看清楚。

主链是：
1. 远端通过约定好的 IPI 线打到 Linux 侧
2. `mcs_km.c: handle_clientos_ipi()` 被触发
   - `atomic_set(&baremetal_irq_ack, 1)`
   - `wake_up_interruptible(&mcs_baremetal_wait_queue)`
3. 用户态线程在 `rproc_wait_event()` 里对 `mcs_fd` 执行 `poll()`
4. `mcs_poll()` 被调用：
   - `poll_wait(file, &mcs_baremetal_wait_queue, wait)`
   - `atomic_cmpxchg(&baremetal_irq_ack, 1, 0)`
   - 若有事件，则返回 `POLLIN | POLLRDNORM`
5. 用户态 `poll()` 返回后，`rproc_wait_event()` 调 `rproc_notify_all()`
6. `rproc_notify_all()` 遍历 `g_client_list`，对处于 `RPROC_RUNNING` 的 baremetal client 调：
   - `remoteproc_get_notification(&client->rproc, 0)`

也就是说，baremetal 的回通知链不是“内核直接回调 remoteproc”，而是：
- KO IRQ handler 置位并唤醒 waitqueue
- `/dev/mcs` 的 `poll()` 变为可读
- 用户态 notifier 线程收到 `POLLIN`
- 再回到 remoteproc/OpenAMP 层继续推进

这条链对排查“远端已经发中断，但 Linux/master 没继续处理消息”这类问题非常关键。

## 5. 跟 `/dev/mcs` 的交互

### 5.1 设备打开和初始化
`rproc_init()` 负责打开 `/dev/mcs`，并把 baremetal 后续运行所需的基础状态挂起来。

### 5.2 共享内存查询
`rproc_config()` 里通过 `IOC_QUERY_MEM` 获取 reserved memory 信息，再用它初始化共享内存池。

### 5.3 远端启动
`rproc_start()` 里通过 `IOC_CPUON` 启动目标 CPU。

### 5.4 远端通知
`rproc_notify()` 通过 `IOC_SENDIPI` 把通知送到目标 CPU。

### 5.5 设备关闭
`rproc_remove()` 在最后一个 baremetal client 被移除后，关闭 `mcs_fd` 并退出 notifier 基础设施。

## 6. KO 里的具体实现

`mcs_km/mcs_km.c` 负责把 baremetal 所需的内核侧能力拼出来：

### 6.1 模块初始化
`mcs_dev_init()` 会依次完成：
- `get_psci_method()`
- `init_reserved_mem()`
- `init_mcs_ipi()`
- `register_mcs_dev()`

### 6.2 字符设备注册
`register_mcs_dev()` 负责：
- `register_chrdev()`
- `class_create()`
- `device_create()`

### 6.3 资源内存初始化
`init_reserved_mem()` 会把 baremetal reserved memory 解析到 `baremetal_mem[]`。

这里要注意一个很关键的规则：
- 如果 DTS 里像 `tools/create_dtb.sh` 这样给了多个 `memory-region`，例如 `client_os_dma_memory_region` 和 `client_os_reserved`
- `mcs_km` 会把 `oe,mcs_remoteproc` 节点下声明的内存段都按 reserved memory 预留起来
- 但真正用于 rpmsg/rproc 通信的共享内存，默认只取第一段，也就是 `memory-region` 列表里的第一块
- 后面的内存段通常用于给 client 预留运行时内存，而不是 rpmsg 通信区

因此可以把这条经验记成一句话：
- “通信共享内存放第一段，其他段可以留给 client 运行内存。”

如果没有 DTS，而是只有 ACPI / 其他没有设备树的场景，`mcs_km` 也支持通过模块参数 `rmem_base` / `rmem_size` 走命令行方式声明共享内存。

但这种方式和 DTS 不同：
- 它只是在内核里声明了通信共享内存的地址和大小
- 并不会替你去做整段 reserved memory 的预留
- 所以用户需要自己保证这段物理内存已经是可用且已预留的

### 6.4 IPI 初始化与回收
`init_mcs_ipi()` / `remove_mcs_ipi()` 负责 baremetal 的 IPI 路径和相关中断资源。

这一节最关键的实现约束其实只有一条：
- 在当前 ARM64 场景下，双方真正应约定的是使用 IPI 7

之所以 `mcs_km.c` 里写的是：
- `#define IPI_MCS 8`

可以把它理解成：
- 当前代码里使用的是 Linux 侧的虚拟中断号 8
- 它对应的实际是物理上的 IPI 7

所以这里不要被代码里的 `8` 迷惑。对接约束应理解为：
- RTOS/client 侧固定使用 IPI 7
- Linux/master 侧 `mcs_km` 里虽然写的是虚拟中断号 8，但它对应的也是这条 IPI 7 通道

这也意味着：
- 如果未来切到别的架构，或者 IPI 的物理号/虚拟号映射关系变化，`mcs_km` 里这处硬编码不一定还能直接复用

### 6.5 ioctl 分发
`mcs_ioctl()` 里和 baremetal 相关的 ioctl 不止三个，比较重要的有：
- `IOC_SENDIPI`
- `IOC_XPUON`（用户态 baremetal 路径里对应 `IOC_CPUON` 宏）
- `IOC_AFFINITY_INFO`
- `IOC_QUERY_MEM`
- `IOC_GET_COPY_MSG_MEM`

其中要注意一点：
- 用户态 `library/remoteproc/baremetal_rproc.c` 里把 `_IOW('A', 1, int)` 定义成了 `IOC_CPUON`
- KO `mcs_km.c` 里同一个 ioctl 号在内核侧名字叫 `IOC_XPUON`
- 两边编号是一致的，只是命名不同

各 ioctl 在 baremetal 下的语义可以概括成：

#### `IOC_SENDIPI`
- 入口：`rproc_notify()`
- KO 行为：`mcs_ioctl()` 里进入 baremetal 分支，调用 `send_clientos_ipi(cpumask_of(info.cpu))`
- 用途：Linux/master 向目标 CPU 发送 IPI，驱动远端处理 vring / message / 状态变化

#### `IOC_XPUON` / `IOC_CPUON`
- 入口：`rproc_start()`
- KO 行为：
  - 先把逻辑 CPU 转成 MPIDR
  - 再通过 `invoke_psci_fn(CPU_ON_FUNCID, mpidr, info.boot_addr, 0)` 发起 PSCI `CPU_ON`
- 用途：真正把 baremetal remote CPU 拉起
- 额外含义：所以 baremetal 启动远端，本质上是经由 PSCI 把目标 CPU 从 Linux/master 侧拉起

#### `IOC_AFFINITY_INFO`
- 入口：当前 baremetal 用户态主链里没有直接调用，但 KO 已提供
- KO 行为：
  - 把逻辑 CPU 转成 MPIDR
  - 再通过 `invoke_psci_fn(AFFINITY_INFO_FUNCID, mpidr, 0, 0)` 查询目标 CPU 状态
  - 要求其处于 OFF 状态
- 用途：做 CPU 上电前的状态检查
- 含义：这是一个有调试价值、但当前主链未显式用起来的能力

#### `IOC_QUERY_MEM`
- 入口：`rproc_config()`
- KO 行为：
  - 从 `baremetal_mem[0]` 取出第一段 reserved memory 的起始地址和大小
  - 返回给用户态的是“整块共享内存/保留内存”的基址和总大小，而不是某个已经切好的子分段
- 用途：让用户态知道底座能用的共享内存总范围，再由上层按照 client / CPU 维度去做池化分段
- 进一步的分段方式：
  - baremetal 用户态会按 `client->ped_setup.cpu_id * SHM_POOL_SIZE` 在这块内存里为每个 client 切出自己的通信分区
  - 所以 `IOC_QUERY_MEM` 返回的是“母体”，真正的 per-client 划分是在用户态 `rproc_config()` 里完成的

#### `IOC_GET_COPY_MSG_MEM`
- 入口：当前 baremetal 的主生命周期代码没有直接使用，但 KO 已实现
- KO 行为：
  - 根据 `instance_id` 和固定偏移，从 `baremetal_mem[0]` 推导一段 copy message memory
  - `copy_mem_offset = instance_id * INSTANCE_SIZE + OPENAMP_SHM_SIZE - OPENAMP_SHM_COPY_SIZE * 3`
  - 返回区域大小 `OPENAMP_SHM_COPY_SIZE * 2`
- 用途：给 UMT 服务使用的 copy-message 共享区提供物理地址与大小
- 规划方式：
  - 主通信共享区大小是 `OPENAMP_SHM_SIZE`
  - 在这块共享区尾部附近再划出 copy-message 区
  - 其中 1MB 用于发送，1MB 用于接收，中间尾部预留 1MB gap
- 含义：这说明 baremetal KO 里除了主通信区，还内置了面向 UMT 服务的辅助共享区规划

所以如果从用户关心的角度总结，baremetal 下这些 ioctl 可以分成三类：
1. 启动控制：`IOC_XPUON` / `IOC_CPUON`、`IOC_AFFINITY_INFO`
2. 通知控制：`IOC_SENDIPI`
3. 内存相关：`IOC_QUERY_MEM`、`IOC_GET_COPY_MSG_MEM`

### 6.6 模块退出
`mcs_dev_exit()` 会按相反顺序回收：
- `remove_mcs_ipi()`
- `unregister_mcs_dev()`
- `release_reserved_mem()`

## 7. RTOS/client 侧契约

baremetal client 侧至少要满足：
- 正确的 resource table
- 正确的共享内存地址约定
- 能响应 `CPU_ON / CPU_OFF / SYSTEM_RESET` 这类状态协商
- 能正确处理 IPI / notify
- 能按 MICA 的运行态约束发布服务

如果这些契约不成立，baremetal 的 `start` 可能能跑到一半，但后续服务和通信就不会稳定。

## 8. baremetal 调试观察顺序

如果 baremetal 有问题，建议按这个顺序看：
1. `mcs_km/mcs_km.c` 是否成功注册了 `/dev/mcs`
2. `rproc_init()` 是否成功打开设备并注册 notifier
3. `rproc_config()` 是否拿到了 `IOC_QUERY_MEM`
4. resource table 是否成功映射并设置
5. `rproc_start()` 是否真正调用了 `IOC_CPUON`
6. `rproc_notify()` / `mcs_poll()` / notifier 线程是否闭环
7. `rproc_shutdown()` 是否把 memory 和状态清干净
8. `rproc_remove()` 是否在最后把 notifier 基础设施收掉

## 9. 建议继续阅读
- `pedestal-overview.md`
- `lifecycle-overview.md`
- `lifecycle-create.md`
- `lifecycle-start.md`
- `lifecycle-stop.md`
- `lifecycle-remove.md`
