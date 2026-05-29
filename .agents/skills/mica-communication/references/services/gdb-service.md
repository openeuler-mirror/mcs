# GDB 服务说明

## 1. 文档目标

这篇文档专门解释 GDB 调试服务在当前 MICA 体系中的位置，以及为什么它虽然也依赖通信链路，但和 TTY、RPC、UMT 这类通用 RPMsg 服务并不是同一类实现模式。

它主要回答：
- Linux/master 侧当前已经具备哪些 GDB 转发能力
- RTOS/client 侧当前主要依赖哪类实现
- GDB 服务和普通 RPMsg service 的差异在哪里
- 调试服务链路建立时最应该看哪些代码位置

## 2. GDB 服务总体模型

GDB 服务和 TTY、RPC、UMT 的最大差异在于：
- 它不是单纯依赖一个普通 `rpmsg-xxx` service 名建立的业务服务
- 它更接近“Linux/master 转发模块 + RTOS/client gdbstub/ringbuffer”的联合调试链

在当前系统里，这条链至少包含三层：

1. Linux/master 侧调试服务层
   - gdb proxy server
   - message queue
   - shared memory/ringbuffer transfer module

2. MICA/OpenAMP 通信层
   - 实例启停
   - RPMsg TTY/RPC 以及 debug 相关 service 辅助能力
   - `RSC_VENDOR_RBUF_PAIR` 相关资源状态控制

3. RTOS/client 侧 gdbstub / ringbuffer 层
   - gdbstub 命令处理
   - ringbuffer 与 Linux 转发侧对接

因此 GDB 服务虽然属于通信体系的一部分，但它的实现重点更偏：
- 调试转发
- ringbuffer 资源
- GDB 远程协议支撑

## 3. Linux/master 侧现有实现

### 3.1 入口与核心文件

Linux/master 侧当前已有一条比较完整的 GDB 转发链，关键文件主要包括：
- `mica/micad/services/debug/mica_debug.c`
- `mica/micad/services/debug/mica_gdb_server.c`
- `mica/micad/services/debug/mica_debug_ring_buffer.c`
- `mica/micad/services/debug/mica_debug.h`

从这些文件可以看到，Linux/master 侧至少已经具备：
- message queue 创建与管理
- gdb proxy server socket 建立
- shared memory / ringbuffer transfer thread
- 与 client 重启、Ctrl-C、调试状态控制相关的辅助逻辑

### 3.2 `create_debug_service()` 的角色

`mica_debug.h` 对外暴露：
- `create_debug_service(struct mica_client *client)`

这说明 Linux/master 侧已经把 debug service 抽成一个明确入口，而不是零散拼装在控制流里。

它的职责更接近：
- 为 GDB 转发准备 message queue
- 启动 server loop
- 接好 ringbuffer / shared memory transfer 路径

### 3.3 `mica_gdb_server.c` 的转发主链

`mica_gdb_server.c` 里的关键能力包括：
- `start_proxy_server()`
- `recv_from_shared_mem_thread()`
- `send_to_shared_mem()`
- `recv_from_gdb()`
- `send_to_gdb()`

这说明 Linux/master 侧当前的 GDB 服务本质上是：
- 一端接 GDB 客户端 socket
- 一端接 message queue / shared memory transfer module
- 在两端之间做消息转发

因此它更像“调试转发服务”，而不是像 TTY 那样直接把用户输入绑定到一个 `/dev/ttyRPMSGX` 设备节点。

## 4. `RBUF` 资源与调试状态控制

### 4.1 `RSC_VENDOR_RBUF_PAIR` 的使用

`mica_gdb_server.c` 中可以看到：
- `find_rsc(rsc_table, RSC_VENDOR_RBUF_PAIR, 0)`

并结合：
- `RBUF_STATE_CPU_STOP`
- `RBUF_STATE_CTRL_C`

控制 remote 的停止、重启和 Ctrl-C 语义。

