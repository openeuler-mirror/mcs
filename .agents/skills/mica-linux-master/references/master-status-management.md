# 状态与服务展示逻辑

## 1. 文档目标

这篇文档专门解释 Linux/master 侧的状态与服务可见性链，也就是：
- `mica status` 请求是怎样进入 Linux/master 侧控制面的
- 状态信息与服务信息分别由哪些函数生成
- 为什么用户看到的状态和服务列表，有时会与底层真实状态不完全一致

它关注的是 Linux/master 侧的可见面，而不是 lifecycle 或 RPMsg 机制本身。

## 2. 状态与服务可见性链

`mica status` 看到的内容不是凭空出现的，而是由控制面查询并拼接输出。

从 Linux/master 侧看，这条链可以概括为：

1. `mica.py` 遍历 `/run/mica` 下的实例 socket
2. 对每个 `/run/mica/{client}.socket` 发送 `status`
3. `micad` 控制面进入 `status` 请求处理路径
4. `show_status()` 组织状态查询结果
5. `mica_status()` / `show_client_status()` 生成实例状态信息
6. `mica_print_service()` 生成 Linux/master 侧可见的服务信息
7. 控制面把状态与服务信息拼接后返回给用户

这说明：
- `mica status` 不是问一个单独的“全局状态服务”
- 它是对每个实例分别查询，再由 Linux/master 侧控制面返回可见结果

## 3. 关键位置与接口

这条可见性链的关键位置主要在：
- `mica/micad/socket_listener.c`
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`

关键函数主要包括：
- `show_status()`
- `mica_status()`
- `show_client_status()`
- `mica_print_service()`

可以先这样理解它们的分工：
- `show_status()`
  - 控制面上的状态查询入口
- `mica_status()` / `show_client_status()`
  - 负责生成实例状态部分
- `mica_print_service()`
  - 负责生成 Linux/master 侧可见的服务部分

## 4. 状态与服务可见面的语义

Linux/master 侧状态与服务展示，至少包含两个不同层面的信息：

1. 实例状态
   - 更接近 lifecycle / remoteproc 的运行状态可见面

2. 服务可见性
   - 更接近 Linux/master 侧已经收集并展示出来的 service 信息

因此用户看到的输出，不应被简单理解为“底层所有状态的完整镜像”。

更准确地说：
- 状态部分反映的是 Linux/master 对实例阶段的判断
- 服务部分反映的是 Linux/master 当前可见、可展示的 service 信息

## 5. 常见现象与语义判断

### 5.1 `Running` 但服务为空

这通常说明：
- lifecycle 状态已经进入运行态
- 但 Linux/master 侧还没有拿到完整的服务可见性结果

这时不要只停在 status 输出本身，而应继续回溯：
- 服务注册链是否闭合
- RTOS/client 侧是否已经进入 ready
- communication 层是否已经完成 service bind

### 5.2 服务列表不对

如果状态看起来正常，但服务列表异常，优先怀疑：
- service 本身还没真正建好
- Linux/master 侧服务注册或展示链没有闭合
- 显示逻辑拿到的是不完整的 service 可见信息

### 5.3 名称或展示内容异常

如果状态和服务都有，但名称或展示格式不对，还要考虑控制面拼接与输出路径本身。

## 6. 阅读分流

如果问题更偏下层机制，建议继续看：

- lifecycle 状态问题：`../../mica-lifecycle/references/lifecycle-overview.md`
- service bind / endpoint / RPMsg 问题：`../../mica-communication/references/openamp-rpmsg.md`
- Linux/master 侧模块交互：`master-module-interaction.md`

## 7. 建议继续阅读

- `master-side-overview.md`
- `master-module-interaction.md`
- `../../mica-lifecycle/references/lifecycle-overview.md`
- `../../mica-communication/references/openamp-rpmsg.md`
