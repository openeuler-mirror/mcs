# restoreSandboxAndContainers 函数深入分析

## 问题：为什么需要这个函数？

### Shim v2 的重启场景

在containerd shim v2架构中，一个关键问题是：**当shim守护进程意外退出后重启时，如何恢复之前的容器状态？**

```
正常情况：
containerd → shim start → shim守护进程 → 长期运行
                           ↓ (运行中...)
                         处理API调用

异常情况：
containerd → shim start → shim守护进程 → 意外崩溃/被杀
                           ↓
                           containerd重新调用
                         shim守护进程(新) → 需要恢复旧状态！
```

### 1:1:1 模型的含义

MicRun遵循 `1:1:1` 模型：
- **1个shim进程** 管理 **1个sandbox** 运行 **1个RTOS实例**

当shim重启后，RTOS可能仍在运行（由micad管理），但shim进程的内存状态丢失。我们需要从磁盘恢复状态以继续管理该RTOS。

---

## 函数工作原理

### 状态持久化架构

```
┌─────────────────────────────────────────────────────────────┐
│                      磁盘持久化                               │
│  /run/micrun/sandbox/<container-id>/state.json              │
│  ├── sandbox配置                                            │
│  ├── container列表                                           │
│  └── 状态信息                                               │
└─────────────────────────────────────────────────────────────┘
                          ↑
                    LoadSandbox() 读取
                          │
                          ↓
┌─────────────────────────────────────────────────────────────┐
│                    shimService (内存)                        │
│  s.sandbox  ← 恢复的sandbox对象                             │
│  s.containers ← 恢复的container列表                         │
│  status      ← 根据sandbox状态确定                          │
└─────────────────────────────────────────────────────────────┘
```

### 代码逐行分析

```go
func (s *shimService) restoreSandboxAndContainers(ctx context.Context) error {
    // 1. 从磁盘加载sandbox配置
    sandbox, err := cntr.LoadSandbox(ctx, s.id)
    if err != nil {
        return err  // 新容器没有持久化状态，这是正常的
    }

    s.sandbox = sandbox

    // 2. 检查sandbox的实际运行状态
    sandboxState := sandbox.GetState()
    var initialStatus task.Status
    if sandboxState == cntr.StateRunning {
        // Sandbox正在运行 → 容器应该是RUNNING状态
        initialStatus = task.Status_RUNNING
    } else {
        // Sandbox未运行 → 容器应该是CREATED状态
        initialStatus = task.Status_CREATED
    }

    // 3. 恢复所有container到内存
    containers := sandbox.GetAllContainers()
    for _, c := range containers {
        // 确定容器类型
        var cType cntr.ContainerType
        if c.GetAnnotations() != nil {
            if _, isSandbox := c.GetAnnotations()["io.kubernetes.cri.sandbox-id"]; isSandbox {
                cType = cntr.PodContainer
            } else if c.GetAnnotations()["io.kubernetes.cri.container-type"] == "sandbox" {
                cType = cntr.PodSandbox
            } else {
                cType = cntr.SingleContainer
            }
        } else {
            cType = cntr.SingleContainer
        }

        // 创建shimContainer对象并加入内存map
        sc := &shimContainer{
            s:           s,
            id:          c.ID(),
            cType:       cType,
            status:      initialStatus,  // 使用恢复的状态
            exitIOch:    make(chan struct{}),
            stdinCloser: make(chan struct{}),
        }
        s.containers[c.ID()] = sc
    }

    return nil
}
```

---

## 三种调用场景

### 场景1：新容器创建（首次Start）

```
containerd → shim start (父进程)
              ↓
           shim守护进程(子进程)
              ↓
           New() 调用
              ↓
           restoreSandboxAndContainers()
              ↓
           LoadSandbox() → 返回 "not found"
              ↓
           log: "no existing sandbox to restore"
              ↓
           继续正常创建流程
```

**结果**：没有旧状态可恢复，这是正常的。

### 场景2：Shim守护进程重启后重新连接

```
时间线：
────────────────────────────────────────────────────────────→
T1: shim start → 创建容器 → 状态保存到 /run/micrun/sandbox/xxx/state.json
T2: 容器运行中... shim守护进程崩溃
T3: containerd检测到shim丢失，重新调用shim
    └→ shim daemon (新进程)
       └→ New() 调用
          └→ restoreSandboxAndContainers()
             └→ LoadSandbox() → 成功读取 state.json
             └→ 发现 sandbox.StateRunning
             └→ 容器状态设为 RUNNING
       └→ 可以继续处理API调用！
```

**结果**：成功恢复状态，shim可以继续管理运行中的RTOS。

### 场景3：Delete命令

```
containerd → shim delete
              ↓
           New() 调用
              ↓
           restoreSandboxAndContainers()  ← 不执行！
```

**结果**：我们已优化，一次性命令不调用此函数。

---

## 关键设计点

### 1. 状态判断逻辑

```go
if sandboxState == cntr.StateRunning {
    initialStatus = task.Status_RUNNING
} else {
    initialStatus = task.Status_CREATED
}
```

为什么这样设计？
- **StateRunning**: ROS正在运行，容器必须标记为RUNNING
- **其他状态**: 可能是Ready/Stopped等，保守地标记为CREATED

### 2. Container类型推断

```go
if _, isSandbox := c.GetAnnotations()["io.kubernetes.cri.sandbox-id"]; isSandbox {
    cType = cntr.PodContainer
} else if c.GetAnnotations()["io.kubernetes.cri.container-type"] == "sandbox" {
    cType = cntr.PodSandbox
} else {
    cType = cntr.SingleContainer
}
```

从annotation推断容器类型，这是Kubernetes CRI的标准做法。

### 3. 错误处理

```go
if err != nil {
    log.Debugf("no existing sandbox to restore: %v", err)
    // This is expected for new containers, not an error
}
```

失败不是错误 - 新容器本就没有持久化状态。

---

## 潜在问题与优化

### 当前实现的特点

✅ **优点**：
- 简单直接
- 处理了shim重启场景
- 新容器场景优雅降级

⚠️ **可改进**：
- 状态不一致风险：如果disk说RUNNING但实际已停止？
- IO状态未恢复：`attachInfo`可能需要额外处理
- 多容器支持：Pod场景下需要仔细处理

### 为什么不在一次性命令中调用？

| 命令 | 是否需要恢复 | 原因 |
|------|-------------|------|
| start | ❌ | 创建新容器，无旧状态 |
| delete | ❌ | Cleanup直接操作bundle，不需要内存状态 |
| daemon | ✅ | 重启后需要继续管理运行中的RTOS |

---

## 总结

`restoreSandboxAndContainers` 是shim重启恢复的核心机制：

1. **目的**：让重启后的shim能够继续管理运行中的RTOS
2. **手段**：从 `/run/micrun/sandbox/<id>/state.json` 恢复状态
3. **关键**：正确判断容器状态（RUNNING vs CREATED）
4. **时机**：只在守护进程模式调用

这是shim v2架构中"无状态API但有状态服务"矛盾的解决方案：
- **API层面**：containerd可以随时调用shim
- **服务层面**：shim通过持久化保持状态一致性
