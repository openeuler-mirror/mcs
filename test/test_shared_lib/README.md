# test shared lib 示例

此为linux侧mica通信应用示例，与 `mcs/test/test_umt` 类似的 UMT 示例应用，**完全独立于 mcs 的构建系统**，仅依赖 mica **安装目录**（如 `/path/to/mica-install/` 下的动态库和头文件）。

## 前置条件

1. 已从 oEE SDK 获取到 libmica、mcs头文件，例如：

```
├── include
│   ├── memory
│   │   └── shm_pool.h
│   ├── mica
│   │   ├── mica_client.h
│   │   └── mica.h
│   ├── rbuf_device
│   │   ├── rbuf_dev.h
│   │   └── ring_buffer.h
│   ├── remoteproc
│   │   ├── mica_rsc.h
│   │   └── remoteproc_module.h
│   ├── rpmsg
│   │   ├── rpmsg_service.h
│   │   └── rpmsg_vdev.h
│   └── user_msg
│       └── user_msg.h
└── lib64
    ├── libmica.a
    └── libmica.so
```

2. 可自行安装至主机固定目录，安装后 `/path/to/mica-install/` 下应包含：
   - `lib/libmica.so`（或 `lib64/libmica.so`）
   - `include/` 下的 `user_msg` `mica` 头文件

## 构建

```bash
# 配置 openEuler 交叉编译工具链
sudo -s . /opt/openeuler/oecore-x86_64/environment-setup-aarch64-openeuler-linux

# 构建
cd mcs/test/test_shared_lib
cmake -S . -B build -DMICA_INSTALL_DIR=/path/to/mica-install
cd build && make
```

注意： 如果自行使用交叉编译工具链，需参照CMAKE自行设置 `CC` 等环境变量。

## 产物

- `build/send-data/send-data`
- `build/rcv-data/rcv-data`
- `build/send-data-2-way/send-data-2-way`

例如：

```
/path/to/sample_app/build$ readelf -d rcv-data/rcv-data 

Dynamic section at offset 0xfd28 contains 30 entries:
  Tag        Type                         Name/Value
 0x0000000000000001 (NEEDED)             Shared library: [libmica.so]
 0x0000000000000001 (NEEDED)             Shared library: [libc.so.6]
 0x0000000000000001 (NEEDED)             Shared library: [ld-linux-aarch64.so.1]
 0x000000000000000f (RPATH)              Library rpath: [/path/to/mica-install/lib64]
......
```

## 运行

需在已部署 **micad**、**libmica.so** 及其依赖（libmetal、libopen_amp）等运行环境执行。

注意，需要将 libmica.so 传至开发板，可使用 `scp` 等工具，放至 `/usr/lib64` 目录 。

