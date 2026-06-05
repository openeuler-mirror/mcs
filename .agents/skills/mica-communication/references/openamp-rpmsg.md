# RPMsg 模型

## 1. 文档目标

这篇文档专门解释 RPMsg 在 MICA 中是如何真正落地的。

它主要回答：
- `create_rpmsg_device()` 之后，MICA 如何把 virtio 设备推进成可用的 RPMsg 设备
- Linux/master 侧怎样把远端 name service、endpoint 和本地 `mica_service` 模型接起来
- RTOS/client 侧怎样创建 TTY / RPC / UMT endpoint，并把 ready 状态暴露出来
- 为什么“实例 running”不等于“RPMsg 服务 ready”
- `rpmsg_hold_rx_buffer()` / `rpmsg_release_rx_buffer()` / `rpmsg_send()` 这些机制在本仓里是怎样被实际使用的

如果你已经确认问题不再只是 `remoteproc` 运行态，而是进入 endpoint、name service、service binding 或消息收发层，应该先读这篇。

## 2. RPMsg 分层模型

在当前仓里，RPMsg 不是单独的一条“消息 API”。

更准确地说，它至少分成两层：
- 机制层：virtio -> RPMsg device -> endpoint -> name service -> buffer 生命周期
- 服务层：TTY / RPC / UMT 这些具体服务怎样声明、绑定、ready、收发

在 MICA 里的代码落点大致是：

- Linux/master 侧 RPMsg 机制接缝
  - `library/rpmsg_device/rpmsg_vdev.c`
  - `library/rpmsg_device/rpmsg_service.c`

- Linux/master 侧具体服务实现
  - `mica/micad/services/pty/rpmsg_pty.c`
  - `mica/micad/services/rpc/rpmsg_rpc.c`
  - `mica/micad/services/umt/rpmsg_umt.c`

- RTOS/client 侧服务实现
  - `rtos/libmica/src/mica_service.c`
  - `rtos/libmica/src/services/tty_service.c`
  - `rtos/libmica/src/services/rpc_service.c`
  - `rtos/libmica/src/services/umt_service.c`

所以理解 RPMsg 时，不要把“device 建起来了”和“服务 ready 了”混成一件事。

## 3. Linux/master 侧 RPMsg 设备建立流程

### 3.1 `create_rpmsg_device()`：RPMsg 的公共入口

Linux/master 侧 RPMsg 设备建立入口在：
- `library/rpmsg_device/rpmsg_vdev.c:create_rpmsg_device()`

这条函数的顺序是：
1. 分配 `struct rpmsg_virtio_device`
2. 调 `setup_vdev(client)`
3. 调 `remoteproc_create_virtio(&client->rproc, 0, VIRTIO_DEV_DRIVER, NULL)`
4. 调 `rpmsg_init_vdev(rpmsg_vdev, vdev, mica_ns_bind_cb, client->shbuf_io, &client->vdev_shpool)`
5. `client->rdev = rpmsg_virtio_get_rpmsg_device(rpmsg_vdev)`

这说明 RPMsg 在 MICA 中并不是凭空出现的，而是建立在：
- `resource_table` 已就绪
- vring 已分配
- virtio 设备已创建
- `rpmsg_init_vdev()` 已成功

如果这里失败，问题往往已经不是 lifecycle 表层，而是：
- `RSC_VDEV` 是否可用
- vring 是否正确
- `client->shbuf_io` 是否有效
- RPMsg 设备初始化是否真正完成

### 3.2 `rpmsg_init_vdev()` 的关键作用

`rpmsg_init_vdev()` 在当前仓里的三个关键输入分别是：
- `mica_ns_bind_cb`
  - 把远端 name service 事件重新接回 MICA
- `client->shbuf_io`
  - 给 RPMsg 数据面提供 libmetal I/O 语义
- `client->vdev_shpool`
  - 给 payload buffer 提供共享内存池

这意味着：
- RPMsg 不是只靠 endpoint 名称工作
- 它要同时依赖回调、地址语义、共享 buffer pool

