# MicRun 故障排查指南

## 概述

本文档提供 MicRun 常见问题的排查步骤和解决方案。排查时优先检查 `runtime.json` 快照，legacy `state.json` 只作为兼容排查入口。

## 诊断工具

### 基础命令

| 命令 | 说明 |
|------|------|
| `ctr task ls` | 列出所有任务 |
| `ctr task status <id>` | 查看任务状态 |
| `ctr container ls` | 列出所有容器 |
| `ps aux \| grep containerd-shim-mica-v2` | 查看 shim 进程 |
| `journalctl -u containerd -f` | 查看 containerd 日志 |
| `cat /var/log/mica/mica-runtime.log` | 查看 micrun 日志（debug 构建） |
| `xl list` | 查看 Xen 域列表 |
| `xl dmesg` | 查看 Xen 日志 |

### 查看 Sandbox 状态

> **依赖**: 以下命令使用 `jq` 格式化 JSON 输出。如未安装，可使用 `sudo dnf install jq` 或 `sudo yum install jq` 安装，或移除 `| jq` 直接查看原始 JSON。

```bash
# 列出所有 sandbox 快照
ls -la /run/micrun/runtime/sandbox/

# 查看当前权威快照
cat /run/micrun/runtime/sandbox/<sandbox-id>/runtime.json | jq

# 查看 container 快照
find /run/micrun/runtime/container -maxdepth 3 -name runtime.json | grep <container-id>

# 查看 legacy 回退文件（仅历史排查时使用）
cat /run/micrun/sandbox/<sandbox-id>/state.json | jq
```

说明：

- 排查 sandbox 生命周期问题时，优先使用 `<sandbox-id>`
- 单容器场景下 `<sandbox-id>` 往往与 `<container-id>` 相同，但在 Kubernetes/Pod 场景里两者不一定相同

### 查看 FIFO 状态

```bash
# 查看 FIFO 路径
ls -la /run/containerd/io.containerd.runtime.v2.task/default/<container-id>/

# 检查 FIFO 是否存在
stat /run/containerd/io.containerd.runtime.v2.task/default/<container-id>/stdin
```

## 常见问题

### 1. 容器启动失败

#### 症状

```bash
ctr run --runtime io.containerd.mica.v2 localhost:5000/rtos:latest test
# 错误: context deadline exceeded
```

#### 排查步骤

1. **检查 shim 进程是否存在**
   ```bash
   ps aux | grep containerd-shim-mica-v2
   ```

2. **检查 containerd 日志**
   ```bash
   journalctl -u containerd -n 100 | grep <container-id>
   ```

3. **检查 micad 是否运行**
   ```bash
   systemctl status micad
   journalctl -u micad -n 100
   ```

4. **检查 Xen 状态**
   ```bash
   xl list
   xl dmesg | tail -50
   ```

#### 常见原因

| 原因 | 解决方案 |
|------|----------|
| micad 未运行 | `systemctl start micad` |
| 固件文件不存在 | 检查 `firmware_path` 注解 |
| Xen 配置错误 | 检查 `ped.conf` 注解 |
| 内存不足 | 检查 `memory.limit` |

---

### 2. Shim 进程退出

#### 症状

```bash
# 容器创建后立即退出
ctr run --runtime io.containerd.mica.v2 localhost:5000/rtos:latest test
# 命令返回，容器无法运行
```

#### 排查步骤

1. **查看 shim 退出日志**
   ```bash
   journalctl -u containerd -f | grep shim
   ```

2. **检查是否有残留资源**
   ```bash
   xl list | grep <container-id>
   ```

3. **检查状态文件**
   ```bash
   cat /run/micrun/runtime/sandbox/<sandbox-id>/runtime.json
   ```

#### 解决方案

| 问题 | 解决方案 |
|------|----------|
| 固件路径错误 | 修正 `firmware_path` 注解 |
| 缺少权限 | 确保用户有访问 `/dev/ttyRPMSG*` 的权限 |
| Sandbox 状态不一致 | 优先核对 `/run/micrun/runtime/sandbox/<id>/runtime.json`，确认 stale 后再清理对应运行时目录 |

---

### 3. IO 无响应

#### 症状

```bash
ctr attach <container-id>
# 无输出，或输入无响应
```

#### 排查步骤

1. **检查 FIFO 状态**
   ```bash
   ls -la /run/containerd/io.containerd.runtime.v2.task/default/<container-id>/
   ```

2. **检查 TTY 设备**
   ```bash
   ls -la /dev/ttyRPMSG*
   ```

