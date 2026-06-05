# remoteproc 模型

## 1. 文档目标

这篇文档专门解释 OpenAMP `remoteproc` 在 MICA 中是怎样真正落地的。

它主要回答：
- `remoteproc` 在 MICA 里对应哪些源码入口
- `remoteproc_init/config/load/start/shutdown/remove` 在当前仓里分别由谁负责
- `resource_table`、共享内存、镜像装载、运行状态是如何串起来的
- 为什么很多看起来像“start 问题”的故障，其实卡在 `remoteproc` 更早的阶段
- 什么问题还属于 `remoteproc` 范畴，什么问题已经继续下沉到 virtio / RPMsg

如果已经确定问题不在命令层，而在“远端处理器机制本身未成立”，应先读这篇。

## 2. remoteproc 基础认知

在当前仓里，MICA 并没有自造一套完全独立的远端处理器管理机制。

更准确的结构是：
- MICA 负责组织 `mica_client`、pedestal、service、共享内存池、生命周期编排
- OpenAMP `remoteproc` 负责远端处理器对象、镜像装载、资源表、virtio 前置条件
- pedestal backend 负责把平台相关能力实现成 `remoteproc_ops`

从代码上看，这个关系大致落在：
- 公共 remoteproc 编排：`library/remoteproc/remoteproc_core.c`
- pedestal backend：
  - `library/remoteproc/baremetal_rproc.c`
  - `library/remoteproc/riscv_rproc.c`
  - `library/remoteproc/jailhouse_rproc.c`
  - `library/remoteproc/xen_rproc.c`
- 上层生命周期入口：`library/mica/mica.c`

因此，MICA 的 `create/start/stop/remove` 很多时候是在推动 OpenAMP `remoteproc` 进入不同阶段，而不是单独维护一条完全平行的状态机。

## 3. remoteproc 在 MICA 里的主入口

### 3.1 `create_client()`：先选 backend，再进入 remoteproc

`library/remoteproc/remoteproc_core.c:create_client()` 是 `remoteproc` 在 MICA 里的第一处公共入口。

它做的第一件关键事不是启动远端，而是：
- 根据 `client->ped` 选择 `remoteproc_ops`

当前主分派条件是：
- `BARE_METAL` -> `rproc_bare_metal_ops`
- `JAILHOUSE` -> `rproc_jailhouse_ops`
- `XEN` -> `rproc_xen_ops`
- `HETERO && cpu_str == "riscv"` -> `rproc_riscv_ops`

然后进入：
- `remoteproc_init(&client->rproc, ops, client)`

所以，从 `remoteproc` 角度看，pedestal 的意义不是“附加配置项”，而是直接决定：
- `.init/.config/.start/.shutdown/.remove/.mmap/.notify` 由谁实现
- 共享内存与资源表怎么准备
- 远端是怎么被启动、停止、通知的

### 3.2 `load_client_image()`：`remoteproc` 真正开始进入装载语义

`library/remoteproc/remoteproc_core.c:load_client_image()` 是第二个关键公共入口。

这里的顺序很重要：
1. `store_open()` 打开镜像
2. `remoteproc_config(rproc, &store)`
3. 如果 `rproc->rsc_table` 已存在且 `rproc->state == RPROC_READY`，则直接返回
4. 否则再走 `remoteproc_load(rproc, client->path, &store, &mem_image_store_ops, NULL)`

这个顺序说明：
- `remoteproc_config()` 不是可有可无的预处理
- 它是每个 backend 先把平台条件准备到 OpenAMP 可接受状态的阶段
- 某些 pedestal 下，`config` 本身就足以把 `rproc` 推到 `RPROC_READY`
- 某些场景下还需要继续通过 `remoteproc_load()` 走 OpenAMP loader 语义

## 4. `remoteproc` 关键阶段在 MICA 中的作用

### 4.1 `remoteproc_init()`：建立 remoteproc 对象与 backend 私有状态

这一阶段的职责是：
- 建立 `remoteproc` 对象
- 初始化 backend 私有数据
- 让后续 `.config/.start/.shutdown/.remove` 有运行上下文

但不同 pedestal 的具体动作差异很大。

例如：
- baremetal / hetero 会建立 `/dev/mcs` 及其后续控制语义
- jailhouse 会建立 ivshmem / UIO 相关运行前提
- xen 会建立 `/dev/mcs_xen` 与 Xen 运行环境相关对象

所以如果 create 阶段就失败，很多时候问题还没进入 OpenAMP 通用机制，而是卡在 backend 初始化本身。

### 4.2 `remoteproc_config()`：把 backend 推到“可以被 OpenAMP 消费”的状态

