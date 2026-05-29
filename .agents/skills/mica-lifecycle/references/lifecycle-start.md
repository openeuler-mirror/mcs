# mica start 过程拆解

## 1. 文档目标

这篇文档专门解释 Linux/master 侧执行 `mica start <name>` 时，MICA 是如何把一个已经 create 完成的 `mica_client` 推进到：
- 镜像已配置/已加载
- remoteproc 已进入运行态
- RPMsg 设备已建立
- Linux 侧后续服务创建得以继续

它主要回答这些问题：
- `mica_start()` 自身到底做了几步
- `load_client_image()`、`start_client()`、`create_rpmsg_device()` 的边界是什么
- `remoteproc_config()` 和 `remoteproc_load()` 分别负责什么
- `mica start` 成功，究竟代表什么，不代表什么
- OpenAMP/virtio/rpmsg 初始化是在 start 主链里的什么位置插入的

如果一个 agent 需要分析下面这些问题，这篇文档应该先读：
- 为什么 `mica start` 失败
- 为什么 remote OS 没有真正跑起来
- 为什么 `Running` 之后服务还没出来
- 为什么 `create` 成功但 `start` 之后 RPMsg 通路还没通

## 2. 涉及文件

与 `mica start` 直接相关的代码主要分布在这几个文件：
- `library/mica/mica.c`
  - `mica_start()` 主入口
  - `mica_stop()` / `mica_remove()` 也在这里定义
- `library/remoteproc/remoteproc_core.c`
  - `create_client()`
  - `load_client_image()`
  - `start_client()`
  - `stop_client()`
  - `destory_client()`
  - `show_client_status()`
- `library/rpmsg_device/rpmsg_vdev.c`
  - `create_rpmsg_device()`
  - `release_rpmsg_device()`
  - `setup_vdev()`
- `library/rpmsg_device/rpmsg_service.c`
  - RPMsg name service 绑定与服务注册配合
- `library/remoteproc/<pedestal>_rproc.c`
  - 不同 pedestal 的 `.config/.start/.mmap/.notify/.shutdown` 真实实现

因此，`mica start` 在代码上体现为：
- 由 `library/mica/mica.c` 提供 start 入口编排
- 由 `remoteproc_core.c` 把 start 过程下沉到对应 pedestal 的 remoteproc 实现
- 再由 `rpmsg_vdev.c` 把已经 ready/running 的生命周期对象接到 virtio/rpmsg 设备建立流程

更完整的生命周期分层结构，应参考 `lifecycle-overview.md`。

## 3. start 前提：client 必须已经 create 完成

`mica_start()` 不负责创建 `mica_client`，它要求传入的 `client` 已经在 create 阶段构造好。

start 之前至少已经完成：
- `mica_create(client)`
- `create_client(client)`
- `remoteproc_init(&client->rproc, ops, client)`
- `client->ped`、`client->ped_setup`、`client->path` 等关键字段已准备好

所以，如果 `start` 失败，不能只盯 `mica_start()`，还必须确认 create 阶段留下的状态是否正确，尤其是：
- `client->ped` 是否选对
- `remoteproc_ops` 是否选对
- `client->path` 是否指向有效镜像
- `client->ped_setup` 是否满足对应 pedestal 要求

## 4. `mica_start()` 主调用链

`library/mica/mica.c` 中的 `mica_start()` 很短，但它是整个 start 流程的总编排入口。

执行顺序固定为：
1. `load_client_image(client)`
2. `start_client(client)`
3. `create_rpmsg_device(client)`
4. 如果 `client->debug` 为真，再执行 `create_rbuf_device(client)`

对应代码语义可以概括成：
- 第一步：把 remoteproc 变成“镜像和资源表已准备好”的状态
- 第二步：真正启动 remote OS
- 第三步：建立 Linux/master 侧 RPMsg 设备，使后续服务注册有底座可用
- 第四步：如果启用了 debug，再额外建立 ring buffer 调试设备

这四步里，任何一步失败，`mica_start()` 都可能返回错误。

## 5. 第一步：`load_client_image(client)`

位置：`library/remoteproc/remoteproc_core.c`

这是 `mica start` 中最常被误解的一步。
它不只是“把 ELF 读出来”，而是至少包含两层动作：
1. `remoteproc_config()`
2. `remoteproc_load()`

### 5.1 先打开镜像文件
`load_client_image()` 开头调用：
- `store_open(&store, client->path, &img_data)`

这里会：
- `fopen(client->path, "r")`
- `fseek/ftell` 获取镜像大小
- `malloc` 一块 buffer
- `fread` 把镜像内容读到内存

