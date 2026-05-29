# mica stop 过程拆解

## 1. 文档目标

这篇文档专门解释 Linux/master 侧执行 `mica stop <name>` 时，MICA 到底会按什么顺序把一个已经运行过的实例退回去，以及 stop 成功究竟代表什么。

它主要回答：
- `mica stop` 的控制面入口在哪里
- `mica_stop()` 内部到底分几段
- 为什么 stop 不是单纯一次 `remoteproc_shutdown()`
- stop 时 service、RPMsg、debug 旁路、remoteproc backend 分别由谁清理
- stop 成功后，哪些对象还在，哪些对象已经不在
- stop 与 remove 的边界到底在哪里

如果一个 agent 需要分析下面这些问题，这篇文档应该先读：
- 为什么 `mica stop` 之后实例还在 `status` 列表里
- 为什么 stop 之后 `{client}.socket` 还在
- 为什么 stop 之后可以再次 start
- 为什么 stop 后 RPMsg 设备没了，但 client 还没被 remove
- 为什么 stop 没有真正释放 `mica_client`

## 2. 涉及文件

与 `mica stop` 直接相关的代码主要分布在这些文件：
- `mica/micad/socket_listener.c`
  - `client_ctrl_handler()`
  - stop/rm 命令的控制面入口
- `library/mica/mica.c`
  - `mica_stop()`
  - stop 的生命周期编排入口
- `library/remoteproc/remoteproc_core.c`
  - `stop_client()`
  - 最终下沉到 `remoteproc_shutdown()`
- `library/rpmsg_device/rpmsg_service.c`
  - `mica_unregister_all_services()`
- `library/rpmsg_device/rpmsg_vdev.c`
  - `release_rpmsg_device()`
- `library/rbuf_device/rbuf_dev.c`
  - `destroy_rbuf_device()`
- `library/remoteproc/*_rproc.c`
  - 各 pedestal 的 `.shutdown` 真正实现

因此，stop 不是单层动作，而是跨越：
- daemon/control-plane
- MICA lifecycle 编排层
- service 清理层
- RPMsg virtio 释放层
- pedestal-specific remoteproc shutdown 层

## 3. stop 的控制面入口

`mica stop <name>` 不是直接进 `library/mica/mica.c`，而是先经过 `micad` 的实例控制 socket。

在 `mica/micad/socket_listener.c` 中：
- `client_ctrl_handler(int epoll_fd, void *data)`
负责处理每个 `{client}.socket` 上收到的控制命令。

stop 分支非常直接：
1. `accept()` 接收控制连接
2. `recv()` 收到控制字符串
3. `strncmp(msg, "stop", CTRL_MSG_SIZE) == 0`
4. `mica_stop(unit->client)`
5. 返回 `MICA_MSG_SUCCESS`

也就是说，stop 命令的控制面语义是：
- 通过已存在的 `/run/mica/{client}.socket`
- 对对应 `listen_unit->client`
- 发起一次运行态回退

这里有个非常重要的边界：
- stop 不会移除 epoll listener
- stop 不会 unlink `{client}.socket`
- stop 不会 free `listen_unit`
- stop 不会 free `unit->client`

这些都不是 stop 的职责，而是 remove 的职责。

## 4. `mica_stop()` 主调用链

`library/mica/mica.c` 中的 `mica_stop()` 很短，但它决定了 stop 的资源回退顺序。

固定顺序如下：
1. `remoteproc_stop(rproc)`
2. `mica_unregister_all_services(client)`
3. `release_rpmsg_device(client)`
4. 如果 `client->debug` 为真，执行 `destroy_rbuf_device(client)`
5. `stop_client(client)`
6. `stop_client()` 再调用 `remoteproc_shutdown(&client->rproc)`

注意这个顺序非常关键。它不是：
- 先 shutdown backend，再清 service/rpmsg

而是：
- 先让 remoteproc 生命周期从运行态开始退
- 再清 Linux/master 侧挂接的服务对象
- 再拆 RPMsg virtio 设备
- 再拆 debug 环
- 最后做 pedestal-specific shutdown 与 backend 清理

因此，`mica_stop()` 的真实语义更接近：
- 把一个“已经运行并已接入通信层”的实例，从上到下拆回到“client 仍存在但不再运行”的状态

## 5. 第一步：`remoteproc_stop(rproc)` 的位置与含义

在 `mica_stop()` 开头，首先执行：
- `remoteproc_stop(rproc)`

这一步和后面的 `remoteproc_shutdown()` 很容易混淆，但它们不是一回事。

从当前仓库代码可以确认：
- `mica_stop()` 先显式调用 `remoteproc_stop(rproc)`
- 然后最后才通过 `stop_client()` 进入 `remoteproc_shutdown(&client->rproc)`

