/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: GPL-2.0
 */

#define pr_fmt(fmt) "mcs: " fmt

#include <linux/acpi.h>
#include <linux/device.h>
#include <linux/file.h>
#include <linux/wait.h>
#include <linux/poll.h>
#include <linux/module.h>
#include <linux/arm-smccc.h>
#include <linux/interrupt.h>
#include <linux/cpumask.h>
#include <linux/uaccess.h>
#include <linux/mm.h>
#include <linux/of.h>
#include <linux/of_address.h>
#include <linux/of_irq.h>
#include <linux/version.h>
#ifdef CONFIG_KPROBES
#include <linux/kprobes.h>
#else
#include <asm/smp.h>
#endif

#define MCS_DEVICE_NAME		"mcs"

/**
 * PSCI Functions
 * For more detail see:
 * Arm Power State Coordination Interface Platform Design Document
 */
#define CPU_ON_FUNCID		0xC4000003
#define AFFINITY_INFO_FUNCID	0xC4000004

/* MCS KM ioctl command */
#define MAGIC_NUMBER		'A'
#define IOC_SENDIPI		_IOW(MAGIC_NUMBER, 0, int)
#define IOC_XPUON		_IOW(MAGIC_NUMBER, 1, int)
#define IOC_AFFINITY_INFO	_IOW(MAGIC_NUMBER, 2, int)
#define IOC_QUERY_MEM		_IOW(MAGIC_NUMBER, 3, int)
#define IOC_GET_COPY_MSG_MEM    _IOWR(MAGIC_NUMBER, 4, struct core_msg_mem_info)
#define IOC_SET_PED_TYPE		_IOW(MAGIC_NUMBER, 5, int)
#define IOC_MAXNR		5
#define IPI_MCS			8
#define RPROC_MEM_MAX		4

/* SHM size */
#define INSTANCE_SIZE  0x2400000       /*实例大小36M*/
#define OPENAMP_SHM_SIZE  0x1000000
#define OPENAMP_SHM_COPY_SIZE 0x100000

/* RISCV interrupt */
#define IPC_INT_SET          (0x00)
#define IPC_INT_CLEAR        (0x04)
#define IPC_INT_MSTS         (0x08)
#define IPC_INT_MASK         (0x0C)
#define IPC_INT_RSTS         (0x10)

#define IPC_INT_A55MP_NUM      0x0
#define IPC_INT_RISCV_NUM      0x6

#define RISCV_REG_SIZE 0x100

static void __iomem *riscv_int_base;
static int riscv_int_irq;

/* Other setups */
static struct class *mcs_class;
static int mcs_major;

enum mcs_km_pedestal_type {
	MCS_KM_PED_BAREMETAL = 0,
	MCS_KM_PED_RISCV = 1,
	MCS_KM_PED_INVALID = 2,
};

/* struct mcs_file_private_data - private data for mcs device file */
struct mcs_file_private_data {
	enum mcs_km_pedestal_type ped_type;
};

static int __percpu *mcs_evt;

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

static int invoke_hvc = 1;
/**
 * struct mcs_rproc_mem - internal memory structure
 * @phy_addr: physical address of the memory region
 * @size: total size of the memory region
 */
struct mcs_rproc_mem {
	u64 phy_addr;
	u64 size;
};
static struct mcs_rproc_mem baremetal_mem[RPROC_MEM_MAX];
static struct mcs_rproc_mem riscv_mem[RPROC_MEM_MAX];

static bool has_baremetal_irq = false;
static bool has_riscv_irq = false;

static unsigned long rmem_base;
module_param(rmem_base, ulong, 0400);
MODULE_PARM_DESC(rmem_base, "The base address of baremetal reserved mem");

static unsigned long rmem_size;
module_param(rmem_size, ulong, 0400);
MODULE_PARM_DESC(rmem_size, "The size of baremetal reserved mem");

