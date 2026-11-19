# remoteproc 状态映射

根据 `library/remoteproc/remoteproc_core.c`，MICA 侧能看到的 remoteproc 状态包括：
- `Offline`
- `Configured`
- `Ready`
- `Running`
- `Suspended`
- `Error`
- `Stopped`

## 可以这样理解
- `Configured`：remoteproc 已完成基础配置
- `Ready`：镜像、资源表等关键准备工作已就绪，具备启动条件
- `Running`：remote OS 已经被拉起
- `Stopped`：remoteproc 停止阶段的状态
- `Offline`：remoteproc 离线状态，实例仍可保留在控制面
- `Error`：生命周期异常态。`start_client()` 和 `stop_client()` 分别在 `remoteproc_start()`、`remoteproc_shutdown()` 返回错误时设置该状态

## 对应分析建议
- create 后异常：先看 `remoteproc_init()` 与 pedestal 选择
- load 后异常：先看 `remoteproc_config()`、`remoteproc_load()`、resource table
- running 后服务异常：不要只盯状态机，要转去看 RPMsg 与 client 服务层
- 进入 `Error`：结合失败阶段确认远端状态和资源清理进度

## 特别注意
`show_client_status()` 在 `Running` 分支中检查 resource table 的 `reserved[0]`：
- 读到 `CPU_OFF_FUNCID` 时调用 `mica_stop()`，成功后返回 `Offline`
- `mica_stop()` 失败时，按当时的 `rproc.state` 返回状态

硬件停机结论以对应 pedestal 的确认机制为准，例如 baremetal 的 PSCI OFF 查询。
