# ped-xen

## 1. 文档目标

这篇文档解释 xen pedestal 在 MICA 中的完整落地方式：它如何把 MICA 的生命周期骨架落到 Linux/master 侧的 `library/remoteproc/xen_rproc.c`、Dom0 侧的 `mcs_km/xen-mcsback.c`、`/dev/mcs_xen` 字符设备、xenbus backend、event channel、grant table，以及远端 Xen guest 侧的 OpenAMP/RPMsg 初始化契约。

它主要回答：
- xen 当前在 MCS 中对应哪些源码文件
- xen 为什么比 baremetal / hetero 更“动态”
- create/start/stop/remove 在 xen 下分别做什么
- `/dev/mcs_xen` 提供了哪些 ioctl / mmap / poll 能力
- domU 的 domid、共享内存、event channel 为什么不是预先约定，而是运行时建立
- xenbus / grant table / evtchn / resource table 是怎样串起来的
- 远端 guest 侧需要满足什么契约，尤其是为什么 vring 地址不能再照搬 master 的物理地址

## 2. 相关源码

MCS 仓内主实现：
- `library/remoteproc/xen_rproc.c`
- `mcs_km/xen-mcsback.c`
- `library/remoteproc/remoteproc_core.c`
- `library/mica/mica.c`
- `library/include/mica/mica_client.h`

未来会落到 `rtos/libmica` 的 Xen 侧实现，可在后续集成完成后再补充对应对端源码映射。

本文的重点是：
- Linux/master 侧以 `xen_rproc.c + xen-mcsback.c` 为主线
- guest 侧先按协议契约理解其运行时行为，后续再映射到 `rtos/libmica`

## 3. xen 底座定位

xen pedestal 和 baremetal、hetero 的最大不同是：
- 远端运行在 Xen 虚拟化环境里
- 远端实例先表现为一个 domU
- Linux/master 自己处在 Dom0
- 共享内存不是靠两边提前约定一段固定 reserved memory
- 通知中断也不是靠预先焊死的 IPI/IRQ 编号
- 而是在运行时通过 xenbus 交换：
  - event channel port
  - grant table references
  - 前后端 state

xen pedestal 的核心心智模型不是“把固定物理地址交给远端”，而是：
1. 先创建一个 domU
2. 再由 Dom0 backend 为这个 domU 分配共享页和事件通道
3. 再通过 xenbus 把这些索引信息发布给 guest
4. guest 用 grant ref 把共享页映射进来，用 evtchn port 建立通知通道
5. 最后双方才进入 OpenAMP / RPMsg 正常工作态

所以 xen 的复杂度主要来自“运行时协商和动态绑定”，而不是单纯的 remoteproc 装载。

## 4. 生命周期实现

### 4.1 创建阶段

主链入口仍然先从公共层进入：
- `library/remoteproc/remoteproc_core.c:create_client()`

这里的分派条件很直接：
- `client->ped == XEN`
- 然后选择 `rproc_xen_ops`

之后进入：
- `remoteproc_init()`
- `xen_rproc.c:rproc_init()`

`rproc_init()` 这一阶段主要做的是“准备 xen 私有上下文”，还没有真的创建 domU：
1. 分配 `struct rproc_pdata`
2. 调 `generate_xen_cfg(client)` 自动生成 xen 配置文件
3. 调 `init_domu_name(client, pdata)` 从 cfg 中解析 `name = "..."`
4. 把 `client->ped_ops` 设成 `xen_ped_ops`
5. 把 `rproc->priv`、`rproc->ops` 挂好

这里要特别注意两个点。

第一，xen pedestal 不是直接消费用户提供的现成 cfg，而是会根据 `struct pedestal_setup` 动态生成 `/etc/xen/<name>-xen.cfg`，其中会写入：
- `name`
- `vcpus`
- `memory`
- `kernel`
- `maxvcpus`
- `maxmem`
- `cpus`
- `cpu_weight`
- `cap`
- `iomem`
- `network`

这些字段来自：
- `library/include/mica/mica_client.h:struct pedestal_setup`

