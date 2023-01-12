# mcs

#### 介绍

目前工控设备、航天设备、机器人系统、智能车系统对功能和生态的需求日益丰富，对实时性、可靠性、安全性提出了更高的要求，由单一OS承载所有功能面临的挑战越来越大。针对这些场景，我们提出了**混合关键性系统(MCS, Mixed Criticality System)**，实现在一颗片上系统中部署多个OS，同时提供Linux的服务管理能力以及实时OS带来的高实时、高可靠的关键能力。

#### 软件架构

mcs_km:  提供OpenAMP所需内核模块，支持Client OS启动、专用中断收发、管理保留内存等功能。

rpmsg_pty_demo: 提供OpenAMP用户态程序Linux端样例，支持在Linux上通过pty设备访问Client OS。

library: 提供OpenAMP样例必需的模块remoteproc、virtio、rpmsg、openamp。

zephyr: 提供样例镜像文件，在每个demo中，zephyr_qemu.bin运行在qemu上，zephyr_rpi.bin运行在树莓派上，该文件需要被加载至设定的0x7a000000起始地址。启动后会运行OpenAMP Client端的样例程序，并与Linux端进行交互。

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

  目前在 openEuler Embedded 版本中已经实现了mcs的**集成构建**，支持一键式构建出包含mcs的**qemu、树莓派镜像**。集成构建方法请参考 openEuler Embedded 在线文档章节：**混合关键性系统镜像构建指导**。

- **单独构建**

  1. 根据openEuler Embedded使用手册安装SDK并设置SDK环境变量。

  2. 交叉编译内核模块 mcs_km.ko，编译方式如下:

     ```shell
     cd mcs_km
     make
     ```

  3.  交叉编译用户态样例 rpmsg_main，编译方式如下:

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

目前mcs支持在**qemu-aarch64**和**树莓派**上部署运行，部署mcs需要预留出必要的内存、CPU资源，并且还需要bios提供psci支持。

**若使用openEuler Embedded提供的混合关键性系统镜像，无需进行单独配置；若使用qemu/树莓派标准镜像，则需要进行下述配置操作：**

1. **通过配置dts预留出mcs_mem**

   - **QEMU**

     QEMU需要制作一份dtb，通过 `-dtb file` 使用，制作步骤如下：

     ```shell
     # 安装 qemu-system-aarch64、dtc
     $ apt install qemu-system-arm device-tree-compiler  # ubuntu
     
     # 获取 QEMU devicetree
     $ qemu-system-aarch64 -M virt,gic-version=3 -m 1G -cpu cortex-a57 -smp 4 -M dumpdtb=qemu.dtb
     $ dtc -I dtb -O dts -o qemu.dts qemu.dtb
     
     # 修改qemu.dts，添加 reserved-memory 节点，预留出 0x70000000 - 0x80000000 的内存
     	reserved-memory {
     		#address-cells = <0x02>;
     		#size-cells = <0x02>;
     		ranges;
     
     		mcs@70000000 {
     			reg = <0x00 0x70000000 0x00 0x10000000>;
     			compatible = "mcs_mem";
     			no-map;
     		};
     	};
     
     # 制作最终使用的dtb文件
     $ dtc -I dts -O dtb -o qemu_mcs.dtb qemu.dts
     ```

   - **Raspberry Pi**

     树莓派支持使用 dt-overlay 的方式，制作步骤如下：

     ```shell
     # 新增 mcs-memreserve-overlay.dts
         /dts-v1/;
         /plugin/;
         / {
             fragment@0 {
                 target-path = "/";
                 __overlay__ {
                     reserved-memory {
                         #address-cells = <2>;
                         #size-cells = <1>;
                         ranges;
     
                    mcs@70000000 {
                             reg = <0x00 0x70000000 0x10000000>;
                             compatible = "mcs_mem";
                             no-map;
                         };
                     };
                 };
             };
         };
     
     # 制作使用的dtbo
     $ dtc -I dts -O dtb -o mcs-memreserve.dtbo mcs-memreserve-overlay.dts
     
     # 挂载树莓派boot分区，将 mcs-memreserve.dtbo 安装到树莓派boot分区的overlays中：
     $ cp mcs-memreserve.dtbo ${rpi_boot_path}/overlays/
     
     # 修改树莓派的config.txt，新增 dtoverlay 使能 mcs-memreserve.dtbo
     $ echo "dtoverlay=mcs-memreserve" >> ${rpi_boot_path}/config.txt
     ```

   