如果镜像路径错误、文件打不开、内存分配失败，这里就会直接失败。

### 5.2 再做 `remoteproc_config(rproc, &store)`
这是比 `remoteproc_load()` 更早的一步。
它的职责不是“拷贝镜像正文”，而是让对应 pedestal 先完成启动前的准备工作。

不同 pedestal 下，这一步差异很大。

#### baremetal
baremetal 的 `.config` 主要负责按当前 CPU 对应的 RTOS 运行内存窗口初始化共享内存池，并按镜像中的 `resource_table` 设备地址建立访问前提。

#### hetero（当前 Linux 侧实现对应 `riscv_rproc.c`）
hetero 的 `.config` 主要负责切换到 RISC-V pedestal、初始化共享内存池、在共享内存首页建立 `resource_table`、设置 `bootaddr`，并把状态推进到 `RPROC_READY`。

#### jailhouse
jailhouse 的 `.config` 主要负责初始化 ivshmem 共享内存池、完成 cell load，并在共享区中建立可被后续阶段消费的 `resource_table`。

#### xen
xen 的 `.config` 主要负责建立 paused domU、初始化动态共享内存池，并形成 `resource_table` 与后续通信资源的对应关系。

这里需要明确一点：
- `remoteproc_config()` 不是一个轻量步骤
- 它经常是“按 pedestal 把 remote OS 运行环境搭起来”的主要阶段
- 具体 backend 实现见 `../../mica-pedestals/references/ped-baremetal.md`、`../../mica-pedestals/references/ped-hetero.md`、`../../mica-pedestals/references/ped-jailhouse.md`、`../../mica-pedestals/references/ped-xen.md`

### 5.3 `RPROC_READY` 快路径
`load_client_image()` 在 `remoteproc_config()` 之后，会检查：
- `if (rproc->rsc_table && rproc->state == RPROC_READY)`

如果成立，就直接返回，不再调用 `remoteproc_load()`。

这表明某些 pedestal 的 `.config` 本身就已经把 resource table 和运行前状态准备到位，此时不需要再重复走标准 loader 路径。

jailhouse 就是很典型的例子：
- `.config` 里已经做了镜像 load 和 resource table 设置
- 因此后面会通过 `RPROC_READY` 快路径跳过 `remoteproc_load()`

### 5.4 标准 `remoteproc_load()` 路径
如果还没有进入 `RPROC_READY`，`load_client_image()` 会调用：
- `remoteproc_load(rproc, client->path, &store, &mem_image_store_ops, NULL)`

这里的关键点是：
- `mem_image_store_ops` 由 `store_open/store_close/store_load` 组成
- 真正把镜像各段写到目标内存时，会通过 OpenAMP remoteproc loader 机制调用 `.load`
- `.load` 在需要物理地址到虚拟地址转换时，会依赖 `metal_io_phys_to_virt(io, pa)`

这也意味着 `remoteproc_load()` 能否正确工作，不只是 loader 问题，也依赖前面 `remoteproc_mmap()` 与 `metal_io_region` 是否建立正确。

### 5.5 resource table 是 load 阶段的硬边界
`load_client_image()` 在 `remoteproc_load()` 后会显式检查：
- `if (!rproc->rsc_table)`
  - 直接报错：`failed to parse rsc table`

所以从 MICA 角度看：
- resource table 不是“可有可无的元数据”
- 它是后续 virtio/rpmsg 设备建立的前提
- 如果 resource table 没解析出来，后面的 `create_rpmsg_device()` 基本不可能正常成立

## 6. 第二步：`start_client(client)`

位置：`library/remoteproc/remoteproc_core.c`

这一步本身很薄：
- `return remoteproc_start(rproc);`

真正的启动行为交给 pedestal-specific `.start` 去做。

### 6.1 baremetal
baremetal 的 `.start` 主要负责触发 remote CPU 上线，并建立后续通知闭环。

### 6.2 jailhouse
jailhouse 的 `.start` 主要负责启动 cell，并建立 ivshmem 事件等待链。

### 6.3 hetero（当前 Linux 侧实现对应 `riscv_rproc.c`）
hetero 的 `.start` 由 `library/remoteproc/riscv_rproc.c` 中的 `rproc_riscv_ops` 承担，主要负责 notifier 建立、必要的 pedestal/bin 装载，以及通过 `IOC_MCUON` 拉起远端。

### 6.4 xen
xen 的 `.start` 主要负责让 domU 从 pause 进入运行态，并接通后续通知与共享内存通路。