第二，`client->ped_cfg` 在 xen 下会被改写成自动生成后的 cfg 路径。因此 xen 的 `ped_cfg` 语义不同于 baremetal / hetero，它更接近“最终用于 `xl create` 的配置入口”。

create 阶段完成后的结果是：
- 已选中 xen backend
- 已有 domU name 和 cfg
- 但 domU 还没有创建
- grant / evtchn / shmem 也还没有建立

### 4.2 启动阶段

xen 这条线在顶层顺序上仍然是：
1. `remoteproc_config()`
2. `remoteproc_start()`
3. `create_rpmsg_device()`

但 xen 的特殊点在于：
- `remoteproc_config()` 内部并不只是静态配置
- 它会先 `xl create -p` 创建一个 paused 的 domU
- 再基于这个 paused domU 去补齐 domid、xenbus、grant table、event channel 和共享内存
- 等这些准备完成后，`remoteproc_start()` 才通过 `xl unpause` 让 guest 真正进入运行态

这里不是“先 start 再 config”，而是：
- 顶层依旧先 config 再 start
- 只是 xen 的 config 内部包含了一个“先 create paused domU，再继续补通信资源”的子过程

### 4.3 停止阶段

公共层 stop 仍然走：
- `remoteproc_shutdown()`
- 落到 `xen_rproc.c:rproc_shutdown()`

xen 的 shutdown 主链是：
1. `xl destroy <domu_name>` 直接销毁 domU
2. `IOC_SET_DOMID` 把 `/dev/mcs_xen` 当前 fd 里的 domid 清成 0
3. 遍历 `rproc->mems`，对每个映射：
   - `munmap()`
   - 从链表删除
   - 释放 `mem->io` 和 `mem`
4. 向 xenstore 写 frontend/backend state 为 `XenbusStateClosed`
5. 关闭 `mcs_fd`
6. 往 pipe 写入数据，让 notifier 线程退出
7. 清空 `rproc->rsc_table`、`rproc->rsc_len`、`rproc->bitmap`

这里要特别注意：
- xen 的 stop 不是向 guest 发一个固定 IPI，然后让 guest 自己关机
- 当前实现更接近“Dom0 直接 destroy domU，然后驱动 backend 清理 grant / evtchn 资源”
- 所以它比 baremetal 更偏向虚拟化生命周期，而不是 PSCI 协商式下电

### 4.4 移除阶段

`remoteproc_remove()` 最终落到：
- `xen_rproc.c:rproc_remove()`

这一层做得相对简单：
1. 删除自动生成的 xen cfg
2. 释放 `rproc_pdata`
3. `rproc->priv = NULL`

这里的语义是：
- 真正运行态资源回收在 `.shutdown()`
- `.remove()` 更接近 backend 私有上下文的析构

### 4.5 生命周期隐含钩子

#### `rproc_config()`

调用时机：
- `mica_start()` -> `load_client_image()` -> `remoteproc_config()`

它是 xen pedestal 的核心阶段，承担的不只是 `resource_table` 解析，而是整个 Xen 通信平面的建立。

其主链可以拆成：
1. 打开 `/dev/mcs_xen`
2. `create_and_pause_domu()`
3. `IOC_QUERY_MEM` 获取 backend 为该 domU 分配的共享内存信息
4. `init_shmem_pool(client, phy_addr, size)` 初始化共享内存池
5. `client->shmem_dynamic = true`
6. 解析 ELF header 和 resource table
7. 检查 resource table 大小不能超过一个 page
8. 把 resource table 拷贝到共享内存第一页
9. `remoteproc_set_rsc_table()` 挂入 remoteproc
10. `rproc->state = RPROC_READY`

其中第 2 步 `create_and_pause_domu()` 又是 xen 的关键中的关键：
- `xl create -p <cfg>` 创建但暂停 domU
- `get_domid()` 通过 `xl list` 找出这个 domU 的 runtime domid
- `ioctl(IOC_SET_DOMID)` 把 domid 告诉 `/dev/mcs_xen`
- `trigger_mcs_backend_probe()` 用 xenstore 主动补齐 frontend/backend 节点，触发 `xen-mcsback` probe