static DECLARE_WAIT_QUEUE_HEAD(mcs_baremetal_wait_queue);
static DECLARE_WAIT_QUEUE_HEAD(mcs_riscv_wait_queue);
static atomic_t baremetal_irq_ack;
static atomic_t riscv_irq_ack;

static void release_reserved_mem(void);
/**
 * make hvc/smc call
 * @return:
 *    CPU_ON_FUNCID:
 *	SUCCESS			0
 *	INVALID_PARAMETERS 	-2
 *	DENIED			-3
 *	ALREADY_ON		-4
 *	ON_PENDING		-5
 *	INTERNAL_FAILURE	-6
 *	INVALID_ADDRESS		-9
 *
 *    AFFINITY_INFO_FUNCID:
 *	ON			0
 *	OFF			1
 *	ON_PENDING		2
 *	INVALID_PARAMETERS 	-2
 *	DISABLED		-8
 */
static unsigned long invoke_psci_fn(unsigned long function_id,
			unsigned long arg0, unsigned long arg1,
			unsigned long arg2)
{
	struct arm_smccc_res res;

	if (invoke_hvc)
		arm_smccc_hvc(function_id, arg0, arg1, arg2, 0, 0, 0, 0, &res);
	else
		arm_smccc_smc(function_id, arg0, arg1, arg2, 0, 0, 0, 0, &res);
	return res.a0;
}

#ifdef CONFIG_KPROBES
static u64 (*cpu_logical_map_fn)(unsigned int cpu);

static int get_cpu_logical_map(void)
{
	int ret;
	struct kprobe probe = {
		.symbol_name = "cpu_logical_map",
	};

	ret = register_kprobe(&probe);
	if (ret < 0)
		return ret;

	cpu_logical_map_fn = (void *)probe.addr;
	unregister_kprobe(&probe);
	return 0;
}
#endif

/**
 * Enumerate the possible CPU set from __cpu_logical_map[]
 * and return the MPIDR values related to the @cpu.
 * If the @cpu is not found or the hwid is invalid, return INVALID_HWID.
 */
static u64 get_cpu_mpidr(u32 cpu)
{
	if (cpu >= NR_CPUS)
		return INVALID_HWID;

#ifdef CONFIG_KPROBES
	if (cpu_logical_map_fn != NULL)
		return cpu_logical_map_fn(cpu);

	return INVALID_HWID;
#else
	return cpu_logical_map(cpu);
#endif
}

static irqreturn_t handle_clientos_ipi(int irq, void *data)
{
	pr_info("received ipi from client os\n");
	atomic_set(&baremetal_irq_ack, 1);
	wake_up_interruptible(&mcs_baremetal_wait_queue);
	return IRQ_HANDLED;
}

static void enable_mcs_ipi(void *data)
{
	enable_percpu_irq(IPI_MCS, IRQ_TYPE_NONE);
}

static void disable_mcs_ipi(void *data)
{
	disable_percpu_irq(IPI_MCS);
}

static void remove_mcs_ipi(void)
{
	if (has_baremetal_irq) {
		on_each_cpu(disable_mcs_ipi, NULL, 1);
		free_percpu_irq(IPI_MCS, mcs_evt);
		free_percpu(mcs_evt);
	}

	if (has_riscv_irq) {
		free_irq(riscv_int_irq, NULL);
		/* clear irq */
		writel(IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_CLEAR);
		/* unmask irq */
		writel(0x80000000 + IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_MASK);
	}
}

static int init_baremetal_irq(void)
{
	int err;
	struct irq_desc *desc;

#if LINUX_VERSION_CODE >= KERNEL_VERSION(6, 6, 0)
	desc = irq_data_to_desc(irq_get_irq_data(IPI_MCS));
#else
	desc = irq_to_desc(IPI_MCS);
#endif
	if (desc->action) {
		pr_err("IRQ %d is not free\n", IPI_MCS);
		return -EBUSY;
	}

	mcs_evt = alloc_percpu(int);
	if (!mcs_evt)
		return -ENOMEM;

	err = request_percpu_irq(IPI_MCS, handle_clientos_ipi, "MCS IPI", mcs_evt);
	if (err) {
		free_percpu(mcs_evt);
		return err;
	}

	on_each_cpu(enable_mcs_ipi, NULL, 1);

	return 0;
}