所以 stop 过程至少被拆成两层 remoteproc 语义：
1. `remoteproc_stop()`
   - 更接近“停止当前 remoteproc 运行阶段”
2. `remoteproc_shutdown()`
   - 更接近“完成 backend 与资源级清理”

即便底层 OpenAMP 或 pedestal 实现会决定二者实际差异有多大，在 MICA 这一层也必须把它们看成两个阶段，否则会误判 stop 的清理顺序。

## 6. 第二步：`mica_unregister_all_services(client)`

位置：`library/rpmsg_device/rpmsg_service.c`

这是 stop 过程中 Linux/master 侧 service 层的统一回收点。

它的主逻辑是：
1. 遍历 `client->services`
2. 对每个 `struct mica_service`：
   - 如果定义了 `svc->remove`
   - 就调用 `svc->remove(client, svc)`
3. 把该 service 节点从链表里删除
4. `free(svc)`

这说明 stop 对服务层的影响非常明确：
- 所有已注册到 `client->services` 的服务对象，都会在 stop 时被批量摘掉
- service 的私有清理，依赖每个服务自己的 `.remove`
- stop 之后，`client->services` 不应再保留之前那批运行态服务对象

从语义上看，这一步清理的是：
- TTY/RPC/UMT/debug 等“建立在通信通路之上的本地服务对象”

而不是：
- `mica_client` 本身
- `remoteproc` 本身
- 控制面 listener 本身

## 7. 第三步：`release_rpmsg_device(client)`

位置：`library/rpmsg_device/rpmsg_vdev.c`

这是 stop 过程中通信桥接层的统一释放点。

它的主链是：
1. 如果 `client->rdev == NULL`，直接返回
2. 通过 `metal_container_of(client->rdev, struct rpmsg_virtio_device, rdev)` 找回 `rpmsg_vdev`
3. `rpmsg_deinit_vdev(rpmsg_vdev)`
   - 注释里明确写了：`destroy all epts`
4. `remoteproc_remove_virtio(&client->rproc, rpmsg_vdev->vdev)`
5. `metal_free_memory(rpmsg_vdev)`
6. `client->rdev = NULL`

这一步很重要，因为它清楚定义了 stop 后 communication 基础设施的状态：
- RPMsg virtio device 会被释放
- endpoint 会被销毁
- `client->rdev` 会被清空

所以 stop 之后如果再看到：
- 服务没了
- RPMsg endpoint 没了
- `ttyRPMSG*` 不再可用

这不是异常，而是 stop 的正常结果。

同时也要反过来理解：
- stop 并不移除 `client`
- 但它确实移除了 client 挂着的 RPMsg 通信底座

## 8. 第四步：debug 旁路清理

在 `library/mica/mica.c` 中：
- 如果 `client->debug` 为真，会执行 `destroy_rbuf_device(client)`

这说明 debug 相关能力被视为：
- 运行态附属对象
- 不是 client 常驻对象

也就是说，debug ring buffer 通道和普通 RPMsg 服务一样，属于 stop 时应回收的运行态资源。

因此 stop 后：
- debug 通道不应继续被认为有效
- 如果之后重新 start，需要重新建立对应调试旁路

## 9. 第五步：`stop_client(client)` -> `remoteproc_shutdown()`

位置：`library/remoteproc/remoteproc_core.c`

`stop_client()` 自身非常薄：
- 如果 `client != NULL`
- 就调用 `remoteproc_shutdown(&client->rproc)`

因此真正的 backend/pedestal-specific 收尾，不在 `mica.c`，而在各自 `*_rproc.c` 的 `.shutdown`。

当前仓库里可以明确看到这些实现入口：
- `library/remoteproc/baremetal_rproc.c: rproc_shutdown()`
- `library/remoteproc/jailhouse_rproc.c: rproc_shutdown()`
- `library/remoteproc/riscv_rproc.c: rproc_shutdown()`
- `library/remoteproc/xen_rproc.c: rproc_shutdown()`

也就是说，`mica_stop()` 最后这一步的职责是：
- 把前面已经在 MICA 层拆掉的 service/rpmsg/debug 之下
- 再把 pedestal-specific 的 remoteproc/backend 资源也收掉

这通常会涉及：
- 共享内存映射或 mem 区域回收
- notifier / event / poll 相关清理
- 远端停止、下线或关闭
- rproc bitmap/state/backend 对象清空或回收

所以 stop 的完整含义必须包含一句话：
- 不是只停服务，而是最终还会进入 remoteproc backend shutdown

## 10. stop 成功后，哪些对象还在

这是 stop 最容易被误判的地方。

