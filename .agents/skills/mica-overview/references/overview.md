# MICA 总览

这里的混合部署不是单仓内的单侧逻辑，而是横跨多个执行域、多个代码层级的系统。Linux/master 侧负责上层控制与组织；RTOS/client 侧承载远端运行时行为；OpenAMP/libmetal 则负责更底层的远程处理器控制、消息传输和平台抽象。

对 agent 来说，最重要的一条规则是：如果代码路径在某一层突然消失，不要停下来，要继续进入下一层。

## 1. 背景模型

任何 MICA 任务都应先建立下面几个基础认识：

- MICA 是跨 Linux/master 与 RTOS/client 的混合部署框架
- Linux/master 侧主要负责控制面、生命周期编排、实例状态和服务可见面
- RTOS/client 侧主要负责远端运行时、service endpoint、receiver、service thread 与 client OS 适配
- pedestal 负责把 MICA 抽象接到具体隔离环境、硬件平台、shared memory、IRQ、notify 与 backend 能力
- OpenAMP/libmetal 是生命周期和通信继续下钻时会进入的底层机制层

## 2. 架构分层

MICA 相关问题通常至少跨越以下层级：

1. Linux/master 控制与生命周期层
2. RTOS/client 运行时层
3. communication service 层
4. pedestal 与平台适配层
5. OpenAMP/libmetal 机制层

因此，单个文件或单侧实现通常不足以解释完整行为。

## 3. 任务入口关系

建立背景模型后，再根据用户任务进入对应工作流：

- 使用、开发、调试、测试、学习、评审任务：`../../mica-common-tasks/SKILL.md`
- Linux/master 侧结构：`../../mica-linux-master/SKILL.md`
- RTOS/client 侧结构：`../../mica-rtos-client/SKILL.md`
- 生命周期机制：`../../mica-lifecycle/SKILL.md`
- 通信机制：`../../mica-communication/SKILL.md`
- pedestal 机制：`../../mica-pedestals/SKILL.md`
- 调试与跨层边界：`../../mica-common-tasks/references/debugging-workflow/debugging-overview.md`

## 4. 代码定位模型

代码定位内容按问题域嵌入对应文档：

- 生命周期命令定位：`../../mica-common-tasks/references/usage-workflow/lifecycle-guide.md`
- 生命周期阶段定位：`../../mica-lifecycle/references/lifecycle-overview.md`
- 通信服务定位：`../../mica-communication/references/communication-overview.md`
- 跨层边界定位：`../../mica-common-tasks/references/debugging-workflow/boundary-diagnosis.md`

定位代码时不要绕过任务分类。先判断使用、开发、调试、测试、学习或评审意图，再进入对应 domain 文档中的代码定位章节。

## 5. 边界原则

本仓是 Linux/master 侧主实现的核心位置，但并不是所有运行时细节都在本仓里。

当调用链在本仓中变黑盒时，应优先判断是否进入：

- OpenAMP `remoteproc`
- OpenAMP virtio / RPMsg
- libmetal 地址、I/O、cache 语义
- RTOS/client 侧 `libmica` 或历史 RTOS 实现

## 6. 外部组件源码获取规则

当分析推进到本仓之外，且需要继续查看外部组件源码时，agent 应先确认用户本地是否已经存在对应源码。

处理规则：
- 先问用户本地是否已有目标组件源码
- 如果用户已经有本地源码，优先使用用户现有路径
- 如果用户本地没有，agent 可以直接在当前仓下创建 `components/` 目录并获取源码
- 外部组件源码获取统一通过 `.agents/tools/fetch_component_source.py` 处理
- 大型仓库下载可能较慢，执行前应明确告知用户耐心等待

常见组件包括：
- `yocto-meta-openeuler`
- `Jailhouse`
- `OpenAMP`
- `libmetal`
- `UniProton`
- 其他 RTOS 相关代码仓

统一工具：

```sh
python3 .agents/tools/fetch_component_source.py <component>
```

例如：

```sh
python3 .agents/tools/fetch_component_source.py yocto-meta-openeuler
python3 .agents/tools/fetch_component_source.py openamp
python3 .agents/tools/fetch_component_source.py jailhouse
python3 .agents/tools/fetch_component_source.py libmetal
python3 .agents/tools/fetch_component_source.py uniproton
python3 .agents/tools/fetch_component_source.py openamp --yocto-root <path-to-yocto-meta-openeuler>
python3 .agents/tools/fetch_component_source.py <component> --url <repo-url>
```

后续源码定位规则：
- 默认获取到当前仓下的 `components/` 目录
- 如果组件目录下有 `source/` 子目录，后续优先阅读 `components/<Component>/source/`
- 如果组件目录下没有 `source/` 子目录，后续直接阅读 `components/<Component>/`
- `--yocto-root` 用于指定本地已有的 `yocto-meta-openeuler` 路径；如果用户本地已经有这个仓，优先传入该参数，避免重复下载大仓
- `--url` 用于脚本内置组件之外的其他代码仓，或当用户明确指定了不同源码地址时直接使用该地址