这说明 GDB 服务并不只依赖普通 RPMsg endpoint；它还直接依赖：
- resource table 中的 vendor ringbuffer 资源
- Linux/master 与 RTOS/client 对这块资源状态的共同约定

### 4.2 重启与 Ctrl-C 路径

当前 Linux/master 侧已经实现了：
- `restart_client()`
- `send_ctrl_c()`

这里可以看到：
- `mica_stop()` / `mica_start()`
- `create_debug_service()`
- `create_rpmsg_tty()`
- `create_rpmsg_rpc_service()`

这些动作会被重新串起来，说明当前 GDB 服务实际上依赖：
- debug service 本身
- TTY/RPC 等辅助通信能力
- ringbuffer 资源状态

## 5. RTOS/client 侧现有参考实现

### 5.1 当前 `libmica` 状态

当前 `rtos/libmica` 中并没有像 TTY/UMT 那样已经收敛好的 GDB 服务实现。

因此在当前阶段，GDB 服务如果要理解 RTOS/client 侧行为，主要还是需要参考现有 `UniProton` 实现。

### 5.2 `UniProton` 现有参考路径

当前可参考的关键路径包括：
- `src/component/gdbstub/`
- `src/component/mica/rpmsg_service.c`
- `doc/om_guide/gdbstub.md`

从 `gdbstub.md` 可以确认：
- 混合部署场景下，需要 Linux 侧转发模块在 gdbstub 与 gdb 之间转发消息
- 当前 MICA 已经集成了这部分转发功能
- `UniProton` 和 Linux 侧转发进程之间通过 ringbuffer 通讯

这说明 RTOS/client 侧当前的关键不只是“有一个 gdb service endpoint”，而是：
- gdbstub 本身
- ringbuffer 配置
- Linux 转发链是否和 RTOS gdbstub 约定一致

### 5.3 `rpmsg_service.c` 中的辅助通信能力

`UniProton` 当前 `src/component/mica/rpmsg_service.c` 里能看到：
- RPC/TTY/UMT 这些 service task
- `rpdev`
- endpoint ready 等待

这说明当前 RTOS/client 侧调试服务虽然有独立的 gdbstub/ringbuffer 实现，但普通 RPMsg service 仍可能作为辅助通信能力出现。

## 6. GDB 服务与普通 RPMsg service 的边界

和 TTY/RPC/UMT 相比，GDB 服务更适合被理解成：
- 调试转发服务
- 共享内存 / ringbuffer 协议服务

而不是：
- 一个简单的 `rpmsg-gdb` 业务 service

因此分析 GDB 相关问题时，重点通常不在：
- 某个单独 endpoint 名字有没有匹配到

而更常在：
- message queue 有没有建立
- ringbuffer 资源有没有对齐
- gdbstub 与 Linux 转发侧的契约是否一致
- Ctrl-C / restart / stop 状态控制是否生效

## 7. 调试 GDB 服务时最值得抓的观察点

### 7.1 Linux/master 侧
- `create_debug_service()` 是否被调用
- message queue 是否创建成功
- `start_proxy_server()` 是否成功启动
- GDB socket 与 shared memory transfer thread 是否都已建立
- `RSC_VENDOR_RBUF_PAIR` 是否能找到

### 7.2 RTOS/client 侧
- gdbstub 是否编译使能
- ringbuffer 配置是否正确
- RTOS 是否已进入 gdbstub 可通信状态

### 7.3 跨层边界
- Linux/master 转发链与 RTOS gdbstub 对 ringbuffer 的地址/方向约定是否一致
- `Ctrl-C` / restart / stop 时 `RBUF` 状态是否正确切换
- 问题是在普通通信底座，还是已经进入 GDB 专用转发链

## 8. 建议继续阅读

- `../communication-overview.md`
- `../transport-foundation.md`
- `../../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`
- `../../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`
