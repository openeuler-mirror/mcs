# Client 侧镜像来源与构建边界

## 1. 文档目标

这篇文档用于判断 MICA RTOS/client image 从哪里来、应按哪条构建路径生成，以及如何确认它能被 Linux/master 侧 `mica` 启动。

Smoke test 步骤不在这里定义；镜像准备完成后进入 `../../mica-common-tasks/references/testing-workflow/smoke-test.md`。

## 2. 先判断 client 属于哪一类

MICA client 侧构建分两类：

1. 历史内嵌适配路径：Zephyr、UniProton
   - 这两类是 MICA 早期接入的 RTOS，当时还没有独立的 RTOS 专用 `rtos/libmica`。
   - MICA 适配代码嵌入在各自 RTOS 源码或 Yocto recipe 补丁中。
   - 不要直接套用 `rtos/libmica/README.md`。

2. 新 RTOS / 后续收敛路径：`rtos/libmica`
   - 后续新增 RTOS 应优先使用 `mcs/rtos/libmica`，把 client 侧 MICA runtime 从 RTOS 私有工程中解耦出来。
   - 使用方法以 `rtos/libmica/README.md` 为准。

如果目标只是验证 Linux/master 侧改动，且 QEMU/开发板镜像中已经预置了 `zephyr.elf`、`zephyr.bin` 或其他 client image，通常先复用镜像内的 client image，不必重编 RTOS。

## 3. Zephyr 路径

### 3.1 来源与 Yocto 集成

当前 openEuler Embedded 版本中的 Zephyr 镜像已经改为源码编译。MCS 镜像中预置的 Zephyr image 通常来自 Yocto 构建：Yocto 拉取 Zephyr 源码包，然后打入 openEuler MCS 定制补丁，最终把 `zephyr.bin`、`zephyr.elf` 放入镜像。

需要跨仓分析或复现构建时，agent 应知道涉及仓库和来源，而不是要求用户自己去网页查：

- Zephyr 源码仓：以 `yocto-meta-openeuler/.oebuild/manifest.yaml` 中的 `zephyrproject` 条目为准，读取 `remote_url` 和 `version`。
- Yocto recipe 仓：`https://gitcode.com/openeuler/yocto-meta-openeuler.git`
- MCS 定制补丁位置：`yocto-meta-openeuler` 仓内的 `rtos/meta-zephyr/recipes-kernel/zephyr-kernel/files/`
  - `zephyr_openeuler_mcs.patch`：用于 baremetal / jailhouse Zephyr image 的 MCS 适配补丁。
  - `boards-xenvm-return-separate-defconfig-for-xenvm-wit.patch`、`xenvm-support-mcs-rpmsg-communication.patch`：用于 Zephyr xen image 的 MCS/Xen 适配补丁。
- Zephyr image recipe：`yocto-meta-openeuler` 仓内的 `rtos/meta-zephyr/recipes-kernel/zephyr-kernel/zephyr-image.bb`

`manifest.yaml` 中 Zephyr 条目的形态通常类似：

```yaml
zephyrproject:
  remote_url: https://atomgit.com/src-openeuler/zephyr.git
  version: <commit>
```

如果用户想走原生 openEuler Embedded 镜像构建路线，应进入 Yocto/oebuild 流程，再由 bitbake 构建对应 Zephyr image。此路线较重，但最贴近镜像集成结果。

### 3.2 手工本地构建

调试时如果 Yocto 构建过重，可以手工构建 Zephyr：

1. 准备 Zephyr 源码。
   - 先读取 `yocto-meta-openeuler/.oebuild/manifest.yaml` 中的 `zephyrproject` 条目，确认 Yocto 当前使用的 Zephyr `remote_url` 和 `version`。
   - 手工构建也应尽量使用 manifest 中同一份源码和 commit，避免本地构建结果与 Yocto 镜像中的 Zephyr 版本不一致。
   - Yocto 下载的软件包源码通常会以 tar 包形式存在；如果复用 Yocto workspace 中的 Zephyr 源码包，需要先解压，例如 `tar -zxf <zephyr-source>.tar.gz`。
   - 如果本地没有源码包，再按 manifest 中的 `remote_url` clone，并 checkout 到 manifest 中的 `version`。

2. 按目标 pedestal 打入 openEuler MCS 定制补丁。
   - baremetal / jailhouse Zephyr image 使用 `zephyr_openeuler_mcs.patch`。
   - xen Zephyr image 使用 `boards-xenvm-return-separate-defconfig-for-xenvm-wit.patch` 和 `xenvm-support-mcs-rpmsg-communication.patch`。
   - 补丁均来自 `yocto-meta-openeuler` 仓的 `rtos/meta-zephyr/recipes-kernel/zephyr-kernel/files/` 目录。
   - 没打对应 pedestal 补丁的原生 Zephyr image 通常不能直接作为 MICA client image 使用。

3. 进入 Zephyr workspace 环境。

   ```sh
   source workspace/.venv/bin/activate
   ```

