# mica remove 过程拆解

## 1. 文档目标

这篇文档专门解释 Linux/master 侧执行 `mica rm <name>` 时，MICA 如何把一个实例从“仍可被控制的生命周期对象”彻底移出系统，以及 remove 成功到底意味着什么。

它主要回答：
- `mica rm` 的控制面入口在哪里
- remove 为什么不是简单调用一次 `mica_remove()` 就结束
- `mica_remove()`、`destory_client()`、`free_listener_by_name()` 分别负责什么
- 谁负责 `remoteproc_remove()`
- 谁负责从 epoll 删除 fd
- 谁负责 unlink `{client}.socket`
- 谁负责最终 free `mica_client`
- remove 与 stop/create 的边界到底怎么划

如果一个 agent 需要分析下面这些问题，这篇文档应该先读：
- 为什么 rm 之后 socket 消失了
- 为什么 remove 之后实例不该再出现在控制面列表里
- 为什么 `mica_remove()` 本身没有 `free(client)`
- 为什么 remove 前要先判断是否需要 stop
- 为什么 remove 是 lifecycle 与 control-plane 双层清理

## 2. 涉及文件

与 `mica rm` 直接相关的文件主要有：
- `mica/micad/socket_listener.c`
  - `client_ctrl_handler()`
  - `free_listener_by_name()`
- `library/mica/mica.c`
  - `mica_remove()`
- `library/remoteproc/remoteproc_core.c`
  - `destory_client()`
- `library/remoteproc/*_rproc.c`
  - 各 pedestal 的 `.remove`
- `mica/micad/services/debug/mica_gdb_server.c`
  - remove 语义上与 gdb 线程收尾相关

remove 的代码路径必须拆成两层来看：
1. lifecycle/backend 清理
   - `mica_remove()`
   - `destory_client()`
   - `remoteproc_remove()`
2. control-plane 清理
   - `epoll_ctl(..., EPOLL_CTL_DEL, ...)`
   - `free_listener_by_name()`
   - `close/unlink/free`

如果只看其中一层，就会误判 remove 的真实完成条件。

## 3. remove 的控制面入口

在 `mica/micad/socket_listener.c` 中：
- `client_ctrl_handler(int epoll_fd, void *data)`
是 `{client}.socket` 的统一控制入口。

当收到 `rm` 时，主链如下：
1. `accept()` 接收控制连接
2. `recv()` 读取控制字符串
3. 匹配 `strncmp(msg, "rm", CTRL_MSG_SIZE) == 0`
4. `epoll_ctl(epoll_fd, EPOLL_CTL_DEL, unit->socket_fd, NULL)`
5. `mica_remove(unit->client)`
6. 发送 `MICA_MSG_SUCCESS`
7. `close(msg_fd)`
8. `free_listener_by_name(unit->name)`

这条链说明 remove 的执行顺序是：先清控制面监听，再清 lifecycle/backend，最后回到控制面完成对象释放。

## 4. remove 的第一步：先从 epoll 中摘掉 listener fd

在 rm 分支里，`mica_remove()` 之前先做：
- `epoll_ctl(epoll_fd, EPOLL_CTL_DEL, unit->socket_fd, NULL)`

这一步的含义是：
- 当前实例对应的控制 socket，不再继续被 micad 的 epoll 主循环监听

也就是说，remove 从一开始就已经进入“控制面摘除”阶段，而不是只做 backend 清理。

如果这一步失败：
- 代码会直接报错并跳到 `err`
- 不会继续执行 `mica_remove(unit->client)`

因此，rm 的成功前提之一是：
- listener fd 必须能先从 epoll 中删掉

这也是 remove 与 stop 的第一条硬边界：
- stop 不碰 epoll listener
- remove 一开始就处理 epoll listener

## 5. `mica_remove()` 主体作用

位置：`library/mica/mica.c`

`mica_remove()` 本身仍然很薄，主逻辑只有三段：
1. 如果 `rproc->state != RPROC_OFFLINE`，先 `mica_stop(client)`
2. 如果 `client->gdb_server_thread` 存在，`pthread_cancel()` + `pthread_join()`
3. `destory_client(client)`

这三段非常关键，因为它明确界定了 remove 的 library 层职责：
- 先保证实例不再处于运行态
- 再收掉 gdb 线程类附属对象
- 再把 client 从 remoteproc/lifecycle 结构里摘掉

但它没有做：
- `free(client)`
- `unlink(socket_path)`
- `close(unit->socket_fd)`
- `free(listen_unit)`

这说明 `mica_remove()` 只覆盖 library/lifecycle/backend 这一半 remove 路径。

## 6. remove 前的 stop 判定条件

`mica_remove()` 开头先取：
- `struct remoteproc *rproc = &client->rproc;`

然后判断：
- `if (rproc->state != RPROC_OFFLINE) mica_stop(client);`

