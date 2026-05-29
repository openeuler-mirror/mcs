# Master 侧模块交互

## 1. 文档目标

这篇文档专门解释 Linux/master 侧几个关键模块之间如何配合，尤其是：
- `mica.py`、`micad`、生命周期编排层、服务展示层之间如何交互
- 用户看到的 create / start / stop / status / gdb 行为是怎样落到 Linux/master 侧模块上的
- 如果问题首先表现为 Linux/master 侧控制面异常，下一跳应该继续看哪一层

它不重复 lifecycle、communication 或 pedestal 的具体机制，只解释 Linux/master 侧的模块关系、控制流与阅读分流。

## 2. Linux/master 侧的交互关系

从 Linux/master 侧看，最关键的模块关系可以先概括为：

1. `mica/micactl/mica.py`
   - 用户态命令入口
   - 负责解析参数、连接 Unix socket、发送控制消息、接收返回结果

2. `mica/micad/main.c`
   - `micad` 主进程入口
   - 负责启动控制面 listener、处理 daemon 生命周期、等待退出信号

3. `mica/micad/socket_listener.c`
   - Linux/master 侧控制面核心
   - 负责创建 socket、维护 listener、统一监听 fd、分发 create 与 per-client 控制请求

4. `library/mica/mica.c`
   - Linux/master 生命周期 API 入口
   - 负责 create / start / stop / remove / status 这些动作的上层调用入口

5. `library/remoteproc/remoteproc_core.c`
   - 生命周期公共调度层
   - 负责把上层动作推进到 remoteproc 与后续桥接阶段

6. `library/rpmsg_device/` 与 service 相关代码
   - 负责 communication bridge 与 service 可见性后续路径

因此这篇文档的重点不是某一个进程或函数，而是：
- 控制命令如何进入 Linux/master 侧
- 命令进入后由哪些模块继续接手
- 哪些问题还属于控制面，哪些问题已经下沉到 lifecycle 或 communication

## 3. 两条主链

Linux/master 侧最常见的用户可见行为，通常可以归到两条主链。

### 3.1 控制命令链

控制命令链可以概括为：

1. `mica.py` 解析用户命令
2. `mica.py` 连接 `/run/mica` 下对应的 Unix socket
3. `micad` 的 listener 线程接收请求并完成分发
4. create 请求进入实例创建路径
5. per-client 请求进入 `start/stop/rm/set/status/gdb` 路径
6. 具体动作再继续进入 `library/mica/` 与 `library/remoteproc/` 的生命周期实现

这一条链的重点是：
- Linux/master 控制面负责接请求和分发请求
- lifecycle 负责真正推进实例状态变化

在这条链里，控制面使用的 socket 路径也很关键：
- 控制面目录固定为 `/run/mica`
- create 总入口 socket 是 `/run/mica/mica-create.socket`
- per-client 控制 socket 是 `/run/mica/{client}.socket`

这意味着：
- `mica create` 先进入固定总入口 socket
- 只有实例创建成功后，Linux/master 才会为该实例创建专属 `{client}.socket`
- 后续 `start/stop/rm/set/status/gdb` 都通过实例专属 socket 进入

### 3.2 状态与服务可见性链

状态与服务可见性链可以概括为：

1. `mica.py` 发起 `status` 查询
2. `micad` 控制面接收并进入状态查询路径
3. `mica_status()` / `show_client_status()` 生成实例状态信息
4. `mica_print_service()` 生成 Linux/master 侧可见的服务信息
5. 控制面把状态与服务信息拼接后返回给用户

这一条链的重点是：
- 用户看到的是 Linux/master 侧收集和组织后的可见面
- 它不等于 remoteproc、RPMsg 或 RTOS/client 侧的全部真实状态

## 4. 控制面结构

### 4.1 socket 分层

Linux/master 控制面至少有两类 socket：

- create 总入口 socket
  - 路径是 `/run/mica/mica-create.socket`
  - 用于接收 `mica create`

- per-client 控制 socket
  - 路径是 `/run/mica/{client}.socket`
  - 用于接收 `start/stop/rm/set/status/gdb`

这两级拆分的根本原因是：
- create 之前还没有现成的 client 对象
- 只有实例创建成功后，Linux/master 才能进入 per-client 控制阶段

从模块交互角度看，这也说明控制面天然分成两级：
- 一级负责“把实例创建出来”
- 二级负责“对已经存在的实例做控制与查询”

### 4.2 listener 与分发

`micad` 主线程本身不直接处理请求。

更准确地说：
- 主线程负责启动 listener
- listener 线程负责 `epoll` 监听和回调分发
- create socket 与 per-client socket 共用监听框架，但进入不同的处理回调

因此，如果问题表现为“命令发了但 `micad` 没响应”，通常优先怀疑的是：
- socket 是否存在
- listener 是否已经启动
- 对应回调是否真的被触发

## 5. 与 lifecycle 和 communication 的边界

这篇文档只解释 Linux/master 侧模块怎样接住和分发动作，不展开下层机制。

更准确地说：

- 当问题已经进入 `mica_create()` / `mica_start()` / `mica_stop()` / `mica_remove()` 的具体阶段推进
  - 应转到 `../../mica-lifecycle/references/lifecycle-overview.md`

- 当问题已经进入 `create_rpmsg_device()`、`mica_ns_bind_cb()`、service bind 或 endpoint ready
  - 应转到 `../../mica-communication/references/openamp-rpmsg.md`

- 当问题表现为 Linux/master 侧状态与服务可见面异常
  - 先看 `master-status-management.md`

## 6. 常见观察点

如果问题首先表现为 Linux/master 侧症状，优先看：

- `mica.py` 是否发到了正确 socket
- `micad` listener 是否已经建立并开始监听
- create 请求与 per-client 请求是否进入了正确分支
- `status` 查询结果是否只是可见面异常，而不是底层状态本身异常

## 7. 建议继续阅读

- `master-side-overview.md`
- `master-status-management.md`
- `../../mica-lifecycle/references/lifecycle-overview.md`
- `../../mica-communication/references/openamp-rpmsg.md`