所以如果 `rpmsg_init_vdev()` 没成功：
- 后面不会有 `client->rdev`
- 也不会有真正可消费的 name service / endpoint 事件

### 3.3 OpenAMP 对象展开路径

如果继续往 `open-amp` 仓里的实现看，`create_rpmsg_device()` 后面不是一个抽象黑盒，而是比较明确的对象链：

1. `remoteproc_create_virtio()`
   - `open-amp/lib/remoteproc/remoteproc.c`
   - 从 `rproc->rsc_table` 找 `RSC_VDEV`
   - 检查同一个 `notifyid` 的 virtio device 是否已经存在
   - 调 `rproc_virtio_create_vdev(...)`
   - 再对 `vdev_rsc->vring[i]` 逐个调 `remoteproc_mmap()` 和 `rproc_virtio_init_vring()`

2. `rproc_virtio_create_vdev()`
   - `open-amp/lib/remoteproc/remoteproc_virtio.c`
   - 分配 `struct remoteproc_virtio`
   - 在里面内嵌 `struct virtio_device`
   - 把 `rproc` 作为 `priv` 传给 `remoteproc_virtio_notify()`，这样后续 virtqueue kick 才能重新回到 `remoteproc_ops->notify`

3. `rpmsg_init_vdev()` / `rpmsg_init_vdev_with_config()`
   - `open-amp/lib/rpmsg/rpmsg_virtio.c`
   - 把 `struct rpmsg_device` 清零并挂进 `struct rpmsg_virtio_device`
   - 安装 `send_offchannel_raw` / `hold_rx_buffer` / `release_rx_buffer` / `get_tx_payload_buffer` 等 ops
   - 调 `rpmsg_virtio_create_virtqueues()` 真正建立 RPMsg 使用的两个 virtqueue
   - 如果协商到 `VIRTIO_RPMSG_F_NS`，再注册 name service endpoint `rdev->ns_ept`

4. `rpmsg_virtio_get_rpmsg_device()`
   - 只是返回 `&rvdev->rdev`
   - 所以 MICA 里保存到 `client->rdev` 的，不是新的包装对象，而是 OpenAMP `struct rpmsg_virtio_device` 内嵌的 `struct rpmsg_device`

这条链说明了一个很重要的边界：
- MICA 负责把 `rproc`、`RSC_VDEV`、共享内存池、name service 回调入口准备好
- OpenAMP 负责把这些输入真正落成 `remoteproc_virtio -> virtio_device -> rpmsg_virtio_device -> rpmsg_device`
- 后续 endpoint 收发、buffer 回收、NS 处理，大部分关键语义都已经在 OpenAMP 里了

## 4. Linux/master 侧服务绑定流程

### 4.1 `mica_ns_bind_cb()`：RPMsg 与 `mica_service` 的关键接缝

`library/rpmsg_device/rpmsg_service.c:mica_ns_bind_cb()` 是 RPMsg 服务发现进入 MICA 的总入口。

它的关键步骤是：
1. 由 `rdev` 反查 `rpmsg_virtio_device`
2. 再反查 `remoteproc_virtio`
3. 再反查 `remoteproc`
4. 再反查 `mica_client`
5. 遍历 `client->services`
6. 对每个 service 调：
   - `svc->rpmsg_ns_match(...)`
   - 如果命中，再调 `svc->rpmsg_ns_bind_cb(...)`
7. 成功绑定后，再调 `rsc_update_ept_table(rproc, rdev)`

也就是说，RPMsg name service 到达之后，OpenAMP 只负责把“远端 endpoint 出现了”这件事告诉上层；真正决定它属于哪类服务、怎样绑定成本地对象的，是 MICA 自己的 service 模型。

### 4.2 OpenAMP NS 语义与 `mica_ns_bind_cb()` 的接缝

如果往 OpenAMP 里继续看，name service 的真实路径是：

1. `rpmsg_init_vdev_with_config()`
   - 如果协商到 `VIRTIO_RPMSG_F_NS`
   - 注册固定地址的 `rdev->ns_ept`
   - callback 是 `rpmsg_virtio_ns_callback()`