3. **查看 shim 日志**
   ```bash
   cat /var/log/mica/mica-runtime.log | grep <container-id>
   ```

#### 常见原因

| 原因 | 解决方案 |
|------|----------|
| FIFO 未创建 | 检查 `Terminal` 配置 |
| TTY 设备未打开 | 检查 micad 日志 |
| Copier 未启动 | 重启 shim |
| 客户端断开 | 使用 `ctr attach` 重新连接 |

---

### 4. 容器状态显示 UNKNOWN

#### 症状

```bash
ctr task status <container-id>
# UNKNOWN
```

#### 排查步骤

1. **检查 shim 进程**
   ```bash
   ps aux | grep containerd-shim-mica-v2 | grep <container-id>
   ```

2. **检查状态文件**
   ```bash
   cat /run/micrun/runtime/sandbox/<sandbox-id>/runtime.json | jq '.state'
   ```

3. **检查 containerd 连接**
   ```bash
   journalctl -u containerd -f | grep "ttrpc"
   ```

#### 解决方案

| 问题 | 解决方案 |
|------|----------|
| shim 崩溃 | 查找 shim 日志中的错误 |
| 状态文件损坏 | 删除并重新创建容器 |
| ttrpc 连接断开 | 重启 containerd |

---

### 5. 多余的空行输出

#### 症状

```bash
ctr run -t --runtime io.containerd.mica.v2 localhost:5000/rtos:latest test
# 输出中有多余的空行
Hello, UniProton!


openEuler UniProton #
```

#### 原因

TTY 输出处理导致 `\r\n` 转换为 `\r\r\n`。

#### 解决方案

确保使用正确版本的 micrun：
- 检查 `internal/domain/container/rpmsg_tty.go` 中禁用了 `OPOST|ONLCR`
- 检查 `internal/adapters/io/copier_stdout.go` 中启用了 `OutputNormalizer` 的 `CompressLineEndings`

---

### 6. Attach 后没有输出

#### 症状

```bash
ctr attach <container-id>
# 连接成功，但没有输出
```

#### 排查步骤

1. **检查容器是否正在运行**
   ```bash
   ctr task status <container-id>
   ```

2. **检查 FIFO 是否被其他进程占用**
   ```bash
   lsof /run/containerd/io.containerd.runtime.v2.task/default/<container-id>/stdout
   ```

3. **检查 Session.Restart() 是否被调用**
   ```bash
   grep "Restart" /var/log/mica/mica-runtime.log | grep <container-id>
   ```

#### 解决方案

| 问题 | 解决方案 |
|------|----------|
| 容器未运行 | 先启动容器 |
| FIFO 被占用 | 断开其他 attach 连接 |
| Session 未重启 | 重新启动 shim |

---

### 7. 状态不一致导致无法删除

#### 症状

```bash
ctr container delete <container-id>
# 错误: sandbox is not ready, paused, or stopped, cannot delete
```

#### 排查步骤

1. **检查当前状态**
   ```bash
   cat /run/micrun/runtime/sandbox/<sandbox-id>/runtime.json | jq '.state.state'
   ```

2. **检查 shim 是否仍在运行**
   ```bash
   ps aux | grep containerd-shim-mica-v2 | grep <container-id>
   ```

#### 解决方案

```bash
# 方法 1: 先停止容器
ctr task kill <container-id>
# 等待停止完成

# 方法 2: 如果 shim 已崩溃，手动清理
xl destroy <container-id>
rm -rf /run/micrun/runtime/sandbox/<sandbox-id>/
find /run/micrun/runtime/container -maxdepth 3 | grep <container-id>
# 确认上面的路径后，再手工删除对应 container 运行时目录
rm -rf /run/micrun/sandbox/<sandbox-id>/
rm -rf /run/micrun/containers/<container-id>/
ctr task delete -f <container-id>
ctr container delete <container-id>
```

---

### 8. 内存不足错误

#### 症状

```bash
# 日志显示
memory allocation failed
```

#### 排查步骤

1. **检查容器内存限制**
   ```bash
   ctr task metrics <container-id>
   ```

2. **检查系统内存**
   ```bash
   free -h
   xl info | grep memory
   ```

#### 解决方案

```yaml
# 增加 memory limit
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: rtos-app
    resources:
      limits:
        memory: "128Mi"  # 增加内存限制
      requests:
        memory: "64Mi"
```

或通过注解：
```yaml
metadata:
  annotations:
    org.openeuler.micrun.container.min_memory_mb: "64"
```