### 10.1 stop 成功后通常还在的对象
- `struct mica_client` 还在
- `client->rproc` 这个生命周期对象还在
- `client` 在 control-plane 上对应的 listener 还在
- `/run/mica/{client}.socket` 还在
- 该实例通常仍可继续接收 `status`、`start`、`rm` 等命令

### 10.2 stop 成功后通常已经不在的对象
- `client->services` 中原有运行态服务对象
- `client->rdev` 对应的 RPMsg device
- RPMsg endpoints
- debug rbuf device（若开启 debug）
- pedestal-specific 运行态 backend 资源

因此，stop 成功的语义不是：
- “实例彻底不存在了”

而是：
- “实例对象仍然存在，但运行态与通信态已经被拆掉了”

## 11. stop 后再次 start 的条件

因为 stop 并没有做 remove 那层工作。

它没有：
- `metal_list_del(&client->node)`
- `remoteproc_remove(&client->rproc)`
- `epoll_ctl(..., EPOLL_CTL_DEL, ...)`
- `unlink(unit->socket_path)`
- `free(unit->client)`

所以从生命周期设计上看：
- create 建 client/control object
- start 推进到 running + communication ready
- stop 回退 running/communication state
- remove 才彻底拆 client/control object

这也是为什么 stop 后还能再次 start：
- 因为 create 阶段建立的对象还在
- remove 阶段负责的销毁动作还没发生

## 12. stop 与 remove 的硬边界

stop 和 remove 非常容易混在一起，但它们的边界其实很清楚。

### 12.1 stop 的职责
- 让 remoteproc 从运行态回退
- 注销 services
- 释放 RPMsg virtio 设备
- 释放 debug 运行态对象
- 执行 backend shutdown

### 12.2 remove 的职责
- 如有必要，先 stop
- 清理 gdb server thread
- 从 `g_client_list` 删除 client 节点
- `remoteproc_remove(&client->rproc)`
- 从 epoll 删除 listener fd
- unlink `{client}.socket`
- free `listen_unit`
- free `unit->client`

因此要明确：
- stop 不负责销毁 client
- stop 不负责移除 socket
- stop 不负责把实例从全局列表中摘掉
- stop 是“退运行态”
- remove 才是“退对象态”

## 13. 常见误区

### 13.1 误区：stop 就等于 remove
不对。
stop 只拆运行态和通信态，不拆控制对象和生命周期对象。

### 13.2 误区：stop 后 socket 还在说明 stop 失败
不对。
`{client}.socket` 本来就属于 control-plane 对象，stop 不负责删它。

### 13.3 误区：stop 后还出现在 status 里说明实例没停
不对。
只要 client 还没 remove，就仍然可能出现在生命周期对象集合中。

### 13.4 误区：service 消失说明 client 被 remove 了
不对。
service 只是运行态挂接物；stop 就会把它们卸掉。

## 14. 调试 stop 问题时该看哪几层

如果 `mica stop` 看起来异常，建议按层次排：

1. 控制面是否真正进入 stop 分支
   - 看 `mica/micad/socket_listener.c: client_ctrl_handler()`
2. `mica_stop()` 是否完整执行到末尾
   - 看 `library/mica/mica.c`
3. service 是否真的被卸掉
   - 看 `mica_unregister_all_services()`
4. `client->rdev` 是否被释放并置空
   - 看 `release_rpmsg_device()`
5. debug 旁路是否已拆
   - 看 `destroy_rbuf_device()`
6. backend shutdown 是否成功
   - 看对应 pedestal 的 `*_rproc.c: rproc_shutdown()`

如果问题表现为：
- stop 后服务没了，但 remote 似乎还活着

重点看：
- `remoteproc_shutdown()` 以及 pedestal-specific `.shutdown`

如果问题表现为：
- stop 后无法再次 start

重点看：
- stop 是否把不该删的 create/control object 误删了
- 对应 pedestal 的 shutdown 是否把重启前提状态破坏了

## 15. 建议阅读顺序

如果你要理解 `mica stop`，建议顺序如下：
1. `mica/micad/socket_listener.c` 中 `client_ctrl_handler()` 的 stop 分支
2. `library/mica/mica.c: mica_stop()`
3. `library/rpmsg_device/rpmsg_service.c: mica_unregister_all_services()`
4. `library/rpmsg_device/rpmsg_vdev.c: release_rpmsg_device()`
5. `library/remoteproc/remoteproc_core.c: stop_client()`
6. 当前目标 pedestal 对应的 `library/remoteproc/*_rproc.c: rproc_shutdown()`
7. 再回看：
   - `lifecycle-overview.md`
   - `lifecycle-start.md`
   - `lifecycle-remove.md`
