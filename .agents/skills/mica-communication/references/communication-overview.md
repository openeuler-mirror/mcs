# 通信概览

## 1. 文档目标

这篇文档只负责给出 MICA 通信面的整体地图，回答三个问题：
- 通信链总体分成哪几层
- 一个服务是沿着什么主链挂到通信面上的
- 需要继续下钻时，下一篇应该看哪份文档

具体机制细节不在这里展开：
- RPMsg / name service / endpoint 机制见 `openamp-rpmsg.md`
- shared memory / notify / vring 基础见 `transport-foundation.md`
- TTY / UMT / RPC / GDB 的具体行为见 `services/*.md`
- 服务就绪与排障判断见 `../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`

## 2. 通信分层模型

从整体上看，MICA 的通信面可以分成四层：

1. lifecycle 层
   - 决定实例何时启动，以及通信底座何时开始建立

2. OpenAMP/RPMsg 层
   - 决定 Linux/master 与 RTOS/client 之间是否已经具备可用的消息传输通道

3. MICA service 层
   - 决定远端发布的服务是否被识别并绑定为本地 service 对象

4. service runtime 层
   - 决定 TTY、UMT、RPC、GDB 这些具体服务是否真正进入可用状态

这一分层的核心意义是：
- transport ready 不等于 service ready
- service ready 也不等于具体业务能力已经可用

## 3. 通信建立主链

从 Linux/master 侧看，最典型的通信建立顺序可以概括为：

1. `mica start` 触发生命周期启动
2. Linux/master 建立 remoteproc、virtio 与 RPMsg device
3. RTOS/client 创建并发布自己的 RPMsg service
4. Linux/master 收到远端服务发现事件
5. MICA 按 service 模型把远端服务绑定到本地 service 对象
6. 各个 service 建立自己的运行时资源
7. TTY、UMT、RPC、GDB 分别进入各自的可用状态

这条主链在 service bind 之前大体共用；在 service bind 之后，不同服务会进入各自的运行时模型。

## 4. 服务分类

当前最关键的几类服务可以先按下面方式理解：

### 4.1 TTY

TTY 是最直观的基础通信观测窗，用来判断字符交互链是否已经建立。

### 4.2 UMT

UMT 是一种“RPMsg 控制面 + 共享内存数据面”的消息传输服务。

### 4.3 RPC

RPC 用来把 Linux/master 一侧的能力包装成 RTOS/client 侧看起来像本地调用的接口。

### 4.4 GDB

GDB 是面向调试转发的专用服务，而不是普通业务消息服务。

## 5. 服务代码定位

### 5.1 Linux 侧服务可见性

主要入口：
- `mica/micad/socket_listener.c`
- `library/include/mica/mica_client.h`
- `library/mica/mica.c`

关键点：
- `struct mica_service`
- `mica_register_service()`
- `mica_unregister_all_services()`
- `mica_print_service()`

### 5.2 TTY 服务

RTOS 侧主要位置：
- `rtos/libmica/src/services/tty_service.c`

关键点：
- `RPMSG_TTY_EPT_NAME = "rpmsg-tty"`
- `rpmsg_create_ept()`
- `mica_tty_is_ready()`
- shell 回调 `shell_cmd_handler`

### 5.3 UMT 服务

RTOS 侧主要位置：
- `rtos/libmica/src/services/umt_service.c`

关键点：
- `RPMSG_UMT_EPT_NAME = "rpmsg-umt"`
- `mica_umt_is_ready()`
- `mica_send_data()`
- callback / passive receive 两种接收路径

### 5.4 RPC 服务

主要位置：
- `rtos/libmica/src/mica_service.c`
- RPC service 相关源码

关键点：
- `SUPPORT_RPC`
- `MICA_SERVICE_RPC`

### 5.5 底层共同依赖

- endpoint 创建：`rpmsg_create_ept()`
- endpoint ready：`is_rpmsg_ept_ready()`
- 收包缓冲：`rpmsg_hold_rx_buffer()` / `rpmsg_release_rx_buffer()`
- 真正消息路径：OpenAMP RPMsg 层

## 6. 阅读路径

如果已经明确想看哪一层，建议直接进入对应文档：

- 想看 RPMsg、name service、endpoint 机制：`openamp-rpmsg.md`
- 想看 shared memory、notify、vring 基础：`transport-foundation.md`
- 想看 TTY：`services/tty-service.md`
- 想看 UMT：`services/umt-service.md`
- 想看 RPC：`services/rpc-service.md`
- 想看 GDB：`services/gdb-service.md`
- 想看服务 ready 与排障：`../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`

## 7. 建议继续阅读

- `transport-foundation.md`
- `openamp-rpmsg.md`
- `services/tty-service.md`
- `services/umt-service.md`
- `services/rpc-service.md`
- `services/gdb-service.md`
- `../../mica-common-tasks/references/debugging-workflow/communication-diagnosis.md`
