# 通信诊断

## 1. 文档目标

这篇文档专门处理 MICA 通信类调试问题，对应外层 `mica-communication` skill：
- 实例已经 `Running`，但服务还不能用
- 服务没有出现、出现不全，或者看起来 ready 但业务链路仍未闭合
- 不确定问题停在 transport、service bind，还是 service runtime
- RPMsg、shared memory、notify、cache 或地址转换导致通信链路不闭合

它只负责建立判断框架，不展开具体实现细节：
- RPMsg / name service / endpoint 机制见 `../../../mica-communication/references/openamp-rpmsg.md`
- TTY / UMT / RPC / GDB 的服务细节见 `../../../mica-communication/references/services/*.md`
- 边界分层与平台语义见 `boundary-diagnosis.md`

## 2. 服务就绪的层次模型

在 MICA 里，“服务 ready”不应该被当成单一状态，而应至少分成四层：

1. remoteproc running
   - 实例已经进入运行态

2. transport ready
   - Linux/master 与 RTOS/client 之间已经具备可用的 RPMsg 通信底座

3. service bind ready
   - 远端服务已经被发现，并成功绑定到本地 service 对象

4. service runtime ready
   - 该服务自己的线程、设备节点、共享内存、代理调用链或调试转发链已经进入可用状态

这一层次模型的核心意义是：
- `Running` 不等于 service ready
- endpoint ready 不等于 service runtime ready
- service bind 成功也不等于最终业务能力已经可用

## 3. 推荐排查顺序

通信诊断开始前，应先按 `debugging-overview.md` 的日志位置确认规则收集 Linux/master、kernel/backend 和 RTOS/client 侧日志。

如果主要问题是“实例在跑，但服务不可用”，建议先按下面顺序判断：

1. 先确认是否只是 lifecycle running，而非 transport ready
2. 再确认是否只是 transport ready，而非 service bind ready
3. 再确认是否只是 service bind ready，而非 service runtime ready
4. 最后再进入具体服务文档判断该服务自己的 ready 含义

结合当前代码路径，实际排查时通常可以按下面顺序继续下钻：

1. 先看实例状态
   - 是否已经 `Running`
   - 如果还没进入运行态，先回到启动问题排查

2. 再看 Linux/master 侧
   - `mica_start()` 是否已经执行到 RPMsg device 建立阶段
   - service 可见性与 bind 入口是否正常

3. 再看 RTOS/client 侧
   - `mica_init()` 是否成功
   - `mica_create_all_services()` 是否被调用
   - receiver 线程与关键 service 线程是否启动
   - `mica_service_is_ready()` 的结果是否符合预期

4. 最后再看底层 transport
   - RPMsg endpoint 是否真正建立
   - name service 是否正常传播
   - shared memory、notify 或中断路径是否工作正常

## 4. 四类关键服务的 ready 含义

不同服务的 ready 含义并不相同，调试时不要用同一标准套全部服务。

### 4.1 TTY

对 TTY 来说，真正有意义的 ready 不是单纯 endpoint 存在，而是字符交互链已经闭合。

典型判断重点是：
- `/dev/ttyRPMSGX` 是否已经出现
- `screen /dev/ttyRPMSGX` 后是否可以完成输入和回显交互

具体实现见：
- `../../../mica-communication/references/services/tty-service.md`

### 4.2 UMT

对 UMT 来说，真正有意义的 ready 不是单纯 RPMsg 可收发，而是控制面和数据面都已经闭合。

典型判断重点是：
- RPMsg 控制面是否已经建立
- copy-message/shared-memory 路径是否可用
- 用户态 `user_msg` 与 micad 的同步链是否成立

补充判断提示：
- `mica_create_all_services()` 会等待 UMT ready，因此 UMT ready 对整体 service 建立状态具有较强指示意义

具体实现见：
- `../../../mica-communication/references/services/umt-service.md`

### 4.3 RPC

对 RPC 来说，真正有意义的 ready 不是单纯 `rpmsg-rpc` endpoint 建立，而是代理调用链已经闭合。

典型判断重点是：
- 默认 endpoint 是否已注入 proxy 层
- request/response 配对是否成立
- 本地调用外观是否已经能稳定映射到远端执行与响应回传

补充判断提示：
- RPC 问题通常还要额外确认相关编译开关和 proxy 处理链是否已经使能

具体实现见：
- `../../../mica-communication/references/services/rpc-service.md`

### 4.4 GDB

对 GDB 来说，真正有意义的 ready 不是普通业务消息链存在，而是调试转发链已经闭合。

典型判断重点是：
- Linux/master 转发侧是否已建立
- RTOS/client gdbstub 是否进入可通信状态
- ringbuffer 或相关调试传输资源是否与两侧约定一致

具体实现见：
- `../../../mica-communication/references/services/gdb-service.md`

## 5. 共享内存与平台语义

通信问题通常先按 endpoint、name service、service ready 分层。只有在这些前提看起来成立但数据面仍异常时，才需要提高 shared memory、cache、地址转换和 I/O region 的怀疑优先级。

### 5.1 shared memory 访问语义

RPMsg 数据面不是简单的普通内存读写，OpenAMP 会通过 `metal_io_region` 这类对象描述共享内存访问语义。

典型落点包括：
- `client->shbuf_io`
- `rproc->rsc_io`
- `metal_io_block_read()`
- `metal_io_block_write()`

这类问题通常表现为：
- endpoint、NS、virtqueue 看起来已经建立，但 payload/header 解释不稳定
- vring、payload buffer 地址能打印出来，但消息链路仍然不闭合
- UMT 等共享内存数据面收发异常

