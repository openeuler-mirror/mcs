# ped-jailhouse

## 1. 文档目标

这篇文档解释 jailhouse pedestal 在 MICA 中的完整落地方式：它如何把 MICA 的生命周期骨架落到 `library/remoteproc/jailhouse_rproc.c` 这套用户态 backend、Jailhouse cell 生命周期命令、ivshmem 共享内存与 doorbell 通知、`/dev/uioX` 设备，以及 OpenAMP / RPMsg 的后续建链。

它主要回答：
- jailhouse 当前对应哪些源码文件
- 为什么 jailhouse 这条线不能只看 `mcs` 仓，还要看 yocto 里的 UIO KO 实现
- create/start/stop/remove 在 jailhouse 下分别做什么
- ivshmem / UIO / cell create/load/start/shutdown/destroy 是怎么接起来的
- resource table 和共享内存如何放到 ivshmem RW section 里
- 隐含在 lifecycle 背后的 `config/mmap/notify` 钩子何时被调用、做了什么
- 它和 baremetal 的关键边界差异是什么

## 2. 相关源码

`mcs` 仓内的主实现：
- `library/remoteproc/jailhouse_rproc.c`
- `library/remoteproc/remoteproc_core.c`
- `library/mica/mica.c`
- `library/rpmsg_device/rpmsg_vdev.c`
- `library/memory/shm_pool.c`

jailhouse 这条线需要额外看的驱动来源：
- `yocto-meta-openeuler` 仓库下的 `meta-openeuler/recipes-kernel/linux/files/meta-data/features/mcs/0003-uio-Add-driver-for-inter-VM-shared-memory-device.patch`

这里要特别注意：
- baremetal 的关键内核配套实现主要就在 `mcs_km/mcs_km.c`
- 但 jailhouse 的关键设备基础并不在 `mcs` 仓内维护
- `mcs` 仓主要维护的是用户态 remoteproc backend
- 与 ivshmem / UIO 相关的内核侧入口，要结合 yocto 里的 `uio_ivshmem` KO 实现一起理解

如果本地没有这个仓库，按 `../../../mica-overview/references/overview.md` 中的外部组件源码获取规则处理。

## 3. jailhouse 底座定位

jailhouse pedestal 对应的是“由 Jailhouse hypervisor 管理 non-root cell，再通过 ivshmem 完成共享内存与 doorbell 通知”的这条 MICA 落地方向。

和 baremetal 相比，jailhouse 有两个关键差异。

第一，它不是直接控制一个裸远端 CPU 的上电/下电。
- baremetal 对应“Linux/master + mcs_km + reserved memory + IPI/PSCI”模型
- jailhouse 对应“Linux/master 通过 `jailhouse cell ...` 命令管理 non-root cell，再把 ivshmem 作为通信底座”

第二，它的设备接口不是 `/dev/mcs`。
- baremetal 侧核心设备是 `/dev/mcs`
- jailhouse 侧核心设备是 `uio_ivshmem` 暴露出的 `/dev/uioX`
- 因而共享内存探测、事件等待、中断 re-enable、doorbell 通知这些行为，都不再通过 `mcs_km.c` 的 ioctl/poll 分支完成，而是走 UIO + ivshmem register 语义

jailhouse 文档关注的核心不是 `ped_type` 切换，而是：
- MICA 用户态 backend 如何接住 Jailhouse cell 生命周期
- ivshmem 的寄存器区、state table、RW section 如何经由 UIO 暴露给 userspace
- OpenAMP 后续如何建立在这套 ivshmem 共享区之上

## 4. 生命周期实现

### 4.1 create

公共入口仍然是：
- `library/remoteproc/remoteproc_core.c:create_client()`

这里当：
- `client->ped == JAILHOUSE`

就会选择：
- `rproc_jailhouse_ops`

随后进入：
- `remoteproc_init()`
- `jailhouse_rproc.c:rproc_init()`

