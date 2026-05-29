# 通信使用指南

## 1. 文档目标

这篇文档集中说明 MICA 通信服务的使用入口和用户侧观察点，包括 TTY、UMT、RPC、GDB。

它回答“怎样使用通信服务”。如果目标是验证改动后服务是否通过测试，应转到 `../testing-workflow/testing-overview.md`；如果目标是诊断服务不可用，应转到 `../debugging-workflow/communication-diagnosis.md`。

## 2. 服务可见性

服务使用前先查看：

```sh
mica status
```

重点观察 `Service` 列：

```text
Name                          Assigned CPU        State               Service
qemu-uniproton-xen            1-3                 Running             rpmsg-tty(/dev/ttyRPMSG0) rpmsg-rpc rpmsg-umt
```

注意：
- `Running` 不等于服务已经可用
- 服务出现在 `mica status` 中，也不等于业务链路完全闭合
- 如果服务不可见或不可用，转到 `../debugging-workflow/communication-diagnosis.md`

## 3. TTY

TTY 是最常见的 client OS 交互入口。

使用步骤：

```sh
mica status
screen /dev/ttyRPMSG0
```

实际设备号以 `mica status` 的 `Service` 列为准，例如：

```text
rpmsg-tty(/dev/ttyRPMSG1)
```

退出 `screen`：
- `Ctrl-a k`
- `Ctrl-a Ctrl-k`

详细机制见：`../../../mica-communication/references/services/tty-service.md`。

## 4. UMT

UMT 使用前应先确认：

- `mica status` 中出现 `rpmsg-umt`
- 实例处于 `Running`
- RTOS/client 侧已创建 UMT service

UMT 不只是 RPMsg endpoint 可见，还依赖 shared memory 数据面和用户态接口链路。

详细机制见：`../../../mica-communication/references/services/umt-service.md`。

## 5. RPC

RPC 使用前应先确认：

- `mica status` 中出现 `rpmsg-rpc`
- RTOS/client 侧 RPC proxy 或 endpoint 处理链已使能
- request/response 能闭合

详细机制见：`../../../mica-communication/references/services/rpc-service.md`。

## 6. GDB

GDB 使用前应先确认：

- 配置文件中 `Debug=yes`
- RTOS 镜像支持 gdbstub 或对应调试后端
- `mica status` 中实例处于可调试状态

启动方式：

```sh
mica gdb <name>
```

详细机制见：`../../../mica-communication/references/services/gdb-service.md`。

## 7. 失败回流

- 服务未出现在 `mica status`：`../debugging-workflow/communication-diagnosis.md`
- TTY 设备存在但 `screen` 后无响应：`../debugging-workflow/communication-diagnosis.md`
- UMT/RPC/GDB endpoint 可见但业务不通：`../debugging-workflow/communication-diagnosis.md`
- shared memory、IRQ、notify 或地址映射可疑：`../debugging-workflow/boundary-diagnosis.md`
