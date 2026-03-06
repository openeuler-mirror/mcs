# 状态持久化架构设计分析

## 核心问题：为什么Micrun需要状态持久化？

### 问题根源：1:1:1 模型中的"双进程"特性

MicRun采用 **1:1:1 模型**：
```
1个Shim进程  ←管理→  1个Sandbox  ←管理→  1个RTOS实例
```

**关键特性**：Shim进程和RTOS进程是**分离的**！

| 进程 | 管理 | 生命周期 | 独立性 |
|------|------|----------|--------|
| Shim进程 | 提供API、IO转发 | 可能崩溃/被杀 | 由containerd管理 |
| RTOS实例 | 实际业务逻辑 | 独立于shim运行 | 由micad管理 |

**问题**：当Shim崩溃时，RTOS仍在运行，但Shim的内存状态丢失！

---

## 场景分析：Shim崩溃后的连锁反应

### 没有状态持久化的问题

```
时间线：
────────────────────────────────────────────────────────────→
T1: containerd → shim start
    └→ shim守护进程启动
    └→ 创建RTOS容器
    └→ 内存: s.containers[id] = container对象

T2: 容器运行中...
    Shim突然崩溃 (OOM/Bug/SIGKILL)
    └→ 内存状态丢失！
    └──→ 但RTOS仍在运行 (由micad管理)

T3: 用户执行: ctr task ls
    └→ containerd → shim (已死)
    └→ 连接失败！

T4: containerd检测到shim丢失
    └→ containerd → shim start (重启)
    └→ 新shim进程启动，内存是空的！
        s.containers = {}  ← 空！
    └→ 用户再执行: ctr task ls
        └→ containerd → shim.State(id)
        └→ shim查找: s.containers[id]
        └→ 返回错误: "container not found"

结果：容器"僵尸化" - RTOS在跑，但没人能管理它！
```

### 有状态持久化的解决方案

```
时间线：
────────────────────────────────────────────────────────────→
T1: containerd → shim start
    └→ 创建容器
    └→ 保存状态到磁盘: /run/micrun/sandbox/xxx/state.json

T2: 容器运行中...
    Shim崩溃
    └→ 内存丢失
    └──→ 但状态文件还在磁盘上！
    └──→ RTOS仍在运行

T3: containerd检测到shim丢失
    └→ containerd → shim start (重启)
    └→ restoreSandboxAndContainers()
        └→ LoadSandbox() 从磁盘读取 state.json
        └→ s.sandbox = 恢复的sandbox
        └→ s.containers[id] = 恢复的container
        └→ c.status = RUNNING (根据state)

T4: 用户执行: ctr task ls
    └→ containerd → shim.State(id)
    └→ shim返回正确的状态！

结果：Shim成功接管运行中的RTOS！
```

---

## Containerd的期望行为

### Containerd如何管理Shim生命周期

Containerd通过**TTRPC socket**与shim通信：

```
containerd                              shim
    │                                    │
    │ ───── Connect to socket ─────────►│
    │                                    │
    │◄────── StateResponse ─────────────│ State()
    │                                    │
    │ ───── Start() ───────────────────►│
    │◄────── StartResponse ─────────────│
    │                                    │
    │        [Shim 崩溃...]              │
    │                                    │
    │ ───── Connect (失败) ─────────────►│
    │                                    │
    │    containerd 检测到 shim 丢失     │
    │                                    │
    │ ───── shim start ────────────────►│ 重新启动 shim
    │                                    │
    │ ───── Connect (重试) ────────────►│
    │◄────── StateResponse ─────────────│ State() - 必须返回正确状态！
```

**关键点**：Containerd期望重启后的shim能够**无缝**接管现有容器，返回正确的状态信息。

### State() API的严格要求

```go
// State() 是containerd最常调用的API之一
func (s *shimService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
    c, found := s.containers[r.ID]  // ← 从内存读取！
    if c == nil || !found {
        return nil, fmt.Errorf("container %s not found", r.ID)
    }
    return &taskAPI.StateResponse{
        Status: c.status,  // RUNNING, CREATED, STOPPED等
        // ... 其他字段
    }
}
```