2. 对端创建 endpoint 时，`rpmsg_create_ept()`
   - `open-amp/lib/rpmsg/rpmsg.c`
   - 如果 endpoint 有名字，且 `support_ns == true`，并且 `dest_addr == RPMSG_ADDR_ANY`
   - 会自动发 `rpmsg_send_ns_message(ept, RPMSG_NS_CREATE)`

3. 本端收到 NS 消息后，`rpmsg_virtio_ns_callback()`
   - 先按 `name + dest` 去找本地是否已经有匹配 endpoint
   - 如果没有，就调 `rdev->ns_bind_cb(rdev, name, dest)`
   - 在 MICA 这里，`rdev->ns_bind_cb` 正是 `mica_ns_bind_cb`

所以从职责上看：
- OpenAMP 的 NS 负责“发现远端有一个 named endpoint 出现/销毁了”
- MICA 的 `mica_ns_bind_cb()` 负责“把这个 discovered endpoint 解释成 TTY / RPC / UMT 哪一类本地 service”

这也是为什么 `mica_ns_bind_cb()` 没被调用时，问题通常还在更下层：
- 要么 `support_ns` 根本没建立起来
- 要么对端 `rpmsg_create_ept()` 没成功或没发出 NS
- 要么 RX 通路没把 NS 报文送到 `rpmsg_virtio_ns_callback()`

### 4.3 `remote_ept_list`：解决“先远端出现，后本地注册”的时序问题

`rpmsg_service.c` 里还有一个很重要的结构：
- `remote_ept_list`

它解决的问题是：
- 远端 endpoint / name service 可能先出现
- 但本地 `mica_service` 还没注册好

这时 `mica_ns_bind_cb()` 不会直接丢弃这个远端 endpoint，而是：
- 分配 `struct remote_ept`
- 把 name / addr / dest_addr 记到 `remote_ept_list`
- 等后续本地 service 再注册时补绑定

对应的补绑定逻辑在：
- `mica_register_service()`

`mica_register_service()` 会遍历 `remote_ept_list`，对每个已有远端 endpoint 再跑一次：
- `rpmsg_ns_match()`
- `rpmsg_ns_bind_cb()`
- `rsc_update_ept_table()`

这说明 MICA 允许两种路径：
- 先注册本地 service，再等远端 endpoint 出现
- 先出现远端 endpoint，再等本地 service 注册

因此很多“服务一开始没出现、稍后又好了”的现象，本质上是时序问题，而不一定是逻辑错误。

## 4.4 RPMsg 服务绑定主链

在当前系统里，`name -> port -> service object` 的建立不是单个函数一次完成的，而是由 OpenAMP 和 MICA service 模型共同完成的一条绑定主链。

### 4.4.1 RTOS/client 侧先创建本地 endpoint

RTOS/client 侧在自己的 vdev 和 RPMsg device 初始化完成后，会由具体服务线程调用：
- `rpmsg_create_ept(..., "rpmsg-tty", RPMSG_ADDR_ANY, RPMSG_ADDR_ANY, ...)`
- `rpmsg_create_ept(..., "rpmsg-rpc", RPMSG_ADDR_ANY, RPMSG_ADDR_ANY, ...)`
- `rpmsg_create_ept(..., "rpmsg-umt", RPMSG_ADDR_ANY, RPMSG_ADDR_ANY, ...)`

这里 OpenAMP 会：
1. 为本地 endpoint 分配或确认 `src`
2. 把 endpoint 注册进 `rdev->endpoints`
3. 如果设备支持 NS 且 `dest_addr == RPMSG_ADDR_ANY`，自动发送 `RPMSG_NS_CREATE`

也就是说，RTOS/client 侧首先建立的是：
- 服务名 `name`
- 本地端口 `src`
- 本地接收回调 `cb`

此时通常还没有完成对端端口绑定。

### 4.4.2 Linux/master 侧先进入 MICA service 选择阶段

