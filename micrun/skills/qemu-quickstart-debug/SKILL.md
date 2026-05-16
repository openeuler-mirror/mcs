---
name: qemu-quickstart-debug
description: 当需要按本仓库的 quick-start 路径启动 MicRun QEMU、验证 usernet SSH 转发或 tap0 网络、进入 guest 后复现 MicRun RTOS shell 与 attach 行为，并进一步判断问题位于 MicRun、原生 mica/micad 还是 RTOS 固件侧时，使用这个 skill。
---

# QEMU Quickstart 排障

## 概述

这个 skill 用来统一 MicRun QEMU 的起机和排查步骤：先按
`docs/quick-start.md` 把环境拉起来，确认 guest 能进入系统；需要跑仓库
tap 网络脚本时，再确认 `tap0` 和 guest 网络都正常。随后用原生 `mica`
路径和 MicRun 路径交叉验证 RTOS shell 行为；必要时再在已有 Yocto 构建容器
里重编 `micad`，做最小替换验证。

## 适用场景

- 按仓库里的方式启动 MicRun 对应的 QEMU 环境。
- 核对 quick-start 的 usernet SSH 转发、Xen 和 guest 网络是否真的就绪。
- 在使用仓库 tap 脚本时，核对 `tap0`、`qemu-ifup`、sudo，以及边侧 SSH
  目标是否可访问，便于后续 IO 测试。
- 在文档要求的环境里复现 `ctr task start -t`、attach、prompt 和 RTOS
  shell 相关问题。
- 需要先判断问题是否已经在原生 `mica start qemu-uniproton-xen` 路径可复现，
  以免把 `micad` 或 RTOS 问题误判成 MicRun IO 回归。
- 需要借助现有构建 `docker` 镜像或容器，把当前分支、历史提交或临时补丁版
  `micad` 编出来后放到 guest 里做 A/B 对比。

## 先看这些文件

开始排查前，优先看下面几个仓库文件，不要凭感觉改启动方式：

- `docs/quick-start.md`
- `tests/common/qemu.sh`
- `tests/k3s/prepare_edge_node.sh`
- `tests/io/test_helpers.sh`
- `mica/micad/services/umt/rpmsg_umt.c`
- `library/user_msg/user_msg.c`
- `library/include/user_msg/user_msg.h`

如果任务和 RTOS shell 回归有关，在确认文档里的 QEMU 路径可用、guest
可达之前，不要先怀疑 MicRun IO。

## 工作流

### 0. 先抽象本地环境

记录、复现和提交文档时，不要写入当前机器的真实绝对路径、sudo 密码、guest
密码或访问密钥。用变量表达本地差异：

```bash
MCS_REPO=<path-to-mcs-repo>
QEMU_OUTPUT_DIR=<path-to-qemu-output-test-dir>
HOST_TAP_IFACE=${HOST_TAP_IFACE:-tap0}
HOST_TAP_IP=${HOST_TAP_IP:-192.168.7.1}
EDGE_IFACE=${EDGE_IFACE:-enp0s1}
EDGE_IP=${EDGE_IP:-192.168.7.2}
EDGE_SSH_USER=${EDGE_SSH_USER:-root}
EDGE_SSH=${EDGE_SSH_USER}@${EDGE_IP}
SSH_FORWARD_PORT=${SSH_FORWARD_PORT:-10023}
```

`192.168.7.0/24` 是仓库 QEMU/K3s 示例网段，不是必须写死的环境。若当前机器
使用其他网段，保持脚本变量一致即可，提交记录中只保留变量名或占位值。

### 1. 先确认启动路径

`docs/quick-start.md` 的标准 QEMU 示例仍然是 Xen + tap 路径，适合仓库原有
测试网络、固定 guest 地址和 K3s 云边联调：