static irqreturn_t handle_riscv_irq(int irq, void *data)
{
	pr_info("received ipi from riscv\n");

	/* mask irq */
	writel(IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_MASK);

	/* clear irq */
	writel(IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_CLEAR);

	atomic_set(&riscv_irq_ack, 1);
	wake_up_interruptible(&mcs_riscv_wait_queue);

	/* unmask irq */
	writel(0x80000000 + IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_MASK);

	return IRQ_HANDLED;
}

static int init_riscv_irq(void)
{
	int ret = 0;
	struct device_node *np = NULL;
	struct resource res;

	np = of_find_compatible_node(NULL, NULL, "oe,mcs_riscv_remoteproc");
	if (!np) {
		pr_err("Failed to find dts node: oe,mcs_riscv_remoteproc\n");
		return -ENODEV;
	}

	if (of_address_to_resource(np, 0, &res)) {
		pr_err("Failed to get reg resource from dts\n");
		of_node_put(np);
		return -ENODEV;
	}

	/* fetch riscv interrupt base addr from dts */
	riscv_int_base = ioremap(res.start, resource_size(&res));
	if (!riscv_int_base) {
		pr_err("failed to map ipc_int base\n");
		of_node_put(np);
		return -ENODEV;
	}

	/* fetch riscv interrupt number from dts */
	riscv_int_irq = irq_of_parse_and_map(np, 0);
	of_node_put(np);

	if (!riscv_int_irq) {
		pr_err("Failed to map interrupt from device tree\n");
		return -ENXIO;
	}

	/* clear irq */
	writel(IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_CLEAR);
	/* unmask irq */
	writel(0x80000000 + IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_MASK);
	ret = request_irq(riscv_int_irq, handle_riscv_irq, 0, "MCS RISCV IRQ", NULL);
	if (ret) {
		pr_err(KERN_ERR "Failed to request irq %d, error: %d\n", riscv_int_irq, ret);
		return ret;
	}

	pr_info("MCS RISCV interrupt registered successfully. irq: %d\n", riscv_int_irq);

	return 0;
}

static int init_mcs_ipi(void)
{
	int baremetal_ret, riscv_ret;

	baremetal_ret = init_baremetal_irq();
	if (baremetal_ret == 0) {
		has_baremetal_irq = true;
	}

	riscv_ret = init_riscv_irq();
	if (riscv_ret == 0) {
		has_riscv_irq = true;
	}

	/* at least one mem region has to exist */
	if (!has_baremetal_irq && !has_riscv_irq) {
		pr_err("At least one irq num (baremetal or riscv) must exist\n");
		return -ENODEV;
	}

	return 0;
}

static void send_clientos_ipi(const struct cpumask *target)
{
	ipi_send_mask(IPI_MCS, target);
}

static void send_riscv_interrupt(void)
{
	writel(IPC_INT_RISCV_NUM, riscv_int_base + IPC_INT_SET);
}

static unsigned int mcs_poll(struct file *file, poll_table *wait)
{
	unsigned int mask = 0;
	struct mcs_file_private_data *priv = file->private_data;

	switch (priv->ped_type) {
	case MCS_KM_PED_RISCV:
		poll_wait(file, &mcs_riscv_wait_queue, wait);
		if (atomic_cmpxchg(&riscv_irq_ack, 1, 0) == 1)
			mask |= POLLIN | POLLRDNORM;
		break;
	case MCS_KM_PED_BAREMETAL:
	default:
		poll_wait(file, &mcs_baremetal_wait_queue, wait);
		if (atomic_cmpxchg(&baremetal_irq_ack, 1, 0) == 1)
			mask |= POLLIN | POLLRDNORM;
		break;
	}

	return mask;
}

