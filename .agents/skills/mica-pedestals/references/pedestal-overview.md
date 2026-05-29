# pedestal 总览

## 1. 文档目标

这篇文档给出 MICA 中 pedestal 这一维度的整体认知框架，帮助 agent 先判断：
- 某个问题是不是已经进入 pedestal-specific 实现
- 应该优先看哪一类源码文件
- 某个 pedestal 文档里应该期待看到哪些内容

它不负责展开某一个具体 pedestal 的全部实现细节；那些内容分别放在：
- `ped-baremetal.md`
- `ped-hetero.md`
- `ped-jailhouse.md`
- `ped-xen.md`

如果一个 agent 先要判断“这个问题是公共 lifecycle 问题，还是某个 pedestal 自己的问题”，应该先读这篇。

## 2. pedestal 在 MICA 中的决定作用

在 `library/remoteproc/remoteproc_core.c: create_client()` 中，MICA 会根据 `client->ped` 和部分 `ped_setup` 字段选择对应的 `remoteproc_ops`：
- `BARE_METAL` -> `rproc_bare_metal_ops`
- `JAILHOUSE` -> `rproc_jailhouse_ops`
- `XEN` -> `rproc_xen_ops`
- `HETERO && cpu_str == "riscv"` -> `rproc_riscv_ops`

这意味着 pedestal 会直接决定：
- 进入哪个 backend 文件
- `.init/.config/.start/.shutdown/.remove/.notify/.mmap` 由谁实现
- 使用哪个 KO 或 `/dev/*` 设备
- ioctl、共享内存、通知、中断、resource table 的具体处理方式
- RTOS/client 侧要满足什么对接契约

因此 pedestal 不是一个轻量配置项，而是同一套 lifecycle 骨架下的实现分支。

## 3. pedestal 在源码中的主要落点

从 Linux/master 侧当前仓内代码看，主要 pedestal backend 文件是：
- `library/remoteproc/baremetal_rproc.c`
- `library/remoteproc/riscv_rproc.c`
- `library/remoteproc/jailhouse_rproc.c`
- `library/remoteproc/xen_rproc.c`

它们分别承载各自 pedestal 的：
- `remoteproc_ops`
- `.init/.config/.start/.shutdown/.remove/.notify/.mmap`
- pedestal-specific 私有状态
- 与 KO、设备节点、外部运行环境之间的接口

从代码定位角度看，pedestal 问题通常先从这四类文件之一开始，而不是先从 `library/mica/mica.c` 找全部答案。

## 4. 当前 pedestal 实现视图

### 4.1 baremetal
主落点：
- `library/remoteproc/baremetal_rproc.c`
- `mcs_km/mcs_km.c`

关键特征：
- 使用 `/dev/mcs`
- 主要通过 PSCI、IPI、reserved memory、poll/notifier 来管理远端
- shared memory 和 resource table 由 baremetal 路径自己准备
- 当前 `ped-baremetal.md` 已按代码追踪手册风格展开到 create/start/stop/remove、`/dev/mcs`/ioctl、共享内存首页规则、notify/poll 闭环、RTOS 契约与调试入口

### 4.2 hetero
主落点：
- `library/remoteproc/riscv_rproc.c`
- `library/remoteproc/remoteproc_core.c`
- `mcs_km/mcs_km.c`
- `rtos/libmica/src/pedestals/hetero.c`

关键特征：
- 当前已落地的 hetero 主分支主要是 riscv
- 也使用 `/dev/mcs`
- 通过 `IOC_SET_PED_TYPE` 区分 hetero 的子类型
- resource table、shared memory、bootaddr、pedestal bin、notify 路径都带有异构分支特征
- 当前 `ped-hetero.md` 已按 baremetal 的深度标准重写，并补上 Linux/master 与 RTOS 两侧的运行时契约

