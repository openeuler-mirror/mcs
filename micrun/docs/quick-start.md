# MicRun

MicRun 是一个基于 containerd shimv2 的容器运行时，专为 Mica 项目设计，用于在不同 CPU 核上运行 RTOS（实时操作系统）。

## 为什么要有 MicRun

边侧的RTOS业务往往需要自用的管理系统，如果能云化处理, 用 Kubeedge 管理，就可以利用这些平台的优势，比如RTOS适配了MQTT接口后，与 Kubeedge在低带宽或者脱机状况下仍然维护集群中节点的状态
而要接入 Kubeedge, 首先应该先让边侧的业务能够接收 Kubeedge 控制面管理的请求, 而该请求是一个 Kubelet-like,因此考虑通用性，问题聚焦为“如何将RTOS适配到Kubernetes中”

运行上述的一切,还需要一个 Linux 设备来作为本地控制面,我们通过混合关键性系统部署(mcs)来运行RTOS在同一SoC上。此处的mcs方案为 MICA, 问题进一步推导为"如何将Mica纳入Kubernetes管理"

想要将 RTOS  接入到 K8s 中，有几种常见路子：

* 实现一个K8s CRD来管理 mica, K8s devops 工程师写 operator 就是做这种事情
* 实现一个 KubeVirt 类型的框架
* 实现一个容器 low-level runtime, 将RTOS作为某种容器 (容器化方案)
* 将RTOS适配到 WASM micro runtime, ocrn 等边缘特化的 runtime 中

第二种方案的讨论价值不高，第四种方案无法充分利用mica混合部署的能力，因此,值得考量的是CRD方案和MicRun

对于专业的 K8s 开发者而言，CRD 方案是十分自然的, 我们可以绕过容器引擎, 并且按经典实践规范好 restAPI, service endpoint, 扩展mica, 将资源的管理和规范都在 operator 中实现好。
而 容器化 方案有十分自然的优点：它还可以做到RTOS容器化, 随着RTOS容器化的支持逐渐增加，可以让mica RTOS支持更多使用特性，并且可以利用容器镜像分发特性，减少镜像的构建,将镜像托管在镜像仓
并且可以在结构上,让混合部署底座逐步通往云侧, 每一步都有对应方案。泛用性会略高。


oci容器化有至少3种策略，
1. 让RTOS自身容器化，并且让混合部署能够将RTOS容器能力暴露给容器引擎, 调整 ocispec中特定RTOS的相关定义
2. 模拟 Linux 容器，
3. 使用 WAMR 策略
而我们优选 containerd 作为容器引擎, 它也是 Kubernetes 现在默认的 runtime endpoint.

从而，我们的问题就变为 "如何实现一个容器 runtime

镜像分发问题：

我们以 dockerhub 的镜像分发为例，对于主流 docker 容器镜像的**使用者**， 一个镜像有这些关键要素用于**选择所需**



## 快速使用


使用 MicRun 包含几步：

1. 构建包含相关特性 openEuler Embedded 镜像 
1. (可选)在host上主动构建镜像
1. 启动镜像，使用容器引擎load镜像或pull镜像
1. 注册 MicRun 作为runtime，使用容器引擎运行RTOS混合部署镜像

如果要试验集群加入：
1. 部署集群node，注册 MicRun 作为 K8s/K3s 集群 Runtimeclass
1. 在云侧部署 RTOS pod

