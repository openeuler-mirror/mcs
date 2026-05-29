# UMT 服务说明

## 1. 文档目标

这篇文档专门解释 UMT 服务在当前 MICA 通信链中的接入方式，以及为什么它虽然建立在 RPMsg 上，但真正承担数据传输职责的核心仍然是共享内存数据面。

它主要回答：
- Linux/master 侧 `create_rpmsg_umt_service()` 到 `umt_service_init()` 具体做了什么
- RTOS/client 侧 `rpmsg-umt` endpoint 是怎样创建和进入 ready 的
- callback 模式与 passive pull 模式有什么区别
- 用户态 `library/user_msg`、`GET_COPY_MSG_MEM`、测试用例和底层 UMT service 是怎样串起来的
- UMT ready 的真实边界在哪里

## 2. UMT 服务总体模型

UMT 不是单纯的一个 RPMsg endpoint。

在当前系统里，它至少跨三层才能真正可用：

1. OpenAMP/RPMsg 机制层
   - `rpmsg-umt` endpoint 已建立
   - 双边 `dest_addr` 已绑定

2. MICA service 层
   - Linux/master 侧 `umt_name_match()` 命中远端服务名
   - `umt_service_init()` 成功创建本地 UMT service 对象

3. Linux/master / RTOS/client 运行时层
   - Linux/master 侧共享内存、同步对象、发送线程和 endpoint 可用
   - RTOS/client 侧接收线程、callback 模式或 passive pull 模式可用

因此看到“endpoint ready 了但 UMT 仍不可用”时，不要把问题只归到 RPMsg 机制层。

### 2.1 UMT 服务的观测价值

UMT 没有像 TTY 那样直接面向用户的终端设备节点，因此它不是最直观的第一通信观测窗。

但它在系统层面的观测价值很高，因为：
- `mica_create_all_services()` 会等待 UMT ready
- UMT 经常是 client 侧服务阶段是否真正完成的重要门槛

因此如果：
- `mica start` 成功
- 但 client 侧服务阶段迟迟没有进入稳定可用态

UMT 往往是最值得优先检查的服务之一。

### 2.2 UMT 服务的工作原理

UMT 的核心语义不是“把业务数据直接塞进 RPMsg 消息体”，而是：
- 用 RPMsg 传控制面描述符
- 用共享内存承载真正的数据面 payload

当前代码里的典型描述符是：
- `umt_data_t { phy_addr, data_len }`

这意味着：
- RPMsg 负责告诉对端“数据在共享内存哪里、长度是多少”
- 真正的大块数据由双方按共享内存语义去访问

所以 UMT 的问题经常不是单纯的 endpoint 或 NS 问题，而是：
- endpoint 建立了
- 但共享内存数据面没有闭合

### 2.3 用户态库接口与测试路径

UMT 在当前仓里不只是 micad 与 RTOS/client 的内部服务，还通过 `library/user_msg/` 暴露了一套面向 Linux 用户态的封装接口。

这层的关键文件包括：
- `library/user_msg/user_msg.c`
- `library/user_msg/user_msg_mem.c`
- `library/include/user_msg/user_msg.h`

当前用户态常见使用方式包括：
- `umt_context_create()`
- `send_data_with_umt_context()`
- `receive_data_with_umt_context()`
- `umt_register_rcv_cb()`
- `send_data_to_rtos()`
- `send_data_to_rtos_and_wait_rcv_ped()`

这意味着在当前系统里，UMT 的典型业务模型不只是“micad 内部 service 存在”，还包括：
- 用户态程序通过 `library/user_msg` 提供的接口向 RTOS 发送数据
- RTOS 收到数据后再回发
- 用户态再通过同一套接口收到回复并打印

仓内对应的典型测试路径包括：
- `test/test_umt/`
- `test/test_shared_lib/send-data/`
- `test/test_shared_lib/send-data-2-way/`