Linux/master 侧 `rpmsg_init_vdev()` 时已经把：
- `rdev->ns_bind_cb = mica_ns_bind_cb`

挂到了 OpenAMP device 上。

因此当 RTOS/client 侧发出 `RPMSG_NS_CREATE` 后，Linux/master 侧会进入：
1. `rpmsg_virtio_ns_callback()`
2. 如果本地没有现成 endpoint 命中，就调 `mica_ns_bind_cb(rdev, name, dest)`
3. `mica_ns_bind_cb()` 再遍历 `client->services`
4. 对每个服务调 `svc->rpmsg_ns_match(...)`

这一阶段的关键作用是：
- 用远端 `name`
- 选择由哪个 MICA service 来接这个远端 endpoint

所以 `rpmsg_ns_match` 的职责不是“发送握手包”，而是：
- 把远端服务名归属到本地服务类型

### 4.4.3 服务命中后再进入服务自己的 bind callback

一旦 `rpmsg_ns_match(...)` 命中，MICA 就会调用该服务自己的：
- `svc->rpmsg_ns_bind_cb(...)`

这一步才真正把“远端 name + 远端 port”落成本地服务对象。

当前仓里的典型模式包括：
- TTY：进入 `rpmsg_tty_init()`
- RPC：进入 `rpc_service_init()`
- UMT：进入 `umt_service_init()`

这些回调通常会继续做两类事：
1. 在 Linux/master 侧创建本地 endpoint
2. 初始化该服务自己的运行时对象

因此从绑定语义上看，RPMsg 服务真正接住远端，不是停在 NS 到达，而是停在：
- `rpmsg_ns_match()` 已选中服务
- 该服务自己的 `rpmsg_ns_bind_cb()` 已成功完成本地对象建立

### 4.4.4 `name` 到 `port` 的匹配落地方式

这条链里最容易被误解的一点，是大家容易把“看到 name”直接当成“端口已经匹配完成”。

更准确的说法是：
- NS 提供了远端 `name`
- NS 也带来了远端 endpoint 的地址信息
- MICA 用 `rpmsg_ns_match()` 决定这个 `name` 应该归哪个服务
- 具体服务再在 `rpmsg_ns_bind_cb()` 里创建本地 endpoint，并把本地/远端端口关系落地到自己的 endpoint 对象中

所以 `name -> port` 的匹配不是只有 OpenAMP 自己完成的；它是：
- OpenAMP 提供远端 namespace 事件
- MICA service 层把 namespace 事件解释成本地服务绑定

### 4.4.5 first message 属于服务层激活，不属于 NS 基础语义

当前仓里有些服务在 bind 完成本地 endpoint 后，还会主动发送一条 first message。

例如：
- Linux/master 侧 RPC 在 `rpc_service_init()` 里会发 `"first message from rpc_service!"`
- Linux/master 侧 UMT 在 `umt_service_init()` 里也会发初始化消息
- TTY 则更多依赖用户输入和 shell 回显形成首次往返

这里要特别注意：
- first message 对某些服务很重要
- 但它不等于 OpenAMP RPMsg 规范层必需的第二次握手

更准确地说，它的作用通常是：
- 服务层激活
- 业务链探活
- 让对端尽快形成第一轮实际收发

所以当你分析服务“牵手”是否成功时，要区分两层：
1. NS / endpoint bind 是否成立
2. 服务自己的 first message / 首轮业务收发是否成立

## 5. RTOS/client 侧服务建立摘要

Linux/master 侧 `client->rdev` 可用之后，RTOS/client 侧还要继续把本地 endpoint 和服务线程建立起来。

### 5.1 `mica_create_all_services()` 服务启动入口

`rtos/libmica/src/mica_service.c:mica_create_all_services()` 做了三件关键事：
1. 启动 TTY 线程
2. 启动 receiver thread
3. 启动 UMT 线程
4. 最后等待 `MICA_SERVICE_UMT` ready

其中 receiver thread 会持续调用：
- `ped_ops->rcv_message()`

