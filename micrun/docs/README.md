# MicRun 文档总览

本文档集面向两类读者：

- 使用者：如何部署、运行、排障、调优 MicRun
- 开发者：当前架构、关键接口、状态恢复、IO 与 pedestal 设计

## 建议阅读路径

### 初次接触 MicRun

1. [项目 README](../README.md)
2. [快速入门](quick-start.md)
3. [配置参考](reference/configuration.md)
4. [注解参考](reference/annotations.md)

### 想理解当前实现

0. [容器生命周期与 IO 会话语义](user/lifecycle-semantics.md)（停止/detach/auto-close/task 可见性的权威口径）
1. [架构设计](internals/architecture.md)
2. [目标架构](internals/target-architecture.md)
3. [状态管理](internals/state-management.md)
4. [IO 系统](internals/io-system.md)
5. [Pedestal 架构](internals/pedestal-architecture.md)
6. [并发模型](internals/concurrency.md)
7. [任务状态机](internals/task-state-machine.md)
8. [micad 控制协议](internals/micad-protocol.md)
9. [API 参考](reference/api-reference.md)

### 做问题排查或回归验证

1. [故障排查](user/troubleshooting.md)
2. [性能调优](user/performance-tuning.md)
3. [测试说明](../tests/README.md)
4. [测试体系（本地验证门/回归测试手册）](internals/testing.md)
5. [IO 测试](../tests/io/README.md)
6. [K3s 测试](../tests/k3s/README.md)

## 流程图速览

以下图示以 Mermaid 源码内嵌维护（各专题文档中的图示均链接至此）。

### 架构总览

对应文档：[架构设计](internals/architecture.md)

```mermaid
graph TB
    subgraph clients["标准容器工具"]
        CTR[ctr / nerdctl]
        K8S[k3s / kubelet]
    end
    subgraph transport["internal/transport/shimv2"]
        SHIM[shim v2 TaskService<br/>RPC 适配 / 平台 bootstrap / 事件转发]
    end
    subgraph application["internal/application"]
        APP[task / lifecycle / attach / recovery<br/>用例编排与策略]
    end
    subgraph domain["internal/domain/container"]
        DOM[Sandbox / Container<br/>核心模型与状态机]
    end
    subgraph ports["internal/ports"]
        PORTS[层间契约<br/>任务状态转移表 / 事件 / 存储接口]
    end
    subgraph adapters["internal/adapters"]
        AD1[guest: libmica / micad]
        AD2["hypervisor: pedestal(xl)"]
        AD3[io: copier / event bus]
        AD4[state: file store]
        AD5[config: oci / runtimeconfig]
    end
    subgraph dom0side["dom0 侧（Linux 宿主域）"]
        MICAD[micad 守护进程]
        XEN[Xen hypervisor]
        RTOS[RTOS guest domain<br/>UniProton / Zephyr]
    end
    CTR --> SHIM
    K8S --> SHIM
    SHIM --> APP
    APP --> DOM
    DOM --> PORTS
    PORTS --> AD1 & AD2 & AD3 & AD4 & AD5
    AD1 --> MICAD
    MICAD --> XEN
    AD2 --> XEN
    XEN --> RTOS
    AD3 -. RPMSG TTY .-> RTOS
```

### 分层运行时

对应文档：[架构设计](internals/architecture.md)

```mermaid
graph LR
    subgraph L5["工具层"]
        A[ctr / nerdctl / k3s]
    end
    subgraph L4["传输层 transport/shimv2"]
        B[ttrpc TaskService · bootstrap · 事件转发]
    end
    subgraph L3["应用层 application"]
        C[task · lifecycle · attach · recovery]
    end
    subgraph L2["领域层 domain/container"]
        D[Sandbox · Container · 资源规则 · console 语义]
    end
    subgraph L1["端口与适配层"]
        E1[ports 契约]
        E2[adapters: libmica · pedestal · io · state · config]
    end
    A --> B --> C --> D --> E1 --> E2
```

### UniProton on Xen 主路径

对应文档：[架构设计](internals/architecture.md)

```mermaid
graph TB
    subgraph dom0["dom0（Linux，openEuler Embedded）"]
        CTNRD[containerd]
        SHIM2[micrun shim]
        MD[micad]
        DRV[mcs 内核驱动<br/>grant table / evtchn / RPMSG]
    end
    XEN2[Xen hypervisor]
    subgraph domU["domU（RTOS guest）"]
        UNI[UniProton 应用 + shell]
    end
    CTNRD --> SHIM2 --> MD
    MD --> DRV
    DRV --> XEN2
    XEN2 --> UNI
    SHIM2 -. console RPMSG tty .-> UNI
```

### IO 链路

对应文档：[IO 系统](internals/io-system.md)

