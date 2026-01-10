# Introduction

为了支持MICA混合部署和通信的功能，除了在Linux部署micad服务和内核态ko，还需要client OS适配MICA接口。

在MICA发展初期，UniProton 和 Zephyr作为对接MICA的RTOS示例，将适配MICA的代码嵌入在各自系统源码中。然而当MICA需要逐渐对接更多RTOS生态，各个RTOS开发人员需要移植很多重复性的代码到自己的系统源码中。因此我们推出libmica静态库，解耦了MICA通信所需的代码，client OS仅需要在各自的源码中调用libmica初始化和通信接口，即可支持MICA混合部署功能。



# 新client OS如何对接

1. 前置准备：新OS集成 [libmetal](https://gitcode.com/src-openeuler/libmetal.git) 和[open-amp](https://gitcode.com/src-openeuler/OpenAMP.git)，确保可顺利编译出 `libmetal.a` 和 `libopen_amp.a`。

2. 将libmica放在 libmetal 和 open-amp 同目录下，例如：

   ```
   $ tree -L 2
   .
   ├── libmetal
   │   ├── cmake
   │   └── ...
   ├── libmica
   │   ├── cmake
   │   ├── CMakeLists.txt
   │   ├── lib
   │   ├── README.md
   │   └── src
   └── open-amp
       ├── apps
       └── ...
   ```

3. 在 libmica/lib/system 下增加新OS的目录，你可以直接将 libmica/lib/system/demo 拷贝成新OS目录，如 libmica/lib/system/newRTOS。

4. 参考 `libmica系统头文件` 章节，在上述头文件中实现系统强相关的函数。如果有需要，可自行增加.c。

5. 在 libmica/cmake/platform 下参考 demo_riscv_gcc.cmake 添加新OS的cmake文件。

   1. CROSS_PREFIX 的赋值需要改为新OS的交叉编译工具链。
   2. CLIENT_OS_PATH 的赋值改为新OS源码目录。
   3. PLATFORM_INCLUDE_DIRS 中添加libmica会依赖的新OS头文件目录。
   4. PROJECT_SYSTEM 的赋值改为 lib/system/ 下新目录的名称。
   5. MICA_PED 当前支持：baremetal、hetero；待支持：jailhouse、xen。

6. 编译libmica

   ```
   cd libmica
   mkdir -p build
   cd build
   rm -rf *
   cmake ../ -DCMAKE_TOOLCHAIN_FILE=../cmake/platform/newRTOS_arch_gcc.cmake
   make VERBOSE=1 DESTDIR=../output install
   ```

7. 查看编译生成件

   ```
   $ tree output/
   output/
	└── usr
		└── local
			├── include
			│   └── mica
			│       ├── mica.h
			│       ├── platform
			│       │   ├── barrier.h
			│       │   ├── compat.h
			│       │   ├── delay.h
			│       │   ├── io.h
			│       │   ├── irq.h
			│       │   ├── log.h
			│       │   ├── macro.h
			│       │   ├── securec.h
			│       │   ├── sem.h
			│       │   └── system
			│       │       └── demo
			│       │           ├── barrier.h
			│       │           ├── compat.h
			│       │           ├── delay.h
			│       │           ├── io.h
			│       │           ├── irq.h
			│       │           ├── log.h
			│       │           ├── macro.h
			│       │           ├── securec.h
			│       │           ├── sem.h
			│       │           └── xxx.h
			│       └── service.h
			└── lib
				└── libmica.a
   ```

8. 将 libmica.a 链接进新RTOS

9. RTOS源码增加libmica API调用

   1. 系统初始化流程中调用mica初始化接口
   2. 通信应用中调用mica通信接口

10. 编译出新RTOS

11. 上板测试

12. WE ARE DONE!



# libmica APIs

## 初始化

### int mica_init(struct mica_config *config);

 - 功能：初始化 MICA 框架，包括底座（pedestal）的中断系统和通信机制。该函数必须在使用任何 MICA 服务之前调用。
 - 入参：
	struct mica_config {
		uintptr_t shm_base_addr; // 共享内存基地址
		size_t shm_size; // 共享内存大小
		uint32_t ipc_irq_num; // 通信中断号
		uintptr_t ipc_irq_base; // 通信中断基地址（暂时仅hetero需要）
		struct mica_sys_ops sys_ops; // 系统特定处理函数
	};
	struct mica_sys_ops {
		void (*shell_cmd_handler)(char c); // 系统shell命令行回调函数
		void (*system_poweroff)(void); // 系统下电回调函数
	};
 - 返回：MICA_SUCCESS (0)成功，负数失败


### int mica_create_all_services(void);

 - 功能：创建并启动所有 MICA 服务，包括

	TTY 服务线程：处理终端消息，支持 Shell 交互
	UMT 服务线程：处理用户消息传输（零拷贝数据传输）
	消息接收线程：监听并分发来自 Linux 的 RPMsg 消息
	该函数会阻塞等待所有服务就绪后才返回。

 - 入参：无
 - 返回：MICA_SUCCESS (0)成功，负数失败

### int mica_create_service(enum mica_service_type type); （待支持）

 - 功能：仅创建并启动用户所需MICA服务

 - 入参：enum mica_service_type type
	enum mica_service_type {
		MICA_SERVICE_RPC = 0,
		MICA_SERVICE_TTY,
		MICA_SERVICE_UMT,
		MICA_SERVICE_MAX
	};
 - 返回：MICA_SUCCESS (0)成功，负数失败

### int mica_service_is_ready(enum mica_service_type type);

 - 功能：检查指定的 MICA 服务是否已就绪（endpoint 已创建并可用）。

 - 入参：enum mica_service_type type
	enum mica_service_type {
		MICA_SERVICE_RPC = 0,
		MICA_SERVICE_TTY,
		MICA_SERVICE_UMT,
		MICA_SERVICE_MAX
	};
 - 返回：
	1 (true): 服务已就绪
	0 (false): 服务未就绪



## 通信

### int mica_send_data(void *data, int offset, size_t len);

 - 功能：通过 UMT（User Message Transfer）服务发送数据到 Linux 侧，采用零拷贝技术，数据通过共享内存传输，性能高效。

 - 入参：
	data: 要发送的数据缓冲区指针
	offset: 共享内存中的偏移量（字节），用于支持多次发送而不覆盖之前的数据
	len: 要发送的数据长度（字节）
 - 返回：
	MICA_SUCCESS (0): 发送成功
	-EAGAIN: UMT 服务未就绪
	-EINVAL: 参数无效（data 为空、len 为 0 或超出共享内存范围）
	-EFAULT: 发送缓冲区地址未初始化
	-EIO: 发送失败

### int mica_rcv_data(void *buffer, size_t *len);

 - 功能：从 Linux 侧接收数据，采用零拷贝技术，数据通过共享内存传输。该函数会阻塞等待直到有数据到达。

 - 入参：
	buffer: 接收数据的缓冲区指针
	len: 指向数据长度的指针，会返回实际接收到的数据长度

 - 返回：
	MICA_SUCCESS (0): 接收成功
	-EAGAIN: UMT 服务未就绪或等待超时
	-EINVAL: 参数无效（buffer 或 len 为空）
	-EFAULT: 接收到的消息无效

### int mica_tty_send(unsigned char *data, size_t len);

 - 功能：通过 TTY endpoint 向 Linux 侧发送数据，用于终端输出。

 - 入参：
	data: 要发送的数据缓冲区指针
	len: 要发送的数据长度（字节）

 - 返回：
	正数: 实际发送的字节数
	0: TTY 服务未就绪或发送失败
	负数: 发送错误

### int mica_tty_printf(const char *format, ...);

 - 功能：格式化打印数据到 TTY endpoint，类似标准 C 库的 printf 函数，支持常见的格式化输出（%d、%s、%x 等）。

 - 入参：
	format: printf 风格的格式化字符串
	...: 可变参数列表，对应格式化字符串中的占位符

 - 返回：
	正数: 实际打印的字符数
	负数: 打印失败（格式化错误或 TTY 服务未就绪）



## client OS侧使用示例

```
#include <mica/mica.h>
#include <mica/service.h>

#define dprintf(fmt, ...) mica_tty_printf(fmt, ##__VA_ARGS__)

void create_random_char(char *buffer)
{
    for (int i = 0; i < STR_SIZE; i++) {
        buffer[i] = 'B' + rand() % 26;
    }
}

void OsUmtTask(void)
{
    char *rcv_buffer = NULL , *result_buffer = NULL;
    int data_len = 0;
    int ret = 0, i = 0;

    srand(time(NULL));
    rcv_buffer = (char*)malloc(STR_SIZE + 1);
    if (rcv_buffer == NULL) {
        dprintf("rcv_buffer malloc failed\n");
    }
    result_buffer = (char*)malloc(STR_SIZE + 1);
    if (result_buffer == NULL) {
        dprintf("result_buffer malloc failed\n");
    }
    memset(result_buffer, 0, STR_SIZE + 1);
    create_random_char(result_buffer);

    dprintf("UMT TASK start ...\n");

    while(1) {
        memset(rcv_buffer, 0, STR_SIZE + 1);
        ret = mica_rcv_data(rcv_buffer, &data_len);
        if (ret != 0) {
            dprintf("mica_rcv_data failed, ret: %d\n", ret);
            continue;
        }
        dprintf("===========================================\n");
        dprintf("received (last 10 char): %s, data_len: %d\n", (char *)rcv_buffer + data_len - 10, data_len);
        (void)mica_send_data(result_buffer, 0, STR_SIZE);
        dprintf("sent back (last 10 char): %s, data_len: %d\n", (char *)result_buffer + STR_SIZE - 10, STR_SIZE);
    }

    return;
}

void umt_listener_init(void)
{
    UINT32 ret;
    TSK_INIT_PARAM umtTask = {0};

    umtTask.TaskEntry = (TSK_ENTRY_FUNC)OsUmtTask;
    // other task initialization...

    ret = OS_TaskCreate(&umtTaskId, &umtTask);
}

char *g_s1 = "Hello, client OS! Wait for init...\r\n";

static void shell_cmd_fn(char c)
{
    ShellCB *shellCb = OsGetShellCB();

    if (shellCb == NULL) {
        mica_tty_send((void *)g_s1, strlen(g_s1) * sizeof(char));
    } else {
        ShellCmdLineParse(c, (pf_OUTPUT)mica_tty_printf, shellCb);
    }
}

void app_init(void)
{
    // other app init...
    struct mica_sys_ops mica_sys_ops = {
        .shell_cmd_handler = shell_cmd_fn,
    };
    struct mica_config mica_config = {
        .shm_base_addr = (uintptr_t)0x40000000,
        .shm_size = 0x100000,
        .ipc_irq_num = 26,
        .ipc_irq_base = 0x11031000,
        .sys_ops = mica_sys_ops,
    };
    
    if (mica_init(&mica_config) != 0) {
        dprintf("mica_init failed\n");
    }

    if (mica_create_all_services() != 0) {
        dprintf("mica_create_all_services failed\n");
    }

    while (!mica_service_is_ready(MICA_SERVICE_TTY)) {
        delay(100);
    }

    umt_listener_init();
}

/* Support shell cmd to trigger mica to send data */
static UINT32 OsShellCmdMicaSend(UINT32 argc, CHAR **argv)
{
    int ret = 0;
    const char *data;
    int data_len = 0;
    int offset = 0;

    if ((argc < 1) || (argc > 2)) {
        dprintf("usage: mica_send <data> [mem_offset]\n");
        dprintf("\t[mem_offset] is optional. Default is 0.\n");
        dprintf("eg:\n");
        dprintf("\tmica_send \"ABCDEFG\"\n");
        dprintf("\tmica_send \"ABCDEFG\" 1024\n");
        return OS_FAIL;
    }

    data = argv[0];
    data_len = strlen(data);

    if (argc > 1) {
        offset = strtoul(argv[1], 0, 0);
    }

    ret = mica_send_data((void *)data, offset, data_len);
    if (ret != 0) {
        dprintf("mica_send_data failed, ret %d data_len %d\n", ret, data_len);
    } else {
        dprintf("MICA: sent data \"%s\" len 0x%x to Linux successfully\n", data, data_len);
    }

    return ret;
}

SHELLCMD_ENTRY(mica_send_shellcmd, CMD_TYPE_EX, "mica_send", XARGS, (CmdCallBackFunc)OsShellCmdMicaSend);
```

## libmica系统头文件

### 待补充




# Support List

## 支持使用libmica的Client OS

### UniProton

    状态: 不支持，暂时嵌入在源码

### Zephyr

    状态: 不支持，暂时嵌入在源码

### LiteOS

    状态: 测试中

## libmica 底座支持

### Baremetal

    状态: 测试中

### Hetero（异构）

    状态: 测试中（RISCV MCU）

### Jailhouse

    状态: 待支持

### Xen

    状态: 待支持

## libmica 特性支持

### TTY服务

    状态: 支持

### UMT服务

    状态: 支持

### RPC服务

    状态: 待支持

### GDB服务

    状态: 待支持

### SMP

    状态: 待支持


# Architecture / Modules
┌─────────────────────────────────────────────────┐
│          Application / Services                 │
│    (rpc_service, tty_service, umt_service)      │
└─────────────────────────────────────────────────┘
                      ↑
                      │ 调用
                      ↓
┌─────────────────────────────────────────────────┐
│          MICA Core Layer                        │
│    (mica_init.c, mica_service.c)                │
└─────────────────────────────────────────────────┘
                      ↑
                      │ 通过 ops 接口
                      ↓
┌─────────────────────────────────────────────────┐
│       Pedestal Abstraction Layer                │
│          (mica_ped.h - 定义接口)                 │
└─────────────────────────────────────────────────┘
                      ↑
                      │ 具体实现
                      ↓
┌─────────────────────────────────────────────────┐
│     Pedestal Implementations                    │
│  (hetero.c, baremetal.c...)        │
└─────────────────────────────────────────────────┘