```bash
ROOTFS="$(ls -t openeuler-image-qemu-aarch64-*.rootfs.cpio.gz | head -n1)"
sudo qemu-system-aarch64 \
  -device virtio-net-pci,netdev=net0 \
  -netdev tap,id=net0,ifname=tap0,script=/etc/qemu-ifup \
  -initrd "$ROOTFS" \
  -device loader,file=Image,addr=0x45000000 \
  -machine virt,gic-version=3 \
  -machine virtualization=true \
  -cpu cortex-a53 -smp 4 -m 4096 \
  -serial mon:stdio -nographic \
  -kernel xen-qemu-aarch64 \
  -append 'root=/dev/ram0 rw debugshell mem=1536M console=ttyAMA0,115200' \
  -dtb openeuler-image-qemu-aarch64.qemuboot.dtb
```

需要宿主端 SSH 端口转发时，可以额外加 usernet 网卡。仓库自动化脚本
`tests/common/qemu.sh` 默认 `QEMU_NET_MODE=both`，即保留 tap 网卡并增加
usernet SSH 转发：

```bash
sudo qemu-system-aarch64 \
  -device virtio-net-pci,netdev=net0 \
  -netdev tap,id=net0,ifname=tap0,script=/etc/qemu-ifup \
  -device virtio-net-pci,netdev=net1 \
  -netdev user,id=net1,hostfwd=tcp::${SSH_FORWARD_PORT:-10023}-:22 \
  ...
```

不要把 usernet 和 tap 的网络现象混在一起判断。usernet 不会创建 `tap0`；
tap 路径通常需要 sudo 和宿主机网络脚本。也不要为了测试解包或改写
`openeuler-image-qemu-aarch64-*.rootfs.cpio.gz`，测试基线必须来自构建产物本身。

### 2. 在怀疑 MicRun 之前先查前置条件

至少确认下面这些条件：

- 选用的构建输出目录里确实有 QEMU 相关产物。
- 没有旧的 QEMU 进程或残留的 `tap0` 干扰判断。
- usernet 路径：宿主端 SSH 转发端口未被占用。
- tap 路径：宿主机上存在 `/etc/qemu-ifup`，并且有可用的 sudo 凭据。

重要：在 usernet 路径下没有 `tap0` 是正常现象，不是 MicRun 信号。只有 tap
路径才应该用 `tap0` 判断宿主侧网络。

### 3. 明确把宿主机网络带起来

usernet 路径优先通过串口登录或宿主端转发端口访问 guest，例如：

```bash
ssh -p "${SSH_FORWARD_PORT:-10023}" root@localhost
```

tap 路径下，仓库默认示例网络是：

- host `${HOST_TAP_IFACE}`: `${HOST_TAP_IP}`
- guest `${EDGE_IFACE}`: `${EDGE_IP}`

QEMU 启动后，用 `ip addr show "$HOST_TAP_IFACE"` 检查宿主侧 tap 网卡。

不要假设 `/etc/qemu-ifup` 一定和仓库示例网段一致。有些机器会配成别的网段。
若宿主侧地址不对，先把宿主网络修正，再排查 guest 可达性。

### 4. 按仓库预期配置 guest

进入 guest 后，如果使用 tap 路径，优先用仓库自带脚本：

```bash
IFACE="$EDGE_IFACE" \
IP_ADDR="$EDGE_IP/24" \
GATEWAY="$HOST_TAP_IP" \
DNS_SERVER="$HOST_TAP_IP" \
"$MCS_REPO"/micrun/tests/k3s/prepare_edge_node.sh
```

如果使用仓库默认 QEMU/K3s 网段，也可以不传这些变量，脚本默认会使用
`192.168.7.2/24` 和 `192.168.7.1`。

如果一开始只能从串口进入 guest，就先在串口里把网络配好，再依赖 SSH 跑
测试流程。

### 5. 让 SSH 自动化可预测

为了反复跑验证，尽早建立下面其中一种方式：

- `root` 的 `authorized_keys` 可用
- 或者一个不会在首次登录时强制改密的已知密码流程

第一次 `sshpass` 失败，不等于 guest 网络有问题。也可能只是系统要求首次
登录改密码。

### 6. 先验证平台，再做 MicRun IO 测试

至少确认：