static int boot_riscv(u64 boot_addr)
{
	int ret = -1;
	int index;
	unsigned int phy_addr[4] = {0x110D2004, 0x11024000, 0x11016400, 0x110D2000};
	void __iomem *v_addr[4] = {NULL, NULL, NULL, NULL};

	pr_info("Booting clientos on RISCV at 0x%llx ...\n", boot_addr);

	for (index = 0; index < 4; index++) {
		v_addr[index] = ioremap(phy_addr[index], RISCV_REG_SIZE);
		if (!v_addr[index]) {
			pr_err("ioremap failed for address 0x%x\n", phy_addr[index]);
			goto iounmap_out;
		}
	}

	writel(boot_addr, v_addr[0]); /* set start addr */
	writel(0x10, v_addr[1]);      /* jtag to mcu */
	writel(0x1, v_addr[3]);       /* core wait */
	writel(0x3, v_addr[2]);       /* rst */
	writel(0x0, v_addr[3]);       /* unwait */
	writel(0x4030, v_addr[2]);    /* unrst */

	ret = 0;

iounmap_out:
	for (index = 0; index < 4; index++) {
		if (v_addr[index]) {
			iounmap(v_addr[index]);
			v_addr[index] = NULL;
		}
	}

	return ret;
}

static long mcs_ioctl(struct file *f, unsigned int cmd, unsigned long arg)
{
	int ret = 0;
	u64 mpidr;
	int ped_type;
	struct mcs_file_private_data *priv = f->private_data;
	struct cpu_info info;
	struct core_msg_mem_info copy_mem_info;
	unsigned long copy_mem_offset;

	if (_IOC_TYPE(cmd) != MAGIC_NUMBER)
		return -EINVAL;
	if (_IOC_NR(cmd) > IOC_MAXNR)
		return -EINVAL;

	switch (cmd) {
	case IOC_SET_PED_TYPE:
		ret = copy_from_user(&ped_type, (int __user *)arg, sizeof(int));
		break;
	case IOC_GET_COPY_MSG_MEM:
		ret = copy_from_user(&copy_mem_info, (struct core_msg_mem_info __user *)arg, sizeof(copy_mem_info));
		break;
	case IOC_QUERY_MEM:
		break;
	default:
		ret = copy_from_user(&info, (struct cpu_info __user *)arg, sizeof(info));
		break;
	}
	if (ret)
		return -EFAULT;

	switch (cmd) {
	case IOC_SET_PED_TYPE:
		if (ped_type >= MCS_KM_PED_INVALID || ped_type < MCS_KM_PED_BAREMETAL) {
			pr_err("invalid pedestal type %d\n", ped_type);
			return -EINVAL;
		}
		priv->ped_type = ped_type;
		break;
	case IOC_SENDIPI:
		switch (priv->ped_type) {
		case MCS_KM_PED_RISCV:
			pr_info("received ioctl cmd to send riscv interrupt to mcu\n");
			send_riscv_interrupt();
			break;
		case MCS_KM_PED_BAREMETAL:
		default:
			pr_info("received ioctl cmd to send ipi to cpu(%d)\n", info.cpu);
			send_clientos_ipi(cpumask_of(info.cpu));
			break;
		}
		break;

	case IOC_XPUON:
		switch (priv->ped_type) {
		case MCS_KM_PED_RISCV:
			pr_info("received ioctl cmd to boot riscv clientos\n");
			ret = boot_riscv(info.boot_addr);
			if (ret) {
				pr_err("boot riscv failed(%d)\n", ret);
				return -EINVAL;
			}
			break;
		case MCS_KM_PED_BAREMETAL:
		default:
			mpidr = get_cpu_mpidr(info.cpu);
			if (mpidr == INVALID_HWID) {
				pr_err("boot clientos failed, invalid MPIDR\n");
				return -EINVAL;
			}
			pr_info("start booting clientos on cpu%d(%llx) addr(0x%llx)\n", info.cpu, mpidr, info.boot_addr);

			ret = invoke_psci_fn(CPU_ON_FUNCID, mpidr, info.boot_addr, 0);
			if (ret) {
				pr_err("boot clientos failed(%d)\n", ret);
				return -EINVAL;
			}
			break;
		}
		break;

	case IOC_AFFINITY_INFO:
		mpidr = get_cpu_mpidr(info.cpu);
		if (mpidr == INVALID_HWID) {
			pr_err("cpu state check failed! Invalid MPIDR\n");
			return -EINVAL;
		}

		ret = invoke_psci_fn(AFFINITY_INFO_FUNCID, mpidr, 0, 0);
		if (ret != 1) {
			pr_err("cpu state check failed! cpu(%d) is not in the OFF state, current state: %d\n",
				info.cpu, ret);
			return -EFAULT;
		}
		break;

	case IOC_QUERY_MEM:
	switch (priv->ped_type) {
	case MCS_KM_PED_RISCV:
		if (riscv_mem[0].phy_addr == 0) {
			pr_err("No riscv memory region available\n");
			return -ENODEV;
		}
		if (copy_to_user((void __user *)arg, &riscv_mem[0], sizeof(riscv_mem[0])))
			return -EFAULT;
		break;
	case MCS_KM_PED_BAREMETAL:
	default:
		if (baremetal_mem[0].phy_addr == 0) {
			pr_err("No baremetal memory region available\n");
			return -ENODEV;
		}
		if (copy_to_user((void __user *)arg, &baremetal_mem[0], sizeof(baremetal_mem[0])))
			return -EFAULT;
		break;
	}
	break;

	case IOC_GET_COPY_MSG_MEM:
		if (copy_mem_info.instance_id > RPROC_MEM_MAX) {
						pr_err("GET_COPY_MSG_MEM failed: The required instance_id max to %d, your instance_id:%d\n", RPROC_MEM_MAX, copy_mem_info.instance_id);
						return -EINVAL;
		}
		copy_mem_offset = copy_mem_info.instance_id * INSTANCE_SIZE + OPENAMP_SHM_SIZE - OPENAMP_SHM_COPY_SIZE * 3;  /* 使用2M。 1M 用来发送， 1M用来接收 尾部1M gap */
		switch (priv->ped_type) {
		case MCS_KM_PED_RISCV:
			copy_mem_info.phy_addr = riscv_mem[0].phy_addr + copy_mem_offset;
			pr_info("GET_COPY_MSG_MEM riscv_mem phy_addr: 0x%lx\n", copy_mem_info.phy_addr);
			if (copy_mem_info.phy_addr  > (riscv_mem[0].phy_addr + riscv_mem[0].size)) {
				pr_err("GET_COPY_MSG_MEM failed: The required memory is out of mcs reserved memory, instance_id:%d\n", copy_mem_info.instance_id);
				return -EINVAL;
			}
			break;
		case MCS_KM_PED_BAREMETAL:
		default:
			copy_mem_info.phy_addr = baremetal_mem[0].phy_addr + copy_mem_offset;
			pr_info("GET_COPY_MSG_MEM baremetal_mem phy_addr: 0x%lx\n", copy_mem_info.phy_addr);
			if (copy_mem_info.phy_addr  > (baremetal_mem[0].phy_addr + baremetal_mem[0].size)) {
				pr_err("GET_COPY_MSG_MEM failed: The required memory is out of mcs reserved memory, instance_id:%d\n", copy_mem_info.instance_id);
				return -EINVAL;
			}
			break;
		}
		copy_mem_info.size = OPENAMP_SHM_COPY_SIZE * 2;  /* 使用2M。1M 用来发送， 1M用来接收 */
		if (copy_to_user((void __user *)arg, &copy_mem_info, sizeof(copy_mem_info)))
			return -EFAULT;
		break;

	default:
		pr_err("IOC param invalid(0x%x)\n", cmd);
		return -EINVAL;
	}
	return 0;
}

