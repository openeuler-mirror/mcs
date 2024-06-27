# mcs

## 介绍

目前工控设备、航天设备、机器人系统、智能车系统对功能和生态的需求日益丰富，对实时性、可靠性、安全性提出了更高的要求，由单一OS承载所有功能面临的挑战越来越大。针对这些场景，我们提出了**混合关键性系统(MCS, Mixed Criticality System)**，实现在一颗片上系统中部署多个OS，同时提供Linux的服务管理能力以及实时OS带来的高实时、高可靠的关键能力。

## 软件架构

mcs_km: 提供OpenAMP所需内核模块，支持Client OS启动、专用中断收发、管理保留内存等功能。

mica: 包含命令行工具 mica 和守护进程 micad。用户通过 mica 命令进行 RTOS 的生命周期管理，包括：启动、停止以及查看状态等。micad 监听来自于 mica 命令行工具的调用，并根据这些调用执行相应的操作。此外，micad 还负责不同实例上的服务注册等功能。

library: 包含生命周期管理框架、服务化框架的具体实现。

rtos: 提供样例镜像文件，每一个 ELF 镜像都关联了对应的配置文件。具体的配置文件介绍请参考：[mica命令与配置文件介绍](https://pages.openeuler.openatom.cn/embedded/docs/build/html/master/features/mica/mica_ctl.html)。

## 原理简介

参阅[多OS混合关键性部署框架介绍](https://pages.openeuler.openatom.cn/embedded/docs/build/html/master/features/mica/intro.html#os)。

## 构建安装指导

mcs支持两种构建安装方式：

1. 集成构建：通过yocto直接构建出包含mcs特性的镜像；

   目前在 openEuler Embedded 版本中已经实现了mcs的**集成构建**，支持一键式构建出包含mcs的**qemu、树莓派等镜像**。集成构建依赖 oebuild 工具，具体请参考 openEuler Embedded 在线文档章节：[混合关键性系统（MCS）镜像构建指导](https://pages.openeuler.openatom.cn/embedded/docs/build/html/master/features/mica/build.html)。

2. 单独构建：单独编译mcs的各个组件，并将它们与相关的依赖库（libmetal.so\*，libopen_amp.so\*，libsysfs.so\*）安装到运行环境中。

   按照集成构建方法构建出**带mcs特性的SDK**后，可以使用SDK快速开发mcs，步骤如下：

   1. 根据[openEuler Embedded使用手册](https://pages.openeuler.openatom.cn/embedded/docs/build/html/master/getting_started/index.html#sdk)安装SDK并设置SDK环境变量。

   2. 交叉编译内核模块 mcs_km.ko、mcs_remoteproc.ko，编译方式如下:
      ```shell
      cd mcs_km
      make
      ```

      将编译出来的 ko 放到运行环境的 `/lib/modules/$(uname -r)` 目录中，并执行以下命令：
      ```shell
      depmod
      echo "mcs_km" > /etc/modules-load.d/mcs_km.conf
      ```

   3. 交叉编译 micad，编译方式如下:
      ```shell
      cmake -S . -B build
      ## 注：若希望构建带调试信息的二进制，请配置 CMAKE_BUILD_TYPE，例如：
      ## cmake -S . -B build -DCMAKE_BUILD_TYPE=Debug

      cd build
      make
      ```

      将编译出来的 micad 放到运行环境的 `/usr/bin` 目录中。为了支持 micad 的自启动，可以手动安装本仓库 `mica/micad/init` 提供的启动脚本或systemd服务。

   4. 安装 mica 命令行工具和配置文件：
      ```shell
      scp mica/micactl/mica.py target@xxx:/usr/bin/mica

      scp rtos/arm64/*.conf target@xxx:/etc/mica/
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

