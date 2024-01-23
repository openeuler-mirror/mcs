# mcs

## 介绍

目前工控设备、航天设备、机器人系统、智能车系统对功能和生态的需求日益丰富，对实时性、可靠性、安全性提出了更高的要求，由单一OS承载所有功能面临的挑战越来越大。针对这些场景，我们提出了**混合关键性系统(MCS, Mixed Criticality System)**，实现在一颗片上系统中部署多个OS，同时提供Linux的服务管理能力以及实时OS带来的高实时、高可靠的关键能力。

## 软件架构

mcs_km:  提供OpenAMP所需内核模块，支持Client OS启动、专用中断收发、管理保留内存等功能。

mica_demo: 提供OpenAMP用户态程序Linux端样例，支持在Linux上通过pty设备访问Client OS，以及通过ring buffer调试支持GDB stub的Client OS。

library: 提供OpenAMP样例必需的模块remoteproc、virtio、rpmsg、openamp。

rtos: 提供样例镜像文件，在每个demo中，qemu_zephyr_\*.bin运行在qemu上，rasp_zephyr_\*.bin运行在树莓派上，该文件需要被加载至设定的0x7a000000起始地址。启动后会运行OpenAMP Client端的样例程序，并与Linux端进行交互。

## 原理简介

OpenAMP旨在通过非对称多处理器的开源解决方案来标准化异构嵌入式系统中操作环境之间的交互。

OpenAMP是一个软件框架，提供了为AMP系统开发软件应用程序所需的软件组件，允许操作系统在复杂的同构和异构体系结构中交互。

OpenAMP包括如下三大重要组件：

- virtio：该组件是rpmsg组件的实现基础。
- rpmsg：实现多核处理器通信的通道，基于virtio组件实现。
- remoteproc：该组件用于主机上实现对远程处理器及相关软件环境的生命周期管理、及virtio和rpmsg设备的注册等。

样例Demo通过提供mcs_km.ko 内核模块实现Linux内核启动从核的功能，并预留了OpenAMP通信所需的专用中断及其收发机制。用户可在用户态通过dev设备实现Client OS的启动，并通过rpmsg组件实现与Client OS的简单通信。

## 构建安装指导

mcs支持两种构建安装方式：

1. 通过yocto直接构建出包含mcs模块的混合关键性系统镜像；
2. 单独构建出mcs包含的内核模块，用户态样例，并将它们与相关的依赖库（libmetal.so\*，libopen_amp.so\*，libsysfs.so\*）安装到运行环境中。

- **集成构建**

  目前在 openEuler Embedded 版本中已经实现了mcs的**集成构建**，支持一键式构建出包含mcs的**qemu、树莓派镜像**。集成构建依赖 oebuild 工具，具体请参考 openEuler Embedded 在线文档章节：[混合关键性系统（MCS）镜像构建指导](https://openeuler.gitee.io/yocto-meta-openeuler/master/features/mica/mica_build.html)。

- **单独构建**

  按照集成构建方法构建出**带mcs特性的SDK**后，可以使用SDK快速开发mcs，步骤如下：

  1. 根据[openEuler Embedded使用手册](https://openeuler.gitee.io/yocto-meta-openeuler/master/getting_started/index.html#sdk)安装SDK并设置SDK环境变量。

  2. 由于 mcs_remoteproc.ko 依赖内核头文件[remoteproc_internal.h](https://gitee.com/openeuler/kernel/blob/5.10.0-153.12.0/drivers/remoteproc/remoteproc_internal.h)，
     需要先将该头文件拷贝到 mcs_km 目录中，如下：
     ```shell
     $ tree mcs_km
     mcs_km
     ├── Makefile
     ├── mcs_km.c
     ├── mcs_remoteproc.c
     └── remoteproc_internal.h
     ```

  3. 交叉编译内核模块 mcs_km.ko、mcs_remoteproc.ko，编译方式如下:
     ```shell
     cd mcs_km
     make
     ```

     如果需要编译支持 Jailhouse 的 mcs_ivshmem.ko, 请按照 [Jailhouse 构建指导](https://openeuler.gitee.io/yocto-meta-openeuler/master/features/jailhouse.html#mcs-sdk) 构建 Jailhouse，并将 `JAILHOUSE_SRC` 指定为 Jailhouse 的构建目录：
     ```shell
     export JAILHOUSE_SRC=/Jailhouse的构建目录
     make
     ```

     将编译出来的 ko 放到运行环境的 `/lib/modules/5.10.0/extra` 目录中，并执行 depmod 生成模块映射文件。

  4. 交叉编译用户态样例 mica_main，编译方式如下:
     ```shell
     cmake -S . -B build
     ## 注：若希望构建带调试信息的二进制，请配置 CMAKE_BUILD_TYPE，例如：
     ## cmake -S . -B build -DCMAKE_BUILD_TYPE=Debug

     cd build
     make
     ```

  5. 在SDK的 sysroots 中获取依赖库，包括 libmetal, libopen_amp, libsysfs，获取方式如下：
     ```shell
     # 若sdk的安装路径为/opt/openeuler/sdk
     cd /opt/openeuler/sdk/sysroot
     find . -name libmetal.so*
     find . -name libopen_amp.so*
     find . -name libsysfs.so*
     ```

     将以上so安装到运行环境中的 `/usr/lib64` 目录中。

## 使用说明

目前mcs支持在**qemu-arm64，树莓派4B，Hi3093，ok3568，x86工控机** 等多个平台上运行，具体的使用方法，请参考[混合关键性系统框架](https://openeuler.gitee.io/yocto-meta-openeuler/master/features/mica/index.html)。