static pgprot_t mcs_phys_mem_access_prot(struct file *file, unsigned long pfn,
					 unsigned long size, pgprot_t vma_prot)
{
	return __pgprot_modify(vma_prot, PTE_ATTRINDX_MASK, PTE_ATTRINDX(MT_NORMAL_NC) | PTE_PXN | PTE_UXN);
}

static const struct vm_operations_struct mmap_mem_ops = {
#ifdef CONFIG_HAVE_IOREMAP_PROT
	.access = generic_access_phys
#endif
};

/* A lite version of linux/drivers/char/mem.c, Test with MMU for arm64 mcs functions */
static int mcs_mmap(struct file *file, struct vm_area_struct *vma)
{
	int i;
	int found = 0;
	size_t size = vma->vm_end - vma->vm_start;
	phys_addr_t offset = (phys_addr_t)vma->vm_pgoff << PAGE_SHIFT;
	struct mcs_file_private_data *priv = file->private_data;

	/* Does it even fit in phys_addr_t? */
	if (offset >> PAGE_SHIFT != vma->vm_pgoff)
		return -EINVAL;

	/* It's illegal to wrap around the end of the physical address space. */
	if (offset + (phys_addr_t)size - 1 < offset)
		return -EINVAL;

	switch (priv->ped_type) {
	case MCS_KM_PED_RISCV:
		for (i = 0; (i < RPROC_MEM_MAX) && (riscv_mem[i].phy_addr != 0); i++) {
			if (offset >= riscv_mem[i].phy_addr && offset + size <= riscv_mem[i].phy_addr + riscv_mem[i].size) {
				found = 1;
				break;
			}
		}
		break;
	case MCS_KM_PED_BAREMETAL:
	default:
		for (i = 0; (i < RPROC_MEM_MAX) && (baremetal_mem[i].phy_addr != 0); i++) {
			if (offset >= baremetal_mem[i].phy_addr && offset + size <= baremetal_mem[i].phy_addr + baremetal_mem[i].size) {
				found = 1;
				break;
			}
		}
		break;
	}

	if (found == 0) {
		pr_err("mmap failed: mmap memory is not in mcs reserved memory for ped_type %d\n", priv->ped_type);
		return -EINVAL;
	}

	vma->vm_page_prot = mcs_phys_mem_access_prot(file, vma->vm_pgoff,
						 size,
						 vma->vm_page_prot);

	vma->vm_ops = &mmap_mem_ops;

	/* Remap-pfn-range will mark the range VM_IO */
	if (remap_pfn_range(vma,
			    vma->vm_start,
			    vma->vm_pgoff,
			    size,
			    vma->vm_page_prot) < 0)
		return -EAGAIN;

	return 0;
}