这里需要特别注意：
- 文档名是 `ped-hetero.md`
- 当前正文会以 riscv 子分支为主
- 后续如果 hetero 扩展到其他架构，应继续在同一篇文档内扩展子章节，而不是重新发明新的顶层分类

### 4.3 jailhouse
主落点：
- `library/remoteproc/jailhouse_rproc.c`
- `yocto-meta-openeuler/meta-openeuler/recipes-kernel/linux/files/meta-data/features/mcs/0003-uio-Add-driver-for-inter-VM-shared-memory-device.patch`

关键特征：
- 主要围绕 cell 生命周期
- 共享内存和通知通常与 ivshmem 绑定
- resource table 与 cell load/start 流程耦合更紧
- 关键设备基础不在 `mcs` 仓内，而要结合 `uio_ivshmem` 驱动一起理解
- 当前 `ped-jailhouse.md` 已覆盖 userspace backend、ivshmem/UIO、resource table 布局、doorbell/notifier 闭环与调试入口

### 4.4 xen
主落点：
- `library/remoteproc/xen_rproc.c`
- `mcs_km/xen-mcsback.c`

关键特征：
- 使用 `/dev/mcs_xen`
- 强依赖 grant table、event channel、xenbus、Dom0/DomU 映射关系
- shared memory 与通知路径都不是 baremetal/hetero 的那套模式
- 当前 `ped-xen.md` 已补齐 Dom0 backend、grant/evtchn、resource table 布局、guest 运行时契约与调试入口

## 5. pedestal 与 KO / 设备节点 / ioctl 的关系

当前代码里，KO 与 `/dev/*` 设备并不是一套完全公共的抽象层，而是明显和 pedestal 实现绑定。

例如：
- baremetal / hetero 当前主要围绕 `mcs_km/mcs_km.c` 和 `/dev/mcs`
- xen 则是 `mcs_km/xen-mcsback.c` 和 `/dev/mcs_xen`
- 各自的 ioctl、poll、shared memory、event 机制也不相同

因此理解某个 pedestal 时，通常应同时回答这三个问题：
1. 对应哪个 `*_rproc.c`
2. 对应哪个 KO 或设备节点
3. 对应哪些 ioctl / notify / mmap / poll 语义

也正因为这种绑定关系，KO / 设备 / ioctl 更适合作为各 ped 文档的主体内容，而不是被抽成一个脱离 pedestal 的统一章节。

## 6. pedestal 与 lifecycle 的关系

从阅读和调试角度，可以把两者关系理解成：
- lifecycle 文档回答“当前系统推进到哪个阶段”
- pedestal 文档回答“这个阶段在某个 pedestal 下究竟怎样落地”

也就是说：
- `lifecycle-create.md` / `lifecycle-start.md` / `lifecycle-stop.md` / `lifecycle-remove.md`
  负责公共主线、阶段语义、分叉入口
- `ped-*.md`
  负责该 pedestal 自己的 backend、KO、设备、shared memory、resource table、notify、RTOS 契约

如果某个问题在 lifecycle 文档里已经进入“pedestal-specific 分支”，下一跳通常就应转到对应 `ped-*.md`。

## 7. 每篇 ped 文档应该回答哪些问题

建议把每篇 `ped-*.md` 都写成尽量一致的结构，至少回答：

1. 这篇文档解决什么问题
2. 涉及哪些 Linux/master 侧源码文件
3. 关键结构体与状态对象
4. 对应哪个 KO / `/dev/*` / ioctl
5. create 在这个 ped 下怎么落
6. start 在这个 ped 下怎么落
7. stop 在这个 ped 下怎么落
8. remove 在这个 ped 下怎么落
9. resource table 在这个 ped 下怎么管理
10. shared memory 在这个 ped 下怎么拿和怎么用
11. notify / interrupt / poll / evtchn 等机制怎么走
12. RTOS/client 侧需要满足什么契约
13. 常见排查入口
14. 与 lifecycle / openamp / rtos 文档如何跳转

这样以后 agent 在不同 pedestal 之间切换时，阅读模型会更稳定。

