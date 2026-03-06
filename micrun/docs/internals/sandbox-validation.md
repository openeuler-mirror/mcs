# MicRun Sandbox 状态验证文档

## 概述

MicRun 通过持久化存储和状态验证机制确保 Sandbox 的状态一致性。当 shim 进程重启（如 containerd 重启）时，可以从存储中恢复 Sandbox 状态，避免状态不一致导致的问题。

## 状态存储

### 存储位置

```
/run/micrun/sandbox/<sandbox-id>/state.json
```

### 存储结构

```go
type SandboxStorage struct {
    ID      string        `json:"id"`        // Sandbox ID
    State   SandboxState  `json:"state"`     // 当前状态
    Config  SandboxConfig `json:"config"`    // 配置
    Network NetworkConfig `json:"network"`   // 网络配置

    // 状态验证元数据
    CreatedAt int64 `json:"created_at,omitempty"` // 状态创建/更新时间戳
    ShimPID   int   `json:"shim_pid,omitempty"`   // 创建此状态的 shim 进程 PID
}
```

### 状态写入时机

| 时机 | 说明 |
|------|------|
| Sandbox 创建后 | 初始状态为 `StateReady` |
| 启动容器后 | 状态变为 `StateRunning` |
| 停止容器后 | 状态变为 `StateStopped` |
| 更新容器后 | 保存更新的配置 |
| 创建/删除容器后 | 更新容器列表 |

## 状态定义

### Sandbox 状态

| 状态 | 说明 | 允许的转换 |
|------|------|-----------|
| `StateCreating` | 创建中 | → `StateStopped` |
| `StateReady` | 已就绪，容器已创建但未启动 | → `StateRunning`, `StateStopped` |
| `StateRunning` | 运行中 | → `StatePaused`, `StateStopped` |
| `StateStopped` | 已停止 | → `StateRunning` (恢复) |
| `StatePaused` | 已暂停 | → `StateRunning`, `StateStopped` |

### 状态转换规则

```
Creating ──stop──► Stopped
    │
    ▼ (内部标准化)
  Ready ──start──► Running ◄──resume── Paused
              │         │                 ▲
              │         └────pause────────┘
              │
              └────────stop──► Stopped ──resume──► Running
```

**注意**：`StateCreating` 是过渡状态，在恢复时会自动标准化为 `StateReady`。

## 状态恢复机制

### 恢复流程

```go
func (s *Sandbox) restore() error {
    // 1. 从存储加载状态
    ss, err := restoreSandbox(s.ctx, s.id)
    if err != nil {
        // 存储不存在，视为新 Sandbox
        return nil
    }

    // 2. 验证 Sandbox ID 匹配
    if ss.ID != s.id {
        return fmt.Errorf("sandbox ID mismatch")
    }

    // 3. 恢复状态
    s.state.Ped = ss.State.Ped
    s.state.Version = ss.State.Version
    s.state.State = ss.State.State
    s.config = &ss.Config

    // 4. 恢复网络配置
    s.network = &s.config.NetworkConfig

    return nil
}
```

### 恢复时的状态处理

| 加载的状态 | 处理方式 |
|-----------|----------|
| `StateCreating` | 标准化为 `StateReady` |
| `StateReady` | 保持不变，可接受操作：Start, CreateContainer, Delete |
| `StateRunning` | 保持不变，尝试启动所有容器 |
| `StateStopped` | 保持不变，可恢复到 Running |
| `StatePaused` | 保持不变，可恢复到 Running |

## 状态验证

### 验证触发条件

1. **Shim 启动时**：从存储恢复状态后验证
2. **状态转换前**：`state.Transition()` 验证转换是否合法
3. **操作前**：如 Start, Stop, Delete 等操作前检查当前状态

### 验证规则

```go
func (s *StateString) transition(old StateString, new StateString) error {
    if *s != old {
        return fmt.Errorf("mismatched state: %s (expecting: %v)", *s, old)
    }

    switch *s {
    case StateCreating:
        if new == StateStopped {
            return nil
        }
    case StateReady:
        if new == StateRunning || new == StateStopped {
            return nil
        }
    case StateRunning:
        if new == StatePaused || new == StateStopped {
            return nil
        }
    case StatePaused:
        if new == StateRunning || new == StateStopped {
            return nil
        }
    case StateStopped:
        if new == StateRunning {
            return nil
        }
    }
    return fmt.Errorf("cannot transition from state %v to %v", s, new)
}
```

