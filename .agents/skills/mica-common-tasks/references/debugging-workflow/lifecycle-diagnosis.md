# 生命周期诊断

## 1. 文档目标

这篇文档专门处理生命周期阶段的问题，例如：
- `mica create` 失败
- `mica start` 失败
- 实例无法进入 `Running`
- `stop` 后再次 `start` 失败
- `remove`、`stop`、`start` 的前后时序与状态转换不符合预期

它负责建立生命周期问题的诊断入口，并覆盖 `mica start` 失败的专项分流。不展开具体实现细节：
- 生命周期总框架见 `../../../mica-lifecycle/references/lifecycle-overview.md`
- `create` 机制见 `../../../mica-lifecycle/references/lifecycle-create.md`
- `start` 机制见 `../../../mica-lifecycle/references/lifecycle-start.md`
- `stop` 机制见 `../../../mica-lifecycle/references/lifecycle-stop.md`
- `remove` 机制见 `../../../mica-lifecycle/references/lifecycle-remove.md`
- OpenAMP `remoteproc` 细节见 `../../../mica-lifecycle/references/openamp-remoteproc.md`

## 2. 生命周期问题的典型范围

这篇文档关注的问题边界是：

1. create 阶段
   - 实例对象未正确建立
   - pedestal 选择或 `remoteproc_ops` 初始化异常

2. start 阶段
   - 配置或镜像异常
   - 镜像加载失败
   - `remoteproc_start()` 失败
   - RPMsg device 建立前就退出

3. running 状态判定阶段
   - 状态没有进入 `Running`
   - 状态显示与真实远端状态不一致

4. stop / restart 阶段
   - `stop` 后资源没有按预期回收
   - `stop` 后再次 `start` 失败
   - 旧状态残留影响下一轮启动

5. remove 阶段
   - remove 前后对象状态不一致
   - stop 与 remove 的语义边界被混淆

如果实例已经进入 `Running`，但主要症状是“服务没出现”或“endpoint ready 但服务不可用”，应转到：
- `communication-diagnosis.md`

## 3. 推荐排查顺序

生命周期诊断开始前，应先按 `debugging-overview.md` 的日志位置确认规则收集 CLI、syslog、journald、kernel log 或开发板特定日志。

### 3.1 生命周期阶段判断

优先判断失败停在哪一段，而不要一开始就下钻具体 backend：

1. create 阶段
2. load 阶段
3. remoteproc start 阶段
4. RPMsg device 建立阶段
5. stop / restart / remove 阶段

### 3.2 生命周期专题分流

- create 问题：看 `../../../mica-lifecycle/references/lifecycle-create.md`
- start 问题：看 `../../../mica-lifecycle/references/lifecycle-start.md`
- stop 后再次 start、stop 语义问题：看 `../../../mica-lifecycle/references/lifecycle-stop.md`
- remove 语义问题：看 `../../../mica-lifecycle/references/lifecycle-remove.md`
- 不确定具体动作归属：先看 `../../../mica-lifecycle/references/lifecycle-overview.md`

### 3.3 下层边界判断

当生命周期主链已经走到某个边界点时，再继续判断是否已经进入：
- pedestal-specific 实现问题
- OpenAMP `remoteproc` 问题
- RPMsg / service 建立问题

不要在生命周期调试入口里直接把这些问题混成一层。

## 4. 常见症状与下一跳

### 4.1 `mica create` 失败

优先看：
- `../../../mica-lifecycle/references/lifecycle-create.md`

重点关注：
- pedestal 选择
- `remoteproc_init()`
- client 对象与公共状态载体是否建立成功

### 4.2 `mica start` 失败

`mica start` 失败时，先判断失败更接近下面哪一类：

1. 配置或镜像问题
   - client 镜像路径、格式、加载地址或 resource table 不符合预期

2. `remoteproc` 启动问题
   - `remoteproc_config()`、`remoteproc_load()` 或 `remoteproc_start()` 返回失败

3. RPMsg device 建立问题
   - remoteproc 已启动，但 virtio/RPMsg device 没有进入可用状态

4. client service ready 问题
   - 生命周期已进入 `Running`，但 service 未出现或未 ready

优先看：
- `../../../mica-lifecycle/references/lifecycle-start.md`
- `../../../mica-lifecycle/references/openamp-remoteproc.md`

重点关注：
- `load_client_image()`
- `remoteproc_config()`
- `remoteproc_load()`
- `remoteproc_start()`

推荐代码落点：
- `library/mica/mica.c`
- `library/remoteproc/remoteproc_core.c`

