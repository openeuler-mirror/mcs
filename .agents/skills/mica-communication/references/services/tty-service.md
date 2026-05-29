# TTY 服务说明

## 1. 文档目标

这篇文档专门解释 MICA 的 TTY 服务是怎样建立起来的，以及为什么“RPMsg endpoint 已经存在”并不自动等于“终端已经可用”。

它主要回答：
- Linux/master 侧 `create_rpmsg_tty()` 到 `rpmsg_tty_init()` 具体做了什么
- RTOS/client 侧 `rpmsg-tty` endpoint 是怎样创建和进入 ready 的
- TTY 为什么依赖 hold/release RX buffer 这条异步处理链
- TTY ready 的真实边界在哪里

## 2. TTY 服务总体模型

TTY 不是单纯的一个 RPMsg endpoint。

在当前系统里，它至少要跨三层才能真正可用：

1. OpenAMP/RPMsg 机制层
   - `rpmsg-tty` endpoint 已建立
   - 双边 `dest_addr` 已绑定

2. MICA service 层
   - Linux/master 侧 `rpmsg_tty_match()` 命中远端服务名
   - `rpmsg_tty_init()` 成功创建本地 TTY service 对象

3. Linux/master / RTOS/client 运行时层
   - Linux/master 侧 PTY/TTY 设备节点、TX 线程、unbind 清理链可用
   - RTOS/client 侧 shell handler、RX 线程、hold/release buffer 路径可用

所以看到“endpoint ready 了但终端不可用”时，不要把问题只归到 RPMsg 机制层。

### 2.1 TTY 服务的观测价值

在实际使用中，TTY 往往是用户判断通信是否通畅的第一观测窗。

最常见的第一步通常是：
- 先看 `mica status`
- 确认状态输出里对应实例的 `/dev/ttyRPMSGX` 是否已经出现

这个观测点之所以重要，是因为 `/dev/ttyRPMSGX` 的出现至少说明：
- Linux/master 侧已经接住了某个远端 TTY service
- 本地 TTY service 实例已经创建到了设备节点可见层

因此如果：
- `mica status` 里还没有对应 `/dev/ttyRPMSGX`

通常就足以说明通信链至少有一层还没有闭合，优先不要把问题理解成“只是终端程序没打开”，而要继续检查：
- TTY service 是否已注册
- 远端 name service 是否到达
- `rpmsg_tty_init()` 是否成功创建设备节点

第二个常见观测动作是：
- `screen /dev/ttyRPMSGX`
- 再直接敲键盘看远端 shell 是否有响应

如果设备节点已经存在，但 `screen` 后敲键盘没有反应，这通常说明：
- TTY bind 可能已经部分成立
- 但双边字符收发链、RTOS/client 侧 shell handler、或 RX buffer 异步处理链仍可能没有真正闭合

所以从诊断价值上说：
- `/dev/ttyRPMSGX` 是否出现，是第一层观测
- `screen /dev/ttyRPMSGX` 是否能交互，是第二层观测

这两个观测都不直接证明“所有通信都完全正常”，但它们是判断 MICA 通信是否基本跑通的最常用入口。

### 2.2 TTY 服务的工作原理

TTY 之所以能成为通信是否通畅的第一观测窗，本质上是因为它把一条完整的用户交互链映射成了一次 Linux <-> client 的双向通信回路。

### 2.2.1 Linux/master -> client 方向

Linux/master 侧 `rpmsg_tty_init()` 在命中远端 TTY service 后会：
- 创建一个 PTY master/slave 对
- 再把用户可见的 `/dev/ttyRPMSGX` 链接到对应的 slave 设备

之后如果用户执行：
- `screen /dev/ttyRPMSGX`

用户实际是在操作 slave 设备。

这条输入链会按下面路径前进：
1. 用户向 PTY slave 写入字符
2. slave 把数据转给 PTY master
3. Linux/master 侧 `rpmsg_tty_tx_task()` 在 master 上 `poll()` 到可读事件
4. `read(pty_master_fd, ...)` 取出用户输入
5. 调 `rpmsg_send(&tty_svc->ept, buf, ret)`
6. 数据通过 `rpmsg-tty` endpoint 发往 client 侧

也就是说，从 Linux/master 到 client 侧，TTY 服务本质上做的是：
- 把用户对本地 PTY 的输入转换成一条 RPMsg 消息