也就是说，RTOS/client 侧并不是靠某个服务自己轮询 RPMsg，而是：
- pedestal 负责把消息从底层通知链取上来
- receiver thread 把它送入 OpenAMP / virtio 通路
- 具体服务线程再消费 endpoint 级别的数据

### 5.2 TTY 服务摘要

TTY 侧的 RPMsg 特征是：
- RTOS/client 侧先创建 `rpmsg-tty` endpoint
- 收包后走 hold -> 线程处理 -> release 的异步路径
- Linux/master 侧在 bind 成功后创建 PTY、设备节点和发送线程

TTY 的详细运行时链路见：
- `services/tty-service.md`

### 5.3 RPC 服务摘要

RPC 侧的 RPMsg 特征是：
- RTOS/client 侧创建 `rpmsg-rpc` endpoint 后等待 endpoint ready
- Linux/master 侧 bind 成功后创建 server endpoint
- 某些实现路径会在 bind 后主动发送 first message 作为服务层激活信号

RPC 的详细服务链路见：
- `services/rpc-service.md`

### 5.4 UMT 服务摘要

UMT 侧的 RPMsg 特征是：
- RTOS/client 侧创建 `rpmsg-umt` endpoint
- RPMsg 主要承载共享内存描述符，而不是业务 payload 本体
- Linux/master 侧 bind 成功后还要继续初始化共享内存、同步对象和发送线程

UMT 的详细服务链路见：
- `services/umt-service.md`

## 6. RPMsg ready 的语义边界

在当前仓里，`is_rpmsg_ept_ready()` 是多个服务 ready 的直接判据，例如：
- `mica_tty_is_ready()`
- `mica_rpc_is_ready()`
- `mica_umt_is_ready()`

但这里要非常小心：
- endpoint ready 只说明 endpoint 对象已经建立到可用状态
- 不自动说明整个业务链条已经完全可用

例如：
- RPC ready 了，不代表对端 handler 逻辑一定正常
- UMT ready 了，不代表共享内存里数据路径一定无误
- TTY ready 了，也不代表 shell 处理逻辑一定正确

所以 RPMsg ready 更像：
- 通信机制层已经基本建立
- 但业务层是否通畅还要继续看具体服务实现

### 6.1 OpenAMP 中 `ready` 的最小语义

`is_rpmsg_ept_ready()` 在 OpenAMP 头文件里的定义其实非常直接：
- `ept != NULL`
- `ept->rdev != NULL`
- `ept->dest_addr != RPMSG_ADDR_ANY`

也就是说，在 OpenAMP 语义里，endpoint ready 的最低门槛不是“业务正常”，而只是：
- 这个本地 endpoint 已注册
- 它已经知道默认对端地址

而这个 `dest_addr` 一般有两种来源：

1. NS 绑定路径
   - `rpmsg_virtio_ns_callback()` 找到已注册 endpoint 后
   - 直接把 `_ept->dest_addr = dest`

2. 首包学习路径
   - `rpmsg_virtio_rx_callback()` 收到某个 endpoint 的第一条消息时
   - 如果 `ept->dest_addr == RPMSG_ADDR_ANY`
   - 就把它更新成 `rp_hdr->src`

这说明一个经常被误判的点：
- “endpoint ready” 在 OpenAMP 层只是地址绑定闭环成立
- 它并不代表上层线程、协议格式、共享内存数据面都已经可用

### 6.2 `DRIVER_OK` 和 service ready 也不是一回事

OpenAMP 里还有一个更底层的 ready 概念：virtio status `VIRTIO_CONFIG_STATUS_DRIVER_OK`。

相关代码点：
- `open-amp/lib/remoteproc/remoteproc_virtio.c:rproc_virtio_wait_remote_ready()`
- `open-amp/lib/rpmsg/rpmsg_virtio.c:rpmsg_init_vdev_with_config()`