static int mcs_open(struct inode *inode, struct file *filp)
{
	struct mcs_file_private_data *file_priv;

	if (!capable(CAP_SYS_RAWIO))
		return -EPERM;

	file_priv = kmalloc(sizeof(struct mcs_file_private_data), GFP_KERNEL);
	if (!file_priv) {
		pr_err("Failed to allocate private data\n");
		return -ENOMEM;
	}

	/* default to baremetal, to be compatible with old pedestal */
	file_priv->ped_type = MCS_KM_PED_BAREMETAL;
	filp->private_data = file_priv;

	return 0;
}

static const struct file_operations mcs_fops = {
	.open = mcs_open,
	.mmap = mcs_mmap,
	.poll = mcs_poll,
	.unlocked_ioctl = mcs_ioctl,
	.compat_ioctl = compat_ptr_ioctl,
	.llseek = generic_file_llseek,
};

static int get_psci_method(void)
{
	const char *method;
	struct device_node *np;
	struct of_device_id psci_of_match[] = {
		{ .compatible = "arm,psci" },
		{ .compatible = "arm,psci-0.2" },
		{ .compatible = "arm,psci-1.0" },
		{},
	};

	if (!acpi_disabled) {
		/* For ACPI, only "smc" is supported */
		invoke_hvc = 0;
		return 0;
	}

	np = of_find_matching_node(NULL, psci_of_match);

	if (!np || !of_device_is_available(np))
		return -ENODEV;

	if (of_property_read_string(np, "method", &method)) {
		of_node_put(np);
		return -ENXIO;
	}

	of_node_put(np);

	if (!strcmp("hvc", method))
		invoke_hvc = 1;
	else if (!strcmp("smc", method))
		invoke_hvc = 0;
	else
		return -EINVAL;

	return 0;
}