### 2.2.2 client -> Linux/master 方向

client 侧收到 `rpmsg-tty` 消息后，会在 `rx_tty_callback()` 中先 hold buffer，再把收到的数据交给 TTY service 线程。

TTY service 线程随后会：
- 把这些字符逐个丢给 RTOS 自己的 shell handler

因此对 RTOS/client 来说，收到的内容可能是：
- 一个回车
- 一个普通字符
- 一段 shell 输入

RTOS shell 模块处理后，如果需要输出答复，例如：
- shell 提示符
- 输入字符的回显
- 命令执行结果

这些输出会再次通过 client 侧 `rpmsg_send()` 发回 Linux/master。

Linux/master 侧 `rpmsg_rx_tty_callback()` 收到消息后会：
1. 把 RPMsg payload 取出来
2. 必要时把 `\n` 转成 `\r\n`
3. `write(tty_svc->pty_master_fd, msg, len)` 写回 PTY master
4. master 再把数据转给 slave
5. 用户态 `screen` 从 slave 读出数据并显示

也就是说，从 client 到 Linux/master 方向，TTY 服务本质上做的是：
- 把 client shell 的输出转换成 Linux PTY 上可见的终端输出

### 2.2.3 “回车 + 提示符 + 回显”的链路闭合意义

如果用户在 `screen /dev/ttyRPMSGX` 中：
- 按下回车
- 看到换行和 shell 提示符，例如 `UniProton #`
- 再输入一个字符，例如 `a`
- 看到字符 `a` 被正常回显

那么至少说明下面这条链已经闭合：

1. 用户输入已经从 Linux PTY slave 到达 PTY master
2. `rpmsg_tty_tx_task()` 已经把输入发到 client 侧
3. client 侧 TTY service 和 shell handler 已经收到并处理输入
4. client 侧已经把答复通过 `rpmsg_send()` 发回 Linux/master
5. Linux/master 侧 `rpmsg_rx_tty_callback()` 已经把答复写回 PTY master
6. `screen` 已经从 PTY slave 读到并显示这段输出

这并不自动代表所有上层服务都完全正常，但它已经足以证明：
- 这条 TTY 通信链至少完成了一次“输入过去、输出回来”的基本往返

如果连字符回显都没有，通常就说明：
- 通信链至少有一层仍未闭合
- 问题可能在 Linux/master 发出路径、client shell 处理路径、或 client -> Linux/master 回程路径中的任意一段

## 3. Linux/master 侧 TTY 服务声明与绑定

### 3.1 `create_rpmsg_tty()`：先注册一个带匹配规则的 `mica_service`

Linux/master 侧入口在：
- `mica/micad/services/pty/rpmsg_pty.c:create_rpmsg_tty()`

它本身只是：
- 调 `mica_register_service(client, &rpmsg_tty_service)`

真正关键的是 `rpmsg_tty_service` 这个声明里包含：
- `.name = "rpmsg-tty"`
- `.init = create_tty_dev_lists`
- `.remove = remove_tty_dev_lists`
- `.rpmsg_ns_match = rpmsg_tty_match`
- `.rpmsg_ns_bind_cb = rpmsg_tty_init`
- `.get_match_device = get_rpmsg_tty_dev`

这说明 TTY 在 MICA service 模型里不是“直接建 endpoint”，而是先声明：
- 什么远端服务名属于 TTY
- 命中之后如何创建本地对象
- 移除时如何统一清理

### 3.2 `rpmsg_tty_match()`：TTY 允许通配式服务名

`rpmsg_tty_match()` 的语义不是只匹配严格相等的 `rpmsg-tty`。

从当前代码看，它允许前缀匹配，例如：
- `rpmsg-tty`
- `rpmsg-tty0`
- `rpmsg-tty1`

这意味着调试时不要只盯一个固定 endpoint 名字；远端如果带编号或变体名，仍可能被本地 TTY 服务接住。

### 3.3 `create_tty_dev_lists()`：每个 client 先准备本地 TTY service 容器

TTY 的 `.init` 是：
- `create_tty_dev_lists()`

它会：
- 分配一个 `metal_list`
- 挂到 `svc->priv`

这个链表后面用来收集当前 client 侧实际创建出来的各个 `rpmsg_tty_service` 实例。

