# client 侧模块交互

## 1. 文档目标

这篇文档专门解释 RTOS/client 侧几个关键模块之间如何配合，尤其是：
- 用户输入的 `mica_config` 怎样进入 `libmica`
- `libmica` 怎样规定初始化顺序、service 创建顺序和 receiver 主链
- pedestal hooks 怎样决定 IRQ、shared memory、notify 与 RPMsg backend 的具体落地方式

它不重复 lifecycle、communication 或 pedestal 的底层机制细节，只解释 RTOS/client 侧的模块关系、主链路与阅读分流。

## 2. RTOS/client 侧的交互关系

RTOS/client 侧的关键模块关系如下：

1. client OS 用户代码
   - 负责准备 `struct mica_config`
   - 负责在合适时机调用 `mica_init()` 与 `mica_create_all_services()`

2. `rtos/libmica/src/mica_init.c`
   - `libmica` 初始化入口
   - 负责保存配置、选择 pedestal、按顺序调用 pedestal 初始化 hooks

3. `rtos/libmica/src/pedestals/*.c`
   - pedestal 实现层
   - 负责把共享内存、中断、notify、resource table、OpenAMP/RPMsg 后端真正建起来

4. `rtos/libmica/src/mica_service.c`
   - service 编排入口
   - 负责 receiver thread、TTY/UMT service thread 与 ready 状态的组织

5. `rtos/libmica/src/services/*.c`
   - service runtime 层
   - 负责 endpoint、callback、线程循环、请求/响应或数据收发逻辑

这篇文档聚焦以下内容：
- 用户输入怎样变成 `libmica` 可用的运行上下文
- `libmica` 怎样把 pedestal 与 service 层接起来
- 哪些问题还属于 RTOS/client 侧模块编排，哪些问题已经下沉到 communication 或 pedestals

## 3. 配置输入与初始化主链

### 3.1 用户输入

RTOS/client 侧最重要的输入入口是：

- `mica_init(struct mica_config *config)`

用户提供的 `struct mica_config` 至少包含：

- `shm_base_addr`
  - 共享内存基地址

- `shm_size`
  - 共享内存大小

- `ipc_irq_num`
  - IPC 中断号

- `ipc_irq_base`
  - pedestal 需要的中断寄存器基址

- `sys_ops`
  - client OS 提供的系统相关回调

从模块交互角度看，这一层的职责是：
- 用户负责提供平台相关输入
- `libmica` 不替用户推导这些平台参数

### 3.2 `libmica` 的初始化编排

`mica_init()` 自身并不直接实现 IRQ 或 RPMsg 机制。

它的职责包括：

1. 校验输入配置是否为空
2. 检查当前是否已经初始化过
3. 保存 `g_mica_config` 与 `g_mica_sys_ops`
4. 调用 `mica_get_ped_ops()` 获取当前 pedestal 的操作集
5. 调用 `ped_ops->init_irq(&g_mica_config)`
6. 调用 `ped_ops->init_rpmsg(&g_mica_config)`
7. 在成功后标记 `initialized`

因此这里的分工很清楚：
- 用户负责输入配置
- `libmica` 负责规定初始化顺序
- pedestal 负责具体实现初始化动作

### 3.3 pedestal hooks 的作用

当前 pedestal 接口的关键 hooks 定义在 `struct mica_pedestal_ops` 中，包括：

- `init_irq`
  - 建立 IPC 中断接收路径

- `init_rpmsg`
  - 建立 OpenAMP / RPMsg backend

- `rcv_message`
  - 在 receiver thread 中处理底层接收事件

- `deinit`
  - 释放 backend 与相关资源

`libmica` 在这一层负责规定扩展点，不把所有平台差异写死在公共层里。

## 4. pedestal 对用户输入的消费方式

在 pedestal 实现中，用户给出的 `mica_config` 会影响以下几类行为：

### 4.1 IRQ 初始化

- `ipc_irq_num` 决定 pedestal 向哪个 IRQ 号注册 handler
- `hetero` 还会使用 `ipc_irq_base` 做中断寄存器的 clear、mask、unmask

因此 `libmica` 不定义具体中断控制方式，而是把控制细节留给 pedestal。

### 4.2 shared memory 与 resource table

- `shm_base_addr` 会被 pedestal 用作 shared memory 映射基础
- 同一块内存也承载 resource table 与 vring 相关数据
- pedestal 会据此建立 `metal_device`、`metal_io_region` 和后续 virtio/RPMsg 所需对象

