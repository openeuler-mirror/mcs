# RPC 服务说明

## 1. 文档目标

这篇文档专门解释 RPC 服务在当前 MICA 通信链中的位置，区分：
- `rtos/libmica` 当前已经具备的最小 endpoint 封装
- 仍主要依赖 `rpc_proxy/` 的 RPC 处理能力
- `UniProton` 现有实现中的 RPC service 参考路径

它主要回答：
- Linux/master 侧 `create_rpmsg_rpc_service()` 到 `rpc_service_init()` 具体做了什么
- `rtos/libmica` 里当前 RPC 到底支持到什么程度
- `rpc_proxy/` 在 RPC 通信链里承担什么角色
- 当前 `libmica` 的 RPC 收敛边界在哪里

## 2. 当前状态总览

RPC 和 TTY、UMT 不同，它在当前仓里的实现状态存在明显分层：

1. Linux/master 侧 RPC service
   - 已有明确的 `mica_service` 声明与 bind callback
   - 能在 NS 命中后创建 Linux/master 侧 RPC endpoint

2. `rtos/libmica` 侧 RPC service
   - 已有一个很薄的 `rpmsg-rpc` endpoint 包装
   - 但完整 RPC 处理能力并没有都沉到这层

3. 更完整的 RPC 处理能力
   - 当前主要仍在 `rtos/libmica/src/services/rpc_proxy/`
    - 以及 `UniProton` 当前 `src/component/mica/` 与 `src/component/proxy/` 路径里可以看到更完整的服务链

当前 `rtos/libmica/README.md` 里：
- TTY：支持
- UMT：支持
- RPC：待支持

更准确地说，RPC 当前已经有基础 endpoint 外壳，但完整、稳定、被 `libmica` 正式吸收的 service 体系还没有像 TTY/UMT 那样完成收敛：
- 基础 endpoint 已经有了
- 但完整的 RPC 处理能力仍主要依赖 `rpc_proxy/` 体系与现有参考实现

## 3. RPC 工作原理

### 3.1 本地调用外观与远程执行本质

RPC 的核心作用不是简单传一条消息，而是把“RTOS/client 本地无法直接完成的能力”包装成一个本地函数调用外观。

从 RTOS/client 应用的视角看：
- 应用像调用本地 `read` 一样发起一次函数调用
- 调用返回后拿到读取结果或错误码

但从通信与执行链的视角看，这个过程实际上包含一轮完整的远程调用：
- RTOS/client 把调用请求编码成 RPC 请求消息
- 请求通过 `rpmsg-rpc` endpoint 发往 Linux/master
- Linux/master 解析请求并在本地执行对应系统调用或代理逻辑
- Linux/master 再把执行结果编码成 RPC 响应消息回发给 RTOS/client
- RTOS/client 收到响应后再把结果还原成本地函数返回值交给应用

因此 RPC 的关键特征是：
- 应用侧看到的是本地函数接口
- 实际发生的是一次请求和一次响应组成的往返通信
- 真正执行能力位于 Linux/master 一侧，而不是 RTOS/client 本地

### 3.2 以远程 `read` 为例的调用链

如果 RTOS/client 侧应用希望读取 Linux/master 文件系统中的一个文件，典型流程可以概括为：

1. RTOS/client 应用调用 proxy 层封装好的 `read` 接口
2. proxy 层把这次调用组织成一个 RPC 请求，里面包含要调用的操作、参数、返回槽位等信息
3. 该请求通过 `rpmsg-rpc` endpoint 发送到 Linux/master
4. Linux/master 收到请求后解析 RPC 报文，识别出这是一条远程 `read` 调用
5. Linux/master 在本地执行真正的 `read`
6. Linux/master 把返回值、错误码以及读到的数据封装成 RPC 响应
7. 响应通过 `rpmsg-rpc` 再发送回 RTOS/client
8. RTOS/client 的 RPC 回调与等待逻辑完成请求/响应配对
9. proxy 层把响应内容还原成 `read` 的返回结果并返回给 RTOS/client 应用

对 RTOS/client 应用来说，它看到的效果仍然是：
- 调用一次 `read`
- 拿到一次 `read` 的返回值和数据

但它背后实际上依赖的是：
- 一次 `read` 请求的参数封装
- 一次 `rpmsg-rpc` 请求发送
- Linux/master 本地执行
- 一次 `rpmsg-rpc` 响应返回
- 一次请求/响应结果配对与返回值还原

这也是 RPC 与普通消息服务的重要差别：
- 普通消息服务更关注“把数据送到对端”
- RPC 更关注“把对端能力伪装成本地调用语义”

### 3.3 RPC 需要 proxy 层的原因

如果只有一个 `rpmsg-rpc` endpoint，本身还不足以形成完整的 RPC 服务，因为远程调用通常还需要：
- 调用编号或操作类型管理
- 参数编码与解码
- 请求与响应配对
- 阻塞等待或异步回调
- 返回值、错误码、输出缓冲区的还原