当前链路里的含义是：
- Linux/master 侧作为 `RPMSG_HOST` / `VIRTIO_DEV_DRIVER`
- 在 `rpmsg_init_vdev_with_config()` 末尾会调 `rpmsg_virtio_set_status(..., DRIVER_OK)`
- RTOS/client 侧作为 `RPMSG_REMOTE` / `VIRTIO_DEV_DEVICE`
- 会在 `rpmsg_init_vdev_with_config()` 早期阻塞等待 host 侧 `DRIVER_OK`

所以：
- `DRIVER_OK` 说明 virtio/RPMsg 基础握手到位
- `is_rpmsg_ept_ready()` 说明某个 endpoint 已完成默认对端地址绑定
- service ready 还要再叠加 MICA 自己的 service 线程、endpoint 创建、共享内存数据面等条件

## 7. `rpmsg_send()` 在本仓里承担哪几类角色

当前仓里 `rpmsg_send()` 至少承担三类不同语义：

1. 普通小消息数据发送
- 例如 TTY 文本发送
- 例如 RPC 响应发送

2. 共享内存描述符发送
- 例如 UMT 发送 `phy_addr + data_len`
- 真正 payload 不在 RPMsg 消息体里

3. 服务握手/控制性通知
- 某些服务建立时 first message 本身就承担了控制意义

因此当你看到 `rpmsg_send()` 成功时，不能立即推断：
- 业务数据已经真正被对端正确消费

从 OpenAMP 实现看，这里至少还隐含两层前提：
- `rpmsg_send()` 最终会走到 `rpmsg_virtio_send_offchannel_raw()`，通过 `metal_io_block_write()` 把 payload/header 写到 `shbuf_io` 指向的共享内存
- 随后通过 `virtqueue_add_buffer()` / `virtqueue_kick()` 把 buffer 挂到发送队列并通知对端

所以 `rpmsg_send()` 成功，只能说明：
- 本端拿到了 TX buffer
- header/payload 已写入共享内存
- buffer 已入队并 kick

### 7.1 OpenAMP 发送主链

如果继续往 OpenAMP 实现里展开，`rpmsg_send()` 的主链是：
1. `rpmsg_send()`
2. `rpmsg_send_offchannel_raw(ept, ept->addr, ept->dest_addr, ...)`
3. `rpmsg_virtio_get_tx_payload_buffer()`
4. `metal_io_block_write()` 把 payload 写进共享内存
5. `rpmsg_virtio_send_offchannel_nocopy()`
6. 再通过 `metal_io_block_write()` 写 RPMsg header
7. `rpmsg_virtio_enqueue_buffer()`
8. `virtqueue_add_buffer()` 或 `virtqueue_add_consumed_buffer()`
9. `virtqueue_kick(rvdev->svq)`

这条链的意义是：
- `rpmsg_send()` 的成功边界在“本端共享内存写入 + 本端 virtqueue 入队 + kick 已发出”
- 它不包含“对端一定已经完成处理”这个语义

所以 send 问题如果要继续下钻，最值得问的是：
- TX buffer 是不是本来就没拿到
- header/payload 写入是不是依赖了错误的 `shm_io`
- `virtqueue_kick()` 之后 `.notify` / 中断 / 对端回调链有没有真正闭合

它并不自动说明：
- 对端已经消费了这个 buffer
- 对端 callback 已经执行成功
- 对端对共享内存 payload 的解释没有偏差

还要继续看：
- 对端 endpoint 是否存在
- 对端 callback 是否正确
- 如果是 UMT，这个 `phy_addr` 所指向的共享内存是否真的有效

## 7.2 `hold_rx_buffer()` / `release_rx_buffer()` 的关键作用

这一点在当前 skill 里原来只说了“要成对使用”，但 OpenAMP 的真实语义值得明确写出来。

`rpmsg_virtio_rx_callback()` 的处理顺序是：
1. 从 `rvq` 取出一个 RX buffer
2. 把 descriptor index 塞进 `rp_hdr->reserved`
3. 调 endpoint callback
4. 如果 callback 没把 `RPMSG_BUF_HELD` 位置起来，就立刻把 buffer 归还给 virtqueue

