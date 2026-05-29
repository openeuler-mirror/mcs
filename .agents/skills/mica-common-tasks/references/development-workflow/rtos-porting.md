# RTOS 对接工作流

## 1. 文档目标

这篇文档用于指导 agent 处理新 RTOS 对接任务，重点是建立 RTOS 侧资源表、OpenAMP/libmetal、`libmica` 适配层和 service runtime 的工作边界。

## 2. 主要参考

- 本仓参考：`rtos/libmica/README.md`
- 外部参考：`yocto-meta-openeuler/docs/source/features/mica/mica_core/developer_guides/rtos.rst`

外部参考内容应总结为本 skill 文档中的判断框架，不应只给外部路径让用户自行比对。

## 3. 对接范围

新 RTOS 对接通常至少包含：

- resource table 适配
- OpenAMP 初始化
- libmetal 平台适配
- `libmica` system 适配层
- `mica_config` 输入组织
- `mica_init()` 与 `mica_create_all_services()` 调用时机
- TTY、UMT、RPC、GDB 等 service 验证

## 4. resource table 要求

RTOS ELF 需要包含 `.resource_table` 段，使 Linux/master 能够在加载镜像时识别远端资源需求。

重点确认：
- resource table header 是否位于 `.resource_table` 起始位置
- 是否包含 `RSC_VDEV` 资源项
- vring 数量、对齐、buffer 数量和 notifyid 是否合理
- Linux 侧是否能够解析 ELF 并找到 resource table

## 5. OpenAMP 初始化链

RTOS 侧 OpenAMP 初始化通常需要完成：

1. 初始化中断或 notify 接收路径
2. 根据 resource table 创建 virtio device
3. 等待 Linux/master 侧 virtio device ready
4. 根据 resource table 初始化 tx/rx vring
5. 初始化 RPMsg device
6. 创建 service endpoint

## 6. `libmica` 适配链

对接 `libmica` 时，需要确认：

- client OS 是否提供 `mica_config`
- `shm_base_addr`、`shm_size`、`ipc_irq_num`、`ipc_irq_base` 是否来自可靠平台配置
- `mica_sys_ops` 中的系统回调是否实现
- `libmica/lib/system/<os>` 是否提供必要 OS 适配头文件和函数
- CMake 平台文件是否指定工具链、OS 路径、include 路径和 pedestal 类型

## 7. 验证路径

新 RTOS bring-up 的验证顺序建议为：

1. ELF 中存在 `.resource_table`
2. Linux/master 能完成 `mica create`
3. `mica start` 能进入 `Running`
4. RTOS 侧 `mica_init()` 成功
5. RTOS 侧 `mica_create_all_services()` 成功
6. TTY 可交互
7. UMT 可完成基本收发
8. RPC/GDB 按目标能力继续验证

共用验证方法见：`../testing-workflow/adaptation-validation.md`
