# mcs

#### 介绍

目前工控设备、航天设备、机器人系统、智能车系统对功能和生态的需求日益丰富，对实时性、可靠性、安全性提出了更高的要求，由单一OS承载所有功能面临的挑战越来越大。针对这些场景，我们提出了**混合关键性系统(MCS, Mixed Criticality System)**，实现在一颗片上系统中部署多个OS，同时提供Linux的服务管理能力以及实时OS带来的高实时、高可靠的关键能力。

#### 软件架构

mcs_km:  提供OpenAMP所需内核模块，支持Client OS启动、专用中断收发、管理保留内存等功能。

rpmsg_pty_demo: 提供OpenAMP用户态程序Linux端样例，支持在Linux上通过pty设备访问Client OS。

library: 提供OpenAMP样例必需的模块remoteproc、virtio、rpmsg、openamp。

#### 原理简介

OpenAMP旨在通过非对称多处理器的开源解决方案来标准化异构嵌入式系统中操作环境之间的交互。

OpenAMP是一个软件框架，提供了为AMP系统开发软件应用程序所需的软件组件，允许操作系统在复杂的同构和异构体系结构中交互。

OpenAMP包括如下三大重要组件：

- virtio：该组件是rpmsg组件的实现基础。
- rpmsg：实现多核处理器通信的通道，基于virtio组件实现。
- remoteproc：该组件用于主机上实现对远程处理器及相关软件环境的生命周期管理、及virtio和rpmsg设备的注册等。

样例Demo通过提供mcs_km.ko 内核模块实现Linux内核启动从核的功能，并预留了OpenAMP通信所需的专用中断及其收发机制。用户可在用户态通过dev设备实现Client OS的启动，并通过rpmsg组件实现与Client OS的简单通信。

#### 构建安装指导

mcs支持两种构建安装方式：

1. 通过yocto直接构建出包含mcs模块的混合关键性系统镜像；
2. 单独构建出mcs包含的内核模块，用户态样例，并将它们与相关的依赖库（libmetal.so\*，libopen_amp.so\*，libsysfs.so\*）安装到运行环境中。

- **集成构建**

  目前在 openEuler Embedded 版本中已经实现了mcs的**集成构建**，支持一键式构建出包含mcs的**x86镜像**。集成构建方法请参考 openEuler Embedded 在线文档章节：[openEuler Embedded x86-64镜像构建](https://openeuler.gitee.io/yocto-meta-openeuler/master/bsp/x86/appendix/build.html)。注意，在创建x86-64的编译配置文件时，需要加上 `-f openeuler-mcs` ，构建步骤如下：
  ```shell
  # 初始化oebuild工作目录，以及下载各软件包代码
  $ oebuild init oebuild_workdir
  $ cd oebuild_workdir
  $ oebuild update

  # 创建 x86 镜像的构建目录
  #  -p 指定 x86-64-std
  #  -f 指定镜像所带特性
  #  -d 指定工作目录
  # 如： -f openeuler-mcs 会为镜像打包 mcs 的相关软件包
  # 如： -f systemd 使能systemd作为init，默认是busybox init
  $ oebuild generate -p x86-64-std -f openeuler-mcs -f systemd -d build_x86_systemd_mcs
  $ cd build_x86_systemd_mcs

  $ oebuild bitbake
  # 敲以上命令后，进入构建容器
  # 在构建容器中构建镜像和sdk
  $ bitbake openeuler-image    # 构建镜像
  $ bitbake openeuler-image -c populate_sdk   # 构建SDK
  ```

- **单独构建**

  按照集成构建方法构建出带mcs功能的SDK后，可以使用SDK快速开发mcs，步骤如下：

  1. 根据[openEuler Embedded使用手册](https://openeuler.gitee.io/yocto-meta-openeuler/master/getting_started/index.html#sdk)安装SDK并设置SDK环境变量。

  2. 交叉编译内核模块 mcs_km.ko，编译方式如下:
     ```shell
     cd mcs_km
     make
     ```

  3. 交叉编译用户态样例 rpmsg_main，编译方式如下:
     ```shell
     cmake -S . -B build -DDEMO_TARGET=rpmsg_pty_demo
     cd build
     make
     ```

  4. 在SDK的 sysroots 中获取依赖库，包括 libmetal, libopen_amp, libsysfs，获取方式如下：
     ```shell
     # 若sdk的安装路径为/opt/openeuler/sdk
     cd /opt/openeuler/sdk/sysroot
     find . -name libmetal.so*
     find . -name libopen_amp.so*
     find . -name libsysfs.so*

     # 将以上so安装到运行环境中的 /usr/lib64 目录中
     ```

#### 使用说明

目前，对于x86机器，仅支持UniProton的混合部署。请按照[UniProton构建安装指导](https://gitee.com/openeuler/UniProton/blob/dev/doc/demoUsageGuide/uvpck_demo_usage_guide.md)编译、构建、部署UniProton。