因此从用户视角看，`send-data` 或 `send-data-2-way` 这类程序如果能够完成“发出去 -> RTOS 回回来 -> 用户态打印回复”，就已经证明 UMT 至少完成了一次基本往返通信。

## 3. Linux/master 侧 UMT 服务声明与绑定

### 3.1 `create_rpmsg_umt_service()` 服务注册入口

Linux/master 侧入口在：
- `mica/micad/services/umt/rpmsg_umt.c:create_rpmsg_umt_service()`

它本身只是：
- 调 `mica_register_service(client, &rpmsg_umt_service)`

而 `rpmsg_umt_service` 这个声明里包含：
- `.name = "rpmsg-umt"`
- `.rpmsg_ns_match = umt_name_match`
- `.rpmsg_ns_bind_cb = umt_service_init`
- `.remove = remove_umt_service`

这说明 UMT 在 MICA service 模型里不是“直接建 endpoint”，而是先声明：
- 什么远端服务名属于 UMT
- 命中之后如何创建本地对象
- 移除时如何统一清理

### 3.2 `umt_name_match()` 服务名匹配规则

`umt_name_match()` 的语义比较直接：
- 只匹配 `rpmsg-umt`

因此 UMT 的服务命中逻辑不像 TTY 那样支持通配式前缀，而是更接近固定服务名匹配。

## 4. Linux/master 侧 UMT bind 后的本地对象建立

### 4.1 `umt_service_init()` 本地对象建立流程

当远端 name service 被 `umt_name_match()` 命中后，Linux/master 侧会进入：
- `umt_service_init()`

这一步才是真正把 UMT 服务落地成本地运行时对象的地方。

从当前文件结构看，它至少负责：
- 分配 `struct rpmsg_umt_service`
- 创建并注册本地 `rpmsg-umt` endpoint
- 把 service 实例挂入 `g_umt_list`
- 初始化共享内存与进程间同步对象
- 创建发送线程 `rpmsg_umt_tx_task()`

因此如果 symptom 是：
- `mica_ns_bind_cb()` 已经命中 UMT
- 但 Linux/master 侧 UMT 仍不可用

优先看 `umt_service_init()` 里面哪一步失败了，而不是先回头怀疑 RPMsg 机制本体。

### 4.2 first message 的作用

UMT 在 bind 成功后通常还会发出一条初始化消息。

当前代码里分两种路径：
- `BARE_METAL` / `HETERO`
  - 通过 `send_data_to_rtos(...)` 通知 client 侧
- `JAILHOUSE` / `XEN`
  - 直接通过 `rpmsg_send(&umt_svc->ept, &msg, sizeof(msg))`

这条消息的作用不是“传完整业务数据”，而是：
- 推进服务层激活
- 让对端尽快开始准备 UMT 数据面

因此 first message 对 UMT 的意义通常比普通小消息更偏“控制面初始化”。

### 4.3 `GET_COPY_MSG_MEM` 与 copy-message 共享区

UMT 在 Linux/master 侧还依赖一块由内核侧规划的 copy-message 共享内存。

用户态库里，`library/user_msg/user_msg_mem.c` 会通过：
- `IOC_GET_COPY_MSG_MEM`

向 `mcs_km.ko` 查询这块内存的信息。

对应的 ioctl 常量和处理路径可以在：
- `library/user_msg/user_msg_mem.c`
- `mcs_km/mcs_km.c`

找到。

这块 copy-message 共享区的意义是：
- 给 Linux 用户态提供一块双方都认可的数据缓冲区
- 让 UMT 可以用“共享内存承载 payload + RPMsg 承载描述符”的方式工作

因此对当前仓里的 UMT 而言，`GET_COPY_MSG_MEM` 不是旁支细节，而是用户态库接口能够成立的重要前提之一。

### 4.4 `BARE_METAL` / `HETERO` 的 copy-message 地址通知路径

`umt_service_init()` 里的 first message 路径存在一个容易误读的差异：

- `BARE_METAL` / `HETERO`
  - 通过 `send_data_to_rtos(...)` 通知 client 侧
