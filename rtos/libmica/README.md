# Introduction

为了支持MICA混合部署和通信的功能，除了在Linux部署micad服务和内核态ko，还需要client OS适配MICA接口。

libmica静态库集中维护通用初始化、服务、OpenAMP通信和底座协议，避免每个RTOS重复移植。client OS完成系统接口适配后，应用通过libmica初始化和通信接口使用混合部署功能。



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

3. 在 `libmica/lib/system` 下参考 demo 目录增加新 OS 的目录，如 `libmica/lib/system/newRTOS`。`lib/system/demo` 提供 io、irq、log、sem、time、thread、lock、memory 映射模板；其中 `demo_*` 是待接入的 OS 类型和函数。

4. 参考 `libmica系统头文件` 章节实现 OS 相关能力。应用使用 `mica/{mica,service,debug}.h`；在 `lib/system/newRTOS/` 中实现相应的平台接口。需要单独编译的适配源码在最终应用中与 `libmica.a` 链接。

5. 参考 `cmake/platform/demo_riscv_gcc.cmake`，在 client OS 仓维护自己的 toolchain 文件。

   1. CROSS_PREFIX 的赋值需要改为新OS的交叉编译工具链。
   2. CLIENT_OS_PATH 的赋值改为新OS源码目录。
   3. PLATFORM_INCLUDE_DIRS 添加新 OS 的头文件目录，供 `lib/system/newRTOS/` 中的适配头引用 OS 原生类型和函数。
   4. PROJECT_SYSTEM 设置为新 OS 的目录名，例如 `newRTOS`，用于选择 `lib/system/newRTOS/` 下的系统定义。
   5. MICA_PED 当前支持：baremetal、hetero、xen。
   6. 选择 Xen 底座时，`MICA_XEN_PROVIDER` 设为 `builtin` 使用 libmica 提供的 Xen 支撑实现（例如 Xen hypercall 等），设为 `native` 使用 client OS 已有实现（需对接适配接口）。

6. 编译libmica

   ```
    cmake -S <MCS>/rtos/libmica -B <build>/libmica \
      -DCMAKE_TOOLCHAIN_FILE=<client-OS>/newRTOS_arch_gcc.cmake \
      -DCMAKE_INSTALL_PREFIX=<output>
    cmake --build <build>/libmica
    cmake --install <build>/libmica
   ```

7. 查看安装生成件。指定 `-DCMAKE_INSTALL_PREFIX=<output>` 时，文件直接位于该目录。以 Xen builtin、`PROJECT_SYSTEM=newRTOS` 为例，安装规则产生以下布局：

   ```
   <output>/
     include/mica/
       mica.h
       service.h
       debug.h
       platform/
         io.h
         irq.h
         lock.h
         log.h
         memory.h
         sem.h
         thread.h
         time.h
         system/newRTOS/
           io.h
           irq.h
           lock.h
           log.h
           memory.h
           sem.h
           thread.h
           time.h
         xen/provider.h
     lib/
       libmica.a
       mica-resource-table.ld
     share/licenses/mica/xen/
       SOURCES.md
       LICENSES/THIRD_PARTY_LICENSES.txt
       LICENSES/distribution.list
   ```

8. 将 `libmica.a`、OS 自己编译的适配代码及 OpenAMP/libmetal 链接进新 RTOS。

   ```
   -Wl,--undefined=rsc_table_get \
   -Wl,-T,<output>/lib/mica-resource-table.ld \
   -Wl,--start-group <output>/lib/libmica.a libopen_amp.a libmetal.a \
   <OS-adapter-and-core-libraries> -Wl,--end-group
   ```

9. RTOS源码增加libmica API调用

   1. 系统初始化流程中调用mica初始化接口
   2. 通信应用中调用mica通信接口

10. 编译出新RTOS

11. 上板测试



# libmica APIs

## 初始化