4. 按 `zephyr-image.bb` 中的 board 和 west 参数执行构建。

   示例模型：

   ```sh
   west build -b xenvm/xenvm/gicv3 samples/synchronization --pristine
   ```

   实际 `-b`、sample、overlay 或额外参数必须以 `rtos/meta-zephyr/recipes-kernel/zephyr-kernel/zephyr-image.bb` 为准，不同平台不能混用。

5. 构建产物通常在：

   ```text
   workspace/zephyr/build/zephyr/zephyr.bin
   workspace/zephyr/build/zephyr/zephyr.elf
   ```

### 3.3 使用 Zephyr image 前的确认点

- `mica.conf` 中的 `ClientPath` 指向实际 `zephyr.elf` 或目标 image。
- board 类型、resource table、内存布局和目标 pedestal 匹配。
- baremetal/QEMU 场景下 Linux 侧 CPU 预留、dtb 中 `mcs-remoteproc` 节点和 Zephyr image 一致。
- 镜像中已包含与目标 pedestal 对应的 openEuler MCS 定制补丁；baremetal/jailhouse 与 xen 补丁不能混用。

## 4. UniProton 路径

### 4.1 来源与构建方式

UniProton 目前没有集成到 Yocto 中构建，需要单独 clone 仓库并使用 UniProton 提供的 Docker 构建环境。

源码仓：

```sh
git clone https://gitcode.com/openeuler/UniProton.git
```

首次准备 Docker 镜像：

```sh
docker pull swr.cn-north-4.myhuaweicloud.com/openeuler-embedded/uniproton:v004
```

进入构建容器：

```sh
cd UniProton
docker run -it -v $(pwd):/home/uniproton swr.cn-north-4.myhuaweicloud.com/openeuler-embedded/uniproton:v004
```

在容器内按目标平台进入 demo 构建目录：

```sh
cd demos/<对应平台>/build
sh build_app.sh
```

生成的 `.bin` 和 `.elf` 通常位于：

```text
demos/<对应平台>/build/
```

具体平台名称、工具链和构建参数参考 UniProton 仓内 `doc/demo_guide/UniProton_build.md`，但 agent 应优先 clone/read 本地仓库内容来判断，不要只把网页链接丢给用户。

### 4.2 UniProton 中 MICA 适配代码位置

UniProton 中大多数平台的 MICA client 侧适配代码在：

```text
src/component/mica/
```

只有极少数定制平台会在自己的 demo 目录下实现一套 MICA 通信逻辑。判断某个平台 image 是否使能 MICA 时，应检查：

- `src/component/mica/` 是否存在该平台需要的初始化、RPMsg、service 逻辑。
- 对应平台的 CMakeLists 或构建脚本是否把 `src/component/mica/` 编入最终 image。
- demo 私有目录是否覆盖或替代了通用 `src/component/mica/` 逻辑。

不能只看到 UniProton image 能构建成功就认为它已可作为 MICA client 使用。

## 5. 新 RTOS / `rtos/libmica` 路径

除 Zephyr、UniProton 这类早期内嵌适配外，后续新增 RTOS 应优先使用 `mcs/rtos/libmica`。

处理这类任务时直接进入：

```text
rtos/libmica/README.md
```

关键判断点：

- 目标 RTOS 是否已经提供 `mica_sys_ops`、线程、同步原语、中断、内存映射等 system 适配。
- `libmetal`、`open-amp`、resource table 和 shared memory 是否与目标 pedestal 匹配。
- `libmica.a` 如何被目标 RTOS 构建系统链接进最终 image。
- RTOS 初始化流程中是否调用 `mica_init()` 并创建所需 service。

`rtos/libmica` 构建出的库不是最终 client image。最终 `.elf`、`.bin` 仍由目标 RTOS 自身构建系统生成。

## 6. 与 Linux/master 配置的关系

无论 image 来自 Zephyr、UniProton 还是 `rtos/libmica` 路径，启动前都要确认配置一致性：

- `Name` 是后续 `mica start <name>` 使用的实例名。
- `ClientPath` 指向实际 image。
- `CPU`、`VCPU`、`Pedestal`、`PedestalConf` 与目标平台一致。
- baremetal 场景下 CPU 预留、dtb、resource table 和 image 内存布局匹配。
- `AutoBoot` 会影响 `mica create` 后是否自动启动。

如果这些条件不明确，不应进入 smoke test，应先回到构建或配置诊断。

## 7. 输出要求

处理 RTOS/client image 相关任务时，agent 应输出：

- 目标 RTOS 属于 Zephyr、UniProton 还是 `rtos/libmica` 路径。
- 涉及的仓库、branch/commit 或 Yocto recipe 来源。
- 实际构建入口：Yocto bitbake、west build、UniProton Docker build，或 RTOS 自身构建系统。
- MICA 适配代码来源：Yocto 补丁、`src/component/mica/`、demo 私有实现或 `rtos/libmica`。
- 产物路径，例如 `zephyr/build/zephyr/zephyr.elf` 或 `demos/<平台>/build/*.elf`。
- 与 Linux/master 侧 `mica.conf` 的匹配关系。
- 是否满足后续 smoke test 前置条件。