- `ping "$EDGE_IP"` from the host works if ICMP is enabled
- `ssh "$EDGE_SSH"` works
- `systemctl is-active containerd` in the guest
- `xl list` works in the guest
- `systemctl is-active xenconsoled` and `systemctl is-active xen-init-dom0`
  report ready in the guest
- `xl cpupool-list` works in the guest

这些都成立后，再做 MicRun 相关复现，例如：

```bash
ctr container create --runtime io.containerd.mica.v2 -t <image> <name>
ctr task start <name>
```

如果 `xen-init-dom0` 或 `xenconsoled` 失败，不要直接判定 MicRun IO 回归。先看
`systemctl --no-pager --full status xen-init-dom0 xenconsoled`。有些 ramfs rootfs
会在早期启动时还没有准备好 Xen 运行目录或 `/var/log -> volatile/log` 目标目录，
导致后续 `micad` 调 `xl create` 全部返回 `-22`。临时排障只能在运行中的 guest
里补齐目录并重启相关服务，不要解包、修改或重新打包构建好的 QEMU rootfs
产物：

```bash
mkdir -p /var/lib/xen /var/volatile/log /var/log/xen /run/xen
systemctl reset-failed xen-init-dom0.service xenconsoled.service
systemctl restart xenconsoled.service
systemctl restart xen-init-dom0.service
xl list
xl cpupool-list
```

这个问题的典型信号是：`journalctl -u micad` 反复出现
`Failed to run 'xl create ...'`、`Create new domU failed, err -22`，profile
探测可能退化成 `shell-regressed`。目录和服务恢复后，再用 MicRun 的 OCI 镜像
路径重新跑 IO 回归，避免把 Dom0 Xen 初始化问题误判成 shim 问题。

### 7. 把 MicRun 跟原生 mica 路径对照起来

在改 MicRun IO 代码前，先用 guest 里的原生 mica 路径跑同一份 firmware：

```bash
mica status
mica start qemu-uniproton-xen
ls -l /dev/ttyRPMSG*
```

注意原生 `qemu-uniproton-xen` target 使用 guest 里预置的
`/lib/firmware/qemu-uniproton-xen.*`，而 MicRun OCI 镜像通常从镜像层解出
`/run/micrun/containers/<id>/firmware.elf` 和 Xen image。二者可能不是同一条
配置路径。若原生 target 失败，但 MicRun profile 探测和 `ctr`/`nerdctl` 的 OCI
镜像路径能正常启动并交互，应把原生 target 失败记录为 native baseline 或
`mica` 配置问题，不要阻塞 MicRun 用户路径结论。必要时比较 hash：

```bash
sha256sum /lib/firmware/qemu-uniproton-xen.elf /lib/firmware/qemu-uniproton-xen.bin
sha256sum <stamped-output>/micrun-files/uniproton.elf <stamped-output>/micrun-files/uniproton.bin
```

On current client-name builds the native device is typically:

```bash
/dev/ttyRPMSG_qemu-uniproton-xen_0
```

不要假设 `mica start` 一返回，`rpmsg-tty` 就已经 ready。在 qemu guest 上，
它可能比 `rpmsg-rpc` 和 `rpmsg-umt` 晚几秒。尝试读写 TTY 前，轮询到下面两件
事同时成立：

- `mica status` shows `rpmsg-tty(...)` for the target
- `/dev/ttyRPMSG_<name>_0` has become the expected `/dev/pts/*` symlink

如果下面两条路径复现出相同行为：

- MicRun: `ctr task start <name>`
- native mica: `mica start qemu-uniproton-xen`

并且都只是重复返回 `Hello, UniProton!`，没有 prompt，那么先不要继续沿着
MicRun FIFO 或 attach 逻辑查。这更像是 RTOS firmware 或 mica 侧 shell 初始化
路径的问题，不是 MicRun 独有回归。

### 8. 区分 shell-ready 镜像和 shell 未初始化状态

分析 RTOS 输出时，先给运行时行为分类：

