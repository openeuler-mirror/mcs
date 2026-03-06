# Pedestal 架构设计

本文档描述 MicRun 的虚拟化抽象层（Pedestal）的架构设计。

## 概述

Pedestal 是 MicRun 的虚拟化抽象层，为不同的虚拟化技术（Xen、Baremetal）提供统一的接口。通过 Facade 模式封装，调用方无需关心底层实现细节。

## 支持的虚拟化类型

| 类型 | 状态 | 说明 |
|------|------|------|
| Xen | 完全支持 | 生产可用的虚拟化方案 |
| Baremetal | 规划中 | 占位实现，暂不支持 |

## 架构设计

### Facade 模式

使用 `PedestalFacade` 封装所有 pedestal 操作：

```
┌─────────────────────────────────────────────────────┐
│                   Caller Code                        │
│         (shim, micantainer, libmica)                │
└─────────────────────┬───────────────────────────────┘
                      │ pedestal.Host.Method()
                      ▼
┌─────────────────────────────────────────────────────┐
│               PedestalFacade                         │
│  ┌─────────────────────────────────────────────┐   │
│  │ - impl: Pedestal (底层实现)                   │   │
│  │ - cpuScheduler: CPUScheduler (缓存)          │   │
│  │ - lifecycleMgr: LifecycleManager (缓存)      │   │
│  │ - stateQuerier: StateQuerier (缓存)          │   │
│  │ - memoryMgr: MemoryManager (缓存)            │   │
│  └─────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────┘
                      │
        ┌─────────────┴─────────────┐
        ▼                           ▼
   ┌─────────┐                 ┌───────────┐
   │   Xen   │                 │ Baremetal │
   └─────────┘                 └───────────┘
```

### 核心接口

#### 基础接口 (Pedestal)

所有虚拟化实现必须支持的基础操作：

```go
type Pedestal interface {
    Type() PedType
    String() string
    GeneratePedConf() string
    MaxCPUNum() uint32
    MemoryMB() (free, total uint32)
    MemLowThreshold() uint32
    MemHighThreshold() uint32
    HostCPUSeta() cpuset.CPUSet
}
```

#### 可选接口

可选接口通过 Facade 的懒加载缓存机制实现，不支持时返回 `ErrNotSupported`：

| 接口 | 方法 | 说明 |
|------|------|------|
| CPUScheduler | SetCPUAffinity, SetCPUWeight, SetCPUCapacity | CPU 调度控制 |
| LifecycleManager | Pause, Resume | 生命周期管理 |
| StateQuerier | ClientState | 状态查询 |
| MemoryManager | SetMemory, SetMaxMemory | 动态内存管理 |

### Xen 特定方法

Xen 特定的功能通过 Facade 方法暴露，非 Xen 平台返回 `ErrNotSupported`：

| 方法 | 说明 |
|------|------|
| DomainState(clientID) | 读取域状态 |
| ConsolePath(clientID) | 获取控制台 PTY 路径 |
| VCPUList() | 获取 VCPU 信息 |
| DomainID(clientID) | 获取域 ID |
| SetVCPUCount(clientID, count) | 设置 VCPU 数量 |

## 使用示例

### 基础调用

```go
// 获取虚拟化类型
pedType := pedestal.Host.Type()

// 获取主机资源信息
maxCPU := pedestal.Host.MaxCPUNum()
freeMem, totalMem := pedestal.Host.MemoryMB()
```

### 可选接口调用

```go
// 无需类型断言，直接调用
if err := pedestal.Host.Pause(clientID); err != nil {
    if errors.Is(err, pedestal.ErrNotSupported) {
        // 当前虚拟化类型不支持暂停
    }
    return err
}
```

### Xen 特定方法

```go
// 通过 Facade 调用 Xen 特定方法
state, err := pedestal.Host.DomainState(clientID)
if err != nil {
    if errors.Is(err, pedestal.ErrNotSupported) {
        // 非 Xen 平台
    }
    return err
}
```

### 能力查询

```go
caps := pedestal.Host.Capabilities()
if caps.LifecycleControl {
    // 支持生命周期控制
    pedestal.Host.Pause(clientID)
}
```

## 平台检测

平台类型在包初始化时自动检测：

1. 检测 Xen：检查 `/proc/xen/xenbus` 是否存在
2. 检测 Baremetal：预留，暂返回 false
3. 未检测到支持的平台：返回 `Unsupported`

## 扩展新平台

添加新的虚拟化平台支持：

1. 在 `types.go` 中添加新的 `PedType` 常量
2. 实现 `Pedestal` 接口及相关可选接口
3. 在 `detection.go` 中添加平台检测逻辑
4. 在 `facade_<platform>.go` 中添加平台特定方法（如有）

## 相关文件

| 文件 | 说明 |
|------|------|
| interface.go | 接口定义 |
| facade.go | Facade 核心实现 |
| facade_xen.go | Xen 特定方法 |
| types.go | 类型定义和工厂函数 |
| detection.go | 平台检测 |
| xen.go, xen_impl.go | Xen 实现 |
| baremetal.go | Baremetal 占位实现 |