`rproc_init()` 里主要做四件事：
1. 分配 `struct rproc_pdata`
2. 调 `init_ivshmem_dev("/dev/uio0", &pdata->ivshmem_dev)` 初始化 ivshmem UIO 设备
3. 读取 `client->ped_cfg` 指向的 non-root cell 配置头，校验 `JAILHOUSE_CONFIG_REVISION`
4. 执行 `jailhouse cell create <ped_cfg>` 创建 cell

然后它再把：
- `pdata->cell_name`
- `rproc->ops`
- `rproc->priv`
挂好。

这里有几个实现细节需要直接关注。

第一，当前实现把 UIO 设备写死成：
- 当前实现写死的是 `/dev/uio0`，这表明 jailhouse 这条线依赖一个事先就绪的 `uio_ivshmem` 设备入口，而不是通过 `/dev/mcs` 统一兜底。

第二，`ped_cfg` 在 jailhouse 下不是 Xen 那种自动生成 cfg，而是：
- 一个 Jailhouse non-root cell 配置文件
- `rproc_init()` 会直接读取其文件头，检查 revision，并用它做 `jailhouse cell create`

第三，cell 在 create 阶段就已经被创建，但这还不是 load/start 完整完成后的运行态。

### 4.2 start

jailhouse 这条线在顶层顺序上仍然是：
1. `remoteproc_config()`
2. `remoteproc_start()`
3. `create_rpmsg_device()`

jailhouse 的这条顺序不同于 baremetal 的“先配 `/dev/mcs` 再 MCUON”，而是：
- `.config()` 里准备 ivshmem 共享区、解析 ELF、执行 `jailhouse cell load`
- `.start()` 里执行 `jailhouse cell start`
- 然后再起 notifier，进入 RPMsg 建链

这里的阶段语义是：
- baremetal 更接近“启动远端 CPU + 共享内存就绪”
- jailhouse 更接近“先装载 cell 与镜像，再启动 cell 进入运行态”

### 4.3 stop

公共层 stop 仍然落到：
- `remoteproc_shutdown()`
- 对应 `jailhouse_rproc.c:rproc_shutdown()`

它的主链是：
1. `jailhouse cell shutdown <cell_name>`
2. 遍历 `rproc->mems`，逐个 `munmap()` 并回收 `remoteproc_mem`
3. 往 pipe 写数据，让 notifier 线程退出
4. 清空 `rproc->rsc_table`、`rproc->rsc_len`、`rproc->bitmap`

这一阶段的重点是：
- shutdown 主要负责让 cell 退出运行态，并回收 userspace 映射
- 它不负责 destroy cell 本身

这和 baremetal 也不同：
- baremetal stop 更偏“对远端发停机语义 + 回收运行态资源”
- jailhouse stop 更偏“让 cell shutdown，再把 userspace 映射和 notifier 收掉”

### 4.4 remove

`remoteproc_remove()` 最终落到：
- `jailhouse_rproc.c:rproc_remove()`

其主链是：
1. `jailhouse cell destroy <cell_name>`
2. 关闭 `ivshmem_dev.uio_fd`
3. 释放 `pdata`
4. `rproc->priv = NULL`

这里的生命周期对应关系是：
- create 对应 `cell create`
- remove 对应 `cell destroy`
- shutdown 和 remove 在 jailhouse 下是两步，不应混淆

这也是 jailhouse 更接近“虚拟化对象生命周期”的原因：
- 一个对象先 create
- 后续 load/start/shutdown
- 最后再 destroy

### 4.5 生命周期隐含钩子

#### `rproc_config()`

调用时机：
- `mica_start()` -> `load_client_image()` -> `remoteproc_config()`

它的主链是：
1. `init_shmem_pool(client, pdata->ivshmem_dev.shmem_addr, pdata->ivshmem_dev.shmem_sz)`
2. `client->shmem_dynamic = false`
3. 解析 ELF header 与 resource table
4. 执行 `jailhouse cell load <cell_name> <client->path>`
5. 把 resource table 拷到 ivshmem RW section 的第一页
6. `remoteproc_set_rsc_table()`
7. `rproc->state = RPROC_READY`

这里有两个关键约束。