如果已经进入 `Running` 后仍异常，应转到：
- `../../../mica-communication/SKILL.md`
- `communication-diagnosis.md`

### 4.3 进入 `Running` 前失败

优先怀疑：
- lifecycle 公共调度层
- pedestal backend
- OpenAMP `remoteproc` 启动链

### 4.4 `stop` 后再次 `start` 失败

优先看：
- `../../../mica-lifecycle/references/lifecycle-stop.md`
- `../../../mica-lifecycle/references/lifecycle-start.md`

重点关注：
- stop 后资源是否按预期回收
- RPMsg/device/debug 相关状态是否残留
- 当前实例是否仍保留可再次启动的前提状态

### 4.5 `Running` 了但行为仍异常

如果问题重点已经变成：
- 服务没出现
- 服务不全
- endpoint 看起来 ready 但服务不可用

应从生命周期调试转到：
- `communication-diagnosis.md`

## 5. 常见误判

### 5.1 把生命周期问题和服务问题混为一谈

`start` 走通只说明生命周期主链基本成立，不等于服务已经 ready。

### 5.2 还没判断阶段，就直接下钻 backend

如果不先确认问题停在 create、start、stop 还是 remove，后续分析会很容易发散。

### 5.3 把 `Running` 当成最终成功

`Running` 只是生命周期状态，不是完整系统可用性的最终判据。

### 5.4 把 stop 后残留现象直接当成 stop 失败

需要先区分：
- 哪些资源在 stop 后应当消失
- 哪些实例控制结构仍然应该保留，以支持后续再次 start

## 6. 常见问题

以下问题来自开发板适配和使用过程中的高频故障。命中这些症状时，优先按本节处理，不需要先展开代码搜索。

### 6.1 `failed to parse rsc table, please check the rsctable`

症状：
- `mica create` 或 `mica start` 报错
- syslog 中提示无法解析 resource table

原因：
- MICA 拉起 client OS 前会解析 ELF 镜像并获取资源表
- RTOS ELF 中缺少 `.resource_table` 段时会直接失败

处理：
- 确认 RTOS 已使能 MICA/OpenAMP 相关配置
- 确认 RTOS ELF 中存在 `.resource_table`
- 新 RTOS 场景转到 `../development-workflow/rtos-porting.md`

### 6.2 `Error occurred! please check if micad is running`

症状：
- `mica status` 或 `mica create` 报错
- CLI 提示检查 `micad` 是否运行

原因：
- `mica` 通过 `/run/mica/mica-create.socket` 与 `micad` 通信
- `micad` 未启动、已退出、socket 未创建，或当前用户权限不足

处理：
- 检查 `micad` 进程或服务状态
- 确认以 root 权限执行 `mica`
- 若 socket 残留或服务异常，先恢复 `micad` 再重试

### 6.3 `mica create` 报 `No such file` 且路径为空或异常字符

症状：
- `mica create` 报 `No such file`
- 报错后的路径为空或包含异常字符

原因：
- 如果路径正常，通常是 `ClientPath` 指向的文件不存在
- 如果路径为空或异常，可能是 `mica.py` 与 `micad` 版本不一致导致 socket 消息结构不匹配
- 也可能是自定义编译环境造成结构体对齐差异

处理：
- 检查配置文件中的 `ClientPath`
- 同步更新 `mica.py` 与 `micad`
- 使用一致的 SDK 和编译选项重新构建组件

### 6.4 `mica start` 报 `boot clientos failed(-4)`

症状：
- baremetal 场景下 `mica start` 失败
- 报错包含 `boot clientos failed(-4)`

原因：
- `-4` 对应 PSCI `ALREADY_ON`
- MICA 通过 PSCI 拉起目标核时，发现目标核已经在线

处理：
- 确认 client OS 目标 CPU 未被 Linux 上线
- 使用 `maxcpus=` 启动参数预留目标核
- 或运行时通过 `/sys/devices/system/cpu/cpuX/online` 下线目标核

### 6.5 `AutoBoot=yes` 后手动 `mica start` 失败

症状：
- 配置文件中 `[Mica]` 设置了 `AutoBoot=yes`
- `mica create <conf>` 已提示 `start <name> successfully!`
- 随后手动执行 `mica start <name>` 又提示 `start <name> failed!`

原因：
- `AutoBoot=yes` 时，`mica create` 成功后会自动向实例控制 socket 发送 `start`
- 再手动执行 `mica start <name>` 属于重复启动，不一定代表首次启动失败