### int mica_init(struct mica_config *config);

 - 功能：初始化 MICA 框架，包括底座（pedestal）的中断系统和通信机制。该函数必须在使用任何 MICA 服务之前调用。
 - 入参：
	struct mica_config {
		uintptr_t shm_base_addr; // 共享内存基地址
		size_t shm_size; // 共享内存大小
		uint32_t ipc_irq_num; // 通信中断号
		uintptr_t ipc_irq_base; // 通信中断基地址（仅 hetero 需要）
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
	成功表示本地创建完成，不等待远端绑定。
	Xen 暂不支持 UMT。

 - 入参：无
 - 返回：MICA_SUCCESS (0)成功，负数失败

### int mica_create_service(enum mica_service_type type);

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

 - 功能：检查指定服务是否已与远端绑定；本地 create 成功不等于远端已就绪。

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



### int mica_stop_services(void);

 - 同步停止全部服务；成功后可调用 `mica_remove()` 清理实例。不支持单服务停止或同域 stop 后重新创建。
  - 超时返回 `-ETIMEDOUT` 并保留仍被访问的资源；待使用者退出后可重试 stop。不要在失败后强制释放实例。

## 独立调试输出（可选）

包含 `<mica/debug.h>`，在普通启动或任务上下文中显式初始化：

```c
if (mica_debug_init() == 0) {
    (void)mica_debug_printf("boot step=%d\n", 1);
}
```

`mica_debug_write(bytes, len)` 和 `mica_debug_printf(format, ...)` 可在 `mica_init()` 前和实例 stop/remove 后使用。Xen builtin 输出到标准 PV console，在 Dom0 用 `xl console -t pv <domain-name>` 查看；shell 通过 `ttyRPMSG` 交互。

client OS 在内存和必要平台设施初始化后调用 `mica_debug_init()`。初始化失败不应阻断 OS 或 TTY 启动。

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
	0: 零长度写入
	负数: 未就绪或发送错误（如 -EAGAIN、-ESHUTDOWN）

### int mica_tty_printf(const char *format, ...);

 - 功能：格式化打印数据到 TTY endpoint，类似标准 C 库的 printf 函数，支持常见的格式化输出（%d、%s、%x 等）。

 - 入参：
	format: printf 风格的格式化字符串
	...: 可变参数列表，对应格式化字符串中的占位符

 - 返回：
	正数: 实际打印的字符数
	负数: 打印失败（格式化错误或 TTY 服务未就绪）



## client OS侧使用示例

```c
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <mica/mica.h>
#include <mica/service.h>
#include <mica/platform/thread.h>

#define STR_SIZE 128

/* 由应用接入自己的 shell 和任务延时接口。 */
extern void client_shell_input(char c);
extern void client_sleep_ms(unsigned int milliseconds);

#define dprintf(fmt, ...) mica_tty_printf(fmt, ##__VA_ARGS__)

void create_random_char(char *buffer)
{
    for (int i = 0; i < STR_SIZE; i++) {
        buffer[i] = 'B' + rand() % 26;
    }
}

static void *umt_listener(void *arg)
{
    char *rcv_buffer = NULL , *result_buffer = NULL;
    size_t data_len = 0;
    int ret = 0;

    (void)arg;

    srand(time(NULL));
    rcv_buffer = (char*)malloc(STR_SIZE + 1);
    if (rcv_buffer == NULL) {
        dprintf("rcv_buffer malloc failed\n");
        return NULL;
    }
    result_buffer = (char*)malloc(STR_SIZE + 1);
    if (result_buffer == NULL) {
        dprintf("result_buffer malloc failed\n");
        free(rcv_buffer);
        return NULL;
    }
    memset(result_buffer, 0, STR_SIZE + 1);
    create_random_char(result_buffer);

    dprintf("UMT TASK start ...\n");

    while(1) {
        memset(rcv_buffer, 0, STR_SIZE + 1);
        data_len = STR_SIZE; /* 每次调用前提供真实缓冲区容量。 */
        ret = mica_rcv_data(rcv_buffer, &data_len);
        if (ret != 0) {
            dprintf("mica_rcv_data failed, ret: %d\n", ret);
            continue;
        }
        dprintf("===========================================\n");
        size_t tail_len = data_len < 10 ? data_len : 10;
        dprintf("received (last 10 char): %.*s, data_len: %zu\n",
                (int)tail_len, rcv_buffer + data_len - tail_len, data_len);
        (void)mica_send_data(result_buffer, 0, STR_SIZE);
        dprintf("sent back (last 10 char): %s, data_len: %d\n", (char *)result_buffer + STR_SIZE - 10, STR_SIZE);
    }

    return NULL;
}

static mica_thread_t umt_thread;

static void shell_cmd_fn(char c)
{
    client_shell_input(c);
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
        return;
    }

    if (mica_create_all_services() != 0) {
        dprintf("mica_create_all_services failed\n");
        return;
    }

    while (!mica_service_is_ready(MICA_SERVICE_TTY)) {
        client_sleep_ms(100);
    }

    if (mica_thread_create(&umt_thread, umt_listener, NULL) != 0) {
        dprintf("create UMT listener failed\n");
    }
}

/* Support shell cmd to trigger mica to send data */
int mica_send_command(int argc, char **argv)
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
        return -EINVAL;
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

/* 将 mica_send_command 注册到应用自己的 shell。 */
```

## libmica系统头文件

`rtos/libmica/lib/` 提供平台接口头，`lib/system/demo/` 提供移植示例。新 OS 在 `lib/system/<OS>/` 中实现同名适配头；`PROJECT_SYSTEM` 选择该目录，`PLATFORM_INCLUDE_DIRS` 提供适配头依赖的 OS 头文件目录。

以下示例中的 `demo_*` 表示待替换的 OS 类型和函数。除特别说明外，接口返回 0 表示成功，负 errno 表示失败。

以下是各个头文件及接口的详细说明：

### 1. io.h - 寄存器读写

**作用**：提供硬件寄存器的读写功能，用于访问和控制硬件设备。

**需要实现的函数**：
```c
void mica_writeb(uint8_t val, unsigned long addr);
void mica_writew(uint16_t val, unsigned long addr);
void mica_writel(uint32_t val, unsigned long addr);
```

**使用场景**：在MICA的中断处理和硬件通信中，用于配置和控制硬件寄存器。

**底座使用情况**：
- baremetal：不需要实现，该底座不直接操作硬件寄存器
- hetero：需要实现，在处理异构中断通信时调用
- xen：不需要实现

**内核态示例**：
```c
static inline void mica_writeb(uint8_t val, unsigned long addr)
{
    *(volatile uint8_t *)addr = val;
}

static inline void mica_writew(uint16_t val, unsigned long addr)
{
    *(volatile uint16_t *)addr = val;
}

static inline void mica_writel(uint32_t val, unsigned long addr)
{
    *(volatile uint32_t *)addr = val;
}
```

### 2. irq.h - 中断处理

**作用**：提供中断请求和管理功能，用于处理硬件中断。

**需要实现的函数**：
```c
typedef void (*mica_irq_handler_t)(void *arg);
int mica_request_irq(unsigned int irq, mica_irq_handler_t handler, void *arg);
int mica_unmask_irq(unsigned int irq);
int mica_trigger_irq(unsigned int irq);
int mica_sync_release_irq(unsigned int irq, mica_deadline_t deadline);
```

**使用场景**：在MICA的通信过程中，用于处理来自Linux侧的中断请求。

`request` 只注册，由 `unmask` 启用；`sync_release` 成功后不得再有回调访问 `arg`，失败则保留注册。`deadline` 为绝对单调纳秒期限。

**底座使用情况**：
- baremetal：注册、启用、触发及同步释放中断
- hetero：注册、启用及同步释放中断；通过寄存器通知对端
- xen：注册和启用事件中断；通过 event-channel 通知对端

**示例**：
```c
static inline int mica_request_irq(unsigned int irq,
                                   mica_irq_handler_t handler, void *arg)
{
    return demo_request_irq(irq, handler, arg);
}

static inline int mica_unmask_irq(unsigned int irq)
{
    return demo_irq_unmask(irq);
}

static inline int mica_trigger_irq(unsigned int irq)
{
    return demo_irq_trigger(irq);
}

static inline int mica_sync_release_irq(unsigned int irq,
                                       mica_deadline_t deadline)
{
    return demo_irq_sync_release(irq, deadline);
}
```

### 3. log.h - 日志功能

**作用**：提供日志输出功能，用于调试和信息输出。

**需要实现的函数**：
```c
void mica_log(const char *fmt, ...);
```

**使用场景**：在MICA的各个模块中，用于输出调试信息和运行状态。

**底座使用情况**：
- baremetal、hetero、xen：用于输出初始化、通信及错误信息

**示例**：

输出到 OS 日志或串口；TTY 就绪后，也可将 `mica_log` 映射到 `mica_tty_printf`。

```c
#include <stdarg.h>

static inline void mica_log(const char *fmt, ...)
{
    va_list args;
    va_start(args, fmt);
    demo_vprintf(fmt, args); /* 输出到 OS 日志或串口。 */
    va_end(args);
}
```

### 4. sem.h - 信号量功能

**作用**：提供信号量，用于线程唤醒与同步。

**需要实现的函数**：
```c
typedef OS_NATIVE_SEMAPHORE mica_sem_t;
#define MICA_SEM_TICK_HZ OS_NATIVE_TICK_HZ
#define MICA_SEM_MAX_FINITE_TICKS OS_MAX_FINITE_TICKS
int mica_sem_init(mica_sem_t *sem, unsigned int initial);
int mica_sem_post(mica_sem_t *sem);
int mica_sem_pend(mica_sem_t *sem, uint32_t ticks);
int mica_sem_destroy(mica_sem_t *sem);
```

`mica_sem_t` 使用 OS 原生类型。`post` 需可在 IRQ 中调用；`pend(0)` 立即尝试，正 tick 为有限等待，超时返回 `-ETIMEDOUT`。最大有限 tick 不含 OS 的永久等待值。libmica 提供 `mica_sem_wait_until()`，按绝对期限等待信号量。

**使用场景**：中断唤醒消息接收线程、通知工作线程有数据到达。

**底座使用情况**：
- baremetal、hetero、xen：均需要实现

**示例**：
```c
typedef demo_sem_t mica_sem_t;
#define MICA_SEM_TICK_HZ DEMO_SEM_TICK_HZ
#define MICA_SEM_MAX_FINITE_TICKS DEMO_SEM_MAX_FINITE_TICKS

static inline int mica_sem_init(mica_sem_t *sem, unsigned int initial)
{
    return demo_sem_init(sem, initial);
}

static inline int mica_sem_post(mica_sem_t *sem)
{
    return demo_sem_post(sem);
}

static inline int mica_sem_pend(mica_sem_t *sem, uint32_t ticks)
{
    return demo_sem_pend(sem, ticks);
}

static inline int mica_sem_destroy(mica_sem_t *sem)
{
    return demo_sem_destroy(sem);
}
```

### 5. time.h - 单调时钟与截止时间

**作用**：提供单调时钟，供等待和超时计算使用。

**需要实现的函数**：
```c
uint64_t mica_now_ns(void);
```

libmica 定义 `mica_deadline_t` 为 `uint64_t`，表示绝对单调纳秒期限，并提供 `mica_timeout_ticks()` 用于换算有限 tick。

**使用场景**：信号量、锁及线程退出等待的超时判断。

**底座使用情况**：
- baremetal、hetero、xen：均需要实现

**示例**：
```c
static inline uint64_t mica_now_ns(void)
{
    return demo_monotonic_ns();
}
```

### 6. lock.h - 限期互斥锁

**作用**：提供具有等待期限的互斥锁。

**需要实现的函数**：
```c
typedef OS_NATIVE_MUTEX mica_lock_t;
int mica_lock_init(mica_lock_t *lock);
int mica_lock_acquire(mica_lock_t *lock, mica_deadline_t deadline);
int mica_lock_release(mica_lock_t *lock);
int mica_lock_destroy(mica_lock_t *lock);
```

`mica_lock_t` 使用 OS 原生 mutex 类型；`acquire` 在任务上下文使用绝对单调纳秒期限，超时返回 `-ETIMEDOUT`。

**使用场景**：Xenstore 事务、事件通道及 Xen 通知路径的同步。

**底座使用情况**：
- baremetal、hetero：不调用这组接口
- xen：需要实现

**示例**：
```c
typedef demo_mutex_t mica_lock_t;

static inline int mica_lock_init(mica_lock_t *lock)
{
    return demo_mutex_init(lock);
}

static inline int mica_lock_acquire(mica_lock_t *lock, mica_deadline_t deadline)
{
    return demo_mutex_acquire(lock, deadline);
}

static inline int mica_lock_release(mica_lock_t *lock)
{
    return demo_mutex_release(lock);
}

static inline int mica_lock_destroy(mica_lock_t *lock)
{
    return demo_mutex_destroy(lock);
}
```

### 7. thread.h - 线程创建与退出回收

**作用**：创建通信线程，并在停止服务时等待线程退出。

**需要实现的函数**：
```c
typedef OS_NATIVE_THREAD mica_thread_t;
typedef void *(*mica_thread_entry_t)(void *arg);
int mica_thread_create(mica_thread_t *out,
                       mica_thread_entry_t entry, void *arg);
int mica_thread_is_self(mica_thread_t thread);
int mica_thread_join(mica_thread_t thread, mica_deadline_t deadline);
```

`mica_thread_t` 使用 OS 原生线程类型。`is_self` 返回 1/0；`join` 使用绝对单调纳秒期限，成功表示线程已退出并完成回收，超时返回 `-ETIMEDOUT`。

**使用场景**：启动消息接收及服务工作线程，在服务停止时回收线程。

**底座使用情况**：
- baremetal、hetero、xen：均需要实现

**示例**：
```c
typedef demo_thread_t mica_thread_t;
typedef void *(*mica_thread_entry_t)(void *arg);

static inline int mica_thread_create(mica_thread_t *out,
                                     mica_thread_entry_t entry, void *arg)
{
    return demo_thread_create(out, entry, arg);
}

static inline int mica_thread_is_self(mica_thread_t thread)
{
    return demo_thread_is_self(thread);
}

static inline int mica_thread_join(mica_thread_t thread, mica_deadline_t deadline)
{
    return demo_thread_join(thread, deadline);
}
```

### 8. memory.h - 原生内存与本地视图

**作用**：分配和释放对齐内存，查询物理地址，以及建立和撤销本地地址映射。

**需要实现的函数**：
```c
int mica_mem_alloc(size_t bytes, size_t align, void **base);
int mica_mem_free(void *base);
int mica_mem_address(const void *local, uint64_t *physical);
int mica_mem_map(uint64_t physical, size_t bytes, void **local);
int mica_mem_unmap(void *local, size_t bytes);
```

**使用场景**：Xen 共享页、grant 映射及底座初始化所需内存操作。

**底座使用情况**：
- baremetal、hetero：当前不调用这组接口
- xen：需要实现

**示例**：
```c
static inline int mica_mem_alloc(size_t bytes, size_t align, void **base)
{
    return demo_mem_alloc(bytes, align, base);
}

static inline int mica_mem_free(void *base)
{
    return demo_mem_free(base);
}

static inline int mica_mem_address(const void *local, uint64_t *physical)
{
    return demo_mem_address(local, physical);
}

static inline int mica_mem_map(uint64_t physical, size_t bytes, void **local)
{
    return demo_mem_map(physical, bytes, local);
}

static inline int mica_mem_unmap(void *local, size_t bytes)
{
    return demo_mem_unmap(local, bytes);
}
```

### 实现建议

1. **保持接口一致性**：尽量按照头文件和本文档中的接口描述实现，确保libmica能够正确调用。

2. **考虑性能影响**：对于频繁调用的函数（如内存屏障、寄存器读写），可考虑优化实现以减少性能开销。

3. **确保线程安全**：如果OS支持多线程，需要确保信号量等同步机制的线程安全实现。

4. **错误处理**：合理处理各种错误情况，返回明确的错误码，便于调试和问题定位。

5. **参考模板**：`lib/system/demo/` 提供八类功能头的映射示例。


# Support List

## 支持使用libmica的Client OS

### UniProton

已支持 aarch64 Xen。

### Zephyr

    状态: 不支持

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

已支持 aarch64 Xen，提供 builtin/native 两种 provider。

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
