# micad 控制协议（libmica 线缆格式）

micrun（Dom0 侧 shim）与 guest 内 mica 守护进程（micad）之间的本地协议。
实现：`internal/adapters/guest/libmica/`。协议的 C 侧权威定义在 mcs 仓
`mica/micad/socket_listener.c`（`struct create_msg`、`CTRL_MSG_SIZE`）与
`library/remoteproc/xen_rproc.c`（资源键表）——**两侧任何一端改格式，另一端必须同改**。

修改 libmica 前先读本文，避免破坏与 micad 的二进制兼容。

## 1. 通道总览

| 通道 | 路径 | 格式 | 用途 |
|------|------|------|------|
| create socket | `/run/mica/mica-create.socket` | 定长二进制 `create_msg` | 注册新 client |
| 控制 socket | `/run/mica/<client-id>.socket` | 文本命令 + 标记响应 | start/stop/rm/pause/resume/status/set |

均为 UNIX domain socket，请求-响应一轮往返（`roundTrip`），读写共享一个 deadline。

## 2. create_msg 二进制布局

`pack()`（`client_config.go`）按 micad 的 `struct create_msg` 逐字段序列化，
小端序，总长 `createMsgSerializedBufSize`：

```
偏移  长度   字段
0     66     name            （MaxNameLen，定长，含 NUL 填充）
66    256    path            firmware 路径（MaxFirmwarePathLen）
322   16     ped             pedestal 类型（MaxPedLen："xen"/"baremetal"）
338   256    pedcfg          pedestal 配置路径
594   1      debug           0/1
595   128    cpuStr          CPU 亲和描述（MaxCPUStringLen）
723   P      padding         对齐到 4 字节边界（P = (-723) mod 4）
...   4×6    vcpuNum, maxVcpuNum, cpuWeight, cpuCapacity,
             memoryMB, memoryThresholdMB        （uint32 小端）
...   512    iomem           （MaxConfigStrLen）
...   512    network         （MaxConfigStrLen）
```

关键约束：

- **名称长度陷阱**：micad 侧 `name[MAX_NAME_LEN-1] = '\0'`——恰好 66 字符的名字会被
  截成 65 字符注册，而后续控制 socket 都用 66 字符 id 寻址，产生一个**无法控制也无法
  回收**的泄漏 client。因此 `validateClientID` 拒绝贴限长度。
- **ARM64 的 maxmem==memory**：Xen ARM 无 Populate-on-Demand，`memoryThresholdMB`
  强制等于 `memoryMB`；其他架构允许气球膨胀（threshold > memory）。缺省 0 回退为
  memory，防止造出 maxmem=0 的 client。

## 3. 文本控制命令

发往 `/run/mica/<id>.socket` 的单行命令：

| micrun 常量 | 线缆命令 | 说明 |
|-------------|----------|------|
| MCreate | `create` | （仅 create socket 收二进制） |
| MStart | `start` | 全量 domain 启动 |
| MStop | `stop` | remoteproc 关停 |
| MRemove | `rm` | 删除 client |
| MPause | `stop` | **线缆上映射为 stop**（见 micaWireCommand） |
| MResume | `start` | **线缆上映射为 start** |
| MStatus | `status` | 查询状态 |
| MUpdate | `set <field> <value>` | 资源更新 |

`set` 的 field 必须命中 micad `key_to_lower()` 后的键表
（cpucapacity/cpuweight/cpu/vcpu/memory/maxmemory/maxvcpu），否则 daemon 返回 -EINVAL。
micrun 侧合法值见 `MicaUpdateField`（`client_protocol.go`）。

**32 字节硬上限**：micad 用固定 `CTRL_MSG_SIZE=32` 缓冲读命令，超长**静默截断**。
所以控制命令组装后必须校验长度（`micaCtrlMsgSize` 常量即为此镜像）。

## 4. 响应格式

响应是文本流，以标记结尾，客户端流式读到标记为止（`micaResponseComplete`，
标记取**最后一次出现**，`lastMicaMarker`）：

- 成功：`MICA-SUCCESS`
- 失败：`MICA-FAILED`（其后到结尾的文本作为错误 payload 透传）

读缓冲上限 `MicaSocketBufSize=512`——防止行为异常的 daemon 把 socket 灌满内存。

## 5. 超时策略

| 场景 | 预算 |
|------|------|
| 默认控制命令（status/set/...） | 5s（`MicaSocketTimeout`） |
| create / start / stop / rm / pause / resume | 30s（`MicaSocketLongTimeout`） |

长预算的原因：这些操作在 daemon 侧是**同步**的（固件加载、domain 生命周期），
daemon 完成前不回包；5s 上限会把"慢而健康"误报为超时，并在 daemon 仍在执行时引发
重试碰撞。调用方 ctx 带 deadline 时以调用方为准（`deadline(ctx)`）。

## 6. micad 存活性判定与自启

`mica_daemon.go`：

1. **pidfile + 身份核验**：`kill(pid,0)` 只能证明"有进程占着这个 pid"，不能防 PID 复用。
   读 `/proc/<pid>/comm` 前缀必须是 `micad`，否则视为陈旧 pidfile。
   （同一防御模式见 netns holder 与 shim 实例恢复。）
2. **socket 探活**（`validSocketPath`）：有界 `net.Dial`。
   `ECONNREFUSED` → socket 陈旧（daemon 已死）；**超时 → 假定存活**（不可因探活误判
   而触发不可逆的 domain 清理）。
3. **自启**：`systemctl start micad` → `service micad start` 依次尝试，
   单次 30s 上限（`serviceStartTimeout`），防 systemd 卡死拖住整个 shim 启动。

## 7. 测试锚点

- 命令映射与 `set` 线缆格式：`client_control_test.go`
  （`TestMicaCommandMessage`、`TestBuildUpdateWireFormat`、
  `TestMicaUpdateRequestWireFormatUsesProtocolFields`）
- socket 语义：`socket_test.go`（deadline 分层 `TestSocketDeadline*`、
  响应标记 `TestMicaResponseCompleteRecognizesTerminalMarkers`、ctx 取消）
- daemon 管理：`mica_daemon_test.go`（自启回退）
- 上游契约变更时的回归入口：改 `socket_listener.c` 的 `create_msg` 或键表，
  必须同步跑 `go test ./internal/adapters/guest/libmica/`。
  `create_msg` 布局本身目前无逐字节断言——若上游改布局，先在
  `client_config.go` 的 pack 测试中补字段偏移断言再动实现。