### 5.2 phys/virt 转换语义

如果问题表现为地址日志看起来正确，但实际读写内容不对，需要确认地址是否真的落在正确的 I/O region，并且转换函数能闭环返回一致结果。

重点关注：
- `metal_io_phys_to_virt()`
- `metal_io_virt_to_phys()`
- `remoteproc_mmap()`
- backend `.mmap`

### 5.3 cache 语义

共享内存通信中的 cache 一致性不是附加优化，可能直接决定对端能否看到最新数据。

典型表现包括：
- 包已经发送但对端看到旧数据
- 对端写入后本端读到旧内容
- notify 正常、virtqueue 也在动，但数据面表现为半通不通

如果需要判断这类问题是否已经超出通信工作流，应转到：
- `boundary-diagnosis.md`

## 6. 常见误判

### 6.1 把 `Running` 当成服务 ready

`Running` 只说明生命周期进入运行态，不说明服务已经完成 bind，也不说明 service runtime 已可用。

### 6.2 把 endpoint ready 当成服务可用

endpoint ready 只能说明 transport 层已经进入某种可通信状态，不能替代 TTY 的交互闭合、UMT 的共享内存数据面、RPC 的代理调用配对或 GDB 的调试转发链。

### 6.3 把 service bind 成功当成业务能力已可用

service bind 解决的是“哪个本地 service 接手远端服务”，不是“具体业务逻辑已经跑通”。

### 6.4 把服务问题直接归因成单侧 bug

很多“服务没起来”其实是双边时序、资源约定、transport 状态和 service runtime 条件共同作用的结果。

### 6.5 把 receiver 线程异常误判成某个单独 service 失效

receiver 线程不工作时，很多服务都会表现成“已初始化但没反应”，因此不要过早把问题收窄到单个 service。

### 6.6 把数据面异常直接归因到 RPMsg 协议

如果 endpoint、NS、virtqueue 看起来都成立，但 payload/header 或共享内存内容异常，问题可能已经进入 shared memory、cache、地址映射或平台 I/O 语义。

## 7. 常见问题

以下问题来自开发板适配和使用过程中的高频通信故障。命中这些症状时，优先按本节处理。

### 7.1 RTOS 已完成 MICA 初始化，但 Linux 侧 TTY 设备未创建

症状：
- RTOS 侧已执行完 MICA 框架和服务初始化
- Linux 侧没有 `/dev/ttyRPMSGx`
- 或 `screen /dev/ttyRPMSGx` 后无反应

原因：
- TTY、RPC、UMT 和自定义服务都依赖 OpenAMP endpoint 握手
- endpoint 握手通常依赖两类条件：Linux/RTOS 两侧 endpoint 名称匹配，TX/RX 共享内存和中断双向可用

处理：
- 确认 Linux 侧和 RTOS 侧 endpoint 名称匹配，例如 `rpmsg-tty*`、`rpmsg-rpc`、`rpmsg-umt`
- 通过串口打印或内存打点确认 TX/RX 共享内存可双向访问
- 通过中断打点确认两侧 notify/IRQ 路径都能触发
- 如果共享内存或 IRQ 路径不确定，转到 `boundary-diagnosis.md`

### 7.2 `rproc_virtio_wait_remote_ready` 卡住

症状：
- RTOS 侧打点显示卡在 `rproc_virtio_wait_remote_ready`
- MICA/OpenAMP 初始化无法继续

原因：
- RTOS 正在等待 Linux 侧完成 RPMsg device 初始化
- 如果 Linux 侧未完成 RPMsg 初始化，或 resource table 状态没有按预期刷新，RTOS 会一直等待
- baremetal 通常直接使用 RTOS ELF 的 `.resource_table` 段
- jailhouse、xen、hetero 等底座可能会把 resource table 搬运到通信共享内存第一页，RTOS 侧必须使用正确地址等待 Linux 状态

处理：
- 确认 Linux 侧 `mica start` 是否已进入 RPMsg device 创建阶段
- 确认 resource table 状态字段是否被 Linux 更新
- 确认 RTOS 等待的是当前底座约定的 resource table 地址
- 若涉及 resource table 搬运或共享内存第一页布局，转到 `boundary-diagnosis.md`

### 7.3 MICA 通信初期未建立

症状：
- `mica start` 成功，但 `/dev/ttyRPMSGx` 未出现
- 无法直接通过 TTY 观察 RTOS 状态

处理：
- 优先使用 RTOS 串口输出确认 client OS 是否运行
- 串口不可用时，使用内存打点确认 RTOS 运行阶段
- 先验证 RTOS 开始运行，再验证共享内存与中断，最后验证 MICA/OpenAMP 初始化和 service ready

## 8. 阅读路径

如果已经知道问题属于哪类服务，建议直接继续阅读：

- TTY：`../../../mica-communication/references/services/tty-service.md`
- UMT：`../../../mica-communication/references/services/umt-service.md`
- RPC：`../../../mica-communication/references/services/rpc-service.md`
- GDB：`../../../mica-communication/references/services/gdb-service.md`

如果还不确定问题是不是已经跨到 OpenAMP/libmetal，继续看：

- `boundary-diagnosis.md`

## 9. 建议继续阅读

- `../../../mica-communication/references/communication-overview.md`
- `../../../mica-communication/references/openamp-rpmsg.md`
- `../../../mica-communication/references/services/tty-service.md`
- `../../../mica-communication/references/services/umt-service.md`
- `../../../mica-communication/references/services/rpc-service.md`
- `../../../mica-communication/references/services/gdb-service.md`
- `boundary-diagnosis.md`
