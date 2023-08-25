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

#define MCS_DEVICE_NAME		"mcs"

/**
 * PSCI Functions
 * For more detail see:
 * Arm Power State Coordination Interface Platform Design Document
 */
#define CPU_ON_FUNCID	   	0xC4000003
#define AFFINITY_INFO_FUNCID	0xC4000004

#define MAGIC_NUMBER		'A'
#define IOC_SENDIPI		_IOW(MAGIC_NUMBER, 0, int)
#define IOC_CPUON		_IOW(MAGIC_NUMBER, 1, int)
#define IOC_AFFINITY_INFO	_IOW(MAGIC_NUMBER, 2, int)
#define IOC_MAXNR		2
#define IPI_MCS			8
#define RPROC_MEM_MAX		4

static struct class *mcs_class;
static int mcs_major;

static int __percpu *mcs_evt;

struct cpu_info {
	u32 cpu;
	u64 boot_addr;
};

static int invoke_hvc = 1;
/**
 * struct mcs_rproc_mem - internal memory structure
 * @phy_addr: physical address of the memory region
 * @size: total size of the memory region
 */
struct mcs_rproc_mem {
	phys_addr_t phy_addr;
	size_t size;
};
static struct mcs_rproc_mem mem[RPROC_MEM_MAX];

static DECLARE_WAIT_QUEUE_HEAD(mcs_wait_queue);
static atomic_t irq_ack;

/**
 * make hvc/smc call
 * @return:
 *    CPU_ON_FUNCID:
 * 	SUCCESS			0
 * 	INVALID_PARAMETERS 	-2
 * 	DENIED			-3
 * 	ALREADY_ON		-4
 * 	ON_PENDING		-5
 * 	INTERNAL_FAILURE	-6
 * 	INVALID_ADDRESS		-9
 *
 *    AFFINITY_INFO_FUNCID:
 * 	ON			0
 * 	OFF			1
 * 	ON_PENDING		2
 * 	INVALID_PARAMETERS 	-2
 * 	DISABLED		-8
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

static irqreturn_t handle_clientos_ipi(int irq, void *data)
{
	pr_info("received ipi from client os\n");
	atomic_set(&irq_ack, 1);
	wake_up_interruptible(&mcs_wait_queue);
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
	on_each_cpu(disable_mcs_ipi, NULL, 1);
	free_percpu_irq(IPI_MCS, mcs_evt);
	free_percpu(mcs_evt);
}

static int init_mcs_ipi(void)
{
	int err;
	struct irq_desc *desc;

	desc = irq_to_desc(IPI_MCS);
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

static void send_clientos_ipi(const struct cpumask *target)
{
	ipi_send_mask(IPI_MCS, target);
}

static unsigned int mcs_poll(struct file *file, poll_table *wait)
{
	unsigned int mask = 0;

	poll_wait(file, &mcs_wait_queue, wait);
	if (atomic_cmpxchg(&irq_ack, 1, 0) == 1)
		mask |= POLLIN | POLLRDNORM;

	return mask;
}

static long mcs_ioctl(struct file *f, unsigned int cmd, unsigned long arg)
{
	int ret;
	struct cpu_info info;

	if (_IOC_TYPE(cmd) != MAGIC_NUMBER)
		return -EINVAL;
	if (_IOC_NR(cmd) > IOC_MAXNR)
		return -EINVAL;
	if (copy_from_user(&info, (struct cpu_info __user *)arg, sizeof(info)))
		return -EFAULT;

	switch (cmd) {
		case IOC_SENDIPI:
			pr_info("received ioctl cmd to send ipi to cpu(%d)\n", info.cpu);
			send_clientos_ipi(cpumask_of(info.cpu));
			break;

		case IOC_CPUON:
			pr_info("start booting clientos on cpu(%d) addr(0x%llu)\n", info.cpu, info.boot_addr);
			ret = invoke_psci_fn(CPU_ON_FUNCID, info.cpu, info.boot_addr, 0);
			if (ret) {
				pr_err("boot clientos failed(%d)\n", ret);
				return -EINVAL;
			}
			break;

		case IOC_AFFINITY_INFO:
			ret = invoke_psci_fn(AFFINITY_INFO_FUNCID, info.cpu, 0, 0);
			if (ret != 1) {
				pr_err("cpu state check failed! cpu(%d) is not in the OFF state, current state: %d\n",
				       info.cpu, ret);
				return -EFAULT;
			}
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

	/* Does it even fit in phys_addr_t? */
	if (offset >> PAGE_SHIFT != vma->vm_pgoff)
		return -EINVAL;

	/* It's illegal to wrap around the end of the physical address space. */
	if (offset + (phys_addr_t)size - 1 < offset)
		return -EINVAL;

	for (i = 0; (i < RPROC_MEM_MAX) && (mem[i].phy_addr != 0); i++) {
		if (offset >= mem[i].phy_addr && size <= mem[i].size) {
			found = 1;
			break;
		}
	}

	if (found == 0) {
		pr_err("mmap failed: mmap memory is not in mcs reserved memory\n");
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
	if (!capable(CAP_SYS_RAWIO))
		return -EPERM;
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

static int init_reserved_mem(void)
{
	int n = 0;
	int i, count, ret;
	struct device_node *np;

	np = of_find_compatible_node(NULL, NULL, "oe,mcs_remoteproc");
	if (np == NULL)
		return -ENODEV;

	count = of_count_phandle_with_args(np, "memory-region", NULL);
	if (count <= 0) {
		pr_err("reserved mem is required for MCS\n");
		return -ENODEV;
	}

	for (i = 0; i < count; i++) {
		struct device_node *node;
		struct resource res;

		node = of_parse_phandle(np, "memory-region", i);
		ret = of_address_to_resource(node, 0, &res);
		if (ret) {
			pr_err("unable to resolve memory region\n");
			return ret;
		}

		if (n >= RPROC_MEM_MAX)
			break;

		if (!request_mem_region(res.start, resource_size(&res), "mcs_mem")) {
			pr_err("Can not request mcs_mem, 0x%llx-0x%llx\n", res.start, res.end);
			return -EINVAL;
		}

		mem[n].phy_addr = res.start;
		mem[n].size = resource_size(&res);
		n++;
	}

	return 0;
}

static void release_reserved_mem(void)
{
	int i;

	for (i = 0; (i < RPROC_MEM_MAX) && (mem[i].phy_addr != 0); i++) {
		release_mem_region(mem[i].phy_addr, mem[i].size);
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

	mcs_class = class_create(THIS_MODULE, MCS_DEVICE_NAME);
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

	if (acpi_disabled) {
		ret = get_psci_method();
		if (ret) {
			pr_err("Failed to get psci \"method\" property, ret = %d\n", ret);
			return ret;
		}

		ret = init_reserved_mem();
		if (ret) {
			pr_err("Failed to get mcs mem, ret = %d\n", ret);
			return ret;
		}
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

MODULE_AUTHOR("OpenEuler Embedded");
MODULE_DESCRIPTION("mcs device");
MODULE_LICENSE("Dual BSD/GPL");
