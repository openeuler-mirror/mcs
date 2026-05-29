# mica create 过程拆解

## 1. 文档目标

这篇文档专门解释 `mica create <conf>` 在 Linux/master 侧到底做了什么，以及 create 成功到底意味着什么。

它主要回答：
- create 阶段会创建哪些对象
- create 为什么还没有真正启动 remote OS
- `mica_create()` 和 `create_client()` 在边界上怎么分工
- create 完成后，系统已经具备了哪些状态，还缺哪些状态
- create 成功与 start 成功的根本区别是什么

如果一个 agent 要分析下面这些问题，这篇文档应该先读：
- 为什么配置文件看起来没问题但 create 失败
- 为什么 create 成功后还是看不到 remote OS 运行
- 为什么 create 后会立即多出一个 `{client}.socket`
- 为什么 start 之前就已经知道 pedestal 类型和部分资源配置

## 2. create 的上下文位置

从整体控制面看，`mica create <conf>` 不是直接调用 `mica_create()` 就结束，而是至少分三层：

1. `mica/micactl/mica.py`
   - 读取配置文件
   - 打包 create 消息
   - 通过 `/run/mica/mica-create.socket` 发送给 `micad`

2. `mica/micad/socket_listener.c`
   - `create_mica_client()` 接收 create 消息
   - 分配并初始化 `struct mica_client`
   - 调 `mica_create(client)`
   - 成功后创建 `/run/mica/{client}.socket`

3. `library/mica/mica.c` + `library/remoteproc/remoteproc_core.c`
   - `mica_create()`
   - `create_client()`
   - `remoteproc_init()`

这说明 create 的职责更接近：
- 先把“实例对象”和“控制对象”建起来
- 再把它接到后续 start/stop/status/rm 控制面上

而不是：
- 立即把 remote OS 跑起来

## 3. create 前的 `mica_client` 配置装载流程

在 `mica/micad/socket_listener.c` 中，create 请求由：
- `create_mica_client(int epoll_fd, void *data)`
处理。

主要流程是：
1. `accept()` 接收来自 `mica-create.socket` 的连接
2. `recv(msg_fd, &msg, sizeof(msg), 0)` 收到 `struct create_msg`
3. `check_create_msg(msg, msg_fd)`
   - 检查镜像路径是否存在
   - 检查实例名是否与当前已有 listener 冲突
4. `calloc()` 一个新的 `struct mica_client`
5. `init_mica_client(client, msg)`

`init_mica_client()` 负责把配置写进 `mica_client`，重点包括：
- `client->path`
- `client->ped`
- `client->ped_cfg`
- `client->debug`
- `client->ped_setup.name`
- `client->ped_setup.cpu_str`
- `client->ped_setup.vcpu_num`
- `client->ped_setup.max_vcpu_num`
- `client->ped_setup.cpu_weight`
- `client->ped_setup.cpu_capacity`
- `client->ped_setup.memory`
- `client->ped_setup.max_memory`
- `client->ped_setup.iomem`
- `client->ped_setup.network`

也就是说，create 阶段已经完成了：
- pedestal 类型判定
- pedestal 资源配置装载
- debug 开关设置
- 远端镜像路径记录

但此时还没有：
- 加载镜像
- 启动 remote OS
- 建立 RPMsg 设备
- 建立 TTY/RPC/UMT 服务

## 4. `mica_create()` 自身有多薄

在 `library/mica/mica.c` 中：
- `mica_create(struct mica_client *client)`
内部基本只做一件事：
- `ret = create_client(client)`

所以 `mica_create()` 本身只是对外 API 入口封装。
真正的 create 实质，落在：
- `library/remoteproc/remoteproc_core.c: create_client()`

## 5. `create_client()` 的真实职责

位置：`library/remoteproc/remoteproc_core.c`

`create_client()` 做的事，决定了 create 完成后实例到底具备了哪些能力。

### 5.1 先选择 `remoteproc_ops`
它会根据 `client->ped` 和部分 pedestal 细节选择对应 `ops`：
- `BARE_METAL` -> `rproc_bare_metal_ops`
- `JAILHOUSE` -> `rproc_jailhouse_ops`
- `XEN` -> `rproc_xen_ops`
- `HETERO && cpu_str == "riscv"` -> `rproc_riscv_ops`

如果这些条件不满足，会直接 `return -EINVAL`。

这说明 create 阶段本身就是 pedestal 分叉的起点。

### 5.2 再调用 `remoteproc_init()`
`create_client()` 随后执行：
- `remoteproc_init(&client->rproc, ops, client)`

这是 create 阶段最关键的一步，因为它会把控制权交给对应 pedestal 的 `.init`。

不同 pedestal 的 `.init` 做的事不一样，但共性是：
- 把 `client->rproc` 和真实底座实现绑定起来
- 初始化 `rproc->ops`
- 建立后续 lifecycle 所需的基础状态

### 5.3 最后把 client 接入全局结构
`remoteproc_init()` 成功后，`create_client()` 还会：
- `metal_list_add_tail(&g_client_list, &client->node)`
- `metal_list_init(&client->services)`