第一，jailhouse 跟 xen 不一样：
- 它虽然也是跨 VM / cell 的共享内存模型
- 但当前 MICA 实现里并没有把它当作 `shmem_dynamic = true` 的那类地址模型
- 代码里明确设的是：`client->shmem_dynamic = false`

当前 jailhouse 这条线仍按“双方共享同一套共享内存基础地址语义”来组织 OpenAMP 共享区，而不是像 xen 那样额外引入 `vring offset` 这一层地址翻译协商。

第二，`jailhouse cell load` 发生在 `.config()` 里，而不是 `.start()`。
这表明：
- `.config()` 不只解析 image
- 它还会把镜像装入目标 cell
- `.start()` 只负责让已经装好的 cell 进入运行态

第三，jailhouse 和 baremetal 在 resource table 管理上的边界非常明确：
- jailhouse 不直接依赖 ELF 中 `resource_table` 原地址可被 Linux/master `mmap`
- 它在 `.config()` 中把资源表复制到 ivshmem RW section 首页，再让双方都围绕这份共享副本工作

#### `rproc_mmap()`

调用时机：
- remoteproc / OpenAMP 在后续需要把共享区映射到用户态时调用

这条函数是 jailhouse 路径中的关键环节，因为它把 UIO 导出的多个 memory section 与 userspace 地址空间接起来了。

主要逻辑是：
- 基于 `lpa` / `lda` 算页对齐后的地址与大小
- 由于 ivshmem 的 UIO 暴露里：
  - 第 1 页是 register region
  - 第 2 页是 state table
  - RW section 从第 3 页开始
- 所以 `uio_mem_addr = aligned_addr - pdata->ivshmem_dev.shmem_addr + 2 * pagesize`
- 然后再用 `mmap(uio_fd, uio_mem_addr)` 把目标共享区映射到用户态
- 建立 `remoteproc_mem` / `metal_io_region`
- 加入 `rproc->mems`

这意味着：
- jailhouse 的 userspace 映射不是直接对物理地址做裸 `mmap`
- 它建立在 UIO driver 已划分 register/state/RW section 的前提上
- 当前 MICA userspace 需要自己跳过前两页，定位到 RW section

#### `rproc_notify()`

调用时机：
- 后续 virtio / rpmsg 通信阶段

作用非常直接：
- 对 `ivshmem_dev.ivshm_regs->doorbell` 写入 `peer_id << 16`

jailhouse 的 notify 路径不是：
- baremetal 的 `IOC_SENDIPI`
也不是：
- xen 的 evtchn
而是：
- ivshmem register doorbell

#### notifier 回通知链

jailhouse 的“对端 -> Linux/master”通知闭环是：
1. 远端触发 ivshmem 中断
2. UIO driver 侧 `uio_event_notify()` 把事件上送到 `/dev/uioX`
3. 用户态 notifier 线程在 `rproc_wait_event()` 里对 `uio_fd` 做 `poll()`
4. `poll()` 返回后，先 `read(uio_fd, &dummy, 4)` 读取事件
5. 再写 `ivshm_regs->int_control = 1` 重新使能 one-shot interrupt
6. 然后调用 `remoteproc_get_notification(rproc, 0)`

这条链很重要，因为它解释了 jailhouse 为什么必须同时看：
- yocto 里的 `uio_ivshmem` KO 实现
- `jailhouse_rproc.c` 里的 userspace poll/read/re-enable 逻辑

只看 `mcs` 仓内代码，很难理解为什么 userspace 收到事件后还要手动写 `int_control = 1`；而 `uio_ivshmem` 的实现里已经写清楚了：
- 中断被配置成 one-shot
- userspace 必须在每次事件后重新使能

## 5. jailhouse `uio_ivshmem` KO 的核心功能

这一节是 jailhouse 与 baremetal 最常见的混淆点。

baremetal 里，很多问题最终都回到：
- `mcs_km/mcs_km.c`
- `/dev/mcs`
- ioctl / poll / reserved memory / IRQ

但 jailhouse 这里不是。

jailhouse 这里内核侧的关键不是 `mcs_km`，而是 yocto 提供的：
- `drivers/uio/uio_ivshmem.c`

