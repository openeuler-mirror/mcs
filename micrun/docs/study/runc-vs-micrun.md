# Runc vs MicRun: Shim v2 架构对比

> 本文档持续更新，记录 runc 和 micrun 之间的所有架构差异
> 详细的技术实现请参考各专题文档

## 目录

- [一、基础架构](#一基础架构)
- [二、启动参数差异](#二启动参数差异)
- [三、进程模型差异](#三进程模型差异)
- [四、生命周期管理](#四生命周期管理)
- [五、资源管理](#五资源管理)
- [六、IO 处理](#六io-处理)
- [七、通信机制](#七通信机制)
- [八、快速对比表](#八快速对比表)
- [九、其他差异](#九其他差异)

---

## 一、基础架构

### 1.1 什么是 Shim v2

Containerd Shim v2 是 containerd 的容器运行时接口规范。每个 shim 实现：

1. **实现 containerd TaskService API** - 通过 TTRPC 提供 Create/Start/Delete/Kill/Exec 等接口
2. **管理容器生命周期** - 从创建到删除的全过程管理
3. **处理容器 IO** - stdin/stdout/stderr 的转发

### 1.2 双重执行模式设计

Shim v2 采用**双重执行模式**（Dual Execution Mode），同一个 shim 二进制根据参数不同执行不同行为：

| 执行模式 | 命令行参数 | 生命周期 | 主要行为 |
|---------|-----------|----------|----------|
| **start 子命令** | `shim -id xxx start` | 短期 | 创建守护进程后立即退出 |
| **delete 子命令** | `shim -id xxx delete` | 短期 | 清理资源后立即退出 |
| **守护进程模式** | `shim -id xxx` | 长期 | 运行 TTRPC 服务器，响应 API 调用 |

```
┌─────────────────────────────────────────────────────────────────┐
│                    Containerd                                   │
└────────────────────────┬────────────────────────────────────────┘
                         │ exec shim -id xxx start
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│              Shim (start 子命令, 短生命周期)                      │
│  1. fork+exec 创建自身为守护进程                                  │
│  2. 将 socket FD 传递给子进程                                     │
│  3. 输出 socket 地址到 stdout                                    │
│  4. 退出                                                        │
└────────────────────────┬────────────────────────────────────────┘
                         │ fork+exec
                         ▼
┌─────────────────────────────────────────────────────────────────┐
│              Shim (守护进程, 长期运行)                            │
│  1. 继承 socket FD                                               │
│  2. 启动 TTRPC 服务器                                            │
│  3. 等待 containerd 的 API 调用                                   │
└─────────────────────────────────────────────────────────────────┘
```

> **延伸阅读**：[Shim 开发笔记](shim-dev-notes.md) - 一次性命令与守护进程的详细实现

### 1.3 统一 API 调用机制

无论 shim 内部如何实现，containerd 通过统一的 TTRPC API 与之通信：

```go
type TaskService interface {
    Create(ctx context.Context, req *CreateRequest) (*CreateResponse, error)
    Start(ctx context.Context, req *StartRequest) (*StartResponse, error)
    Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error)
    Exec(ctx context.Context, req *ExecProcessRequest) (*ExecProcessResponse, error)
    ResizePty(ctx context.Context, req *ResizePtyRequest) (*ResizePtyResponse, error)
    State(ctx context.Context, req *StateRequest) (*StateResponse, error)
    Kill(ctx context.Context, req *KillRequest) (*KillResponse, error)
    CloseIO(ctx context.Context, req *CloseIORequest) (*CloseIOResponse, error)
    Update(ctx context.Context, req *UpdateTaskRequest) (*UpdateTaskResponse, error)
    Wait(ctx context.Context, req *WaitRequest) (*WaitResponse, error)
    Connect(ctx context.Context, req *ConnectRequest) (*ConnectResponse, error)
    Shutdown(ctx context.Context, req *ShutdownRequest) (*ShutdownResponse, error)
}
```

---

## 二、启动参数差异

### 2.1 参数对比表

MicRun 的 main 函数调用 shimv2.Run 时传递了三个与 runc shim 不同的参数：

```go
// micrun/main.go:26
shimv2.Run(ShimName, shim.New, noReaper, noSubreaper, setupLogger)
```

```go
// containerd/cmd/containerd-shim-runc-v2/main.go:30
shim.Run(context.Background(), manager.NewShimManager("io.containerd.runc.v2"))
```

| 参数 | runc shim | micrun | 原因 |
|------|-----------|--------|------|
| `noReaper` | `false` (默认) | `true` | micrun 没有子进程需要收割 |
| `noSubreaper` | `false` (默认) | `true` | micrun 不需要成为孤儿进程的父进程 |
| `NoSetupLogger` | `false` (默认) | `true` | micrun 使用自己的日志系统 |

### 2.2 差异原因分析

#### 2.2.1 noReaper：进程收割需求差异

**什么是 Reaper？**

Linux 中，父进程需要 `wait()` 子进程以回收其资源（收割僵尸进程）。Shim 作为容器进程的管理者，默认需要监听 `SIGCHLD` 信号并收割退出的子进程。

```go
// containerd/pkg/shim/shim_unix.go:41
if !config.NoReaper {
    smp = append(smp, unix.SIGCHLD)  // 监听 SIGCHLD
}
```

**Runc 的场景**：
```
shim (父进程)
  └─ runc (子进程)
       └─ 容器进程 (孙进程)
```
- 容器进程是 shim 的间接子进程
- Shim 需要设置 reaper 来收割这些进程

**MicRun 的场景**：
```
shim
  └─ (无子进程)
micad (独立守护进程)
  └─ RTOS (由 micad 管理)
```
- RTOS 不是 shim 的子进程
- 由 micad 守护进程管理 RTOS 生命周期
- Shim 通过 Unix socket 与 micad 通信

#### 2.2.2 noSubreaper：孤儿进程处理差异

**什么是 Subreaper？**

Linux 3.4+ 引入的 PR_SET_CHILD_SUBREAPER 功能。当进程设置为 subreaper 后，其子孙进程变成孤儿时，会被 reparent 到这个 subreaper，而不是 init 进程(PID 1)。

```go
// containerd/pkg/shim/shim.go:243
if !config.NoSubreaper {
    if err := subreaper(); err != nil {
        return err
    }
}
```

**Runc 的场景**：
- 容器内可能产生多级进程
- 需要确保孤儿进程被 shim 接管
- 便于正确收集退出状态

**MicRun 的场景**：
- RTOS 不像 Linux 那样有多进程模型
- 不存在进程树的 reparent 问题
- 不需要 subreaper 功能

#### 2.2.3 setupLogger：日志系统差异

**Containerd 默认日志系统**

使用 logrus 将日志输出到 containerd 提供的 FIFO：

```go
// containerd/pkg/shim/shim.go:307
if !config.NoSetupLogger {
    ctx, err = setLogger(ctx, id)  // 配置 logrus + FIFO
}
```

**MicRun 的日志系统**

MicRun 实现了自定义的双输出日志系统：

| 特性 | 描述 |
|------|------|
| 双输出 | Debug 模式：同时输出到 FIFO 和 `/var/log/mica/mica-runtime.log` |
| Release 模式 | 仅输出到 containerd FIFO |
| 格式兼容 | 符合 containerd 日志格式 (`time`, `level`, `msg`, `id`, `namespace`) |

```go
// micrun/main.go:95-99
func setupLogger(c *shimv2.Config) {
    c.NoSetupLogger = true  // 禁用 containerd 默认日志
    log.Initialize(nil)     // 使用自定义日志系统
}
```

> **延伸阅读**：[日志重构设计](../logger-refactor/design.md)

---

## 三、进程模型差异

### 3.1 完整调用链对比

#### Runc 调用链

```
┌──────────────┐
│  Containerd  │
└──────┬───────┘
       │ CreateTask API
       ▼
┌──────────────────────────────────────┐
│  containerd-shim-runc-v2 (start)     │  ← 短生命周期
│  - 解析 OCI spec                      │
│  - 调用 manager.Start()               │
│  - fork 守护进程                      │
│  - 返回 socket 地址                   │
└──────┬───────────────────────────────┘
       │ fork+exec
       ▼
┌──────────────────────────────────────┐
│  containerd-shim-runc-v2 (daemon)    │  ← 长期运行
│  - 启动 TTRPC 服务器                  │
│  - 等待 API 调用                      │
└──────┬───────────────────────────────┘
       │ Create() → Start() API
       ▼
┌──────────────────────────────────────┐
│  runc binary                          │  ← 被调用后退出
│  - fork 容器进程                      │
│  - 设置 cgroup                        │
│  - 设置 namespace                     │
└──────┬───────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────┐
│  容器进程 (PID 1)                     │  ← 实际工作负载
└──────────────────────────────────────┘
```

#### MicRun 调用链

```
┌──────────────┐
│  Containerd  │
└──────┬───────┘
       │ CreateTask API
       ▼
┌──────────────────────────────────────┐
│  containerd-shim-mica-v2 (start)     │  ← 短生命周期
│  - 解析 OCI spec                      │
│  - 调用 StartShim()                   │
│  - fork 守护进程                      │
│  - 返回 socket 地址                   │
└──────┬───────────────────────────────┘
       │ fork+exec
       ▼
┌──────────────────────────────────────┐
│  containerd-shim-mica-v2 (daemon)    │  ← 长期运行 (1:1:1模型)
│  - 启动 TTRPC 服务器                  │
│  - 等待 API 调用                      │
└──────┬───────────────────────────────┘
       │ Create() → Start() API
       ▼
┌──────────────────────────────────────┐
│  libmica client                       │  ← Unix socket 通信
│  - 连接到 /tmp/mica/mica-create.sock │
│  - 发送 create/start 命令            │
└──────┬───────────────────────────────┘
       │ Unix socket
       ▼
┌──────────────────────────────────────┐
│  micad (守护进程)                     │  ← 独立运行
│  - 接收 shim 请求                     │
│  - 调用 hypervisor API                │
└──────┬───────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────┐
│  Xen / OpenAMP                        │  ← Hypervisor
│  - 创建 domain / 通信通道             │
└──────┬───────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────┐
│  RTOS (Zephyr/UniProton)              │  ← 实际工作负载
│  /dev/ttyRPMSG_<container>_0          │
└──────────────────────────────────────┘
```

### 3.2 进程关系图

#### Runc 进程树

```
systemd (PID 1)
  └─ containerd
       └─ containerd-shim-runc-v2 <container-id> (daemon)
            └─ runc --start <container-id>
                 └─ container_entrypoint (PID 1)
                      ├─ process_a
                      └─ process_b
```

**特点**：
- Shim 是容器进程的直接/间接祖先
- 容器进程退出时，shim 也会退出
- 进程树完整，可通过 ps 命令查看

#### MicRun 进程树

```
systemd (PID 1)
  ├─ micad
  │    └─ (管理多个 RTOS 实例)
  │         ├─ RTOS_A (Xen domain)
  │         └─ RTOS_B (Xen domain)
  │
  └─ containerd
       └─ containerd-shim-mica-v2 <container-id> (daemon)
            └─ (无子进程)
            └─ (通过 Unix socket 连接 micad)
```

**特点**：
- Shim 与 RTOS 完全分离
- Shim 不在 RTOS 的进程树上
- Micad 作为中心守护进程管理所有 RTOS

### 3.3 关键差异

| 方面 | Runc | MicRun |
|------|------|--------|
| **子进程管理** | 直接管理容器进程 | 通过 micad 间接管理 |
| **通信方式** | fork+exec runc 二进制 | Unix socket → micad |
| **进程可见性** | 进程树完整，可用 ps 查看 | RTOS 独立，不在 shim 进程树 |
| **资源隔离** | Linux cgroup + namespace | Hypervisor 硬件隔离 |

---

## 四、生命周期管理

### 4.1 1:1:1 模型 vs 退出模型

#### Runc：退出模型

```
时间线：
──────────────────────────────────────────────────────→
T1: containerd → shim start
    └→ shim 守护进程启动

T2: containerd → shim.Create() + shim.Start()
    └→ runc 创建容器进程

T3: 容器运行中...

T4: 容器进程退出
    └→ shim 检测到退出
    └→ shim 也退出
    └→ containerd 检测到 shim 丢失
```

**特点**：
- Shim 生命周期绑定于容器
- 容器退出 = Shim 退出
- 不需要额外的状态持久化

#### MicRun：1:1:1 持久化模型

```
时间线：
──────────────────────────────────────────────────────→
T1: containerd → shim start
    └→ shim 守护进程启动
    └→ 创建 RTOS 实例

T2: RTOS 运行中...
    (可能经过多次 attach/detach)

T3: Shim 崩溃/重启
    └→ RTOS 继续运行 (由 micad 管理)
    └→ containerd 重启 shim
    └→ shim 恢复状态，重新连接 RTOS

T4: 用户显式删除容器
    └→ containerd → shim.Delete()
    └→ micad 停止 RTOS
    └→ shim 退出
```

**1:1:1 模型的含义**：
```
1 个 shim 进程  ↔  1 个 sandbox  ↔  1 个 RTOS 实例
```

**特点**：
- Shim 不随 RTOS 退出而退出
- Shim 崩溃后可重启恢复
- 支持多次 attach/detach 周期

### 4.2 状态持久化

> **详细分析**：[状态持久化架构设计](state-persistence-rationale.md)
>
> 该文档深入分析了为什么 micrun 需要状态持久化，以及与 runc 进程模型的根本差异。

| 运行时 | 状态存储 | 恢复机制 |
|--------|----------|----------|
| Runc | 内存为主 | 容器重启时重新创建 |
| MicRun | 磁盘 (`/run/micrun/sandbox/*/state.json`) | Shim 重启时从磁盘恢复 |

**MicRun 状态持久化的必要性**：

1. **Shim 崩溃恢复**：Shim 重启后需要知道 RTOS 还在运行
2. **1:1:1 模型**：Shim 和 RTOS 分离，状态必须持久化
3. **Attach 支持**：多次 attach 需要保持连接状态

### 4.3 崩溃恢复策略

| 场景 | Runc | MicRun |
|------|------|--------|
| **容器崩溃** | Shim 检测到退出，shim 也退出 | Micad 检测到 RTOS 停止，shim 保持运行 |
| **Shim 崩溃** | 容器变成孤儿，被 init 接管 | RTOS 继续运行，shim 可重启恢复 |
| **重启后** | 需要 cleanup 孤儿进程 | Shim 从磁盘恢复状态，重新连接 RTOS |

---

## 五、资源管理

### 5.1 CPU 资源映射

#### Runc 的 CPU 处理

Runc 直接将 OCI spec 的 CPU 参数写入 Linux cgroup：

| OCI 参数 | Cgroup v1 | Cgroup v2 |
|----------|-----------|-----------|
| `cpu.quota` | `cpu.cfs_quota_us` | `cpu.max` 的 quota 部分 |
| `cpu.period` | `cpu.cfs_period_us` | `cpu.max` 的 period 部分 |
| `cpu.shares` | `cpu.shares` | `cpu.weight` |
| `cpu.cpus` | `cpuset.cpus` | `cpuset.cpus` |

#### MicRun 的 CPU 映射

MicRun 将 cgroup 资源映射到 Xen hypervisor 资源：

```
Linux cgroup                    Xen hypervisor
──────────────────────────────────────────────────────
cpu.shares (1024)          →   weight = max(1, min(S/4, 65535)) = 256
cpu.quota/period (200000/100000) → capacity = 200%
cpu.cpus ("0-3")            →   affinity = [0, 1, 2, 3]
```

**关键约束规则**：
```
effective_capacity = min(
    (quota × 100) / period,   # quota/period 转换的百分比容量
    cpuset_size × 100          # cpuset 核心数 × 100%
)
```

> **详细内容**：[资源映射设计](resource-design.md)

### 5.2 内存资源映射

| 运行时 | 内存限制机制 | 热更新 |
|--------|-------------|--------|
| Runc | cgroup `memory.limit_in_bytes` | 支持 |
| MicRun | Xen domain 静态内存分配 | 仅支持增加 |

### 5.3 资源热更新

| 运行时 | 支持程度 | 实现方式 |
|--------|----------|----------|
| Runc | 完全支持 | 直接修改 cgroup 配置 |
| MicRun | 部分支持 | 通过 micad 调用 hypervisor API |

---

## 六、IO 处理

### 6.1 IO 模式支持对比

| IO 模式 | Runc | MicRun |
|---------|------|--------|
| 前台 TTY (`-i -t`) | ✅ | ✅ |
| 前台非 TTY (`-i`) | ✅ | ✅ |
| 后台 TTY (`-d -t`) | ✅ | ✅ |
| 后台非 TTY (`-d`) | ✅ | ✅ |
| 多次 attach/detach | ❌ | ✅ (1:1:1 模型) |

### 6.2 数据流向对比

#### Runc IO 流

```
用户终端
   │ stdin
   ▼
containerd → shim → runc → 容器进程 stdin
   │                             │
   └──────── stdout/stderr ←──────┘
   ▼
用户终端
```

#### MicRun IO 流

```
用户终端
   │ stdin
   ▼
containerd → shim → pkg/io/copier → /dev/ttyRPMSG_* → RTOS
   │                                                 │
   └──── stdout/stderr ← epoll ←─────────────────────┘
   ▼
用户终端
```

**MicRun 的特殊处理**：
- **NUL 字节过滤**：RTOS 发送的 0x00 字节被过滤
- **换行压缩**：连续的 `\r\n` 被压缩为单个换行
- **Epoll 优化**：空闲 CPU 使用率从 70% 降至 ~0%
- **回声抑制**：避免 PTY 和 RTOS 重复回显

> **详细内容**：[IO 系统设计](io/io-design.md)

---

## 七、通信机制

### 7.1 与容器运行时通信

| 方面 | Runc | MicRun |
|------|------|--------|
| **通信对象** | runc 二进制 | micad 守护进程 |
| **通信方式** | fork + exec | Unix socket |
| **协议格式** | 命令行参数 + 环境变量 | 自定义二进制协议 |
| **生命周期** | 同步调用 | 异步消息 |

#### Runc 调用方式

```bash
# Runc 通过 fork+exec 直接调用
runc create <container-id> --bundle <bundle-path>
runc start <container-id>
runc delete <container-id>
```

#### MicRun 调用方式

```go
// MicRun 通过 Unix socket 与 micad 通信
func micaCtl(cmd MicaCommand, id string, opts ...string) error {
    clientSocketPath := filepath.Join(defs.MicaStateDir, id+".socket")
    s := newMicaSocket(clientSocketPath)
    return s.handleMsg([]byte(string(cmd)))
}
```

### 7.2 协议差异

#### Runc 协议

- **方式**：命令行参数 + stdout 返回
- **格式**：文本/JSON
- **示例**：
  ```bash
  $ runc state mycontainer
  {
    "ociVersion": "1.0.2",
    "id": "mycontainer",
    "pid": 1234,
    "status": "running"
  }
  ```

#### MicRun 协议 (libmica)

- **方式**：Unix socket 二进制协议
- **格式**：固定头部 + 变长数据
- **命令**：create, start, stop, rm, pause, resume, status, set
- **示例**：
  ```go
  type CreateMsg struct {
      Name          [66]byte  // 容器名称
      FirmwarePath  [256]byte // 固件路径
      Pedestal      [16]byte  // hypervisor 类型
      // ... 更多字段
  }
  ```

---

## 八、快速对比表

### 8.1 核心架构对比

| 方面 | Runc | MicRun |
|------|------|--------|
| **目标平台** | Linux 容器 | RTOS (Zephyr/UniProton/LiteOS) |
| **进程模型** | 父-子进程 | 客户端-服务器 |
| **通信方式** | 二进制调用 (runc) | Unix socket (micad) |
| **生命周期** | 随容器退出 | 1:1:1 持久化 |
| **状态存储** | 内存为主 | 磁盘持久化 |
| **资源目标** | Linux cgroup | Xen hypervisor |
| **隔离机制** | namespace + cgroup | 硬件虚拟化 |

### 8.2 启动参数对比

| 参数 | Runc | MicRun | 原因 |
|------|------|--------|------|
| noReaper | false | **true** | 无子进程需收割 |
| noSubreaper | false | **true** | 无孤儿进程处理需求 |
| NoSetupLogger | false | **true** | 自定义日志系统 |

### 8.3 进程树对比

```
Runc:
systemd → containerd → shim → runc → 容器进程

MicRun:
systemd → containerd → shim
systemd → micad → Xen/OpenAMP → RTOS
         ↑_______________|
         Unix socket 通信
```

### 8.4 资源管理对比

| 资源类型 | Runc | MicRun |
|----------|------|--------|
| CPU | cgroup quota/period/shares | Xen vCPU/capacity/weight |
| 内存 | cgroup memory limit | Xen domain memory |
| IO | 直接管道 | RPMSG TTY + epoll |
| 网络 | veth + bridge | (待实现) |

### 8.5 IO 处理对比

| 特性 | Runc | MicRun |
|------|------|--------|
| TTY 支持 | 标准 PTY | RPMSG TTY |
| 特殊处理 | 无 | NUL 过滤、换行压缩 |
| 性能优化 | 标准 | Epoll 零 CPU 等待 |
| 多次 attach | 不支持 | 支持 (1:1:1) |

### 8.6 错误处理对比

| 场景 | Runc | MicRun |
|------|------|--------|
| 容器崩溃 | shim 退出 | shim 保持运行 |
| shim 崩溃 | 容器成为孤儿 | RTOS 继续运行，shim 可恢复 |
| 网络断开 | 容器无感知 | (取决于实现) |

---

## 九、其他差异

> 本章节预留，用于后续补充更多差异点

### 9.1 待补充内容

- [ ] Exec 支持（在容器内执行新命令）
- [ ] 资源限制边界情况处理
- [ ] 容器指标收集差异
- [ ] 调试工具对比

---

## 总结

Runc 和 MicRun 虽然都实现 containerd shim v2 规范，但由于目标平台不同（Linux 容器 vs RTOS），在架构设计上有根本差异：

1. **进程模型**：Runc 是直接的进程管理，MicRun 是远程代理模式
2. **生命周期**：Runc 随容器退出，MicRun 持久化运行
3. **资源管理**：Runc 使用 cgroup，MicRun 使用 hypervisor
4. **通信方式**：Runc 调用二进制，MicRun 使用 socket

这些差异使得 MicRun 需要状态持久化、特殊的 IO 处理、以及不同的崩溃恢复策略，最终形成了独特的 1:1:1 生命周期模型。