因此当前实现里真正关键的不只是 endpoint 是否建立成功，还包括 proxy 层是否已经接管以下职责：
- 把本地接口调用转换成 RPC 请求
- 为请求分配记录项、slot 或等待对象
- 在收到响应后找到对应请求并唤醒等待方
- 把响应重新还原为本地函数语义

这也是 RPC 虽然表面上只是一个 `rpmsg-rpc` service，但其真实复杂度明显高于 TTY 和多数 UMT 场景的原因。

## 4. Linux/master 侧 RPC 服务声明与绑定

### 4.1 `create_rpmsg_rpc_service()` 服务注册入口

Linux/master 侧入口在：
- `mica/micad/services/rpc/rpmsg_rpc.c:create_rpmsg_rpc_service()`

它会先：
- `rpmsg_rpc_service_init()`
- 再调 `mica_register_service(client, &rpmsg_rpc_service)`

而 `rpmsg_rpc_service` 这个声明里包含：
- `.name = "rpmsg-rpc"`
- `.rpmsg_ns_match = rpc_name_match`
- `.rpmsg_ns_bind_cb = rpc_service_init`
- `.remove = remove_rpc_service`

这说明 RPC 在 MICA service 模型里至少分两层：
- `rpc_name_match()` 负责判定远端 `name` 是否属于 RPC
- `rpc_service_init()` 负责在 Linux/master 侧真正建立 RPC endpoint 和服务对象

### 4.2 `rpc_name_match()` 服务名匹配规则

`rpc_name_match()` 的语义比较直接：
- 只匹配 `rpmsg-rpc`

因此 RPC 的服务命中逻辑和 UMT 一样，更接近固定服务名匹配，而不是 TTY 那种前缀通配式模型。

## 5. Linux/master 侧 RPC bind 后的本地对象建立

### 5.1 `rpc_service_init()` 本地对象建立流程

当远端 name service 被 `rpc_name_match()` 命中后，Linux/master 侧会进入：
- `rpc_service_init(struct rpmsg_device *rdev, const char *name, uint32_t remote_addr, uint32_t remote_dest, void *priv)`

这一步才是真正把 RPC 服务落地成本地运行时对象的地方。

从当前代码看，它至少负责：
- 分配本地 `struct rpmsg_endpoint`
- 通过 `rpmsg_create_ept(...)` 建立 Linux/master 侧 `rpmsg-rpc` endpoint
- 立即发出一条 first message

因此如果现象是：
- `mica_ns_bind_cb()` 已经命中 RPC
- 但 Linux/master 侧 RPC 仍不可用

优先看 `rpc_service_init()` 里 endpoint 创建和 first message 发送是否成功。

### 5.2 first message 的作用

Linux/master 侧 `rpc_service_init()` 在 bind 完成本地 endpoint 后，会主动发送：
- `"first message from rpc_service!"`

这条消息的作用更接近：
- 服务层激活
- 首轮实际收发探活

而不是 RPMsg 协议层必需的第二次握手。

因此分析 RPC 是否真正可用时，要区分：
1. NS / endpoint bind 是否成立
2. first message 与后续 RPC 收发链是否成立

## 6. `rtos/libmica` 当前 RPC 侧实现

### 6.1 `rpc_service_thread()` RTOS/client 侧入口

`rtos/libmica/src/services/rpc_service.c` 当前提供了一个较薄的 RPC service 包装：
- 创建 `g_rpc_ept`
- 用 `mica_rpc_is_ready()` 检查 endpoint ready
- 收包后直接交给 `rpmsg_client_cb()`
- 通过 `mica_rpc_send()` 回发响应

从这段代码看，`rtos/libmica` 当前至少已经具备：
- `rpmsg-rpc` endpoint 创建
- endpoint ready 判定
- 收到 RPC 消息后转交统一处理函数
- 使用 `rpmsg_send()` 回发响应

### 6.2 `rpmsg_client_cb()` 并不在 `rpc_service.c` 里实现

`rpc_service.c` 里把真正的处理逻辑声明成了外部函数：
- `extern int rpmsg_client_cb(...)`

这说明当前 `rpc_service.c` 本身只是一层 endpoint 包装；真正的 RPC 处理能力主要还不在这里。

### 6.3 当前实现状态与收敛边界

从 `rtos/libmica/README.md` 的状态标记，以及当前代码分布看，更稳妥的结论是：
- `rtos/libmica` 已经开始收敛 RPC 的最小 endpoint/service 外壳
- 但完整 RPC 能力仍依赖更底下的 `rpc_proxy/` 体系
- 因此它还没有像 TTY/UMT 那样形成一个清晰、完整、正式收敛后的 service 层实现

## 7. `rpc_proxy/` 的真实处理链

### 7.1 `rpmsg_client_cb()` 当前主要落在 `rpc_proxy/`