这说明 remove 的设计前提是：
- 如果实例还没 offline，先用 stop 路径把运行态资源退掉
- 然后再进入最终对象移除

因此 remove 并不是独立于 stop 的另一套完整清理链，而是：
- stop + remove-finalize

这样设计有两个后果：

### 6.1 remove 自动继承 stop 的运行态清理
包括：
- `remoteproc_stop(rproc)`
- `mica_unregister_all_services(client)`
- `release_rpmsg_device(client)`
- `destroy_rbuf_device(client)`
- `remoteproc_shutdown(&client->rproc)`

### 6.2 remove 只在 stop 之后补做对象级清理
也就是：
- gdb thread 清理
- `destory_client()`
- 后续 listener/socket/free 清理

所以如果一个问题已经明显属于：
- service 没卸干净
- RPMsg device 没释放

那排查点通常应该先看 stop 链，而不是直接怪 remove。

## 7. gdb server thread 的 remove 阶段处理

在 `mica_remove()` 中：
- 如果 `client->gdb_server_thread` 非空
- 就 `pthread_cancel()`
- 然后 `pthread_join()`

这说明 gdb server thread 的语义不是 stop 层必然回收的普通运行态对象，而是：
- 更接近实例级附属线程
- 在 remove 阶段必须保证被完全收掉

这里的分工是：
- stop 主要回收通信/服务/debug 通道
- remove 还要额外保证实例级线程不再悬挂

这也是 remove 比 stop 更彻底的表现之一。

## 8. `destory_client(client)` 的真实职责

位置：`library/remoteproc/remoteproc_core.c`

`destory_client()` 的实现虽然很短，但语义非常重：
1. `metal_list_del(&client->node)`
2. `remoteproc_remove(&client->rproc)`

源代码里还专门写了注释：
- 为了让 `remoteproc_remove()` 看到更新后的列表
- 必须先把 node 删除

这说明 remove 阶段这里处理的是：
- 生命周期公共结构中的 client 摘除
- remoteproc backend 对象的 remove

### 8.1 `metal_list_del(&client->node)` 的语义
它说明：
- create 阶段加进 `g_client_list` 的 client
- 到 remove 阶段才真正从全局生命周期集合里摘掉

因此：
- stop 不会把 client 从 `g_client_list` 移除
- remove 才会

### 8.2 `remoteproc_remove(&client->rproc)` 的语义
这一步表示：
- remoteproc 对象本身进入最终 remove 阶段
- pedestal-specific `.remove` 会被调用

当前仓库里可见的实现入口包括：
- `library/remoteproc/baremetal_rproc.c: rproc_remove()`
- `library/remoteproc/jailhouse_rproc.c: rproc_remove()`
- `library/remoteproc/riscv_rproc.c: rproc_remove()`
- `library/remoteproc/xen_rproc.c: rproc_remove()`

所以 `destory_client()` 不是简单“删链表节点”，而是：
- lifecycle 公共层与 pedestal-specific backend 最终 remove 的汇合点

## 9. 真正 free `client` 的地方不在 `mica_remove()`

这是 remove 阶段最常见的误解点之一。

在 `mica/micad/socket_listener.c` 中：
- `free_listener_by_name(const char *name)`
才会做最终的对象释放。

它找到对应 `listen_unit` 后，会依次执行：
1. `metal_list_del(node)`
2. `free(unit->client)`
3. `close(unit->socket_fd)`
4. `unlink(unit->socket_path)`
5. `free(unit)`

这说明：
- `mica_remove()` 并不 free `client`
- `destory_client()` 也不 free `client`
- 真正的 `free(unit->client)` 发生在 control-plane 的 listener 清理函数里

这一分工需要与 `mica_remove()` 主体职责明确区分，否则容易误判：
- “为什么 `mica_remove()` 结束了，但代码里看不到 free(client)”

原因在于 `client` 的最终所有权与其绑定的 listener/control object 耦合在一起回收。

## 10. `{client}.socket` 是谁删除的

同样是在 `free_listener_by_name()` 中：
- `close(unit->socket_fd)`
- `unlink(unit->socket_path)`

所以必须明确：
- 删除 `/run/mica/{client}.socket` 的不是 `mica_remove()`
- 也不是 `destory_client()`
- 而是 `free_listener_by_name()`

remove 由两条路径共同完成：
1. library/lifecycle/backend 线
2. listener/socket/control-plane 线

因此 remove 完成需要两条路径都闭合。

## 11. remove 成功语义

### 11.1 remove 成功至少代表
- 该 listener fd 已从 epoll 中删除
- 如有必要，实例已先走完 stop 链
- gdb server thread 已被 cancel/join
- client 已从生命周期公共链表中摘除
- `remoteproc_remove()` 已执行
- `{client}.socket` 已被 close/unlink
- `listen_unit` 已释放
- `unit->client` 已释放

