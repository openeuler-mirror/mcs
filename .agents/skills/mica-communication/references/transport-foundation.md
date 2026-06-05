# 通信底座

## 1. 文档目标

这篇文档专门解释：在 MICA 通信链里，为什么共享内存、通知/中断、vring 推进这些更底层前提没有成立时，后面的 virtio、RPMsg device、name service、TTY/RPC/UMT 都不可能真正建立。

它主要回答：
- 通信为什么不能只从 RPMsg service 往上看
- 共享内存、`.mmap`、`.notify`、poll/中断、vring 在通信链里各自承担什么角色
- 哪些症状说明问题还卡在 transport foundation，而不是 RPMsg service 层

## 2. 通信底座的基础认知

在当前系统里，通信建立顺序更准确地说是：

1. 共享内存与地址可访问性成立
2. 通知/中断闭环成立
3. vring 能被两边真正推进
4. virtio device / RPMsg device 成立
5. name service / endpoint / service bind 成立
6. 具体业务服务进入运行时可用态

所以看到 TTY/RPC/UMT 没 ready 时，不要立刻假设问题就在服务代码；更底层的 transport foundation 失败，足以让上层全部失效。

## 3. transport foundation 在当前仓里主要落在哪些位置

从 Linux/master 侧看，这层主要落在：
- `library/remoteproc/*_rproc.c`
- `library/remoteproc/remoteproc_core.c`
- `library/rpmsg_device/rpmsg_vdev.c`
- `library/memory/shm_pool.c`

从 RTOS/client 侧看，这层主要落在：
- `rtos/libmica/src/pedestals/baremetal.c`
- `rtos/libmica/src/pedestals/hetero.c`
- 对应 RTOS 的 OpenAMP/libmetal 初始化路径

从更下层机制上看，它依赖：
- OpenAMP `remoteproc`
- OpenAMP `remoteproc_virtio`
- libmetal `metal_io_region`

## 4. transport foundation 的组成部分

### 4.1 共享内存布局

这是所有后续通信的第一前提。

至少要保证：
- `resource_table` 可访问
- vring 共享区已分配并映射
- RPMsg payload buffer pool 已分配
- 两边对共享区布局的解释一致

如果这里不成立，后续常见表现是：
- `remoteproc_create_virtio()` 起不来
- `rpmsg_init_vdev()` 起不来
- endpoint/service 根本无从建立

### 4.2 地址映射与 I/O 语义

共享内存“有地址”不等于“可被当前侧正确访问”。

这一层依赖：
- backend `.mmap`
- `remoteproc_mmap()`
- `metal_io_region`
- `metal_io_phys_to_virt()` / `metal_io_block_read()` / `metal_io_block_write()`

所以如果症状是：
- 地址打印看起来合理
- 但 payload/header/resource_table 读写结果不对

就说明问题仍可能卡在 transport foundation，而不是上层 RPMsg service。

### 4.3 通知与中断闭环

即使共享内存已经准备好，如果通知链不通，vring 也不会真正推进。

这层通常表现为：
- Linux/master 侧通过 backend `.notify` 发出事件
- KO / 平台层把它翻译成 IPI、evtchn、ivshmem event 等机制
- RTOS/client 侧收到通知并继续推进 OpenAMP/virtqueue
- 必要时再把通知回送到 Linux/master 一侧

所以“内存有了但消息不动”时，首先要怀疑的不是 service 逻辑，而是 kick/notify/poll/中断闭环。

### 4.4 vring 推进能力

分配了 vring，不等于 vring 已经能被两边正确推进。

至少还要满足：
- vring descriptor 数量和对齐合理
- `notifyid` 对得上
- 远端通知能被正确送回 `remoteproc_get_notification()`
- virtqueue 能真正收到 notification 并推进

这一步不通时，常见现象是：
- 看起来像 endpoint 没 ready
- 或 name service 根本不到
- 但真正根因还在更下层的 vring/notify 路径

## 5. 哪些症状通常说明问题还在这层

下面这些现象，优先怀疑 transport foundation：

- `create_rpmsg_device()` 失败
- `remoteproc_create_virtio()` 失败
- `rpmsg_init_vdev()` 失败
- `client->rdev` 为空
- NS 从未到达
- endpoint 看起来声明了，但两边都收不到真正消息
- 共享内存 payload/header/resource_table 读写异常
- 单边 kick 之后对端完全没反应

## 6. 这层和其他文档的边界

这篇文档主要关注：
- 共享内存
- 地址可访问性
- notify/中断
- vring 推进前提

不重点展开：
- RPMsg endpoint / NS / send / RX callback 细节
  - 看 `openamp-rpmsg.md`
- service bind / service ready / TTY/RPC/UMT 运行时行为
   - 看 `communication-overview.md`、`../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`、`services/*.md`
- pedestal-specific 平台差异细节
  - 看 `mica-pedestals`

## 7. 建议继续阅读

- `openamp-rpmsg.md`
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `../../mica-lifecycle/references/openamp-remoteproc.md`