- `JAILHOUSE` / `XEN`
  - 直接通过 `rpmsg_send(&umt_svc->ept, &msg, sizeof(msg))`

从当前代码关系看，这个差异至少反映了一个事实：
- 对 `BARE_METAL` / `HETERO` 而言，Linux/master 侧需要显式把 copy-message 共享区相关信息告诉 RTOS/client
- 否则 RTOS 侧无法仅靠普通 RPMsg service 建立阶段就自然知道 Linux 规划的 copy-message 地址

而当前仓里的用户态 UMT 库与 `GET_COPY_MSG_MEM` 这条能力，正是建立在：
- `mcs_km.ko` 支持查询 copy-message 共享区

这也是为什么当前代码只对：
- `BARE_METAL`
- `HETERO`

使用这条显式通知路径更有意义。

对于 `JAILHOUSE` / `XEN`，当前代码路径则更接近：
- 只把一条 RPMsg 初始化消息送给对端
- 但并没有体现出与 `GET_COPY_MSG_MEM` 同一套能力绑定的显式 copy-message 地址告知流程

因此如果要描述当前支持边界，更稳妥的说法是：
- 当前仓里的 copy-message 共享区用户态接口能力，主要与 `mcs_km.ko` 提供的 `GET_COPY_MSG_MEM` 路径相关
- 这条路径当前与 `BARE_METAL` / `HETERO` 的结合最直接
- `JAILHOUSE` / `XEN` 并未在当前代码里体现出同等完整的 copy-message 地址协商路径

## 5. RTOS/client 侧 `rpmsg-umt` 建立流程

### 5.1 `umt_service_thread()` RTOS/client 侧入口

RTOS/client 侧关键入口在：
- `rtos/libmica/src/services/umt_service.c:umt_service_thread()`

它的顺序很清楚：
1. 取 `mica_get_rpdev()`
2. 初始化 `rx_sem` 和 `passive_rcv_sem`
3. 设置 `g_umt_ept.priv = &g_umt_priv`
4. 调 `rpmsg_create_ept(&g_umt_ept, rpdev, "rpmsg-umt", RPMSG_ADDR_ANY, RPMSG_ADDR_ANY, umt_rx_callback, NULL)`
5. 进入主循环等待 UMT 描述符消息

这说明 RTOS/client 侧 UMT ready 的最小前提是：
- `rpdev` 已存在
- `rpmsg_create_ept()` 成功

### 5.2 `mica_umt_is_ready()` 的状态含义

`mica_umt_is_ready()` 只是：
- `return is_rpmsg_ept_ready(&g_umt_ept);`

也就是说，它本质上还是沿用 OpenAMP 的 endpoint ready 语义：
- endpoint 已存在
- `dest_addr` 已绑定

它并不自动说明：
- 共享内存发送区已经初始化
- callback 模式或 passive pull 模式已经真正可用
- 双边对描述符和 payload 的解释一定一致

所以 UMT ready 只是通信机制 ready 的一个重要门槛，不是 UMT 数据面闭合的全部条件。

这里的阶段重点是：
- RTOS/client 侧 `rpmsg-umt` endpoint 如何被创建
- endpoint ready 的最小前提是什么

在这一步之后，服务已经具备进入共享内存数据面收发阶段的基础，但真正的 UMT 数据链还要继续看运行时处理路径。

## 6. UMT 服务运行时收发路径

前面的第 4 章和第 5 章分别对应：
- Linux/master 侧的 service bind 与本地对象建立
- RTOS/client 侧的 endpoint 创建与 ready 形成

从这一章开始，关注点切换到：
- 服务已经建立之后
- 描述符消息和共享内存 payload 在运行时是怎样真正完成双向收发的

### 6.1 RTOS/client 侧接收路径

RTOS/client 侧接收回调是：
- `umt_rx_callback()`

它做的事情是：
1. `rpmsg_hold_rx_buffer(ept, data)`
2. 把收到的描述符暂存到 `g_umt_priv.rx_msg`
3. 如果是第一条有效消息，初始化 `send_buffer_addr`
4. 如果 `phy_addr` 有效，则 post `rx_sem`
5. 如果消息无效，则立即 release buffer

