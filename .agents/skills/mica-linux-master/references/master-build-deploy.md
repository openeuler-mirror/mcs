# Master 侧构建与部署

## 1. 文档目标

这篇文档说明 Linux/master 侧 MICA 组件如何在 openEuler Embedded SDK 环境下构建并部署到目标系统。

它只覆盖 Linux/master 侧组件。RTOS/client image 的来源和构建边界见 `../../mica-rtos-client/references/client-build-deploy.md`。

## 2. 构建前提

构建前应先 source 目标环境对应的 SDK：

```sh
. /path/to/sdk/environment-setup-aarch64-openeuler-linux
```

至少确认：

```sh
aarch64-openeuler-linux-gcc -v
test -n "$SDKTARGETSYSROOT"
```

如果需要构建 KO，还应确认：

```sh
test -n "$KERNEL_SRC"
```

SDK 应与目标运行环境匹配。涉及 KO 时，不能用无关镜像或无关开发板的 SDK 替代。

如果用户态构建失败并提示缺少 `metal/alloc.h` 或找不到 `libmetal`、`libopen_amp`，优先检查 SDK 是否来自带 MCS 能力的目标镜像。普通 `qemu-aarch64` SDK 可能没有这些开发依赖。

## 3. 用户态组件构建

仓根 CMake 构建会覆盖 `library` 和 `mica/micad` 等 Linux/master 侧用户态组件。

常规构建：

```sh
cmake -S . -B build
cmake --build build
```

实测在 MCS SDK 下，`micad` 可成功交叉构建为 aarch64 可执行文件。构建过程中可能出现非阻断 warning，例如 `socket_listener.c` 中 `snprintf` 截断风险提示，或 `rpmsg_pty.c` 中 `ptsname_r` 隐式声明提示。warning 不等于 smoke test 失败，但 review 时应按代码改动范围判断是否需要处理。

需要调试信息时：

```sh
cmake -S . -B build -DCMAKE_BUILD_TYPE=Debug
cmake --build build
```

主要产物：

- `build/mica/micad/micad`
- `mica/micactl/mica.py`
- `rtos/arm64/*.conf`

`mica.py` 是目标侧命令脚本，不需要 C 编译，但部署后应具备可执行权限并位于目标系统 PATH 中。

## 4. KO 构建

baremetal 和 hetero 场景通常使用 `mcs_km.ko`，xen 场景使用 `xen-mcsback.ko`。

构建入口：

```sh
cd mcs_km
make
```

`mcs_km/Makefile` 通过 `KERNEL_SRC` 进入 SDK 提供的内核构建目录：

```makefile
$(MAKE) -C $(KERNEL_SRC) M=$(SRC)
```

因此 KO 构建前必须确认 `KERNEL_SRC` 有效，并且该 SDK 与目标镜像或开发板运行内核匹配。

## 5. 目标侧部署对象

Linux/master 侧常见部署对象包括：

- `micad` -> `/usr/bin/micad`
- `mica/micactl/mica.py` -> `/usr/bin/mica`
- `rtos/arm64/*.conf` 或用户配置 -> `/etc/mica/`
- `mcs_km.ko` 或 `xen-mcsback.ko` -> `/lib/modules/$(uname -r)/`
- `libmetal.so*`、`libopen_amp.so*`、`libsysfs.so*` -> 目标系统运行库目录，常见为 `/usr/lib64`

部署路径应以目标镜像实际目录为准。不要假设所有环境都使用相同 rootfs 布局。

## 6. 依赖库部署

用户态 `micad` 依赖 SDK sysroot 中的运行库。需要从 SDK 中确认并部署目标架构库：

```sh
find "$SDKTARGETSYSROOT" -name 'libmetal.so*'
find "$SDKTARGETSYSROOT" -name 'libopen_amp.so*'
find "$SDKTARGETSYSROOT" -name 'libsysfs.so*'
```

如果目标系统已经预置同版本依赖库，可以不重复覆盖；如果替换依赖库，应确认它们与当前 `micad` 构建使用的 SDK 一致。

## 7. KO 部署与加载

KO 部署到目标系统后，通常需要更新模块依赖并加载：

```sh
depmod
modprobe mcs_km
```

或按目标 pedestal 加载对应 KO。

如果使用 `insmod`，应记录完整错误输出和 `dmesg`。KO 加载失败优先检查 SDK 与目标内核是否匹配、符号是否缺失、kABI 是否变化、目标设备树或内核配置是否满足要求。

## 8. `micad` 启动

部署新 `micad` 后，应确认旧进程处理方式。常见策略包括：

- 停止已有 `micad`
- 清理旧 runtime socket 或状态文件，按调试目标决定是否保留现场
- 启动新 `micad`
- 确认 `mica status` 能连接到 `micad`

如果目标系统使用 init 脚本或 systemd 管理 `micad`，应优先按目标系统服务管理方式重启，不要假设可以直接后台执行 `micad &`。

## 9. 验证入口

部署完成后，进入测试工作流：

- 环境准备确认：`../../mica-common-tasks/references/testing-workflow/test-env.md`
- 基础 smoke test：`../../mica-common-tasks/references/testing-workflow/smoke-test.md`

如果失败表现为生命周期异常，进入 `../../mica-common-tasks/references/debugging-workflow/lifecycle-diagnosis.md`。