它提供的核心能力包括：
1. 把 ivshmem PCI 设备注册成 UIO 设备
2. 暴露多个 memory region 给 userspace
3. 用 one-shot 中断模型把事件聚合到 UIO notifier
4. 允许 userspace 通过 `/dev/uioX` 来：
   - mmap 寄存器区与共享内存区
   - poll 等待事件
   - 在事件后自己 re-enable interrupt

### 5.1 UIO 内存区导出

从 `uio_ivshmem` 的实现看，驱动至少会导出：
- `mem[0] = registers`
- `mem[1] = state_table`
- `mem[2] = rw_section`

如果 output section 存在，还会继续导出：
- `input_sections`
- `output_section`

而 `jailhouse_rproc.c` 当前真正依赖的是：
- map0: register region
- map2: RW section

它通过 sysfs 读取：
- `/sys/class/uio.../maps/map2/size`
- `/sys/class/uio.../maps/map2/addr`

来初始化：
- `ivshmem_dev.shmem_sz`
- `ivshmem_dev.shmem_addr`

### 5.2 one-shot 中断模型

`uio_ivshmem` 的实现里明确写了：
- interrupts are configured in one-shot mode
- userspace needs to re-enable them after each event via Interrupt Control register

这正好对应 `jailhouse_rproc.c:rproc_wait_event()` 里的：
- `read(uio_fd, &dummy, 4)`
- `write32(&ivshmem_dev->ivshm_regs->int_control, 1)`

这不是附带的一行寄存器写，而是整个通知模型成立的必要步骤。

### 5.3 与 baremetal 的关键边界

对开发和排障来说，最重要的边界差异可以压缩成一句话：
- baremetal 把“共享内存 + 中断 + 控制”主要收在 `mcs_km` / `/dev/mcs` 这套私有接口里
- jailhouse 则把“共享内存 + doorbell + 事件”建立在 hypervisor 暴露出来的 ivshmem PCI 设备之上，再通过通用 `uio_ivshmem` 让 userspace 接住

因此：
- baremetal 出问题，优先去看 `mcs_km.c`、`IOC_*`、reserved memory、IPI
- jailhouse 出问题，优先去看：
  - `jailhouse cell ...` 生命周期命令是否成功
  - `/dev/uioX` 是否真的对应 ivshmem
  - UIO map2 的 addr/size 是否正确
  - one-shot interrupt 是否被 userspace 重新使能

## 6. 调试入口

### 6.1 cell 生命周期成功确认点
优先检查：
- `jailhouse cell create <ped_cfg>`
- `jailhouse cell load <cell_name> <image>`
- `jailhouse cell start <cell_name>`
- `jailhouse cell shutdown <cell_name>`
- `jailhouse cell destroy <cell_name>`

如果 create/load/start 本身就失败，后面的 OpenAMP / RPMsg 都不用看。

### 6.2 `/dev/uioX` 目标 ivshmem 设备确认点
当前实现写死的是：
- `/dev/uio0`

因此要先确认：
- 它确实对应 ivshmem
- sysfs 下 `maps/map2/addr` 和 `size` 能读到合理值

### 6.3 中断闭环确认点
确认：
- `poll(uio_fd)` 是否能返回
- `read(uio_fd, &dummy, 4)` 是否成功
- `int_control = 1` 是否在每次事件后被重新写回
- doorbell 写入后对端是否真有响应

### 6.4 OpenAMP / RPMsg 确认点
确认：
- resource table 是否已被放到 RW section 首页
- `init_shmem_pool()` 是否成功
- `client->shmem_dynamic == false` 下共享内存地址解释是否一致
- `create_rpmsg_device()` / `rpmsg_init_vdev()` 是否成功

如果这里失败，问题往往已经不在 Jailhouse 命令本身，而是在 ivshmem 共享区和事件链的解释是否一致。

## 7. 继续阅读
- `pedestal-overview.md`
- `ped-baremetal.md`
- `lifecycle-overview.md`
- `lifecycle-start.md`
- `lifecycle-stop.md`
- `lifecycle-remove.md`
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `../../mica-communication/references/communication-overview.md`