这说明 UMT 在 RTOS/client 侧同样采用了：
- callback hold
- 线程消费
- 再决定何时 release

### 6.1.1 RTOS/client 侧 callback 模式与 passive pull 模式

`umt_service_thread()` 被唤醒后，会按两种模式之一继续：

1. callback 模式
   - 如果用户已注册 `mica_umt_register_rcv_cb()`
   - 直接把 payload 指针和长度交给回调
   - 回调返回后再 release buffer

2. passive pull 模式
   - 如果没有注册回调
   - 只 post `passive_rcv_sem`
   - 等 `mica_rcv_data()` 主动取数据后再 release buffer

这意味着 UMT 的运行时数据面至少有两条可能的消费路径。

因此调试 UMT 时必须先分清：
- 当前系统走的是 callback 模式
- 还是 passive pull 模式

### 6.2 Linux/master 侧接收路径

Linux/master 侧 `rpmsg_rx_umt_callback()` 目前兼容两种回复格式：

1. 旧 ABI
   - `len == sizeof(int)`
   - 只返回接收长度
   - Linux/master 侧默认认为实际数据仍位于既定共享内存位置

2. 新 ABI
   - `len >= sizeof(umt_send_msg_t)`
   - 返回 `phy_addr + data_len`
   - Linux/master 侧根据 RTOS 返回的物理地址和长度解释数据位置

这段兼容逻辑存在的原因，是 UMT 的早期实现并没有那么灵活：
- 早期模型默认数据都放在固定共享内存位置
- Linux/master 侧不需要从 RTOS 回复里拿到地址，只拿长度就够了

而当前模型为了支持更灵活的数据布局，例如：
- 在 copy-message 共享区里使用不同 offset

就要求 RTOS 在回包时把地址也显式告诉 Linux/master。

因此现在才同时兼容：
- 老 RTOS 只回 `int len`
- 新 RTOS 回 `umt_send_msg_t`

当前代码里用：
- `len == sizeof(int)`

来判定旧 ABI，这本质上是一个兼容旧实现的过渡判断。

### 6.3 Linux/master 侧发送路径

Linux/master 侧 UMT 的一个关键路径是：
- `rpmsg_umt_tx_task()`

它会等待用户态信号量：
- `sem_user_to_micad`

然后把共享内存中的：
- `phy_addr`
- `data_len`

封装成 `umt_send_msg_t`，再通过：
- `rpmsg_send(&umt_svc->ept, &msg, sizeof(msg))`

发给 client 侧。

这说明 Linux/master 侧 UMT 发送本质上也是：
- 共享内存准备 payload
- RPMsg 只发描述符

### 6.3.1 `library/user_msg` 对 Linux/master 发送路径的封装

如果从用户态 API 视角看，Linux/master 侧常见发送路径不是直接调 micad 内部线程，而是通过：
- `send_data_with_umt_context()`
- `send_data_to_rtos()`
- `send_data_to_rtos_and_wait_rcv_ped()`

这层封装的关键点是：
- 用户态先通过 `umt_context_create()` 建立上下文
- 底层查询 `GET_COPY_MSG_MEM` 对应的共享区
- 再把数据按 offset 拷贝进共享区
- 最后再通过 UMT 的 RPMsg 描述符通路通知对端

`library/user_msg` 与 UMT service 的同步主要靠两组信号量完成：
- `sem_user_to_micad`
  - 用户态写入 `process_shared_memory->{phy_addr, data_len}` 后 `sem_post()`
  - Linux/master 侧 `rpmsg_umt_tx_task()` 等待这个信号量，再把描述符通过 `rpmsg_send()` 发给 RTOS/client
- `sem_micad_to_user`
  - Linux/master 侧 `rpmsg_rx_umt_callback()` 收到 RTOS/client 回复后 `sem_post()`
  - 用户态 `receive_data_with_umt_context()` 或内部回调线程在等待这个信号量，再去读取回复数据