这是 `remoteproc` 在 MICA 中最容易被低估的一步。

从作用上说，它通常负责：
- 初始化共享内存池
- 解析镜像头
- 定位或布置 `resource_table`
- 设置 boot 地址或等价运行前提
- 必要时把 `rproc->state` 推到 `RPROC_READY`

但不同 pedestal 的落地方式不同：
- baremetal：按当前 CPU 对应的 RTOS 运行内存窗口初始化共享内存池，并按镜像中的 `resource_table` 设备地址建立访问前提
- hetero/riscv：初始化共享内存池，在共享内存首页建立 `resource_table`，并设置 `bootaddr`
- jailhouse：初始化 ivshmem 共享内存池，并在 cell load 阶段形成共享区中的 `resource_table`
- xen：初始化动态共享内存池，并在动态共享内存契约下形成 `resource_table` 与 ready 前提

这一阶段最重要的判断标准不是“配置动作看起来做了没”，而是：
- `rproc->rsc_table` 是否已经成立
- `rproc->state` 是否达到 `RPROC_READY`
- 后续 OpenAMP 是否已经有足够上下文去创建 virtio/RPMsg

### 4.3 `remoteproc_load()`：真正进入 OpenAMP loader 语义

如果 `config` 之后仍未 ready，公共层会继续调用：
- `remoteproc_load()`

在 `remoteproc_core.c` 里，loader 的 `store_load()` 有两个很关键的分支：
- `pa == METAL_BAD_PHYS` 时，直接把数据读到内存缓冲区
- 否则通过 `metal_io_phys_to_virt(io, pa)` 找到目标可写地址，再把镜像段读进去

这说明 `remoteproc_load()` 在 MICA 中并不是抽象操作，而是明确依赖：
- backend `.mmap`
- `metal_io_region`
- `metal_io_phys_to_virt()`

所以如果这里失败，问题往往集中在：
- 镜像段地址是否合理
- backend `.mmap` 是否正确挂了 `remoteproc_mem`
- `metal_io_region` 是否能把 `pa` 正确翻译成可访问地址

### 4.4 `remoteproc_start()`：把远端推进到运行态

在公共层里，`start_client()` 只是简单调用：
- `remoteproc_start(rproc)`

真正的平台差异都在 backend `.start` 里。

例如：
- baremetal 会走 remote CPU 启动路径
- hetero/riscv 会完成 notifier 建立、必要的 bin 装载和 MCUON 启动
- jailhouse 会推进 cell start
- xen 会推进 domU 运行态建立

因此 `remoteproc_start()` 的语义是：
- 公共语义统一
- 具体平台动作下沉到各 `remoteproc_ops`

但要注意一个常见误判：
- `remoteproc_start()` 成功并不自动说明 OpenAMP 后续 virtio/RPMsg 握手已经闭合
- 在当前链路里，它只说明 remoteproc 运行态已经成立
- 后面还要继续经过 `create_rpmsg_device()`、`remoteproc_create_virtio()`、`rpmsg_init_vdev()`、name service 和 service bind

这一点非常重要，因为很多“start 失败”表面都落在 `mica start`，但真正的根因未必在公共层，而是在某个 backend 的启动动作没完成。

`remoteproc_init()`、`remoteproc_config()` 和 `remoteproc_start()` 的 pedestal-specific 完整实现见 `../../mica-pedestals/references/ped-baremetal.md`、`../../mica-pedestals/references/ped-hetero.md`、`../../mica-pedestals/references/ped-jailhouse.md`、`../../mica-pedestals/references/ped-xen.md`。

### 4.5 `remoteproc_shutdown()` / `remoteproc_remove()`：停机与回收不是一回事

在 MICA 里：
- `stop_client()` -> `remoteproc_shutdown()`
- `destory_client()` -> `remoteproc_remove()`

这两步语义不同：
- `shutdown` 更偏运行态收尾
- `remove` 更偏对象与 backend 资源最终回收

这在 jailhouse/xen 这类更像虚拟化对象生命周期的 pedestal 下尤其明显：
- shutdown 不等于 destroy
- remove 才是最终把 cell/domain/backend 对象清掉

所以调试时不要把 stop/remove 混成一件事。

## 5. `resource_table` 的核心作用

在当前仓里，`resource_table` 是 `remoteproc` 向后续 virtio/RPMsg 交接的关键中枢。

从机制上说，它至少承担三件事：
1. 让 backend / loader 知道资源描述长什么样
2. 让 `setup_vdev()` 能找到 `RSC_VDEV`
3. 让后续 endpoint / vendor resource 能继续被消费