### Delete 前的状态检查

```go
func (s *Sandbox) Delete(ctx context.Context) error {
    // 只允许在 Ready/Paused/Stopped 状态下删除
    if s.state.State != StateReady &&
        s.state.State != StatePaused &&
        s.state.State != StateStopped {
        return fmt.Errorf("sandbox is not ready, paused, or stopped, cannot delete")
    }
    // ... 删除逻辑
}
```

## 状态一致性保证

### 关键设计

1. **原子性写入**：状态文件在每次状态变更后立即写入
2. **元数据记录**：`CreatedAt` 和 `ShimPID` 用于验证状态的有效性
3. **状态机保护**：所有状态转换必须经过验证

### Stop 操作的状态保护

```go
func (s *Sandbox) Stop(ctx context.Context, force bool) error {
    if s.state.State == StateStopped {
        return nil  // 幂等性
    }

    // 1. 先验证状态转换是否合法
    originalState := s.state.State
    if err := s.state.Transition(originalState, StateStopped); err != nil {
        return err
    }

    // 2. 修改状态
    s.state.State = StateStopped

    // 3. 立即保存到磁盘
    if err := s.StoreSandbox(ctx); err != nil {
        log.Errorf("Failed to save sandbox state during Stop: %v", err)
    }

    // 4. 执行停止操作
    for _, c := range s.containers {
        if err := c.stop(ctx, force); err != nil {
            return err
        }
    }

    return nil
}
```

## 故障恢复

### Shim 崩溃恢复

当 shim 进程崩溃后重启：

```
+---------------------------------------------------------------+
|                    Shim Crash Recovery                        |
|                                                               |
|  1. Shim restarts                                            |
|     |                                                        |
|     v                                                        |
|  2. Try to restore Sandbox state from storage                |
|     |                                                        |
|     +-> Storage not exist --> Create new Sandbox            |
|     |                                                        |
|     +-> Storage exists --> Verify Sandbox ID                 |
|                     |                                        |
|                     +-> ID mismatch --> Error exit           |
|                     |                                        |
|                     +-> ID match --> Restore state            |
|                                   |                          |
|                                   v                          |
|                            Based on restored state:           |
|                            - StateRunning: Try to start       |
|                            - StateReady: Wait for start      |
|                            - StateStopped: Wait for cleanup   |
|                                                               |
+---------------------------------------------------------------+
```

### 状态不一致处理

| 场景 | 处理方式 |
|------|----------|
| 存储 Sandbox ID 与当前不匹配 | 返回错误，不恢复 |
| 存储状态为 `StateCreating` | 标准化为 `StateReady` |
| 存储状态为 `StateRunning` 但容器已退出 | 保持状态，允许操作恢复 |
| 存储文件损坏 | 记录错误，尝试创建新 Sandbox |

## 调试

### 查看 Sandbox 状态文件

```bash
# 查看所有 Sandbox
ls -la /run/micrun/sandbox/

# 查看特定 Sandbox 状态
cat /run/micrun/sandbox/<sandbox-id>/state.json | jq
```

### 日志标识

```
[StoreSandbox] ...    # 状态保存日志
[RESTORE] ...         # 状态恢复日志
[STOP] State ...      # 状态转换日志
SetSandboxState: ...  # 状态设置日志
```

### 常见问题

**Q: Shim 重启后容器状态显示错误？**
A: 检查 `/run/micrun/sandbox/<id>/state.json` 是否存在且内容正确。

**Q: 无法删除 Sandbox？**
A: 确保状态为 `Ready`/`Paused`/`Stopped`。如果状态为 `Running`，先执行 Stop 操作。

**Q: 状态文件损坏怎么办？**
A: 删除 `/run/micrun/sandbox/<id>/` 目录，然后重新创建容器。

## 相关代码

| 文件 | 说明 |
|------|------|
| `pkg/micantainer/sandbox.go` | Sandbox 状态管理和存储 |
| `pkg/micantainer/interfaces.go` | SandboxTraits 接口定义 |
| `definitions/paths.go` | 存储路径定义 |