xen 的 `.config()` 同时完成了：
- domU 实例化
- domid 发现
- xenbus 前后端挂接
- backend 资源分配
- shared memory 注册
- resource table 入驻

这里和 baremetal 的差异也需要明确：
- xen 不会直接去 `mmap` ELF 中 `resource_table` 的原始地址
- 它在 `.config()` 里把资源表复制到共享内存首页，再把这份共享副本挂给 OpenAMP
- 因此 xen 对 resource table 的要求更接近“能否解析并复制到 grant/shared page”，而不是“ELF 原地址是否本身处于 Linux/master 可直接 `mmap` 的保留内存里”

#### `rproc_mmap()`

调用时机：
- 后续 remoteproc / OpenAMP 需要把共享内存映射到用户态时使用

作用：
- 通过 `mmap(..., pdata->mcs_fd, aligned_addr)` 映射 `/dev/mcs_xen` 暴露的共享内存
- 建立 `remoteproc_mem`
- 建立 `metal_io_region`
- 加入 `rproc->mems`

xen 下的共享内存并不是某个用户态文件直接 `malloc` 出来的，而是：
- Dom0 backend 分配一组 grant 页
- backend 把这组页的物理地址暴露给 `/dev/mcs_xen`
- 用户态再经由 `mmap()` 接入 remoteproc / libmetal

#### `rproc_notify()`

调用时机：
- 后续 virtio / rpmsg 正常通信阶段

作用：
- `ioctl(IOC_INVOKE_EVTCHN)`
- backend 侧调用 `notify_remote_via_evtchn(mcs_info->evtchn)`
- 从而触发 guest 收到 event channel 通知

这里跟 baremetal / hetero 不同：
- 不再是 `IOC_SENDIPI` -> 某个固定硬件中断
- 而是 `IOC_INVOKE_EVTCHN` -> Xen 事件通道

#### notifier 回通知链

xen 的“guest -> Linux/master”回通知闭环是：
1. guest 通过 event channel 通知 Dom0
2. `xen-mcsback.c:evtchn_handler()` 触发
3. `atomic_set(&irq_triggered, 1)`
4. `wake_up_interruptible(&mcs_evtchn_wait)`
5. 用户态线程在 `rproc_wait_event()` 中对 `/dev/mcs_xen` 做 `poll()`
6. `mcs_xen_poll()` 检测到 `irq_triggered == 1`，返回 `POLLIN | POLLRDNORM`
7. `rproc_wait_event()` 收到 `POLLIN` 后调用 `remoteproc_get_notification(rproc, 0)`

xen 的回通知链不是“内核直接推进 OpenAMP”，而是：
- event channel -> backend irq handler
- waitqueue 唤醒 -> `/dev/mcs_xen poll`
- 用户态 notifier thread -> remoteproc/OpenAMP

这和 hetero 的 poll 模型很像，但底层触发源已经从固定 IRQ 变成 Xen evtchn。

## 5. `/dev/mcs_xen` 接口语义

xen pedestal 当前主要围绕：
- `mcs_km/xen-mcsback.c`
- `/dev/mcs_xen`

关键 ioctl 有三个：
- `IOC_SET_DOMID`
- `IOC_QUERY_MEM`
- `IOC_INVOKE_EVTCHN`

### 5.1 `IOC_SET_DOMID`

用途：
- 把当前打开的 `/dev/mcs_xen` 文件句柄绑定到某个 domU

原因：
- xen backend 是按 domid 管理 `mcs_backend_info`
- 文件句柄本身并不知道自己服务哪个 guest
- 所以 `create_and_pause_domu()` 在拿到 runtime domid 后，必须立刻把 domid 写进设备 fd 私有数据

内核侧落点：
- `mcs_file_private_data.domid`
- `find_mcs_info()` 后续就靠这个 domid 去匹配对应 backend 实例

### 5.2 `IOC_QUERY_MEM`

用途：
- 取出 backend 为当前 domU 分配的共享内存物理地址和大小