- shell 镜像：会出现 `openEuler UniProton #` 这类标记
- hello 镜像：只会输出固定 banner，例如 `Hello, UniProton!`
- 带 shell 代码但 shell 未初始化：运行时只重复输出 `Hello, UniProton!`，
  但对 firmware 做 `strings` 还能看到 `openEuler UniProton #`、
  `support shell commond`、`shell init fail`、
  `shell is not yet initialized!` 这类字符串

用 `tests/io/test_helpers.sh` 和现有 expect 脚本里的规则，保持这个分类清晰。

常用确认命令：

```bash
strings output/test/micrun-files/uniproton.elf | \
  grep -E 'openEuler UniProton #|support shell commond|shell init fail|shell is not yet initialized'
sha256sum output/test/micrun-files/uniproton.elf rtos/arm64/qemu-uniproton-xen.elf
```

如果要确认 guest 实际回了什么，优先在 RPMSG TTY symlink ready 之后，在
guest 里抓原始字节。qemu 镜像里可能没有 `xxd`，可以用 `od`：

```bash
tty=/dev/ttyRPMSG_qemu-uniproton-xen_0
stty -F "$tty" raw -echo -onlcr -ocrnl -icrnl -inlcr
(timeout 2 dd if="$tty" bs=1 count=128 status=none | od -An -tx1 -v) & pid=$!
sleep 0.2
printf '\r' > "$tty"
wait $pid || true
```

如果 `Enter` 和 `help` 回来的原始字节完全一样，都是 `Hello, UniProton!`，
就按 RTOS 侧行为处理，不要当成 MicRun 把 prompt 过滤掉了。

如果 hash 一致，且原生 mica 路径仍然只打印 `Hello`，说明当前构建使用的是
同一份 RTOS 二进制，问题不在 MicRun shim 自身。

### 9. 先读日志，再决定往哪一层钻

如果原生 `mica` 路径已经异常，优先看：

```bash
journalctl -u micad --no-pager -n 200
tail -n 200 /var/log/mica/mica-runtime.log
mica status
```

经验上可以这样分层：

- `mica-runtime.log` 有 `Opened RPMSG TTY`、`TTY ready`、`Observed output after stdin`
  一类日志，说明 MicRun attach 和 FIFO 基本在工作。
- `journalctl -u micad` 能看到 `Starting qemu-uniproton-xen`、`start done`、
  `Create rpmsg_umt_service failed` 之类日志，更适合判断 `micad`、`rpmsg`
  服务和底座路径。
- 如果 `mica-runtime.log` 看起来正常，但原生 `mica start` 仍然只有 `Hello`，
  先查 `micad` 和 RTOS，不要先改 `micrun/internal/adapters/io`。

### 10. 必要时在现成构建容器里重编 `micad`

如果宿主机没有完整的 Yocto 交叉环境，或用户已经给出可用的构建镜像/容器，
优先复用它，而不是在宿主机临时拼工具链。

常用流程：

```bash
docker cp <repo>/. <container>:<container-workdir>/mcs-current-test

docker exec <container> bash -lc '
set -e
buildroot=<yocto-workdir>
sysroot=$buildroot/recipe-sysroot
sysroot_native=$buildroot/recipe-sysroot-native
toolchain=$buildroot/toolchain.cmake
src=<container-workdir>/mcs-current-test
bld=<container-build-root>/<tmp-build-dir>

export PATH="...$sysroot_native/usr/bin/...:$PATH"
export PKG_CONFIG_DIR="$sysroot/usr/lib64/pkgconfig"
export PKG_CONFIG_LIBDIR="$sysroot/usr/lib64/pkgconfig"
export PKG_CONFIG_PATH="$sysroot/usr/lib64/pkgconfig:$sysroot/usr/share/pkgconfig"
export PKG_CONFIG_SYSROOT_DIR="$sysroot"

cmake -S "$src" -B "$bld" -G Ninja -DCMAKE_TOOLCHAIN_FILE="$toolchain" ...
cmake --build "$bld" --target micad -j 32
file "$bld/mica/micad/micad"
readelf -d "$bld/mica/micad/micad" | grep NEEDED || true
sha256sum "$bld/mica/micad/micad"
'

docker cp <container>:<container-build-root>/<tmp-build-dir>/mica/micad/micad /tmp/micad-test
```