而：
- `rpmsg_hold_rx_buffer()` 只是把 `rp_hdr->reserved |= RPMSG_BUF_HELD`
- `rpmsg_release_rx_buffer()` 才会根据 `reserved` 里保存的 descriptor index 把 buffer 真正放回 `rvq`，并 `virtqueue_kick(rvdev->rvq)` 通知对端

所以对 MICA 来说：
- TTY 那种“callback 里只缓存指针，真正处理在线程里”的模型，必须先 hold 再 release
- 否则 callback 返回后，OpenAMP 会把 RX buffer 立刻回收到 vring，中间层保存的数据指针就可能失效
- 反过来，如果 hold 了但不 release，对端可复用的 RX buffer 数量就会持续减少，最终表现成后续收包异常或卡住

## 7.3 `rpmsg_virtio_rx_callback()` 到 endpoint callback 的精确路径

OpenAMP 的接收主链也值得单独记住，因为很多“数据到了但服务没反应”的问题就卡在这里。

它的顺序是：
1. `rpmsg_virtio_rx_callback(vq)`
2. `rpmsg_virtio_get_rx_buffer(rvdev, &len, &idx)`
3. 把当前 descriptor index 写进 `rp_hdr->reserved`
4. `rpmsg_get_ept_from_addr(rdev, rp_hdr->dst)`
5. 找到本地 endpoint 后，必要时先学习 `ept->dest_addr = rp_hdr->src`
6. 调 `ept->cb(ept, RPMSG_LOCATE_DATA(rp_hdr), rp_hdr->len, rp_hdr->src, ept->priv)`
7. callback 返回后，再决定是立即回收 RX buffer，还是保留给上层稍后 release

这说明如果 symptom 是：
- 对端已经发包
- 但本地服务 callback 没进

就应该优先检查：
- `rp_hdr->dst` 是否真能匹配到本地 endpoint
- endpoint 是否已经挂进 `rdev->endpoints`
- 是否其实卡在 NS/ready 之前，本地还没形成正确的 `dest_addr` 绑定

## 8. RPMsg 与 `communication-overview.md` 的边界

这篇 `openamp-rpmsg.md` 关注的是 RPMsg 机制本体，主要回答：
- device / endpoint / name service / buffer 生命周期 / ready 语义

而 `communication-overview.md` 更偏：
- MICA 当前有哪些服务
- 每类服务如何通过 `mica_service` 模型注册和绑定
- service-ready 问题怎么排查

你可以把两者关系理解成：
- 本文 = RPMsg 机制层模型
- `communication-overview.md` = MICA 通信分层与服务层落地

如果问题是：
- endpoint 怎么绑定
- hold/release buffer 为什么这么做
- `remote_ept_list` 为什么存在

优先看本文。

如果问题是：
- 为什么 UMT 没 ready
- 为什么 RPC / TTY 没显示出来
- Linux/master 和 RTOS/client 服务时序哪里错位

再继续看 `communication-overview.md`。

## 9. 调试 RPMsg 时最有价值的观察点

### 9.1 `client->rdev` 建立确认点
如果 `create_rpmsg_device()` 都没成功，后面的 endpoint/service 都不用看。

### 9.2 `mica_ns_bind_cb()` 接收远端 name service 的确认点
如果收不到：
- 问题可能还在 virtio / RPMsg device 层
- 也可能是对端 endpoint 根本没创建

### 9.3 `rpmsg_ns_match()` / `rpmsg_ns_bind_cb()` 命中确认点
如果 name service 到了但没绑定成本地服务，问题通常在匹配和绑定规则。

### 9.4 RTOS/client 侧 `rpmsg_create_ept()` 成功确认点
如果对端 endpoint 没建起来，Linux/master 再怎么等也等不到 ready。

### 9.5 hold/release / send 路径配对确认点
尤其是：
- TTY 的 hold -> thread process -> release
- UMT 的 callback 模式与 passive pull 模式
- 共享内存描述符和真实 payload 是否一致

## 10. 建议继续阅读

理解完这篇后，通常继续转读：
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `communication-overview.md`
- `../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `../../mica-lifecycle/references/lifecycle-start.md`