---

### 9. CPU 绑定失败

#### 症状

```bash
# 日志显示
failed to pin vcpu: invalid argument
```

#### 排查步骤

1. **检查 cpuset 配置**
   ```bash
   cat /run/micrun/runtime/sandbox/<sandbox-id>/runtime.json | jq '.config.container_configs'
   ```

2. **检查主机 CPU 数量**
   ```bash
   lscpu
   xl info
   ```

#### 解决方案

确保 cpuset 有效：
- cpus 编号必须在有效范围内
- SharedCPUPool 模式下，CPU 数量必须等于 vCPU 数量

---

### 10. 容器在离开后停止/回收，或没有按预期回收

> 停止/回收/task 记录可见性的完整语义见
> [容器生命周期与 IO 会话语义](lifecycle-semantics.md)（权威口径，含 12 行结局矩阵）。

#### 先分清"正常行为"与"真故障"

**以下都是设计内行为，不是 auto_close 故障**：

| 你观察到的 | 实际语义 |
|------------|----------|
| detach（Ctrl+P Ctrl+Q）后约 30 秒容器被回收 | 默认 `auto_close=true`：detach 即"最后一个 stdin 写端消失"，30s 内无人 reattach 就回收（计时从 detach 起算） |
| attach 会话被关闭/断开后容器秒级停止、task 记录消失 | attach **客户端进程死亡**路径：绕过 auto-close 计时直接停止，与 `auto_close` 值无关 |
| 普通（非 raw）终端里 Ctrl+C 后容器停止但看不到 STOPPED(130) | Ctrl+C 被终端转成 SIGINT 杀死了 attach 客户端，`0x03` 从未送达，属上一行的进程死亡路径 |
| `ctr task start -d` 后不 attach，约 30 秒容器被回收 | 同"写端消失"计时：start 后无人 attach 即开始计时 |
| `nerdctl ps -a` 显示 `Created` | task 记录消失后的回退显示，判断容器生死看 `xl list` 与 `/dev/ttyRPMSG*` |

**确属"想要的行为没配置对"时**，检查注解：

1. **检查注解配置**
   ```bash
   ctr container info <container-id> | grep annotations
   ```
   注意 `auto_close` 是布尔值：`"false"` 生效；数字值（如 `"60"`）会被忽略；
   非法值（如 `"notabool"`）静默按默认 `true` 处理。

2. **检查超时设置**（debug shim 才有文件日志）
   ```bash
   grep "auto_close" /var/log/mica/mica-runtime.log
   ```

#### 解决方案

```yaml
metadata:
  annotations:
    # 方法 1: 禁用自动回收（detach/EOF/无人 attach 都不再回收；attach 进程死亡仍会停止容器，属设计语义）
    org.openeuler.micrun.container.auto_close: "false"

    # 方法 2: 设置超时（优先级更高）
    org.openeuler.micrun.container.auto_close_timeout: "0"  # 禁用
    org.openeuler.micrun.container.auto_close_timeout: "60s"  # 60秒后回收
```

### 11. start 报 "mica daemon reported failure: unknown"

#### 症状

```bash
ctr task start -d <container-id>
# ctr: failed to start sandbox for <container-id>:
#      failed to start container <container-id>:
#      mica daemon reported failure: unknown
```

`unknown` 是 micad 控制通道失败应答的固定文案，具体失败原因在
systemd 日志里，不在命令行输出中。

#### 排查步骤

```bash
# 查看 micad 日志中的失败细节（域创建、固件加载等阶段信息都在这里）
journalctl -u micad --no-pager | tail -50
# 或全量过滤（guest 无 journalctl -u 时）
journalctl --no-pager | grep -i micad | tail -50
```

#### 典型原因

- `min_memory_mb` 注解超过宿主可用内存：Xen 拒绝创建域，日志可见
  域创建失败记录（注解值本身已正确写入 xen cfg，属资源不足而非
  配置未生效）
- 固件加载/解析失败（如镜像内固件与平台不匹配）：日志可见
  `load client image failed` / `failed to parse rsc table` 等行
- rpmsg tty 等待超时（60s）：报错文案为
  `wait for rpmsg tty ... context deadline exceeded`，域已启动但
  RTOS 侧 rpmsg 前端未就绪，详见 micad 日志与 `/dev/ttyRPMSG*` 状态

### 12. CLI 输出与进程可见性的正常行为速查

以下观察结果都不是故障，无需处理：

