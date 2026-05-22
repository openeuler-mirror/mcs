# MicRun 目标架构

本文档描述 MicRun 后续重实现的目标形态。当前代码中可用的运行证据会保留，但历史包结构和早期 TODO 不再作为设计约束。

## 核心原则

1. `transport` 只适配 containerd shim v2 协议，不承载业务判断。
2. `application` 只编排用例，不直接操作文件、TTY、micad socket 或 Xen 命令。
3. `domain` 拥有 MicRun 规则：状态机、资源规划、console 语义、恢复一致性。
4. `ports` 表达 MicRun 需要的能力，接口按用例拆小，不按底层实现拼大。
5. `adapters` 只把端口落到外部系统：containerd FIFO、RPMSG TTY、micad、pedestal、文件状态。
6. 兼容逻辑必须有边界和退出路径；不能散落在主流程里。

## 目标分层

```text
containerd / ctr / nerdctl / k3s
        |
        v
internal/transport/shimv2
  - RPC request/response mapping
  - event forwarding
  - process bootstrap
        |
        v
internal/application
  - task use cases
  - lifecycle use cases
  - attach use cases
  - recovery use cases
        |
        v
internal/domain
  - container aggregate
  - console input semantics
  - resource planning
  - runtime state model
  - recovery validation
        |
        v
internal/ports
        |
        v
internal/adapters
  - io/fifo/tty
  - guest/libmica
  - hypervisor/pedestal
  - state/file
  - config/oci/runtimeconfig
```

## 已开始落地的切片

### Console 领域

`internal/domain/console` 已成为用户交互语义的所有者：

- TTY `Ctrl+C` -> interrupt/stop
- TTY `Ctrl+P Ctrl+Q` -> detach
- TTY / non-TTY `exit` -> 兼容退出
- TTY CRLF、backspace、local echo 动作生成
- non-TTY `0x03` 保持普通输入字节
- RTOS 输出中的 NUL 过滤和连续换行压缩

`internal/adapters/io.Copier` 只执行 `InputInterpreter` 返回的动作：写 TTY、写 stdout、记录 echo、发布事件、停止 copier。
输出方向则通过 `OutputNormalizer` 做跨 read chunk 的规范化，然后再写入 containerd FIFO。

## 演进顺序

1. `domain/console`：输入/输出两侧语义状态机已落地；后续只在发现新交互语义时扩展领域状态机。
2. `domain/resource`：把 CPU/VCPU/memory/hugepage/pinning 规划从 `container` 和 adapter 中抽离。
3. `domain/runtime`：把 task/sandbox/container 状态转换收敛为显式状态机。
4. `application/*`：把重复的 lock/status/exit/event 编排收敛成用例级 command handler。
5. `transport/shimv2`：只保留 shim v2 协议适配和依赖装配。
6. 兼容层：legacy state、binary IO、ctr/nerdctl 差异必须留在命名适配器或兼容 repository 中，主流程不直接感知历史格式。

## 判断标准

每一轮重构完成后必须满足：

- 新规则优先进入 `domain` 的纯模型或状态机。
- adapter 不能反向拥有业务语义。
- application 不能依赖 adapter 具体包。
- 新接口必须能用 fake 在单元测试中驱动。
- 文档描述的是当前已落地行为，不写“未来会做”的债务占位。