所以 TTY 的 service 声明和具体 TTY device 实例不是一个对象：
- `mica_service` 是声明层
- `rpmsg_tty_service` 是实际运行时实例层

## 4. Linux/master 侧 TTY bind 后的本地对象建立

### 4.1 `rpmsg_tty_init()`：真正创建本地 TTY 运行时对象

当远端 name service 被 `rpmsg_tty_match()` 命中后，Linux/master 侧会进入：
- `rpmsg_tty_init()`

这一步才是真正把 TTY 服务落地成本地运行时对象的地方。

从当前文件结构看，它至少负责：
- 分配 `struct rpmsg_tty_service`
- 创建并注册本地 `rpmsg_endpoint`
- 创建 PTY 主从设备
- 创建可见的 TTY 设备节点路径
- 把实例挂到 `svc->priv` 维护的 TTY device list
- 建立后续发送线程和 unbind 清理链

所以如果 symptom 是：
- `mica_ns_bind_cb()` 已经命中 TTY
- 但 Linux/master 侧没有看到对应 TTY 设备

优先看 `rpmsg_tty_init()` 里面哪一步失败了，而不是先回头怀疑 RPMsg 机制本体。

### 4.2 `get_rpmsg_tty_dev()`：TTY 的“服务可见性”不只是 endpoint

TTY service 还实现了：
- `get_match_device()`

这意味着在 MICA 里，TTY 的可见性不仅体现在“有一个 RPMsg endpoint”，还体现在：
- 当前已经匹配出来哪些 TTY device
- 它们对应哪个设备节点路径

所以 TTY 问题经常要同时看两件事：
- RPMsg bind 是否成立
- 本地设备节点是否已经真正创建出来

## 5. RTOS/client 侧 `rpmsg-tty` 建立流程

### 5.1 `tty_service_thread()`：RTOS/client 侧 TTY 的真正入口

RTOS/client 侧关键入口在：
- `rtos/libmica/src/services/tty_service.c:tty_service_thread()`

它的顺序很清楚：
1. 取 `mica_get_rpdev()`
2. 初始化 `g_tty_priv.rx_sem`
3. 设置 `g_tty_ept.priv = &g_tty_priv`
4. 调 `rpmsg_create_ept(&g_tty_ept, rpdev, "rpmsg-tty", RPMSG_ADDR_ANY, RPMSG_ADDR_ANY, rx_tty_callback, NULL)`
5. 进入循环等待接收数据

这说明 RTOS/client 侧 TTY ready 的最小前提是：
- `rpdev` 已存在
- `rpmsg_create_ept()` 成功

### 5.2 `mica_tty_is_ready()`：TTY ready 的直接判据其实很弱

`mica_tty_is_ready()` 只是：
- `return is_rpmsg_ept_ready(&g_tty_ept);`

也就是说，它本质上还是沿用 OpenAMP 的 endpoint ready 语义：
- endpoint 已存在
- `dest_addr` 已绑定

它并不自动说明：
- Linux/master 侧 TTY 设备节点一定已经建立
- shell handler 一定已经正确工作
- 整个终端收发链一定已经通畅

所以 TTY ready 只是通信机制 ready 的一个重要门槛，不是终端业务链闭合的全部条件。

这里的阶段重点是：
- RTOS/client 侧 `rpmsg-tty` endpoint 如何被创建
- endpoint ready 的最小前提是什么

在这一步之后，服务已经具备进入双向收发阶段的基础，但真正的字符收发链和终端交互链还要继续看运行时收发路径。

## 6. TTY 服务运行时收发路径

前面的第 4 章和第 5 章分别对应：
- Linux/master 侧的 service bind 与本地对象建立
- RTOS/client 侧的 endpoint 创建与 ready 形成

从这一章开始，关注点切换到：
- 服务已经建立之后
- 字符消息在运行时是怎样真正完成双向收发的

这一章重点描述的是服务建立之后的双向收发链，包括：
- RTOS/client 侧接收方向的运行时处理链
- Linux/master 侧 PTY 与 RPMsg 的转发链
- RX buffer 的异步生命周期管理

### 6.1 RTOS/client 侧 RX buffer 处理模型

`rx_tty_callback()` 不是同步处理，而是异步移交。