| 你观察到的 | 实际语义 |
|------------|----------|
| `nerdctl ps` 的 CONTAINER ID 只有 12 个字符 | docker/nerdctl 生态的短 ID 显示惯例（`docker ps` 同此），数据未丢失；`nerdctl ps --no-trunc` 展开完整 ID，`ctr container ls` 始终显示完整 ID |
| `ctr task delete` 打印 `WARN ... exit with non-zero exit code 137`（或 130/143 等） | ctr 对非零退出码 task 删除时的通用提示性日志，**删除本身已成功**；`N-128` 即信号编号（137=SIGKILL、130=SIGINT、143=SIGTERM），是容器真实退出状态的如实反映 |
| `ctr container delete` / `ctr task delete` 后 `ps` 仍能看到 `containerd-shim-mica-v2` 进程 | delete 后的收尾窗口（退出事件转发与服务退出），数秒（约 5s）后进程自行消失；窗口过后仍存在才需按第 7 节排查 |
| start 失败后 `ctr task ls` 显示 CREATED | 失败但可重试的状态：直接再次 `ctr task start -d` 即可重试。如需清理，containerd 要求 task 为 STOPPED 才能 delete，先 `ctr task kill -s 9 <container-id>` 再 `ctr task delete` / `ctr container delete` |
| start 失败报 `wait for rpmsg tty ... context deadline exceeded` | rpmsg tty 等待超时（60s）：guest 域已启动但 RTOS 侧 rpmsg 前端未在窗口内就绪。偶发慢启动可直接重试；持续失败说明固件/平台不匹配，按第 11 节查 micad 日志 |

## 调试技巧

### 启用 Debug 日志

使用 debug 构建的 micrun：
```bash
# 查看 debug 日志
cat /var/log/mica/mica-runtime.log

# 或实时跟踪
tail -f /var/log/mica/mica-runtime.log
```

### 使用 Mock Micad

```bash
cd micrun/tests/mock_micad
make run
```

### 检查状态文件

```bash
# 查看完整 sandbox 快照
cat /run/micrun/runtime/sandbox/<id>/runtime.json | jq '.'

# 查看状态
cat /run/micrun/runtime/sandbox/<id>/runtime.json | jq '.state'

# 查看配置
cat /run/micrun/runtime/sandbox/<id>/runtime.json | jq '.config'

# 必要时再查看 legacy 回退文件
cat /run/micrun/sandbox/<id>/state.json | jq '.'
```

### 清理残留资源

```bash
# 完整清理脚本
#!/bin/bash
TASK_ID=$1
SANDBOX_ID=${2:-$TASK_ID}

# 1. 销毁 Xen 域
xl destroy $TASK_ID 2>/dev/null

# 2. 删除 containerd 任务
ctr task delete -f $TASK_ID 2>/dev/null

# 3. 删除 containerd 容器
ctr container delete $TASK_ID 2>/dev/null

# 4. 删除运行时快照
rm -rf /run/micrun/runtime/sandbox/$SANDBOX_ID

# 5. 输出待清理的 container 运行时路径
find /run/micrun/runtime/container -maxdepth 3 | grep "$TASK_ID"

# 6. 删除 legacy Sandbox 状态
rm -rf /run/micrun/sandbox/$SANDBOX_ID

# 7. 删除 legacy 容器状态
rm -rf /run/micrun/containers/$TASK_ID

echo "Cleanup complete for task=$TASK_ID sandbox=$SANDBOX_ID"
```

### 日志标识

| 标识 | 含义 | 典型来源 |
|------|------|---------|
| `[IO]` / `[TTY]` | IO 会话与 RPMSG TTY 生命周期 | `internal/adapters/io`、`internal/domain/container/rpmsg_tty.go` |
| `[EVENTS]` / `[ATTACH]` | attach 事件策略处理 | `internal/application/attach` |
| `[PERF]` | 阶段耗时埋点（`MICRUN_PERF=1` 时输出） | `internal/support/perf` |
| `[SHIM]` | shim 侧生命周期日志 | `internal/transport/shimv2` |
| `[TIMEOUT]` | auto-close 计时 | `internal/application/lifecycle` |

### 错误定位

`ERRO[0123]` 中的数字是进程启动以来经过的秒数（logrus 格式），不是可检索的错误码。
排障时以日志消息关键字 `rg` 检索源码定位，常用关键字见上表。

## 相关文档

- [注解参考手册](../reference/annotations.md) - 注解配置
- [API 参考手册](../reference/api-reference.md) - API 接口
- [配置参考](../reference/configuration.md) - 运行时配置说明