返回值来自：
- `mcs_backend_info.shmem_phys`
- `mcs_backend_info.shmem_size`

这一步很关键，因为 xen 下共享内存不是静态 DTS 预留，而是 backend probe 时动态分配的 grant 页。

### 5.3 `IOC_INVOKE_EVTCHN`

用途：
- 通知远端 guest 处理消息

实现：
- 找到当前 domid 对应的 `mcs_backend_info`
- 取出其中的 `evtchn`
- 调 `notify_remote_via_evtchn(evtchn)`

它是 xen 下 `rproc_notify()` 的最终落点。

### 5.4 `mmap`

`mcs_xen_mmap()` 会严格检查：
- 要映射的 offset 是否落在该 domid 的 `shmem_phys..shmem_phys+shmem_size` 范围内

只有匹配到对应 backend 的共享内存范围，才允许 `remap_pfn_range()`。

这说明 `/dev/mcs_xen` 不是一个随便 mmap 任意物理地址的接口，而是“只暴露当前 domU 对应那块 backend grant 内存”。

### 5.5 `poll`

`mcs_xen_poll()` 基于：
- `mcs_backend_info.mcs_evtchn_wait`
- `atomic irq_triggered`

工作流程是：
- 用户态 `poll()` 挂到 waitqueue 上
- evtchn irq handler 把 `irq_triggered` 置 1
- `poll()` 看到事件后返回可读
- 同时用 `atomic_cmpxchg(..., 1, 0)` 清 ack

## 6. xen-mcsback.ko 实现

`mcs_km/xen-mcsback.c` 其实同时承担两层角色：
1. `/dev/mcs_xen` 字符设备
2. xenbus backend 驱动

真正让 xen pedestal 成立的是第 2 层。

### 6.1 backend 对象模型

核心对象是：
- `struct mcs_backend_info`

里面保存：
- `xdev`
- `grant_refs[SHMEM_NPAGES]`
- `evtchn`
- `evtchn_irq`
- `shmem_virt`
- `shmem_phys`
- `shmem_size`
- `domuid`
- `mcs_evtchn_wait`
- `irq_triggered`

可以把它理解为：
- “一个 domU 对应一份 backend 通信上下文”

用户态 fd 侧则是：
- `struct mcs_file_private_data`

它只记：
- `domid`
- `backend_info`

这里的绑定顺序是：
- 用户态先通过 `IOC_SET_DOMID` 完成“fd -> domid”绑定
- 之后 `find_mcs_info()` 再把这个 fd 缓存到对应 backend_info

### 6.2 backend probe 触发路径

xen backend 不是靠设备树起一个固定实例，而是靠 xenbus 探测。

用户态 `trigger_mcs_backend_probe()` 会向 xenstore 写入：
- frontend/backend 路径
- frontend-id / backend-id
- frontend/state = `XenbusStateInitialising`
- backend/state = `XenbusStateInitialising`

然后它等待：
- `/local/domain/0/backend/mica/<domid>/0/evtchn_port`

一旦这个键出现，就说明：
- `xen-mcsback` 已经完成 probe
- event channel 已建立
- grant table 等 backend 资源也已经准备就绪

xen 下的 probe 路径是：
- 用户态先搭 xenstore 节点
- 再由 xenbus backend 响应
- 再由用户态等待 backend 完成资源发布

### 6.3 grant table 建立流程

在 `mcs_backend_probe()` 里，backend 会：
1. 分配 `mcs_backend_info`
2. 设定 `shmem_size = 4096 * SHMEM_NPAGES`
3. 调 `mcs_init_gnttab()`

`mcs_init_gnttab()` 的关键动作是：
- `alloc_pages(GFP_KERNEL | __GFP_ZERO, SHMEM_ORDER)` 分配一大块连续页
- `page_address()` / `page_to_phys()` 得到虚拟地址和物理地址
- `xenbus_grant_ring()` 为每个页生成 grant ref
- 把每一页对应的 `gref_i` 写到 xenstore
- 把总页数 `gref_num` 也写到 xenstore