static int init_ped_rsv_mem(const char *compatible_node, struct mcs_rproc_mem *mem,
						const char *mem_region_name, bool support_cmdline)
{
	int n = 0;
	int i, count, ret;
	struct device_node *np;

	if (support_cmdline && rmem_base != 0 && rmem_size != 0) {
		pr_info("assign memory region for %s mcs: 0x%lx - 0x%lx (%ld MB)\n",
				mem_region_name, rmem_base, rmem_base + rmem_size - 1, rmem_size >> 20);

		mem[0].phy_addr = rmem_base;
		mem[0].size = rmem_size;
		return 0;
	}

	np = of_find_compatible_node(NULL, NULL, compatible_node);
	if (np == NULL) {
		pr_info("%s node not found in DTS\n", compatible_node);
		return -ENODEV;
	}

	count = of_count_phandle_with_args(np, "memory-region", NULL);
	if (count <= 0) {
		pr_info("reserved mem is required for %s\n", mem_region_name);
		ret = -ENODEV;
		goto out;
	}

	for (i = 0; i < count; i++) {
		struct device_node *node;
		struct resource res;

		node = of_parse_phandle(np, "memory-region", i);
		ret = of_address_to_resource(node, 0, &res);
		if (ret) {
			pr_err("unable to resolve %s memory region\n", mem_region_name);
			goto out;
		}

		if (n >= RPROC_MEM_MAX)
			break;

		if (!request_mem_region(res.start, resource_size(&res), mem_region_name)) {
			pr_err("Can not request %s, 0x%llx-0x%llx\n", mem_region_name, res.start, res.end);
			ret = -EINVAL;
			goto out;
		}

		mem[n].phy_addr = res.start;
		mem[n].size = resource_size(&res);
		n++;
	}

out:
	of_node_put(np);
	return ret;
}

static int init_baremetal_rsv_mem(void)
{
	return init_ped_rsv_mem("oe,mcs_remoteproc", baremetal_mem, "mcs_baremetal_mem", true);
}

static int init_riscv_rsv_mem(void)
{
	return init_ped_rsv_mem("oe,mcs_riscv_remoteproc", riscv_mem, "mcs_riscv_mem", false);
}

static int init_reserved_mem(void)
{
	int baremetal_ret, riscv_ret;
	bool has_baremetal = false;
	bool has_riscv = false;

	baremetal_ret = init_baremetal_rsv_mem();
	if (baremetal_ret == 0) {
		has_baremetal = true;
	}

	riscv_ret = init_riscv_rsv_mem();
	if (riscv_ret == 0) {
		has_riscv = true;
	}

	/* at least one mem region has to exist */
	if (!has_baremetal && !has_riscv) {
		pr_err("At least one memory region type (baremetal or riscv) must exist\n");
		return -ENODEV;
	}

	return 0;
}