实践要点：

- 优先只编 `micad`，不要为了排一个 shell 问题把整套镜像全重打。
- 编完立刻记录 `sha256sum`，后面 guest 里的替换验证靠 hash 对齐。
- 如果需要对比旧提交，把旧提交放到独立 `worktree` 后，用同一个容器和
  同一套 sysroot 编，减少环境差异。

### 11. 用旧提交或补丁版 `micad` 做 A/B 对比

当你怀疑“固件没变，但最近 Linux 侧重构把交互搞坏了”时，最有效的切法通常是：

1. 对当前分支和历史提交分别编出 `micad`
2. 在 guest 里临时替换 `/usr/bin/micad`
3. 重启 `micad`
4. 用同一份 firmware、同一条 `mica start` 或 `ctr task attach` 路径复测
5. 结束后恢复原始二进制

推荐用带恢复钩子的方式，避免把 guest 留在测试态：

```bash
backup=/root/micad.backup.$(date +%s)
restore() {
  set +e
  mica stop qemu-uniproton-xen >/dev/null 2>&1 || true
  [ -f "$backup" ] && cp -af "$backup" /usr/bin/micad
  systemctl restart micad >/dev/null 2>&1 || true
}
trap restore EXIT

cp -af /usr/bin/micad "$backup"
install -m 755 /tmp/micad-test /usr/bin/micad
systemctl restart micad
```

如果出现下面这种对比结果：

- firmware hash 一样
- 旧 `micad` 能看到 `openEuler UniProton #`
- 新 `micad` 只有 `Hello, UniProton!`

那么问题就在 `micad` 或它静态链接进去的 `library`，不在 RTOS 固件，也不在
MicRun attach 层。

### 12. 检查 API 返回值语义，不要把“成功”当“失败”

在 `micad` 这种同时混用 `pthread`、`rpmsg`、`libmetal`、`open_amp` 的代码里，
不要假设所有接口都按“成功返回 0”工作。

排查建议：

- 对照旧版本行为和当前日志，确认某个 `goto free_*` 或错误分支是不是被误触发
- 特别检查 `rpmsg_send`、`send_*` 这类接口的成功返回语义
- 如果日志显示“初始化消息发出后立刻清理资源”，优先怀疑返回值判断写错了

这类问题在重构后很常见，尤其是把旧代码从“只判负值失败”改成了“非零即失败”时。

## 仓库内提醒

- `tests/io/README.md` 和 `tests/README.md` 默认把远端测试目标视为
  仓库示例边侧地址；真实环境应通过 `TEST_REMOTE_HOST` 覆盖。
- `tests/common/qemu.sh` 可以参考，但要确认它的本地假设和当前使用的
  quick-start 路径一致。
- 如果问题是“回车后没有 RTOS prompt”，先在文档要求的 QEMU 环境里复现，
  再去改 MicRun IO 代码。
- 如果原生 `mica start qemu-uniproton-xen` 和 MicRun 表现一致，也先按
  firmware 或 mica 侧问题处理，除非后续证据推翻这个判断。
- 如果需要快速证明问题是不是 MicRun 独有，优先顺序通常是：
  `mica start` -> 原始 `ttyRPMSG` 抓字节 -> `journalctl -u micad` ->
  再看 `ctr task attach` 和 `mica-runtime.log`。

## 完成标准

只有当下面这些条件都满足时，才算 QEMU 环境已经准备好，可以进入 MicRun
排障：

- QEMU 按仓库文档里的 tap 路径启动
- `tap0` 存在且宿主侧地址正确
- guest 网络已经配置到预期边侧地址
- SSH 或其他稳定控制路径可用
- guest 里的 containerd 和 Xen 都是活的
- 已确认原生 `mica` 路径和 MicRun 路径各自的表现，知道问题是共享底层问题，
  还是 MicRun 独有问题