这正是 xen 模式“动态共享内存”的核心：
- 真正共享的是页授权能力（grant ref）
- 不是两边提前固定记住一段物理地址

Linux/master 自己仍然知道这组页的 `shmem_phys`
- 因为 Dom0 是分配者
- 所以用户态 master 还能通过 `/dev/mcs_xen` + `mmap()` 去映射它

但 guest 侧并不会直接拿到这个物理地址，它拿到的是：
- 一组 grant references

### 6.4 event channel 建立流程

`mcs_init_evtchn()` 的主链是：
1. `xenbus_alloc_evtchn()` 分配 evtchn port
2. `bind_evtchn_to_irqhandler()` 绑定本地 irq handler
3. 把 `evtchn_port` 写到 xenstore

之后：
- Dom0 通知 guest：`notify_remote_via_evtchn(evtchn)`
- guest 绑定该 port 后即可收到事件

这意味着 xen 下的通知通道也是动态获得的，不是编译期固定。

### 6.5 frontend state 变化与资源释放

backend 通过：
- `mcs_frontend_changed()`

跟踪 guest 侧 xenbus state。

当 guest/frontend 进入：
- `XenbusStateClosed`

backend 会：
- `mcs_backend_cleanup(dev)`
- 清理 grant table
- 清理 event channel
- 从活动链表中删除 backend 实例

同时 `rproc_shutdown()` 也会显式写 xenstore state 为 closed，促使 backend 做最终回收。

## 7. resource table 与共享内存组织

### 7.1 Linux/master 侧的放置规则

`xen_rproc.c:rproc_config()` 明确采用：
- 共享内存第一页存 resource table

代码里写得很直接：
- `rw_addr = pdata->shmem_addr`
- `alloc_shmem_region(client, rw_addr, pagesize)`
- `memcpy(rsc_table, image->buf + offset, rsc_size)`

这和 hetero 的“第一页放 rsc table”表面相似，但语义不同：
- hetero 的第一页来自预留共享内存
- xen 的第一页来自 backend 动态分配的一组 grant 页的第一页

### 7.2 `shmem_dynamic` 的关键作用

xen 这里专门引入 `client->shmem_dynamic = true`，本质上不是为了表示“这块内存是动态申请的”，而是为了切换共享内存的地址解释模型。

和 baremetal / hetero / jailhouse 的差异是：
- 这些 static shmem 场景里，MICA 和远端通常共享同一套基础地址语义，所以对应代码都会把 `client->shmem_dynamic` 设成 `false`
- xen 下共享内存来自运行时 grant 映射，Linux/master 侧看到的是 Dom0 这边的物理地址/映射结果，guest 侧看到的是自己通过 grant table 映射后的本地地址
- 因此 xen 不能再假设“resource table 里的 vring da 可以被两边按同一基址直接解释”

这个标志位后续会直接影响 RPMsg virtio 设备建立：
- `rpmsg_vdev.c:setup_vdev()` 在 `client->shmem_dynamic` 为 true 时，会额外查找 `RSC_VENDOR_VRING_OFFSET`
- 当 vring 采用动态分配时，代码不是只回填 `vring_rsc->da`
- 还会把 `vring_offset_rsc->offset[i] = da - client->phys_shmem_start`

xen 这里真正保留的稳定信息不是某个双方共享的绝对地址，而是“vring 相对共享内存起点的偏移”。

### 7.3 guest 侧为何不能直接使用 `vring.da`

这正是 xen 最容易误读的地方。

在普通 reserved-memory 模式下：
- resource table 里的 `vring.da` 往往可以直接作为远端可访问地址理解

但 xen 下不行。

因为：
- Linux/master 在 resource table 里看到的是自己一侧的物理/设备地址语义
- guest 侧拿到的是通过 grant table 映射进来的本地虚拟连续页
- 两边共享的是同一块通信内存的内容和布局，不是同一套绝对地址