在代码上，典型关键点包括：
- backend `.config()` 中定位或拷贝 `resource_table`
- `remoteproc_set_rsc_table(rproc, ...)`
- `rproc->rsc_table` 被 `rpmsg_vdev.c:setup_vdev()` 消费
- `mica_rsc_table.c` 继续读 vendor resource，例如 endpoint table

所以如果 `rproc->rsc_table` 不成立，后面很多现象都会一起塌：
- virtio 起不来
- RPMsg 起不来
- service ready 起不来
- 甚至状态看起来 running，但通信面完全不存在

## 6. `.mmap` 的 remoteproc 核心作用

OpenAMP loader 和后续 virtio/RPMsg 并不直接知道：
- 某个平台上某个 `pa/da` 应该怎样映到当前 userspace 可访问地址

所以 backend 必须提供：
- `.mmap`

而在当前仓里，这通常会进一步完成：
- 建立用户态虚拟地址
- 创建 `remoteproc_mem`
- 创建 `metal_io_region`
- 把 memory 挂进 `rproc->mems`
- 最终返回 `metal_io_phys_to_virt()` 可消费的地址

这就是为什么：
- `remoteproc` 虽然名字像“远端处理器控制”
- 但它在当前仓里并不只负责 start/stop
- 它还承担了镜像段、资源表、共享内存可访问性的前置组织工作

如果 `.mmap` 这一步不通，`remoteproc` 层面常见后果是：
- `remoteproc_load()` 失败
- `resource_table` 无法访问
- vring 描述能找到，但后续不能真正消费

### 6.1 `rproc->rsc_io` 的关键上下文作用

除了 `rproc->rsc_table`，OpenAMP 在 `remoteproc_create_virtio()` 里还会直接取：
- `rproc->rsc_io`

从 `open-amp` 仓下的 `lib/remoteproc/remoteproc.c` 看，这个对象会被作为：
- `vdev_rsc_io = rproc->rsc_io`

再继续传给：
- `rproc_virtio_create_vdev(role, notifyid, vdev_rsc, vdev_rsc_io, rproc, ...)`

这意味着 `resource_table` 并不只是“有一块内存内容可读”就够了。
还要同时满足：
- OpenAMP 知道这块资源表/virtio resource 区域的访问语义
- 后续对 `status`、`dfeatures`、`gfeatures`、virtio config 区的读写都能通过 `rsc_io` 正常完成

所以如果你看到下面这类现象：
- `RSC_VDEV` 能找到
- 但 `remoteproc_create_virtio()` 后续行为异常
- virtio status / features 看起来不对

问题就不能只盯 `rsc_table` 内容本身，还要检查：
- backend 是怎么建立 `rsc_io` 的
- 这块 I/O region 是否覆盖了当前资源区
- 后续 libmetal 读写语义是否和平台共享内存契约一致

## 7. `remoteproc` 与 virtio/RPMsg 的边界在哪

理解 `remoteproc` 最容易混乱的一点，是它和 virtio/RPMsg 的边界。

一个简化但准确的理解是：
- `remoteproc` 负责把“远端可以被启动，并且通信资源描述可被消费”这件事准备好
- virtio/RPMsg 负责把这些资源描述真正变成可运行的消息设备与 endpoint

在当前仓里，大致边界是：
- `remoteproc_config/load/start` 之前与之中：还是 `remoteproc` 主导
- `create_rpmsg_device()` 开始：进入 `RSC_VDEV`、vring、virtio、RPMsg 主线

因此如果问题是：
- 镜像加载不进内存
- `rproc->rsc_table` 没成立
- `RPROC_READY` 没形成
- backend `.start` 没成功

优先看 `remoteproc`。

### 7.1 `notifyid` 是 remoteproc 向 virtio 交棒时的关键资源

在 `remoteproc_create_virtio()` 里，OpenAMP 会先从 `struct fw_rsc_vdev` 取：
- `vdev_rsc->notifyid`

这个 `notifyid` 会作为 virtio device 的通知标识，传给：
- `rproc_virtio_create_vdev()`

随后 OpenAMP 又会继续遍历每个 vring，分别读取：
- `vring_rsc->notifyid`

再交给：
- `rproc_virtio_init_vring(vdev, i, notifyid, ... )`

这说明当前链路里至少有两层通知标识：
- vdev 级 `notifyid`
- vring 级 `notifyid`

它们的作用不是装饰字段，而是后续：
- `remoteproc_get_notification(rproc, notifyid)`
- `rproc_virtio_notified()`
- virtqueue notification 分发