因此共享内存地址虽然由用户输入，但怎样解释这块地址、怎样划分 resource table 与 vring，取决于 pedestal 与 OpenAMP 接入方式。

### 4.3 notify 与远端唤醒

- `baremetal` pedestal 通过 `mica_trigger_irq(config->ipc_irq_num)` 发 notify
- `hetero` pedestal 通过 `ipc_irq_base + IPC_INT_SET` 对寄存器写入来发 notify

“通知远端”在 `libmica` 的抽象层面表现为 hook 调用结果，真正的触发动作由 pedestal 决定。

## 5. service 创建与接收主链

### 5.1 `mica_create_all_services()` 的作用

在当前 `libmica` 代码里，`mica_init()` 只负责底层基础环境；真正把 RTOS/client 侧服务拉起来的是：

- `mica_create_all_services()`

它主要完成以下三项工作：

1. 初始化共享的线程属性
2. 启动 TTY service thread、receiver thread、UMT service thread
3. 等待 `MICA_SERVICE_UMT` ready 后返回

因此从模块关系上要明确区分：
- `mica_init()` 不等于服务已创建
- `mica_create_all_services()` 也不等于所有业务语义都已经闭合

### 5.2 receiver thread 的位置

receiver thread 是 RTOS/client 侧一个很关键的中间层。

它的工作不是直接理解每个 service 协议，而是循环调用：

- `ped_ops->rcv_message()`

这条链表示：
- receiver thread 归 `libmica` 编排
- 实际接收动作归 pedestal 实现
- 底层消息进入 OpenAMP / RPMsg 后，才继续流向各个 endpoint 与 service callback

如果问题表现为“RTOS/client 侧没有持续收到消息”，应优先检查以下位置：
- receiver thread 没拉起
- `rcv_message()` 没有正常推进
- pedestal 底层接收事件没有正确送进来

### 5.3 service ready 的语义

`mica_service_is_ready()` 负责查询各个 service 的 ready 状态。

它当前把 ready 判断分发给：

- `mica_rpc_is_ready()`
- `mica_tty_is_ready()`
- `mica_umt_is_ready()`

从模块交互角度看，这里的关键含义是：
- `libmica` 统一提供 ready 查询入口
- 具体 ready 状态由各自 service runtime 决定

因此“线程已创建”与“service ready”不是同一个概念。

## 6. `libmica` 与历史 RTOS 私有实现的边界

当前 RTOS/client 侧虽然已经有比较清晰的 `libmica` 主链，但不能直接把所有行为都当成已经完整收敛。

边界可以表述为：

- 初始化编排、pedestal hooks、receiver/service 组织已经有比较明确的公共结构
- 某些 service 细节、平台适配或历史能力，仍可能需要继续参考旧 RTOS 实现或 UniProton 参考代码

因此当你使用这篇文档判断模块关系时，需要同时区分两个问题：

- 当前问题是不是已经能在 `rtos/libmica` 主链中解释清楚
- 如果不能，缺口是在 service runtime，还是仍在平台/历史实现侧

## 7. 与 communication、pedestals、debugging 的边界

这篇文档只解释 RTOS/client 侧模块怎样接住配置、初始化 backend、拉起 service，不展开更低层机制。

分流边界如下：

- 当问题已经进入 RPMsg device、endpoint、name service 或 service bind 语义
  - 应转到 `../../mica-communication/references/openamp-rpmsg.md`

- 当问题已经进入 shared memory、notify、IRQ、resource table、platform notify 差异
  - 应转到 `../../mica-pedestals/references/*.md`

- 当问题表现为 service ready、service 不可见、消息收发异常
   - 应转到 `../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`

## 8. 常见观察点

如果问题首先表现为 RTOS/client 侧症状，优先看：

- 用户是否正确填充了 `mica_config`
- `mica_init()` 是否已经把 pedestal hooks 跑通
- pedestal 是否已经建立 IRQ 与 RPMsg backend
- `mica_create_all_services()` 是否已经把 receiver 与关键 service thread 拉起来
- ready 状态是否真的闭合，而不只是线程已经创建

## 9. 建议继续阅读

- `client-side-overview.md`
- `../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`
- `../../mica-communication/references/openamp-rpmsg.md`
- `../../mica-pedestals/references/*.md`