所以 guest 侧真正可依赖的不是 `vring.da` 本身，而是前一节提到的 `vring_offset`：
1. 把共享内存第一页当成 resource table
2. 读取 `vring_offset.offset[i]`
3. 用 `local_da = shm_start_addr + vring_offset` 计算本地可访问地址
4. 再调用 `rproc_virtio_init_vring()`

这里稳定的语义是：
- xen 下稳定的是“vring 相对共享内存起点的偏移”
- 而不是“跨双方都相同的绝对物理地址”

这正是 xen 与 baremetal/hetero 在共享内存模型上的本质差异。

### 7.4 `vring_offset` 的适用边界

这里还有一个很容易被忽略、但对开发者非常关键的点：
- `vring_offset` 只解决了 vring 本体的位置定位问题
- 它并不能单独解决 vring 内部 desc 所引用的 buffer 地址翻译问题

原因在于，MICA/OpenAMP 在共享内存池里实际放进去的不只是一份 resource table 和两个 vring，还包括后续 RPMsg/virtio 使用的 shared buffer 池。

从 Linux/master 侧 `rpmsg_vdev.c:setup_vdev()` 可以看到，这块共享内存至少被依次消费为三类内容：
1. resource table
2. vring0 / vring1
3. `vdev_shpool` 对应的 shared buffers

`vring_offset` 解决的是“vring 入口定位”；OpenAMP 运行时继续访问的还包括 desc 和 desc 指向的 shared buffer。如果 guest 侧没有把整块共享通信区建立成统一的 phys->virt 翻译关系，后续到了 desc/buffer 层仍然会出问题。

更细的地址翻译逻辑，放到 8.7 再展开。

## 8. guest 侧运行时契约

虽然当前 `mcs` 仓内 `rtos/libmica` 还没有集成 xen pedestal，但从 Linux/master 侧协议设计已经能看出对端契约。

### 8.1 guest 先从 xenstore 读运行时参数

`rpmsg_backend_rsc_table_xen.c:get_xen_info()` 会：
1. 初始化 xenbus
2. 扫描当前可访问的 `/local/domain/<domid>`，确认自身 domid
3. 从 `/local/domain/0/backend/mica/<domid>/0/` 读取：
   - `gref_num`
   - `gref_0 ... gref_n`
   - `evtchn_port`

这表明 guest 侧并不预先知道：
- 我该映射哪段共享内存
- 我该监听哪个 port

而是完全依赖 xenbus 运行时发布的信息。

### 8.2 guest 通过 grant ref 建立共享内存映射

`init_gnttab_shmem()` 的做法是：
1. 根据 `gref_num` 推出共享内存大小
2. 申请连续页作为本地映射承载区
3. 对每个 `gref[i]` 构造 `gnttab_map_grant_ref`
4. `gnttab_map_refs()` 映射 grant 页
5. 最终得到本地 `shm_start_addr`

这说明 guest 侧共享内存不是“我去 mmap 某个固定 pa”，而是：
- 我根据 grant ref 把 Dom0 授权给我的页映射到当前 guest 自己的地址空间

### 8.3 guest 通过 event channel 建立通知

`init_evtchn()` 会：
- `bind_interdomain_event_channel(0, port, evtchn_cb, NULL)`

其中 `evtchn_cb()` 最终调用：
- `rproc_virtio_notified(cur_vdev, VRING1_ID)`

这表明 guest 收到 Dom0 通知后，也是回到 remoteproc/virtio 层推进队列处理。

### 8.4 guest 把 resource table 直接放在共享内存首页

guest 初始化时：
- `rsc_table_get(&rsc_table, &rsc_size)` 先取到本地编译进镜像的模板
- 紧接着又把 `rsc_table = shm_start_addr`

xen guest 最终实际使用的 resource table，不是镜像内部那份静态拷贝，而是：
- Dom0 已经复制到 grant shared memory 首页的那份运行态 resource table

这与 Linux/master 侧 `.config()` 的第一页拷贝逻辑正好闭环。

### 8.5 guest 通过 vring offset 建立本地 vring