2. **隔离cpu用于启动实时OS**

   通过修改内核cmdline，增加`maxcpus=3 `隔离3核。

   - **QEMU**

     在启动qemu时，增加 `-append 'maxcpus=3'`即可。

   - **Raspberry Pi**

     树莓派使用支持 psci 的 uefi 引导固件，因此通过 grub.cfg 配置cmdline，修改 `${rpi_boot_path}/EFI/BOOT/grub.cfg`，添加 `maxcpus=3`即可。

     

3. **使用支持psci的bios启动镜像**

   - **QEMU**

     qemu无需单独配置bios，启动命令如下：

     ```shell
     $ qemu-system-aarch64 -M virt,gic-version=3 -m 1G -cpu cortex-a57 -nographic -append 'maxcpus=3 console=ttyAMA1' -smp 4 -kernel zImage -initrd *.rootfs.cpio.gz -dtb qemu_mcs.dtb
     ```

   - **Raspberry Pi**

     树莓派需要使用支持 psci 的 uefi 引导固件，具体参考 openEuler Embedded 在线文档章节：**树莓派的UEFI支持和网络启动**



​	按照上述3个步骤，准备好运行环境后，接下来就可以进行 mcs 的安装和使用：

4. **根据前文的构建安装指导，安装**：

   - 构建出来的 **mcs_km.ko，rpmsg_main**
   - rpmsg_pty_demo中提供的实时os： **zephyr_qemu.bin / zephyr_rpi.bin**
   - 安装依赖库 **libmetal, libopen_amp, libsysfs** 到运行环境上的 /usr/lib64 中

   

5. **插入内核模块**

   ```shell
   $ insmod mcs_km.ko
   ```

   插入内核模块后，可以通过 `cat /proc/iomem`查看预留出来的 mcs_mem，如：

   ```shell
   qemu-aarch64 ~ # cat /proc/iomem
   ...
   70000000-7fffffff : reserved
     70000000-7fffffff : mcs_mem
   ...
   
   ```

   若mcs_km.ko插入失败，可以通过dmesg看到对应的失败日志，可能的原因有：

   - 使用的交叉工具链与内核版本不匹配

   - 未预留内存资源

   - 使用的bios不支持psci

     

6. **运行rpmsg_main程序，使用方式如下：**

   ```shell
   $ ./rpmsg_main -c [cpu_id] -t [target_binfile] -a [target_binaddress]
   eg:
   # qemu
   $ ./rpmsg_main -c 3 -t zephyr_qemu.bin -a 0x7a000000
   
   # Raspberry Pi
   $ ./rpmsg_main -c 3 -t zephyr_rpi.bin -a 0x7a000000
   ```

   若rpmsg_main成功运行，会有如下打印：

   ```shell
   qemu-aarch64 ~ # ./rpmsg_main -c 3 -t zephyr.bin -a 0x7a000000
   ...
   start client os
   ...
   pls open /dev/pts/1 to talk with client OS
   pty_thread for uart is runnning
   ...
   ```

   根据提示，可以通过`/dev/pts/1`与client os进行shell交互，例如：

   ```shell
   # 新建一个terminal，登录到运行环境
   $ ssh user@ip
   
   # 连接pts设备
   $ screen /dev/pts/1
   
   # 敲回车后，可以打开client os的shell，对client os下发命令，例如
     uart:~$ help
     uart:~$ kernel version
   ```
