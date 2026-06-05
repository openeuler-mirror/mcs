# MICA 实例生命周期

## 1. 文档目标

这篇文档不聚焦某一个命令，而是给出 MICA 生命周期的整体代码结构。

它主要回答：
- `create/start/status/stop/remove` 这些动作各自落在哪些代码层
- 哪些文件是生命周期公共骨架，哪些是某一步骤的专属实现
- Linux/master、pedestal-specific remoteproc、RPMsg 设备建立之间的层次关系是什么

如果一个 agent 要修改生命周期逻辑、梳理架构、或者判断某个问题究竟属于 create/start/stop/remove 哪个层级，应该先读这篇，再进入更细的单篇文档。

## 2. 生命周期不是单条函数，而是分层结构

从代码组织上看，MICA 生命周期至少分成四层：

1. API 入口层
   - 文件：`library/mica/mica.c`
   - 作用：对外暴露 `mica_create()`、`mica_start()`、`mica_stop()`、`mica_remove()`、`mica_status()`

2. 生命周期公共调度层
   - 文件：`library/remoteproc/remoteproc_core.c`
   - 作用：
     - 选择 pedestal 对应的 `remoteproc_ops`
     - 提供 `create_client()`、`load_client_image()`、`start_client()`、`stop_client()`、`destory_client()`、`show_client_status()`
     - 把上层 API 串到 OpenAMP remoteproc 抽象上

3. pedestal-specific 实现层
   - 文件：`library/remoteproc/*_rproc.c`
   - 当前主要包括：
     - `baremetal_rproc.c`
     - `jailhouse_rproc.c`
     - `xen_rproc.c`
     - `riscv_rproc.c`（当前 hetero/RISC-V 对应实现）
   - 作用：
     - 真正实现 `.init/.config/.start/.mmap/.notify/.shutdown/.remove`
     - 把不同部署底座的共享内存、通知、中断、resource table、远端启动方式落地

4. lifecycle 到 communication 的桥接层
   - 文件：`library/rpmsg_device/rpmsg_vdev.c`
   - 作用：
     - 在生命周期推进到 remoteproc 已 ready/running 后，建立 OpenAMP virtio/rpmsg device
     - 为通信服务层提供 `client->rdev` 这样的基础设施

因此，`mica start` 只是生命周期中的一个阶段；而 `rpmsg_vdev.c` 虽然常在 start 阶段被调用，但它本质上是“生命周期进入通信阶段”的桥，不应被误解成 start 专属文件。

## 3. 各命令在生命周期框架中的位置

### 3.1 create
主入口：
- `library/mica/mica.c: mica_create()`

公共调度层：
- `library/remoteproc/remoteproc_core.c: create_client()`

核心作用：
- 根据 `client->ped` 选择对应 `remoteproc_ops`
- `remoteproc_init(&client->rproc, ops, client)`
- 将实例接入 `g_client_list`
- 初始化 `client->services` 链表

### 3.2 start
主入口：
- `library/mica/mica.c: mica_start()`

公共调度层：
- `load_client_image()`
- `start_client()`

桥接层：
- `create_rpmsg_device()`

这一阶段既推进 remoteproc 生命周期，也把后续 RPMsg 通信基础设施接起来。

### 3.3 status
主入口：
- `library/mica/mica.c: mica_status()`

公共调度层：
- `show_client_status()`

补充显示层：
- `mica_print_service()` / `print_device_of_service()`

status 既依赖 `remoteproc` 状态，也依赖当前服务注册与设备显示逻辑。

### 3.4 stop
主入口：
- `library/mica/mica.c: mica_stop()`

涉及层次：
- `remoteproc_stop(rproc)`
- `mica_unregister_all_services(client)`
- `release_rpmsg_device(client)`
- `destroy_rbuf_device(client)`（如果 debug 打开）
- `stop_client(client)` -> `remoteproc_shutdown(&client->rproc)`

也就是说，stop 同时处理：
- remoteproc 生命周期回退
- service 清理
- rpmsg/virtio 清理
- debug 旁路清理

### 3.5 remove
主入口：
- `library/mica/mica.c: mica_remove()`

公共调度层：
- `destory_client(client)`

核心语义：
- 必要时先 stop
- 清理 gdb 相关线程
- 最终 `remoteproc_remove(&client->rproc)`
- 把 client 从公共结构中摘除

## 4. 生命周期中的公共状态载体

生命周期的核心状态主要挂在：
- `struct mica_client`
- `struct remoteproc`

其中 `struct mica_client` 比较关键的字段包括：
- `ped`
- `ped_setup`
- `ped_ops`
- `rproc`
- `rdev`
- `services`
- `rbuf_dev`
- `debug`