`platform_create_vdev()` 中最关键的逻辑是：
- 先 `rproc_virtio_wait_remote_ready(vdev)`
- 然后分别取 `vring0/1` 的 `notifyid/num/align`
- 再从 `vring_offset` 中拿偏移
- 用 `local_da = shm_start_addr + vring_offset`
- 再 `rproc_virtio_init_vring()`

这个实现已经明确证明：
- xen 模式下双方对 vring 的一致性依赖“共享内存布局 + offset”
- 不是依赖某个固定的绝对地址协定

### 8.6 guest 仍然复用 reserved[0] 状态语义

这条实现路径说明：
- `CPU_ON_FUNCID`
- `CPU_OFF_FUNCID`
- `SYSTEM_RESET`

收到 evtchn 后，guest 的 `rpmsg_ipi_handler()` 会：
- 正常情况：`PRT_SemPost(msg_sem)`，推进消息处理
- `SYSTEM_RESET`：重置 virtqueue
- `CPU_OFF_FUNCID`：执行 `PRT_SysPowerOff()`

这表明 xen 虽然把“通知通道”和“共享内存地址来源”都动态化了，但 MICA/OpenAMP 上层的很多状态语义仍沿用原有 resource table 协议。

### 8.7 guest 侧 `metal_io` 需要把 Linux/master 物理地址映射成 guest 本地虚拟地址

这一点和前面的 `vring_offset` 是配套的，而且更容易在开发时被低估。

如果只看 `platform_create_vdev()`，容易得到下面的误判：
- guest 已经通过 `vring_offset` 算出了 `local_da`
- `rproc_virtio_init_vring()` 也已经拿到了本地可访问的 vring 地址
- 那么地址问题就已经解决了

但地址问题并未在这里结束。

关键在于，OpenAMP 后续访问的不只是 vring 本体，还会继续访问：
- vring desc
- desc 里记录的 buffer 地址
- RPMsg/virtio 使用的 shared buffer 池

而这些 buffer 并不是 guest 自己重新初始化出一套“guest 视角地址”，它们来自 Linux/master 侧更早的初始化过程。

从 MICA 侧可以看到：
- `rpmsg_vdev.c:setup_vdev()` 先分配 vring0 / vring1
- 然后又分配 `vdev_shpool` 对应的 shared buffer 区
- 再通过 `rpmsg_virtio_init_shm_pool(&client->vdev_shpool, buf, bufsz)` 交给 OpenAMP/RPMsg 使用

后续在 OpenAMP/RPMsg 初始化和收发过程中，这些 buffer 会进入 virtqueue；而 vring desc 里保存的 buffer 地址，是 Linux/master 一侧建立队列时写进去的地址语义。

这里的主要风险是：
- `vring_offset` 只能帮助 guest 找到 vring0 / vring1 本体
- 但它并不会自动把 desc 中的 buffer 地址逐个改写成 guest 本地地址
- 这些地址又可能是运行时分配出来的，guest 也不可能靠额外接口逐个获知

所以 Xen guest 侧必须像现在这样建立 `metal_io`：
- `virt` 侧使用 guest 通过 grant table 映射后的本地虚拟地址
- `phys` 侧保留 Linux/master 在 resource table / vring / shared buffer 中写进去的那套物理地址语义

这样做以后，OpenAMP/libmetal 在 guest 侧后续处理共享内存对象时，才能统一完成翻译：
- 访问 vring 本体时，能从 Linux/master 侧地址语义映射到 guest 本地虚拟地址
- 访问 vring desc 时，也能继续沿同一套映射关系工作
- 访问 desc 指向的 shared buffer 时，仍然能把 Linux/master 写进去的 buffer 地址翻译成 guest 自己可访问的虚拟地址

这就是为什么 Xen 侧的：
- `metal_io_init(&device->regions[0], (void *)shm_virtmap, &shm_physmap, shm_size - vring_offset, -1, 0, NULL)`

不能简单退化成 baremetal 那种近似一比一的：
- `metal_io_init(&device->regions[0], (void *)VDEV_START_ADDR, &shm_physmap, SHM_SIZE, -1, 0, NULL)`