能否正确把平台中断/事件重新送回正确 virtqueue 的关键索引。

所以如果现象是：
- 远端似乎已经 kick 了
- 但 Linux/master 侧 virtqueue 没被正确推进

要怀疑的不只是 `.notify`，还包括：
- `resource_table` 里的 vdev/vring `notifyid` 是否合理
- backend 收到的通知编号是否和 OpenAMP 预期一致
- `remoteproc_get_notification()` 是否被正确调用到

如果问题是：
- `remoteproc` 已 ready / running
- 但 `remoteproc_create_virtio()` 或 `rpmsg_init_vdev()` 失败
- 或 service 没出现

那就应该继续转去看：
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `../../mica-communication/references/openamp-rpmsg.md`

## 8. RTOS/client 对端在 remoteproc 模型中的位置

虽然本篇主要从 Linux/master 讲 `remoteproc`，但在 MICA 里，remoteproc 真正成立还依赖对端约定。

从 `rtos/libmica/src/pedestals/baremetal.c` 和 `hetero.c` 可以看到，对端在 OpenAMP 初始化时会继续做：
- `metal_init()`
- `metal_register_generic_device()`
- `metal_device_open()`
- `platform_create_vdev()`
- `rproc_virtio_wait_remote_ready()`
- `rproc_virtio_init_vring()`
- `rpmsg_init_vdev_with_config()`

这说明：
- Linux/master 侧把 `resource_table`、共享内存、运行态准备好之后
- RTOS/client 侧还要把同一套资源描述重新解释成本地的 virtio/RPMsg 对象

所以 remoteproc 问题很多时候不是“单边失败”，而是：
- 一边已经 ready
- 但另一边还没按同样语义把对象建起来

### 8.1 `rproc_virtio_wait_remote_ready()` 的不对称行为要单独记住

从 `open-amp` 仓下的 `lib/remoteproc/remoteproc_virtio.c` 看：
- `rproc_virtio_wait_remote_ready()` 对 `VIRTIO_DEV_DRIVER` 侧会直接返回
- 只有 `VIRTIO_DEV_DEVICE` 一侧才会循环等待 `VIRTIO_CONFIG_STATUS_DRIVER_OK`

这对应到当前系统就是：
- Linux/master 侧作为 host/driver，不在这里等待 remote ready
- RTOS/client 侧作为 remote/device，会阻塞等待 host 把 virtio status 置到 `DRIVER_OK`

这个不对称行为非常重要，因为它解释了一个常见现象：
- Linux/master 侧可以先继续向下执行 `remoteproc_create_virtio()` / `rpmsg_init_vdev()`
- 但 RTOS/client 侧还可能卡在等待 host ready 的位置

所以当两侧表现出“master 已经继续跑，client 还没起来”时，不要立刻把它当成异常；先判断是否正处在这个握手窗口。

## 9. 调试 remoteproc 时最值得抓的观察点

### 9.1 backend 选择确认点
先确认：
- `client->ped` 是否正确
- `remoteproc_ops` 是否进入了预期 backend
- hetero 是否真的走到了 riscv 分支

### 9.2 `remoteproc_config()` 后的 ready 前提确认点
确认：
- `rproc->rsc_table` 是否已存在
- `rproc->state` 是否进入 `RPROC_READY`
- 共享内存池是否初始化成功

### 9.3 `remoteproc_load()` 的进入条件与卡点
确认：
- 是不是本来就该 skip load
- 如果进入 load，失败点是在镜像解析、`.mmap`，还是 `phys_to_virt`

### 9.4 `.start` 平台动作完成确认点
确认：
- CPU / cell / domain / guest 是否真的进入运行态
- notifier / poll / 等待线程是否已经建立

### 9.4.1 `rsc_io` / `notifyid` 成立确认点
确认：
- `rproc->rsc_io` 是否已经建立且可访问当前 resource 区域
- `vdev_rsc->notifyid` 和各 `vring_rsc->notifyid` 是否合理
- backend 通知路径最终是否能把 `notifyid` 正确送到 `remoteproc_get_notification()`

### 9.5 不要把 service 问题误判成 remoteproc 问题
如果：
- `rproc->state == RPROC_RUNNING`
- `rproc->rsc_table` 也存在
- backend start 也成功

但服务仍然不可用，那问题通常已经不在 remoteproc 本身，而在 virtio / RPMsg / service 绑定层。

## 10. 建议继续阅读

理解完这篇后，通常继续转读：
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `../../mica-communication/references/openamp-rpmsg.md`
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `lifecycle-start.md`
- `../../mica-communication/references/communication-overview.md`