`start_client()` 返回成功的语义应理解为：
- remoteproc 生命周期已经被推进到“远端开始运行”的阶段
- 但这并不等价于“RPMsg 服务已经 ready”
- 具体 backend 实现见 `../../mica-pedestals/references/ped-baremetal.md`、`../../mica-pedestals/references/ped-hetero.md`、`../../mica-pedestals/references/ped-jailhouse.md`、`../../mica-pedestals/references/ped-xen.md`

## 7. 第三步：`create_rpmsg_device(client)`

位置：`library/rpmsg_device/rpmsg_vdev.c`

这是 `mica start` 真正把 OpenAMP virtio/rpmsg 设备接起来的一步。

它的主链如下：
1. 分配 `struct rpmsg_virtio_device`
2. `setup_vdev(client)`
3. `remoteproc_create_virtio(&client->rproc, 0, VIRTIO_DEV_DRIVER, NULL)`
4. `rpmsg_init_vdev(rpmsg_vdev, vdev, mica_ns_bind_cb, client->shbuf_io, &client->vdev_shpool)`
5. `client->rdev = rpmsg_virtio_get_rpmsg_device(rpmsg_vdev)`

这个顺序非常重要。

### 7.1 `setup_vdev(client)` 的作用
`setup_vdev()` 是整个 RPMsg 设备建立里最核心的准备阶段之一。

它承担的不只是“注册一个对象”，而是：
- 从 `rproc->rsc_table` 中找到 `RSC_VDEV`
- 读取其中的 vring 资源描述
- 为每个 vring 分配共享内存区域
- 必要时更新 vring 的 device address
- 最后再额外分配一块 vdev shared buffer pool
- 调 `rpmsg_virtio_init_shm_pool(&client->vdev_shpool, buf, bufsz)`

`setup_vdev()` 实际完成的是：
- vring 所需的共享内存布局准备
- RPMsg payload buffer pool 准备
- `client->vdev_shpool` 初始化

#### 动态共享内存路径
当 `client->shmem_dynamic == true` 时：
- `alloc_shmem_region(client, 0, bufsz)` 先从 MICA 共享内存池里分配 vring 内存
- `shm_pool_virt_to_phys(client, buf)` 把虚拟地址转物理地址
- 再 `remoteproc_mmap(rproc, &pa, &da, bufsz, 0, NULL)` 建立 OpenAMP 可用映射
- 对 xen 这类“双方共享内存基址不同”的场景，还会更新 `vring_offset_rsc->offset[i]`

#### 静态共享内存路径
如果 vring 的 `da` 已经固定：
- `alloc_shmem_region(client, da, bufsz)` 按指定地址恢复/占用这片共享内存
- 再 `remoteproc_mmap()` 建立映射

所以这里至少涉及三层地址概念：
- MICA 自己的共享内存池地址管理
- resource table / vring 里的 device address
- OpenAMP/libmetal 通过 `remoteproc_mmap()` 建立的可访问映射

### 7.2 `remoteproc_create_virtio()` 的位置
在 `setup_vdev()` 把内存布局准备好之后，MICA 才调用：
- `remoteproc_create_virtio(&client->rproc, 0, VIRTIO_DEV_DRIVER, NULL)`

这一步的含义是：
- 根据 remoteproc 中已有的 resource table vdev 描述
- 创建 OpenAMP 侧的 `virtio_device`

如果 resource table 不对、vring 内存没准备好、`.mmap` 或 `.notify` 不可用，这一步就可能失败。

### 7.3 `rpmsg_init_vdev()` 的三个关键输入
MICA 调用：
- `rpmsg_init_vdev(rpmsg_vdev, vdev, mica_ns_bind_cb, client->shbuf_io, &client->vdev_shpool)`

这里 3 个关键入参分别意味着：
1. `mica_ns_bind_cb`
   - RPMsg name service 到达时，交给 MICA 自己的 service 匹配逻辑
2. `client->shbuf_io`
   - RPMsg/OpenAMP 做物理/虚拟地址转换时依赖的 `metal_io_region`
3. `client->vdev_shpool`
   - RPMsg payload buffer pool

这一步完成后，`client->rdev` 才真正可用。

## 8. 第四步：debug ring buffer 是独立附加阶段

如果 `client->debug` 为真，`mica_start()` 在 RPMsg 设备创建后还会调用：
- `create_rbuf_device(client)`