```mermaid
graph LR
    CLIENT[attach 客户端<br/>ctr / nerdctl / kubectl] <-->|stdin / stdout / stderr| FIFO[containerd FIFO]
    FIFO <--> COPIER[shim IO copier<br/>输入规范化 / 输出规则]
    COPIER <-->|RPMSG tty 读写| TTY[guest TTY]
    TTY <--> SHELL[RTOS shell]
    COPIER --> EV[EventBus<br/>attach/detach/exit 事件]
    EV --> POLICY[应用层事件策略<br/>auto-close / interrupt / detach]
```

### IO 交互细节

对应文档：[IO 系统](internals/io-system.md)

```mermaid
sequenceDiagram
    participant U as 用户(nerdctl run -it / ctr task attach)
    participant S as shim IO session
    participant G as RTOS shell(guest)
    U->>S: attach(stdin 打开)
    S->>G: 绑定 RPMSG TTY
    U->>G: 命令输入
    G-->>U: 回显与命令输出
    Note over U,S: detach(Ctrl-P Ctrl-Q) 或非 TTY EOF
    S->>S: 保留 FIFO 供 reattach,启动 auto-close 计时
    U->>S: reattach
    S->>G: 刷新 fresh TTY 句柄
    U->>G: Ctrl-C
    S->>G: 0x03 → 停止容器(exit 130)
    G-->>S: exit 命令
    S->>U: 会话结束,任务终止
```

### 恢复与校验

对应文档：[Sandbox 校验](internals/sandbox-validation.md)

```mermaid
graph TB
    START[shim 重启 / recovery] --> LOAD[loadSandbox<br/>读取合并状态文档]
    LOAD -->|文档缺失| FRESH[按新 sandbox 处理]
    LOAD -->|文档存在| V[validateOrCleanup]
    V --> COLL{shim PID 冲突?}
    COLL -->|是| FAIL[恢复失败,拒绝启动]
    COLL -->|否| STALE{RTOS client 判定}
    STALE -->|micad 存在或域存活| RESTORE[恢复容器并挂 exit watcher]
    STALE -->|确认 stale| CLEAN[清理持久化状态与 netns holder]
```

### 状态恢复决策

对应文档：[Sandbox 校验](internals/sandbox-validation.md)

```mermaid
graph TB
    Q1{sandbox 状态为<br/>Stopped?} -->|是| KEEP1[无需恢复动作]
    Q1 -->|否| Q2{micad 中 client 存在?}
    Q2 -->|是| LIVE[可恢复<br/>对照存活状态收敛]
    Q2 -->|否| Q3{Xen domain 存活?<br/>hypervisor 探测}
    Q3 -->|探测失败| ERR[上抛错误<br/>阻止误删状态]
    Q3 -->|存活| KEEP2[保留状态<br/>由正常路径收敛]
    Q3 -->|不存在| STALE2[判定 stale<br/>清理持久化状态]
```

### 配置与资源控制

对应文档：[架构设计](internals/architecture.md)

```mermaid
graph LR
    subgraph sources["配置来源(低 → 高优先级)"]
        D1[内置默认]
        D2[配置文件<br/>micrun.conf / drop-in]
        D3[OCI 注解<br/>org.openeuler.micrun.*]
    end
    D1 & D2 & D3 --> RS[RuntimeStack 解析]
    RS --> SPEC[OCI spec / RuntimeConfig<br/>state_dir · os · firmware · 资源]
    SPEC --> PLAN[资源规划<br/>CPU/内存/cpuset]
    PLAN --> MD2[micad client 配置]
    PLAN --> XL2[Xen domain 资源<br/>xl mem-max / vcpus]
```

## 文档分区

### 用户文档

入口：[user/README.md](user/README.md)。

- [快速入门](quick-start.md)
- [Kubernetes 集成](user/kubernetes.md)
- [故障排查](user/troubleshooting.md)
- [性能调优](user/performance-tuning.md)

### 参考文档

- [参考文档入口](reference/README.md)
- [注解参考](reference/annotations.md)
- [配置参考](reference/configuration.md)
- [API 参考](reference/api-reference.md)
- [资源映射](reference/resources.md)

### 内部设计文档

入口：[internals/README.md](internals/README.md)（含阅读顺序）。

- 架构与状态：[架构设计](internals/architecture.md) · [目标架构](internals/target-architecture.md) · [Pedestal 架构](internals/pedestal-architecture.md) · [状态管理](internals/state-management.md) · [Sandbox 校验](internals/sandbox-validation.md)
- 行为契约：[并发模型](internals/concurrency.md) · [任务状态机](internals/task-state-machine.md) · [micad 协议](internals/micad-protocol.md) · [IO 系统](internals/io-system.md)
- 工程流程：[测试体系](internals/testing.md) · [贡献指南](internals/contribution-guide.md) · [可靠性架构](internals/reliability.md) · [日志系统](internals/logging.md)

## 当前文档状态

文档与代码保持同步。当前架构形态、已知边界与后续方向见
[架构设计的第 6/7 节](internals/architecture.md)。