可以把它理解为：
- `mica_client` 是 MICA 生命周期对象
- `remoteproc` 是 OpenAMP 生命周期对象
- `rdev` 是通信设备对象
- `services` 是 Linux 侧已注册服务对象集合

所以不同阶段的问题，经常可以通过观察这几个字段来快速定位：
- `rproc.state`
- `rproc.rsc_table`
- `rdev`
- `services`
- `debug` / `rbuf_dev`

## 5. 生命周期代码定位

### 5.1 create 阶段

主要位置：
- `mica/micad/socket_listener.c`
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`

关键链路：
- micad 收到 create 请求
- 校验配置和镜像路径
- 创建 `mica_client`
- `mica_create()` -> `create_client()`
- `remoteproc_init()`

### 5.2 start 阶段

主要位置：
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`

关键链路：
- `load_client_image()`
- `remoteproc_config()`
- `remoteproc_load()`
- `remoteproc_start()`
- `create_rpmsg_device()`

### 5.3 service ready 阶段

主要位置：
- `rtos/libmica/src/mica_init.c`
- `rtos/libmica/src/mica_service.c`
- `rtos/libmica/src/services/`

关键链路：
- `mica_init()`
- `mica_create_all_services()`
- `rpmsg_create_ept()`
- `mica_service_is_ready()`

### 5.4 stop/remove 阶段

主要位置：
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`

关键链路：
- `mica_stop()`
- `mica_unregister_all_services()`
- `release_rpmsg_device()`
- `stop_client()` / `remoteproc_shutdown()`
- `destory_client()` / `remoteproc_remove()`

## 6. 生命周期专题文档的分工

这篇 `lifecycle-overview.md` 负责的是生命周期总框架：
- 哪些文件属于生命周期总入口
- 哪些文件属于 pedestal 实现层
- 哪些文件属于 communication bridge 层
- create/start/status/stop/remove 各自占据哪一段生命周期框架

在此基础上，可以再进入更细的专题文档。例如：
- `lifecycle-create.md`：聚焦 create 过程
- `lifecycle-start.md`：聚焦 start 过程
- `lifecycle-stop.md`：聚焦 stop 过程
- `lifecycle-remove.md`：聚焦 remove 过程

也就是说，这篇负责“总框架”，而具体动作分别拆到 create/start/stop/remove 单独专题。

## 7. 不同 pedestal 在生命周期中的进入点

从公共调度层进入不同 pedestal 的关键点在：
- `library/remoteproc/remoteproc_core.c: create_client()`

当前选择逻辑是：
- `BARE_METAL` -> `rproc_bare_metal_ops`
- `JAILHOUSE` -> `rproc_jailhouse_ops`
- `XEN` -> `rproc_xen_ops`
- `HETERO && cpu_str == "riscv"` -> `rproc_riscv_ops`

这意味着：
- pedestal 差异在 create 阶段就已经决定
- 之后的 `remoteproc_config()`、`remoteproc_start()`、`remoteproc_shutdown()` 等，都会落到对应 ops
- 所以后面所有 lifecycle 行为分析，都必须记得先确认 `client->ped` 和 `client->ped_setup.cpu_str`

## 8. lifecycle 与 communication 的边界

生命周期与通信不是完全分开的两套系统，而是前后衔接关系。

可以这样理解：
- create：创建生命周期对象
- start：把生命周期推进到 remote 已 ready/running
- create_rpmsg_device：把生命周期对象接到通信基础设施
- service register/bind：在通信基础设施之上继续建立具体服务

因此：
- `create_rpmsg_device()` 不只是 communication 话题
- 也是 lifecycle 跨入 communication 的边界点

这也是为什么很多“服务没起来”的问题，既不能只看 lifecycle，也不能只看 service 层。

## 9. 常见误区

### 9.1 误区：`rpmsg_vdev.c` 只是通信实现
也不完全对。
它确实服务于通信，但代码上它承担的是 lifecycle 到 communication 的桥接角色。

### 9.2 误区：只看 `library/mica/mica.c` 就够了
不够。
`mica.c` 只是 API 编排层，真正的 pedestal 落地、共享内存、通知、RPMsg 设备建立，都在更下层。

## 10. 建议阅读顺序

如果你要理解整个生命周期，建议顺序如下：
1. `library/include/mica/mica.h`
2. `library/include/mica/mica_client.h`
3. `library/mica/mica.c`
4. `library/remoteproc/remoteproc_core.c`
5. 当前目标 pedestal 对应的 `library/remoteproc/*_rproc.c`
6. `library/rpmsg_device/rpmsg_vdev.c`
7. 再进入具体专题文档：
   - `lifecycle-start.md`
   - `remoteproc-state-mapping.md`
   - `../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
   - `../../mica-communication/references/communication-overview.md`