## 8. 各 ped 文档的阅读重点

### 8.1 `ped-baremetal.md`
重点看：
- `baremetal_rproc.c`
- `mcs_km.c` 中 baremetal 相关分支
- `/dev/mcs`
- `IOC_QUERY_MEM` / `IOC_CPUON` / `IOC_SENDIPI`
- reserved memory / PSCI / IPI / notifier
- shared memory 和 resource table 的准备
- create/start/stop/remove 如何与 `/dev/mcs`、共享内存首页、notify/poll 闭环对应起来

### 8.2 `ped-hetero.md`
重点看：
- `riscv_rproc.c`
- `remoteproc_core.c` 中 hetero -> riscv 的分派条件
- `mcs_km.c` 中 hetero/riscv 相关分支
- `rtos/libmica/src/pedestals/hetero.c`
- `/dev/mcs`
- `IOC_SET_PED_TYPE` / `IOC_QUERY_MEM` / `IOC_MCUON` / `IOC_SENDIPI`
- resource table 放共享内存首页的路径
- pedestal bin / bootaddr / notifier / poll
- Linux/master 与 RTOS 两侧如何共同完成 virtio ready、service ready 与停机语义

### 8.3 `ped-jailhouse.md`
重点看：
- `jailhouse_rproc.c`
- `uio_ivshmem` 驱动 patch
- jailhouse cell create/load/start/shutdown/destroy
- ivshmem 共享内存与 doorbell 通知路径
- resource table 如何放到 RW section 首页
- `/dev/uioX`、sysfs map2、one-shot interrupt re-enable
- Linux/master userspace backend 与 ivshmem PCI/UIO 之间的衔接

### 8.4 `ped-xen.md`
重点看：
- `xen_rproc.c`
- `xen-mcsback.c`
- `/dev/mcs_xen`
- `IOC_SET_DOMID` / `IOC_QUERY_MEM` / `IOC_INVOKE_EVTCHN`
- grant table / evtchn / xenbus / backend_info
- Dom0/DomU 之间共享内存和通知的映射
- guest 侧运行时契约、`shmem_dynamic`、`vring_offset` 与地址翻译边界

## 9. pedestal 问题的入口判断

可以用下面这个简单规则：

1. 如果问题是：
- create/start/stop/remove 走到哪一步
- 为什么控制面显示某个状态
- 为什么实例还在 / 不在
先看：
- `mica-lifecycle`

2. 如果问题是：
- 为什么这个 pedestal 的共享内存不通
- 为什么这个 pedestal 的 ioctl / /dev 行为不同
- 为什么这个 pedestal 的 resource table / notify / backend 清理不同
先看：
- 对应 `ped-*.md`

3. 如果 pedestal 文档已经解释不动，继续下沉到：
- `mica-lifecycle`
- `mica-rtos-client`
- `mica-common-tasks/references/debugging-workflow/debugging-overview.md`

## 10. 建议阅读顺序

如果你还不知道问题属于哪一层，建议顺序如下：
1. `mica-lifecycle/references/lifecycle-overview.md`
2. 本文 `pedestal-overview.md`
3. 对应 `ped-*.md`
4. 如果问题继续下沉，再看：
   - `mica-common-tasks/references/debugging-workflow/debugging-overview.md`
   - `mica-rtos-client`
   - `mica-common-tasks/references/debugging-workflow/debugging-overview.md`

从当前完成度看，ped 体系里：
- `ped-baremetal.md` 已是高细节、可直接带代码追踪的版本，当前应保持冻结
- `ped-hetero.md` 已按 baremetal 深度补齐，并覆盖 Linux/master + RTOS 两侧契约
- `ped-jailhouse.md` 已补齐 userspace backend、ivshmem/UIO 与 cell 生命周期这条主线
- `ped-xen.md` 已补齐到可用于理解虚拟化 backend、grant/evtchn 与 guest 契约的深度
