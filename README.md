# mcs

#### 介绍

该模块用于提供OpenAMP样例的内核态与用户态支持。

#### 软件架构

mcs_km:  提供OpenAMP所需内核模块，支持Client OS启动、专用中断收发、管理保留内存等功能。

openamp_demo: 提供OpenAMP用户态程序Linux端样例，支持与指定Client OS进行通信。

rpmsg_pty_demo: 提供OpenAMP用户态程序Linux端样例，支持通过shell命令行访问Client OS。

modules: 提供OpenAMP样例必需的模块remoteproc、virtio、rpmsg、openamp，这些模块静态编译成libopenamp.a。

zephyr: 提供样例镜像文件，在每个demo中，zephyr_qemu.bin运行在qemu上，zephyr_rpi.bin运行在树莓派上，该文件需要被加载至设定的0x7a000000起始地址。启动后会运行OpenAMP Client端的样例程序，并与Linux端进行交互。

#### 原理简介

OpenAMP旨在通过非对称多处理器的开源解决方案来标准化异构嵌入式系统中操作环境之间的交互。

OpenAMP是一个软件框架，提供了为AMP系统开发软件应用程序所需的软件组件，允许操作系统在复杂的同构和异构体系结构中交互。

OpenAMP包括如下三大重要组件：

-virtio：该组件是rpmsg组件的实现基础。

-rpmsg：实现多核处理器通信的通道，基于virtio组件实现。

-remoteproc：该组件用于主机上实现对远程处理器及相关软件环境的生命周期管理、及virtio和rpmsg设备的注册等。

样例Demo通过提供mcs_km内核KO模块实现Linux内核启动从核的功能，并预留了OpenAMP通信所需的专用中断及其收发机制。用户可在用户态通过dev设备实现Client OS的启动，并通过rpmsg组件实现与Client OS的简单通信。

#### 安装教程

1.  qemu使用openeuler-image镜像运行混合部署。

树莓派使用openeuler-image-uefi镜像运行混合部署，该镜像对齐tiny镜像的软件包配置，并集成openssh支持网络登录、混合部署依赖库、混合部署保留内存mcsmem dtoverlay、以及第三方UEFI固件支持PSCI功能。openeuler-image-uefi镜像的构建、烧录和启动，请参考openEuler Embedded在线文档章节：树莓派的UEFI支持和网络启动。

2.  根据openEuler Embedded使用手册安装SDK并设置SDK环境变量。

3.  编译内核模块mcs_km.ko，编译方式如下:

````
    cd mcs_km
    make
````

4.  编译openamp_demo用户态程序rpmsg_main，编译方式如下:

````
    cmake -S . -B build -DDEMO_TARGET=openamp_demo
    cd build
    make
````

注意：此处定义OpenAMP通信设备保留内存起始地址为0x70000000，可根据实际内存分配进行修改。
如果需要查看调试日志，可以给cmake增加-DDEBUG参数。

5.  将编译好的KO模块、用户态程序，以及zephyr_qemu.bin镜像拷贝到openEuler Embedded系统的目录下。如何拷贝可以参考使用手册中共享文件系统场景。

6.  将OpenAMP的依赖库libmetal.so\*，libopen_amp.so\*拷贝至文件系统/usr/lib64目录，libsysfs.so\*拷贝至文件系统/lib64目录。对应so可在sdk目录中找到，如何拷贝可以参考使用手册中共享文件系统场景。

#### 使用说明

1.  通过QEMU启动openEuler Embedded镜像，如何启动可参考使用手册中QEMU使用与调试章节。树莓派跳过这一步。

-以上述demo为例，需要预留出地址0x70000000为起始的内存用于OpenAMP demo和Client OS启动。通过QEMU启动时，当指定-m 1G时默认使用0x40000000-0x80000000的系统内存。添加内核启动参数mem=768M，可预留地址为0x70000000-0x80000000的256M保留内存。
-在样例中在cpu 3启动Client OS，需要预留出3号cpu。
-样例中zephyr镜像默认gic版本为3，需要在QEMU中设置。

可参考如下命令进行启动：

````
    qemu-system-aarch64 -M virt,gic-version=3 -m 1G -cpu cortex-a57 -nographic -kernel zImage -initrd *.rootfs.cpio.gz -append 'mem=768M maxcpus=3' -smp 4 
````

当使用的QEMU版本过老时可能会由于内存分布不一致导致段错误，可升级QEMU版本或手动修改DTB空出对应内存。

2.  在openEuler Embedded系统上插入内核KO模块mcs_km.ko。

````
    insmod mcs_km.ko
````

3.  运行rpmsg_main程序，使用方式如下:

````
    ./rpmsg_main -c [cpu_id] -t [target_binfile] -a [target_binaddress]
    eg:
    ./rpmsg_main -c 3 -t zephyr_qemu.bin -a 0x7a000000
````

此处定义Client OS起始地址为0x7a000000，Client OS镜像名为zephyr_qemu.bin，Client OS从3号cpu启动。树莓派换成zephyr_rpi.bin。

#### 用户样例开发

mcs提供了4个API，供用户做样例开发，用户无需感知OpenAMP实现细节，接口定义在modules/openamp_module.h。

1.  openamp_init: 初始化保留内存，加载zephyr镜像文件，初始化remoteproc、virtio、rpmsg，建立Linux与Client OS两端配对的endpoint，供消息收发使用。
````
    int openamp_init(void);
````

2.  openamp_deinit: 释放openamp资源。
````
    void openamp_deinit(void);
````

#### 串口服务样例
rpmsg_pty_demo包含2个源文件，实现通过Linux shell命令行访问Client OS的功能，样例支持多用户多线程场景。

rpmsg_main.c: 初始化OpenAMP，用户创建线程，在线程中运行shell程序。

rpmsg_pty.c: 用户创建PTY虚拟串口设备，将PTY的slave端作为shell，PTY的master端与OpenAMP Endpint节点通信。

1.  安装教程，其他步骤与openamp_demo相同，把DEMO_TARGET换成rpmsg_pty_demo编译。
````
    cmake -S . -B build -DDEMO_TARGET=rpmsg_pty_demo
````

2.  使用说明，为了不影响shell的使用，建议启动qemu时加上console=ttyAMA1内核参数，将内核打印屏蔽。
````
    qemu-system-aarch64 -M virt,gic-version=3 -m 1G -cpu cortex-a57 -nographic -kernel zImage -initrd *.rootfs.cpio.gz -append 'mem=768M maxcpus=3 console=ttyAMA1' -smp 4
````

树莓派可以通过设置printk在console上的打印优先级屏蔽内核打印。
````
    cat /proc/sys/kernel/printk > /tmp/printk_bak
    echo 1       4       1      7 > /proc/sys/kernel/printk
````

3.  运行rpmsg_main程序时，把程序放在后台，然后通过screen打开shell。
````
    ./rpmsg_main -c 3 -t zephyr_qemu.bin -a 0x7a000000 &
    screen /dev/pts/0
````

4.  进入shell，输入help可以查看Client OS支持哪些命令，输入history可以查看历史命令，ctrl+d退出。
````
    uart:~$ help
    uart:~$ history
````