baremetal 里，共享区的物理地址和远端本地地址通常沿固定基址关系解释，OpenAMP 后续看到的地址天然一致；
xen 里，共享内容虽然一样，但 guest 自己看到的是 grant 映射后的本地地址空间，如果 `metal_io` 还按 baremetal 思路做，一旦 OpenAMP 进入 desc/buffer 层，guest 就会把 Linux/master 视角下的 buffer 地址当成本地可访问地址去解，最终在底层收发路径上出错。

这一层真正解决的问题不是“如何找到 vring”，而是：
- 如何让 guest 侧 OpenAMP/libmetal 在运行期正确解释整块共享通信区里的所有地址对象

## 9. 调试入口

如果 xen 起不来，建议按下面顺序看。

### 9.1 domU 创建成功确认点

优先检查：
- `generate_xen_cfg()` 生成内容是否符合预期
- `xl create -p <cfg>` 是否成功
- `get_domid()` 是否能从 `xl list` 找到目标 domU

如果 domU 都没创建出来，后面的 xenbus / gref / evtchn 都不会成立。

### 9.2 xenbus backend probe 确认点

关键点：
- `trigger_mcs_backend_probe()` 是否成功写入 xenstore
- `wait_for_xenbus_probe()` 是否等到了 `evtchn_port`
- `xen-mcsback` 是否真的进入 `mcs_backend_probe()`

如果 `evtchn_port` 一直不出现，重点不是 OpenAMP，而是 xenbus 前后端根本没挂起来。

### 9.3 共享内存建立确认点

Linux/master 侧看：
- `IOC_QUERY_MEM` 是否返回有效 `phy_addr/size`
- `mmap()` 是否能映射这段范围

guest 侧看：
- `gref_num` / `gref_i` 是否能从 xenstore 读到
- `gnttab_map_refs()` 是否成功
- `shm_start_addr` / `shm_size` 是否合理

### 9.4 event channel 双向可用确认点

Dom0 侧看：
- backend 是否分配到 `evtchn`
- `bind_evtchn_to_irqhandler()` 是否成功
- `IOC_INVOKE_EVTCHN` 是否能触发远端

guest 侧看：
- `evtchn_port` 是否读到
- `bind_interdomain_event_channel()` 是否成功
- `evtchn_cb()` 是否被调用

### 9.5 OpenAMP / RPMsg 确认点

确认：
- resource table 是否已被复制到 grant shared memory 首页
- guest 是否按 `vring_offset` 而不是直接按 `vring.da` 建 vring
- guest 侧 `metal_io` 是否按 Xen 模型建立，而不是沿用 baremetal 那种近似一比一的地址解释
- 如果 vring 能初始化但后续 virtio/rpmsg 收发阶段崩掉，要继续怀疑 desc/buffer 地址仍在按 Linux/master 视角被解释
- `rproc_virtio_wait_remote_ready()` 是否过了
- `rpmsg_init_vdev_with_config()` 是否成功

如果这里失败，问题往往已经不在 xenbus，而是在“共享内存布局解释是否一致”。

## 10. 关键差异总结

xen pedestal 和 baremetal / hetero 的本质区别可以压缩成三句话：

第一，实例身份是运行时发现的。
- 不是固定 cpu_id
- 而是 `xl create` 后拿到 domid

第二，通信资源是运行时发布的。
- 不是两边提前约定的中断号和共享内存物理地址
- 而是 xenbus 发布的 evtchn port 和 grant refs

第三，地址一致性靠 offset，不靠绝对地址。
- Linux/master 可以看到 backend 页的物理地址
- guest 只能把 grant 引用映射成自己的本地地址
- 所以 vring 建链必须以共享内存布局偏移为准

理解了这三点，再回头看 `xen_rproc.c`、`xen-mcsback.c` 和 Xen guest 侧运行时契约，整个 xen pedestal 的代码逻辑就会顺很多。

## 11. 继续阅读

- `pedestal-overview.md`
- `lifecycle-overview.md`
- `lifecycle-start.md`
- `lifecycle-stop.md`
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `../../mica-communication/references/communication-overview.md`