> **如果没有 docker, nerdctl, podman 的使用经验，建议先[快速上手 docker](https://docs.docker.com/get-started/)**
>
> MicRun目前处于 preview 阶段，欢迎在[混合部署repo](https://gitee.com/openeuler/mcs/issues)反馈缺陷，


### 构建 MicRun

进入 `oebuild bitbake` 创建的容器环境中构建 opeuler-image 镜像。MicRun 在构建镜像时会被自动打包进系统。使用 `bitbake micrun` 可以单独构建该软件包

MicRun 是一个不使用 CGO的用户态静态链接的golang二进制, 是一个标准的golang实践。

> 在此文档中，你会使用 openEuler/mcs 下的 micrun 代码来构建,
> 这是一个 minimal micrun, 尽可能保证了代码和结构清晰, 方便后续敏捷.
> 当前版本的 MicRun 的 vendor dir 是不稳定的，所以暂时是 go mod 构建, 想要使用vendor构建，可以手动在源码目录运行 `go mod vendor` 生成 vendor 目录

为了在openEuler Embedded 中使用 MicRun, 镜像需要具备:

* kernel支持 容器功能，具备相关容器引擎
* kernel支持 K8s功能(如果要使用集群功能)
* 添加mcs,micrun特性
* 使用 xen 作为 mcs 底座
* systemd (最好是)

本指导使用 containerd + nerdctl 演示，你可以这样来构建镜像：

```
oebuild generate -p qemu-aarch64 -f micrun -f mcs -f xen -f containerd -f systemd -d playmicrun
# 如果你使用了最新了 oebuild 以及配套的 新版 yocto features目录，你可以运行：
# oebuild neo-generate -p qemu-aarch64 -f micrun -f mcs/xen -f containerd -d playmicrun
# 如果需要k3s,则添加 -f k3s-agent
cd playmicrun
oebuild bitbake
bitbake openeuler-image
```

### 进入 openEuler Embedded

1. [参考mica-xen指导文档](https://embedded.pages.openeuler.org/master/features/mica/instruction.html) 确保配置好 xen, 

如果使用 qemu-aarch64 来试用，请注意 qemu 版本不宜过低:
> 1. 低版本qemu存在影响xen的bug，会造成RTOS xen镜像卡死，建议使用高版本qemu。 
> 可参考 [qemu.org — Build instructions](https://www.qemu.org/download/)

1. (可选)构建镜像
> 和标准 docker 镜像 `[os,arch]` 2元组不同，RTOS 容器的匹配需要这样的4元组: `[board, os, arch, hypervisor]`
> 因此, 可用的构建镜像需要同时匹配这四个特征，我们需要用特化的镜像打包方式
> 
1. 启动系统
1. load本地镜像或pull镜像
1. 为 containerd 注册运行时
1. (如果使用集群) 为 k3s-agent 注册 micrun runtimeclass


#### 在 containerd 上注册

> 通过 `--runtime io.containerd.<runtime name>` 选项，用户可以指定运行容器的运行时（如果在 `$PATH` 上安装）。
> 我们可以使用 containerd shim 运行时 [无需在 PATH 上安装](https://docs.docker.com/engine/daemon/alternative-runtimes/#use-a-containerd-shim-without-installing-on-path)

通常，在 `/etc/containerd/config.toml` 中添加一个新插件：

```toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micrun."]

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  # 保持为空
  # micrun 命令行配置选项设计还不稳定
```

#### 注册为 Kubernetes RuntimeClass

```yaml
version: v1
runtimeClass:
  name: micrun
  type: RuntimeClass

```

```shell
kubelet --cpu-manager-policy=static
# isolcpus, nohz_full, ... 可以自定义
```


#### 使用运行时

使用 nerdctl

```shell
# 注意，在 nerdctl 中 '--label' 选项不是 docker 中的 "Label"，它是 "Annotation"
# 因此 -l 选项将注解传递给容器 oci 配置
nerdctl run -d --runtime io.containerd.mica.v2 -l org.openeuler.micran.auto_disconnect=true <image>
nerdctl update --memory 1024m  <container_id>
```

使用 ctr (`containerd-ctr` 测试工具, 用于开发者)

```shell
ctr container create --runtime io.containerd.mica.v2 -t --annotation org.openeuler.micran.auto_disconnect=true <image> <container_id>
ctr task start <container_id> # 会进入容器shell, 确认到 RTOS 被拉起了
ctr task kill <container_id>
ctr task del <container_id>
```