static void release_reserved_mem(void)
{
	int i;

	/*
	 * When configuring the rmem, memory resources are requested
	 * using memmap, so there is no need to free it.
	 */
	if (!(rmem_base != 0 && rmem_size != 0)) {
		for (i = 0; (i < RPROC_MEM_MAX) && (baremetal_mem[i].phy_addr != 0); i++) {
			release_mem_region(baremetal_mem[i].phy_addr, baremetal_mem[i].size);
			baremetal_mem[i].phy_addr = 0;
			baremetal_mem[i].size = 0;
		}
	}

	for (i = 0; (i < RPROC_MEM_MAX) && (riscv_mem[i].phy_addr != 0); i++) {
		release_mem_region(riscv_mem[i].phy_addr, riscv_mem[i].size);
		riscv_mem[i].phy_addr = 0;
		riscv_mem[i].size = 0;
	}
}

static int register_mcs_dev(void)
{
	int ret;
	struct device *mcs_dev;

	mcs_major = register_chrdev(0, MCS_DEVICE_NAME, &mcs_fops);
	if (mcs_major < 0) {
		ret = mcs_major;
		pr_err("register_chrdev failed (%d)\n", ret);
		goto err;
	}

#if LINUX_VERSION_CODE >= KERNEL_VERSION(6, 6, 0)
	mcs_class = class_create(MCS_DEVICE_NAME);
#else
	mcs_class = class_create(THIS_MODULE, MCS_DEVICE_NAME);
#endif
	if (IS_ERR(mcs_class)) {
		ret = PTR_ERR(mcs_class);
		pr_err("class_create failed (%d)\n", ret);
		goto err_class;
	}

	mcs_dev = device_create(mcs_class, NULL, MKDEV(mcs_major, 0),
				NULL, MCS_DEVICE_NAME);
	if (IS_ERR(mcs_dev)) {
		ret = PTR_ERR(mcs_dev);
		pr_err("device_create failed (%d)\n", ret);
		goto err_device;
	}
	return 0;

err_device:
	class_destroy(mcs_class);
err_class:
	unregister_chrdev(mcs_major, MCS_DEVICE_NAME);
err:
	return ret;
}

static void unregister_mcs_dev(void)
{
	device_destroy(mcs_class, MKDEV(mcs_major, 0));
	class_destroy(mcs_class);
	unregister_chrdev(mcs_major, MCS_DEVICE_NAME);
}

static int __init mcs_dev_init(void)
{
	int ret;

	ret = get_psci_method();
	if (ret) {
		pr_err("Failed to get psci \"method\" property, ret = %d\n", ret);
		return ret;
	}

#ifdef CONFIG_KPROBES
	ret = get_cpu_logical_map();
	if (ret) {
		pr_err("Failed to get cpu_logical_map symbol, ret = %d\n", ret);
		return ret;
	}
#endif

	ret = init_reserved_mem();
	if (ret) {
		pr_err("Failed to get mcs mem, ret = %d\n", ret);
		return ret;
	}

	ret = init_mcs_ipi();
	if (ret) {
		pr_err("Failed to init mcs ipi, ret = %d\n", ret);
		goto err_free_mcs_mem;
	}

	ret = register_mcs_dev();
	if (ret) {
		pr_err("Failed to register mcs dev, ret = %d\n", ret);
		goto err_remove_ipi;
	}

	return ret;
err_remove_ipi:
	remove_mcs_ipi();
err_free_mcs_mem:
	release_reserved_mem();
	return ret;
}
module_init(mcs_dev_init);

static void __exit mcs_dev_exit(void)
{
	remove_mcs_ipi();
	unregister_mcs_dev();
	release_reserved_mem();
	pr_info("remove mcs dev\n");
}
module_exit(mcs_dev_exit);

MODULE_AUTHOR("openEuler Embedded");
MODULE_DESCRIPTION("mcs device");
MODULE_LICENSE("Dual BSD/GPL");