**如果不恢复状态**：
- `s.containers[id]` 不存在
- State() 返回 "container not found"
- Containerd认为容器已丢失
- 用户无法通过`ctr`或`kubectl`查询容器状态
- **容器变成"孤儿"**！

---

## 为什么不能依赖外部存储？

### 方案对比

| 方案 | 优点 | 缺点 | MicRun是否采用 |
|------|------|------|---------------|
| **内存状态** | 最快 | 崩溃丢失 | ❌ 不足够 |
| **磁盘文件** | 简单、可靠 | 需要序列化 | ✅ 当前方案 |
| **Containerd** | 集中管理 | 依赖外部服务 | ❌ 违反设计原则 |
| **数据库** | 功能强大 | 过重、依赖外部 | ❌ 过度设计 |

### 为什么不用Containerd存储状态？

**Shim v2的设计哲学**：
- Shim是**自包含**的runtime
- Containerd只通过API与shim交互
- Shim应该独立管理容器状态

```
Standard Architecture:

┌─────────────┐         API          ┌─────────────┐
│ Containerd  │◄────────────────────►│    Shim     │
│             │                      │             │
│  Task API   │                      │  Containers │
└─────────────┘                      └──────┬──────┘
                                            │
                                            ▼
                                     ┌─────────────┐
                                     │ RTOS/VM     │
                                     └─────────────┘
```

**Shim拥有状态的权威性**：
- Shim知道容器的真实状态
- Containerd只是客户端，通过API查询
- **状态必须存储在Shim可访问的地方** → 即Shim本地磁盘

---

## 状态持久化的技术实现

### 存储位置与格式

```
/run/micrun/sandbox/<container-id>/state.json
{
    "id": "xxx",
    "state": "running",  // 或 "ready", "stopped" 等
    "config": { ... },   // sandbox配置
    "network": { ... }   // 网络配置
}
```

### 为什么是JSON而不是二进制？

| 方面 | JSON | 二进制 |
|------|------|--------|
| 可读性 | ✅ 调试友好 | ❌ |
| 兼容性 | ✅ 跨语言 | ❌ Go特有 |
| 性能 | ✅ 足够快 | ✅ 稍快 |
| 工具支持 | ✅ 可用jq等 | ❌ |

**结论**：对于小配置文件，JSON的可读性优势远大于性能劣势。

---

## 状态一致性的挑战

### 潜在问题：内存与磁盘不一致

```
场景：
1. Shim保存状态: RUNNING → state.json
2. Shim崩溃
3. Shim重启，读取: state.json → RUNNING
4. 但实际上RTOS可能已经停止了！

问题：磁盘状态 ≠ 实际状态
```

### 当前MicRun的处理

```go
// restoreSandboxAndContainers 中
sandboxState := sandbox.GetState()  // 从磁盘读取
if sandboxState == cntr.StateRunning {
    initialStatus = task.Status_RUNNING
} else {
    initialStatus = task.Status_CREATED
}
```

**限制**：当前假设磁盘状态是准确的。如果RTOS停止但状态未更新，会有不一致。

**可能的改进**：
- 定期同步状态到micad
- 启动时验证RTOS是否真的在运行
- 实现状态探测机制

---

## 总结：状态持久化的核心价值

### 1. 可靠性
```
Shim崩溃 → 自动重启 → 状态恢复 → 继续服务
```

### 2. 透明性
```
Containerd视角：Shim一直是"活"的
用户视角：容器一直可以查询/操作
```

### 3. 符合Shim v2规范
```
Shim v2要求：Shim必须能够管理其创建的容器
持久化是实现这一要求的必要手段
```

### 4. 1:1:1模型的必然选择
```
因为Shim和RTOS分离
所以Shim崩溃 ≠ RTOS停止
因为需要继续管理RTOS
所以必须持久化状态
```

---

## 设计决策回顾

| 问题 | 答案 |
|------|------|
| 为什么需要？ | Shim崩溃后需要继续管理运行中的RTOS |
| 为什么不用Containerd存储？ | Shim拥有状态权威性，应自包含 |
| 为什么用JSON？ | 可读性好，调试友好 |
| 为什么在/run/下？ | 临时文件，系统重启自动清理 |
| 什么时候保存？ | 创建容器、状态变更时 |
| 什么时候恢复？ | Shim守护进程启动时 |