在当前仓里，`rpmsg_client_cb()` 的关键实现位于：
- `rtos/libmica/src/services/rpc_proxy/rpc_routines.c`

这里可以看到：
- `rpmsg_set_default_ept()`
- `rpmsg_client_cb()`
- `wait4resp()`
- 大量 `LOS_Proxy*` 封装

这说明当前 RPC 的有效能力不只是一个 `rpmsg-rpc` endpoint，而是：
- 收到响应后由 `rpmsg_client_cb()` 统一分发
- 再根据 `msg->id`、slot、callback、records 等内部模型完成请求/响应配对

### 7.2 `g_ept` / `wait4resp()` 反映出的代理调用框架语义

`rpc_routines.c` 里很关键的一条链是：
- `rpmsg_set_default_ept()` 保存默认 endpoint
- `wait4resp()` 通过 `rpmsg_send(g_ept, ...)` 发请求
- 然后等待对应 slot 被回调填回结果

这表明当前 RPC 更接近：
- 一个建立在 `rpmsg-rpc` endpoint 之上的代理调用框架
- 而不是“收到包就本地执行少量逻辑”的轻量服务

因此 RPC 的复杂度天然高于 TTY，也高于多数 UMT 使用场景。

## 8. `UniProton` 现有实现的参考价值

### 8.1 `src/component/mica/rpmsg_service.c`

在 `UniProton` 当前实现中：
- `src/component/mica/rpmsg_service.c`

可以看到更完整的 RPC service 参考链：
- `rpc_ept`
- `rpmsg_rpc_task()`
- `rpmsg_create_ept(&rpc_ept, rpdev, RPMSG_RPC_EPT_NAME, RPMSG_ADDR_ANY, RPMSG_ADDR_ANY, rpmsg_client_cb, rpmsg_service_unbind)`
- `while (!is_rpmsg_ept_ready(&rpc_ept))`
- `rpmsg_set_default_ept(&rpc_ept)`

这条链说明，现有参考实现中的 RPC service 至少明确完成了：
- endpoint 创建
- endpoint ready 等待
- 默认 endpoint 注入到 proxy 层

### 8.2 `src/component/proxy/`

`UniProton` 下的：
- `src/component/proxy/`

提供了更完整的 RPC / proxy 处理链参考。

这说明未来如果要把 RPC 真正沉到 `rtos/libmica`，目标不应只是：
- 复制一个 `rpc_service.c` 外壳

而是要同时考虑：
- endpoint 生命周期
- `rpmsg_set_default_ept()` 这类默认通信对象注入
- 请求/响应配对模型
- proxy worker / callback / slot 管理体系

## 9. RPC ready 的真实边界

对于 RPC 来说，真正的“可用”至少要满足：

1. Linux/master 侧 `client->rdev` 已建立
2. RTOS/client 侧 `rpmsg-rpc` endpoint 已创建
3. Linux/master 侧 `create_rpmsg_rpc_service()` 已注册本地 service
4. 远端 RPC service/name 已被 `rpc_name_match()` 命中
5. `rpc_service_init()` 已成功创建 Linux/master 侧 RPC endpoint
6. first message 与首轮实际收发链已经成立
7. `rpmsg_client_cb()`、默认 endpoint、proxy request/response 配对链已经闭合

所以 RPC 特别容易出现一种现象：
- endpoint 看起来 ready 了
- 但真正的代理调用仍然不可用

这时最该怀疑的不是“RPMsg 没起来”，而是：
- proxy 层没有真正接管 endpoint
- 默认 endpoint 没注入
- 请求/响应配对链没有闭合

## 10. 调试 RPC 时最值得抓的观察点

### 10.1 Linux/master 侧
- `create_rpmsg_rpc_service()` 是否被调用
- `mica_register_service()` 是否成功
- `rpc_name_match()` 是否命中远端 name
- `rpc_service_init()` 里 endpoint 创建与 first message 发送是否成功

### 10.2 RTOS/client 侧
- `rpmsg_create_ept(&g_rpc_ept, ...)` 是否成功
- `mica_rpc_is_ready()` 是否成立
- `rpc_rx_callback()` 是否被触发
- `rpmsg_client_cb()` 实际是否存在并被正确链接

### 10.3 `rpc_proxy/` 层
- `rpmsg_set_default_ept()` 是否已被调用
- `g_ept` 是否已设置
- `wait4resp()` 是否真正把请求发出去并等到对应响应
- 请求/响应 slot 与 callback 配对是否正确

### 10.4 参考实现比对
- `UniProton` 的 `rpmsg_rpc_task()` 是否完成了当前 `rtos/libmica` 尚未收敛的阶段
- 当前 `rtos/libmica` 是否仍只具备 endpoint 外壳，而未形成完整 proxy 收敛能力

## 11. 建议继续阅读

- `../openamp-rpmsg.md`
- `../communication-overview.md`
- `../../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`
