/* SPDX-License-Identifier: GPL-2.0 */
/*
 * MCS kernel module pedestal interface.
 *
 * Exactly one pedestal backend is compiled into mcs_km.ko.  The core
 * owns the /dev/mcs character device and the ioctl ABI, while each
 * backend only implements the operations that are specific to its
 * remote-core transport.
 */
#ifndef __MCS_KM_PEDESTAL_H
#define __MCS_KM_PEDESTAL_H

#include <linux/fs.h>
#include <linux/ioctl.h>
#include <linux/poll.h>
#include <linux/types.h>

#define MCS_DEVICE_NAME		"mcs"

/* MCS KM ioctl command */
#define MAGIC_NUMBER		'A'
#define IOC_SENDIPI		_IOW(MAGIC_NUMBER, 0, int)
#define IOC_XPUON		_IOW(MAGIC_NUMBER, 1, int)
#define IOC_AFFINITY_INFO	_IOW(MAGIC_NUMBER, 2, int)
#define IOC_QUERY_MEM		_IOW(MAGIC_NUMBER, 3, int)
#define IOC_GET_COPY_MSG_MEM	_IOWR(MAGIC_NUMBER, 4, struct core_msg_mem_info)
#define IOC_SET_PED_TYPE	_IOW(MAGIC_NUMBER, 5, int)
#define IOC_MCUOFF		_IOW(MAGIC_NUMBER, 6, int)
#define IOC_MAXNR		6

#define RPROC_MEM_MAX		4

/* SHM size */
#define INSTANCE_SIZE		0x2400000	/* 实例大小36M */
#define OPENAMP_SHM_SIZE	0x1000000
#define OPENAMP_SHM_COPY_SIZE	0x100000

/*
 * Pedestal type values are part of the kernel/user ABI: mica passes
 * these integers through IOC_SET_PED_TYPE.  MCS_KM_PED_AARCH64 keeps
 * the historical value 0 of the baremetal pedestal.
 */
enum mcs_km_pedestal_type {
	MCS_KM_PED_AARCH64 = 0,
	MCS_KM_PED_HISI_RISCV = 1,
	MCS_KM_PED_STM32MP2 = 2,
	MCS_KM_PED_INVALID = 3,
};

struct cpu_info {
	u32 cpu;
	u64 boot_addr;
};

struct core_msg_mem_info {
	unsigned int instance_id; /* 当前不支持多实例，使用时赋值为0；支持多实例以后修改成具体实例号 */
	unsigned long phy_addr;
	void *vir_addr;
	size_t size;
	size_t align_size;
};

/**
 * struct mcs_rproc_mem - internal memory region description
 * @phy_addr: physical address of the memory region
 * @size: total size of the memory region
 */
struct mcs_rproc_mem {
	u64 phy_addr;
	u64 size;
};

/**
 * struct mcs_pedestal_ops - operations implemented by a pedestal backend
 *
 * Optional callbacks may be NULL; the core returns -EOPNOTSUPP for an
 * ioctl that has no backend implementation.
 */
struct mcs_pedestal_ops {
	const char *name;
	enum mcs_km_pedestal_type type;

	int (*init)(void);
	void (*exit)(void);

	int (*query_mem)(struct mcs_rproc_mem *mem);
	bool (*mmap_allowed)(phys_addr_t offset, size_t size);
	__poll_t (*poll)(struct file *file, poll_table *wait);

	int (*send_ipi)(const struct cpu_info *info);
	int (*xpu_on)(const struct cpu_info *info);
	int (*xpu_off)(void);
	int (*affinity_info)(const struct cpu_info *info);

	int (*copy_msg_mem)(struct core_msg_mem_info *info);
};

int mcs_init_ped_rsv_mem(const char *compatible_node,
			 struct mcs_rproc_mem *mem,
			 const char *mem_region_name,
			 bool support_cmdline,
			 unsigned long rmem_base,
			 unsigned long rmem_size);
void mcs_release_ped_rsv_mem(struct mcs_rproc_mem *mem, bool skip_release);
int mcs_copy_msg_mem(struct mcs_rproc_mem *mem,
		     struct core_msg_mem_info *info);

extern const struct mcs_pedestal_ops mcs_aarch64_ops;
extern const struct mcs_pedestal_ops mcs_hisi_riscv_ops;
extern const struct mcs_pedestal_ops mcs_stm32mp2_ops;

#endif /* __MCS_KM_PEDESTAL_H */