这说明：
- debug ring buffer 不是 RPMsg 初始化的组成部分
- 它是 start 流程中的额外旁路能力
- 即使 RPMsg 正常，也不等于 debug ring buffer 一定正常
- 反过来，debug 能用也不代表 RPMsg service 就没问题

## 9. start 成功语义与非语义边界

### 9.1 start 成功至少代表
- `load_client_image()` 成功
- `remoteproc` 已完成 config/load 的必要阶段
- `remoteproc_start()` 成功返回
- `create_rpmsg_device()` 已成功建立 Linux/master 侧 RPMsg 设备
- 如果启用 debug，对应 rbuf 也已建立成功

### 9.2 start 成功不代表
- RTOS/client 侧服务线程已经全部创建完成
- endpoint 一定已经 ready
- name service 一定已经完成绑定
- TTY/RPC/UMT 一定已经出现在 Linux 侧
- 应用层通信一定已经可用

换句话说：
- `mica start` 成功，代表“生命周期主链 + Linux 侧 rpmsg device 基础设施”已经走通
- 但服务层是否 ready，还要继续看 `client_ctrl_handler()` 后续创建的 rpmsg service，以及 RTOS 侧 `mica_create_all_services()` / `rpmsg_create_ept()` / ready 流程

## 10. start 与 stop/remove 的对应关系

理解 start 时，最好顺便看 stop/remove 才不会误解资源边界。

### 10.1 `mica_stop()`
执行顺序：
1. `remoteproc_stop(rproc)`
2. `mica_unregister_all_services(client)`
3. `release_rpmsg_device(client)`
4. 如果 debug 打开：`destroy_rbuf_device(client)`
5. `stop_client(client)` -> `remoteproc_shutdown(&client->rproc)`

这里要注意：
- 注释写的是先 remove services，再 remove rpmsg device，再 shutdown
- 但当前实际代码顺序是先 `remoteproc_stop()`，再做服务和 rpmsg 清理，最后 `remoteproc_shutdown()`
- 所以后续如果 agent 分析 stop 语义，要以代码实际顺序为准，不要只看注释

### 10.2 `release_rpmsg_device()`
在 `library/rpmsg_device/rpmsg_vdev.c` 中：
- 先 `rpmsg_deinit_vdev(rpmsg_vdev)`
- 再 `remoteproc_remove_virtio(&client->rproc, rpmsg_vdev->vdev)`
- 最后释放 `rpmsg_vdev` 内存并清空 `client->rdev`

这说明 RPMsg device 的销毁也分两层：
- 先退 RPMsg
- 再退 virtio

### 10.3 `mica_remove()`
- 如果 `rproc->state != RPROC_OFFLINE`，先 `mica_stop(client)`
- 如果有 gdb server thread，先 cancel + join
- 最后 `destory_client(client)` -> `remoteproc_remove(&client->rproc)`

所以 remove 是在 stop 之上的更高层清理，不应与 stop 混为一谈。

## 11. 适合在哪些位置下断点/加日志

如果要分析 `mica start`，比较有价值的观察点通常是：

### 11.1 生命周期主入口
- `library/mica/mica.c: mica_start()`

### 11.2 load 阶段
- `library/remoteproc/remoteproc_core.c: load_client_image()`
- `remoteproc_config()` 的 pedestal-specific 实现
- `remoteproc_load()` 是否进入、是否返回错误
- `rproc->rsc_table` 是否为空

### 11.3 start 阶段
- `library/remoteproc/remoteproc_core.c: start_client()`
- 对应 pedestal 的 `.start`

### 11.4 RPMsg 建立阶段
- `library/rpmsg_device/rpmsg_vdev.c: setup_vdev()`
- `remoteproc_create_virtio()`
- `rpmsg_init_vdev()`
- `client->rdev` 是否最终非空

### 11.5 start 后服务层
如果 `mica_start()` 成功但服务没出来，应该继续跳到：
- `mica/micad/socket_listener.c: client_ctrl_handler()`
- `create_rpmsg_tty()`
- `create_rpmsg_rpc_service()`
- `create_rpmsg_umt_service()`
- RTOS 侧 `mica_create_all_services()`

## 12. 延伸阅读路径

理解完 `mica start` 主链后，通常要继续转读：
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
  - 理解 `remoteproc_create_virtio`、`rpmsg_init_vdev`、`.mmap/.notify` 的接合点
- `mica-communication/references/communication-overview.md`
  - 理解 start 后 TTY/RPC/UMT 是怎么真正建立服务的
- `mica-common-tasks/references/debugging-workflow/lifecycle-diagnosis.md`
  - 用于把 start 失败按阶段归因