RTOS/client 侧接收回调：
- `rx_tty_callback()`

它做的事情是：
1. `rpmsg_hold_rx_buffer(ept, data)`
2. 把 `data/len` 暂存到 `g_tty_priv.rx_msg`
3. `mica_sem_post()` 唤醒 `tty_service_thread()`
4. 立即返回

也就是说，TTY 不是在 callback 里立刻消费完消息，而是：
- callback 只做“把 buffer 暂时保住 + 通知线程”
- 真正的字符处理在线程里完成

### 6.2 RTOS/client 侧线程消费与 buffer 归还

`tty_service_thread()` 被唤醒后会：
- 读取 `g_tty_priv.rx_msg`
- 调 `shell_cmd_handler()` 逐字符交给平台 shell
- 最后 `rpmsg_release_rx_buffer(&g_tty_ept, g_tty_priv.rx_msg.data)`

这条链说明 TTY 的 RX 路径有一个非常清晰的生命周期：
- callback hold
- 线程消费
- 线程 release

如果这里配对出错，常见后果是：
- buffer 太早被回收，线程里拿到悬空数据
- buffer 长期不 release，后续接收链路被拖死

这也是 TTY 和 RPC 的一个明显差异：
- TTY 是强异步线程消费模型
- RPC 更接近回调即处理模型

### 6.3 Linux/master 侧运行时收发路径

和 RTOS/client 侧 RX 路径对应，Linux/master 侧运行时链路主要由两个方向组成：

1. Linux/master -> client
   - `screen` / 用户输入进入 PTY slave
   - slave 转发到 PTY master
   - `rpmsg_tty_tx_task()` 在 master 上 `poll/read`
   - `rpmsg_send()` 把字符发往 client

2. client -> Linux/master
   - client shell 输出通过 `rpmsg_send()` 发回 Linux/master
   - `rpmsg_rx_tty_callback()` 收到消息
   - 写回 PTY master
   - master 再把数据转发到 slave
   - `screen` 从 slave 读出并显示

因此第 6 章的整体语义是：
- 第 4/5 章回答“服务对象和 endpoint 是怎样建立起来的”
- 第 6 章回答“建立之后，双向收发链是怎样真正跑起来的”

## 7. TTY ready 的真实边界

对于 TTY 来说，真正的“可用”至少要满足：

1. Linux/master 侧 `client->rdev` 已建立
2. RTOS/client 侧 `rpmsg-tty` endpoint 已创建
3. Linux/master 侧 `create_rpmsg_tty()` 已注册本地 service
4. 远端 TTY service/name 已被 `rpmsg_tty_match()` 命中
5. `rpmsg_tty_init()` 已成功创建本地 TTY service 实例
6. PTY/TTY 设备节点已建立
7. RTOS/client 侧 `shell_cmd_handler()` 已可用
8. RX buffer 的 hold/release 异步链可正常运行

所以 TTY 特别容易出现一种现象：
- endpoint 看起来 ready 了
- 但终端实际仍不可用

这时最该怀疑的不是“RPMsg 没起来”，而是：
- Linux/master 本地设备节点链没建好
- RTOS/client shell handler 没闭合
- 或 hold/release 线程模型异常

## 8. 调试 TTY 时最值得抓的观察点

### 8.1 Linux/master 侧
- `create_rpmsg_tty()` 是否被调用
- `mica_register_service()` 是否成功
- `rpmsg_tty_match()` 是否命中远端 name
- `rpmsg_tty_init()` 里创建 PTY/TTY 设备是否成功
- `svc->priv` 里的 TTY device list 是否已有实例

### 8.2 RTOS/client 侧
- `mica_get_rpdev()` 是否非空
- `rpmsg_create_ept(&g_tty_ept, ...)` 是否成功
- `mica_tty_is_ready()` 是否成立
- `rx_tty_callback()` 是否被触发
- `tty_service_thread()` 是否收到信号并最终 `rpmsg_release_rx_buffer()`

### 8.3 跨层边界
- NS 是否真的到达 Linux/master
- `dest_addr` 是否已经正确绑定
- 终端不可用时，问题是卡在 endpoint ready 前，还是卡在 TTY 运行时资源闭环后

## 9. 建议继续阅读

- `../communication-overview.md`
- `../openamp-rpmsg.md`
- `../../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`
- `../../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