这两步很重要：
- `g_client_list` 让这个 client 进入全局生命周期对象集合
- `client->services` 为空表，但已经为后续 service 注册准备好了容器

## 6. 不同 pedestal 的 create 完成状态

create 不同于 start，它通常只推进到“初始化控制对象和底座对象”的阶段。

### 6.1 baremetal
create 完成后，通常已经：
- 打开 `/dev/mcs`
- 解析 CPU 号
- 准备 notifier 基础设施
- 让 `client->rproc` 绑定到 baremetal 的 `remoteproc_ops`

但还没有：
- 获取共享内存池
- load 镜像
- 发送启动 ioctl

这些都属于后续 `start`。

### 6.2 hetero（当前 Linux 侧对应 `riscv_rproc.c`）
create 完成后，通常已经：
- 打开 `/dev/mcs`
- `IOC_SET_PED_TYPE` 切换到 `MCS_KM_PED_RISCV`
- 让 `client->rproc` 绑定到 `rproc_riscv_ops`

但还没有：
- `IOC_QUERY_MEM`
- 初始化共享内存池
- 解析 resource table
- 载入 pedestal bin
- `IOC_MCUON` 真正启动远端

这些同样属于 `start`。

### 6.3 jailhouse
create 完成后，通常已经：
- 建立 `rproc_jailhouse_ops` 绑定
- 准备好 cell 相关私有状态

但还没有：
- `jailhouse cell load`
- ivshmem 共享内存池初始化
- resource table 写入
- `jailhouse cell start`

### 6.4 xen
create 完成后，通常已经：
- 生成和准备 xen cfg 所需的基础状态
- 绑定 `rproc_xen_ops`
- 记录 domU 相关后续控制入口

但还没有：
- create/pause domU
- grant table / xenstore / event channel 建立
- 共享内存映射
- remote OS 真正运行

## 7. create 成功语义

### 7.1 create 成功至少代表
- 配置消息已成功进入 `micad`
- `mica_client` 已分配并填入关键字段
- `remoteproc_ops` 已根据 pedestal 选择完成
- `remoteproc_init()` 成功
- `client` 已加入 `g_client_list`
- `client->services` 已初始化为空链表
- 对应的 `/run/mica/{client}.socket` 已创建并加入 listener/epoll

### 7.2 create 成功不代表
- remote OS 已经启动
- 镜像已经加载
- resource table 已经解析完成
- RPMsg 设备已经建立
- service 已经 ready
- `/dev/ttyRPMSGx` 已出现

这里需要明确：
- create 成功，主要是“控制对象建立成功”
- start 成功，才是“运行主链推进成功”

## 8. create 后 `{client}.socket` 的生成机制

这是 create 阶段最常见的误解点之一。

在 `create_mica_client()` 中，只要：
- `init_mica_client()` 成功
- `mica_create()` 成功

就会立刻执行：
- `add_listener(msg.name, client, client_ctrl_handler, epoll_fd)`

也就是说：
- `/run/mica/{client}.socket` 的创建条件不是“remote 已启动”
- 而是“这个 client 对象已经建立成功，并且可以接受后续控制命令”

所以 `{client}.socket` 的出现，只能证明：
- create 阶段完成
不能证明：
- start/service 通路已经正常

## 9. create 失败常见会卡在哪几层

### 9.1 控制面前置失败
发生在：
- 配置文件解析
- `mica-create.socket` 不存在
- create 消息未送达 `micad`

### 9.2 create 消息校验失败
发生在：
- `check_create_msg()`
- 镜像文件不存在
- client 名称重复

### 9.3 `mica_client` 初始化失败
发生在：
- `calloc()` 失败
- `init_mica_client()` 失败

### 9.4 pedestal 绑定失败
发生在：
- `create_client()` 选不出合法 `remoteproc_ops`
- `remoteproc_init()` 失败
- 对应 pedestal 的 `.init` 失败

所以 create 失败时，应该先区分：
- 是控制面没到 `mica_create()`
- 还是已经进入 `create_client()`，但 pedestal 初始化失败

## 10. remove 对 create 阶段对象的依赖关系

`mica_remove()` 的清理目标，本质上就是 create 阶段建立起来的这些对象：
- `client->node` 在 `g_client_list` 中的挂接
- `client->rproc`
- `client` 自身的各类状态
- 以及控制面上的 `{client}.socket`

所以如果 create 阶段结构没建对，后面的 remove 往往也会异常。
这也是为什么 lifecycle 不能只看 start，而必须把 create 和 remove 成对理解。

## 11. 建议阅读顺序

如果你要理解 create，建议顺序如下：
1. `mica/micactl/mica.py` 中 `send_create_msg()`
2. `mica/micad/socket_listener.c` 中 `create_mica_client()` 和 `init_mica_client()`
3. `library/mica/mica.c: mica_create()`
4. `library/remoteproc/remoteproc_core.c: create_client()`
5. 目标 pedestal 对应的 `library/remoteproc/*_rproc.c` 中 `.init`
6. 再回看：
   - `lifecycle-overview.md`
   - `lifecycle-start.md`
   - `lifecycle-stop.md`
   - `lifecycle-remove.md`
