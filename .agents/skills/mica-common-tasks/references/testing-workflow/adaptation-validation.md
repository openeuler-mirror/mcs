# 适配验证

## 1. 文档目标

这篇文档用于新 RTOS、新开发板、新 pedestal、新硬件平台适配过程中的共用验证，也可作为相关 bugfix 或 review 后的基础功能回归检查清单。

## 2. 验证对象

适配类问题通常同时涉及：

- RTOS ELF 与 `.resource_table`
- Linux/master 资源解析和镜像加载
- shared memory 预留与映射
- IRQ 或 notify 双向路径
- OpenAMP virtio / RPMsg 初始化
- service endpoint 与 ready 语义
- TTY、UMT、RPC、GDB 基础验证

## 3. 推荐验证顺序

1. RTOS 镜像存在 `.resource_table`
2. Linux/master 能解析 ELF 并完成 `mica create`
3. shared memory 和 RTOS 运行内存在 Linux 侧可见
4. IRQ 或 notify 路径注册成功
5. `mica start` 能进入 `Running`
6. OpenAMP virtio 与 RPMsg device 建立
7. name service 或 endpoint 建立
8. service ready 状态符合预期
9. TTY 交互闭合
10. UMT 数据面收发闭合

## 4. 关键观察点

- `/proc/iomem`
- `/proc/interrupts`
- `mica status`
- RTOS 侧 `mica_init()` 返回值
- RTOS 侧 `mica_create_all_services()` 返回值
- RPMsg endpoint 名称
- service ready 查询结果

## 5. 下一跳

- 生命周期诊断：`../debugging-workflow/lifecycle-diagnosis.md`
- 通信与服务不 ready：`../debugging-workflow/communication-diagnosis.md`
- 边界诊断：`../debugging-workflow/boundary-diagnosis.md`