因此从用户态视角看，`library/user_msg` 和 UMT service 的交互模型不是直接互调函数，而是：
- 用户态写共享区并 post 信号量
- micad UMT service 读取共享区并发出 RPMsg 描述符
- micad 收到回包后再 post 另一组信号量
- 用户态继续取回回复数据

因此测试 `test/test_umt` 或 `test/test_shared_lib/send-data` 时，实际验证的不只是一个孤立函数，而是：
- 用户态库接口
- copy-message 共享区
- micad UMT 服务
- RTOS/client UMT 服务

共同组成的一条完整业务链。

### 6.4 RTOS/client 侧发送路径

RTOS/client 侧发送入口是：
- `mica_send_data()`

它会：
1. 检查 `send_buffer_addr` 是否已初始化
2. 把 payload `memcpy` 到共享内存发送区
3. 组装 `umt_data_t { phy_addr, data_len }`
4. 用 `rpmsg_send(&g_umt_ept, &msg, sizeof(msg))` 把描述符发出去

因此从 client 到 Linux/master 方向，UMT 也遵循同样原则：
- 共享内存承载 payload
- RPMsg 承载描述符和控制面语义

## 7. UMT ready 的真实边界

对于 UMT 来说，真正的“可用”至少要满足：

1. Linux/master 侧 `client->rdev` 已建立
2. RTOS/client 侧 `rpmsg-umt` endpoint 已创建
3. Linux/master 侧 `create_rpmsg_umt_service()` 已注册本地 service
4. 远端 UMT service/name 已被 `umt_name_match()` 命中
5. `umt_service_init()` 已成功创建本地 UMT service 实例
6. Linux/master 侧共享内存、同步对象和发送线程已建立
7. RTOS/client 侧 callback 模式或 passive pull 模式至少有一条数据路径可用
8. 双边对 `phy_addr`、`data_len` 和共享内存布局的理解一致

所以 UMT 特别容易出现一种现象：
- endpoint 看起来 ready 了
- 但真正的数据仍然不可用

这时最该怀疑的不是“RPMsg 没起来”，而是：
- 共享内存数据面没有闭合
- 同步对象链没有闭合
- callback / passive pull 模式与实际调用路径不一致

## 8. 调试 UMT 时最值得抓的观察点

### 8.1 Linux/master 侧
- `create_rpmsg_umt_service()` 是否被调用
- `mica_register_service()` 是否成功
- `umt_name_match()` 是否命中远端 name
- `umt_service_init()` 里 endpoint、共享内存、同步对象、线程是否建立成功
- `g_umt_list` 是否已有实例

### 8.2 RTOS/client 侧
- `mica_get_rpdev()` 是否非空
- `rpmsg_create_ept(&g_umt_ept, ...)` 是否成功
- `mica_umt_is_ready()` 是否成立
- `umt_rx_callback()` 是否被触发
- `send_buffer_addr` 是否已初始化
- 当前走的是 callback 模式还是 passive pull 模式

### 8.3 跨层边界
- NS 是否真的到达 Linux/master
- `dest_addr` 是否已经正确绑定
- `phy_addr` / `data_len` 是否和共享内存布局一致
- 数据不可用时，问题是卡在 endpoint ready 前，还是卡在共享内存数据面闭合后

### 8.4 用户态库与测试路径
- `umt_context_create()` 是否成功
- `IOC_GET_COPY_MSG_MEM` 是否返回了合理共享区
- `send_data_with_umt_context()` 或 `send_data_to_rtos_and_wait_rcv_ped()` 是否成功返回
- `test/test_umt` 或 `test/test_shared_lib/send-data-2-way` 是否能完成一次“发出 -> 回来 -> 打印”的闭环

## 9. 建议继续阅读

- `../communication-overview.md`
- `../transport-foundation.md`
- `../openamp-rpmsg.md`
- `../../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`
- `../../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