### 11.2 remove 成功不只是代表
- remote 没在运行

“没在运行”只覆盖 stop 语义的一部分。remove 还额外意味着：
- 该实例对应的控制对象已经不存在
- 该实例对应的生命周期对象也已经被销毁

## 12. remove 成功后，哪些对象不该再存在

remove 后通常不应再存在：
- `/run/mica/{client}.socket`
- 对应 `listen_unit`
- `unit->client`
- `g_client_list` 中该 client 节点
- 该 client 挂着的 `remoteproc` backend 对象
- 其上层运行态 service/rpmsg/debug 对象

也就是说，remove 成功后，它与 stop 后最根本的区别是：
- stop 后还能继续控制这个实例
- remove 后这个实例已经不再是 micad 当前管理对象

## 13. remove 与 create 的对应关系

从对象配对角度看，create 和 remove 是一对。

### 13.1 create 建立的主要对象
- `struct mica_client`
- `client->rproc` 初始化绑定
- `client->node` 挂入 `g_client_list`
- `client->services` 空链表初始化
- `listen_unit`
- `/run/mica/{client}.socket`
- epoll listener 绑定

### 13.2 remove 拆除的主要对象
- 从 epoll 中删 listener fd
- 如有必要先 stop 运行态
- 清 gdb thread
- `metal_list_del(&client->node)`
- `remoteproc_remove(&client->rproc)`
- `free(unit->client)`
- `close/unlink/free listener`

如果 create 阶段对象没建立完整，remove 阶段也可能表现异常；反过来，如果 remove 后还残留 socket、listener 或 client 指针，也往往说明 create/remove 的对象配对关系在某层断了。

## 14. remove 与 stop 的硬边界

### 14.1 stop 的职责
- 退运行态
- 卸服务
- 卸 RPMsg 设备
- 卸 debug 运行态对象
- shutdown backend

### 14.2 remove 的职责
- 必要时先 stop
- 清理实例级 gdb thread
- 从全局生命周期结构中删除 client
- 执行 `remoteproc_remove()`
- 从 epoll 删除监听
- 删除 socket 文件
- 最终 free client 和 listener

一句话概括：
- stop 结束后，实例“还存在，只是不运行”
- remove 结束后，实例“已经不存在于 micad 管理体系中”

## 15. 常见误区

### 15.1 误区：`mica_remove()` 已经负责全部 remove
不对。
它只负责 lifecycle/backend 这一半，最终 free client 和删 socket 在 `free_listener_by_name()`。

### 15.2 误区：remove 不依赖 stop
不对。
如果 `rproc->state != RPROC_OFFLINE`，remove 会先 stop。

### 15.3 误区：socket 消失就是 `mica_remove()` 做的
不对。
真正 unlink socket 的地方在 `free_listener_by_name()`。

### 15.4 误区：stop 和 remove 只是命令名字不同
不对。
二者处理的对象层级不同：
- stop 处理运行态
- remove 处理对象态

## 16. 调试 remove 问题时该看哪几层

如果 `mica rm` 看起来异常，建议按下面顺序排：

1. rm 是否真正进入 `client_ctrl_handler()` 的 rm 分支
2. `epoll_ctl(... EPOLL_CTL_DEL ...)` 是否成功
3. `mica_remove()` 是否真正被执行
4. 是否因为 `rproc->state != RPROC_OFFLINE` 先进入 stop 链
5. gdb thread 是否被正常 cancel/join
6. `destory_client()` 是否执行到了 `metal_list_del()` 和 `remoteproc_remove()`
7. `free_listener_by_name()` 是否成功执行到：
   - `free(unit->client)`
   - `close(unit->socket_fd)`
   - `unlink(unit->socket_path)`
   - `free(unit)`

如果问题表现为：
- rm 后服务没了，但 socket 还在

重点看：
- `epoll_ctl DEL` 后是否走到了 `free_listener_by_name()`

如果问题表现为：
- rm 后 socket 没了，但内存或 backend 状态仍异常

重点看：
- `mica_remove()`
- `destory_client()`
- 各 pedestal 的 `.remove`

## 17. 建议阅读顺序

如果你要理解 `mica rm`，建议顺序如下：
1. `mica/micad/socket_listener.c` 中 `client_ctrl_handler()` 的 rm 分支
2. `library/mica/mica.c: mica_remove()`
3. `library/mica/mica.c: mica_stop()`（因为 remove 可能先走它）
4. `library/remoteproc/remoteproc_core.c: destory_client()`
5. 当前 pedestal 对应的 `library/remoteproc/*_rproc.c: rproc_remove()`
6. `mica/micad/socket_listener.c: free_listener_by_name()`
7. 再回看：
   - `lifecycle-create.md`
   - `lifecycle-stop.md`
   - `lifecycle-overview.md`