常见识别线索：
- `mica status` 显示该实例已经是 `Running`
- syslog 中可能出现 `Start failed, ret(...)`、`start client OS failed`、`create rpmsg device failed` 或 service 重复创建相关错误
- 具体底层日志与 pedestal 相关，不应只按单一报错判断

处理：
- 先运行 `mica status` 确认状态
- 如果已经是 `Running`，无需再执行 `mica start <name>`
- 如果不是 `Running`，再按 `mica start` 失败路径继续诊断

代码依据：
- `mica/micactl/mica.py` 中 `send_create_msg()` 解析 `AutoBoot`
- create 返回 `MICA-SUCCESS` 后，`AutoBoot=yes` 分支会发送 `start`
- `mica/micad/socket_listener.c` 中 `start` 控制命令最终调用 `mica_start()`
- `library/mica/mica.c` 中 `mica_start()` 会继续执行 `load_client_image()`、`start_client()`、`create_rpmsg_device()` 和 debug/rpmsg service 创建

### 6.6 baremetal 场景 `mmap failed: mmap memory is not in mcs reserved memory`

症状：
- baremetal 场景下 `mica create` 成功，但 `mica start` 失败
- `mica status` 常停在 `Ready`
- 内核日志出现：
  - `mmap failed: mmap memory is not in mcs reserved memory for ped_type ...`

原因：
- baremetal 路径会直接按 ELF 中的 `resource_table` 等地址执行 `remoteproc_mmap()`
- DTS 多段 `memory-region` 场景下，`mcs_km` 可以识别多个 reserved memory 区间
- ACPI 或无 DTS、只通过 `rmem_base/rmem_size` 传参时，`mcs_km` 只能看到一个连续窗口
- 如果 `.resource_table`、vring 或其他需要 `mmap` 的地址不在这个窗口内，`mica start` 就会失败

常见识别线索：
- `mica status` 为 `Ready`，而不是 `Running`
- `grep mcs /var/log/messages` 或 `dmesg` 中能看到 `/dev/mcs` 的 `mmap failed`
- `readelf -S/-x .resource_table <client.elf>` 显示 `.resource_table` 地址不在 Linux/master 当前声明的 `rmem_base/rmem_size` 窗口里
- 现场历史上用 DTS 多段 reserved memory 能工作，切到 ACPI/单窗口 `rmem_base/rmem_size` 后开始失败

处理：
- 先确认当前部署是 DTS 多段 `memory-region`，还是 ACPI/`rmem_base/rmem_size` 单窗口
- 用 `readelf -S/-x .resource_table <client.elf>` 核对 `.resource_table` 的真实地址
- 确保所有会被 `/dev/mcs` `mmap` 的地址都落在 `mcs_km` 可见范围内，而不只是共享内存第一页

### 6.7 MCS feature 构建时不可选

症状：
- 首次通过 oebuild generate 选择 feature 时，发现 `mcs` 不可选

原因：
- 新开发板未加入 MCS 特性支持列表

处理：
- 在 yocto-meta-openeuler 的 MCS feature 支持列表中增加目标开发板名称
- 该问题属于构建/平台接入问题，不属于运行时 lifecycle 失败

## 7. 阅读路径

如果已经知道问题属于哪个动作，建议直接继续阅读：

- create：`../../../mica-lifecycle/references/lifecycle-create.md`
- start：`../../../mica-lifecycle/references/lifecycle-start.md`
- stop：`../../../mica-lifecycle/references/lifecycle-stop.md`
- remove：`../../../mica-lifecycle/references/lifecycle-remove.md`

如果已经怀疑问题落到 OpenAMP `remoteproc`：

- `../../../mica-lifecycle/references/openamp-remoteproc.md`

如果主要症状已经转成服务不可用：

- `communication-diagnosis.md`

## 8. 输出要求

生命周期诊断回答应包含：

- 失败阶段或归属层
- 当前状态
- 关键证据
- 排除项
- 最可疑代码层
- 下一步验证动作
- 继续下钻的文档或代码位置

`mica start` 失败回答应明确给出：
- 失败阶段
- 最可疑代码层
- 下一跳建议

## 9. 建议继续阅读

- `../../../mica-lifecycle/references/lifecycle-overview.md`
- `../../../mica-lifecycle/references/lifecycle-create.md`
- `../../../mica-lifecycle/references/lifecycle-start.md`
- `../../../mica-lifecycle/references/lifecycle-stop.md`
- `../../../mica-lifecycle/references/lifecycle-remove.md`
- `../../../mica-lifecycle/references/openamp-remoteproc.md`
- `communication-diagnosis.md`
