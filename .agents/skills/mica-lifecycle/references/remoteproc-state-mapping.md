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
- `Stopped/Offline`：已经停止或不在线
- `Error`：生命周期中的异常态

## 对应分析建议
- create 后异常：先看 `remoteproc_init()` 与 pedestal 选择
- load 后异常：先看 `remoteproc_config()`、`remoteproc_load()`、resource table
- running 后服务异常：不要只盯状态机，要转去看 RPMsg 与 client 服务层

## 特别注意
`show_client_status()` 在 `Running` 分支里还会检查 resource table 的保留字段，以判断 remote 是否已经离线。因此“状态看似还在运行”与“真实远端是否还活着”并不总是完全等价。