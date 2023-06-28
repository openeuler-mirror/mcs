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
#define CPU_ON_FUNCID	   	0xC4000003
#define AFFINITY_INFO_FUNCID	0xC4000004

#define MAGIC_NUMBER		'A'
#define IOC_SENDIPI		_IOW(MAGIC_NUMBER, 0, int)
#define IOC_CPUON		_IOW(MAGIC_NUMBER, 1, int)
#define IOC_AFFINITY_INFO	_IOW(MAGIC_NUMBER, 2, int)
#define IOC_MAXNR		2
#define IPI_MCS			8

static struct class *mcs_class;
static int mcs_major;

static int __percpu *mcs_evt;

static phys_addr_t valid_start;
static phys_addr_t valid_end;
static int invoke_hvc = 1;

static DECLARE_WAIT_QUEUE_HEAD(mcs_wait_queue);
static atomic_t irq_ack;

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
	unsigned int cpu_id;
	unsigned long cpu_boot_addr, ret;

	if (_IOC_TYPE(cmd) != MAGIC_NUMBER)
		return -EINVAL;
	if (_IOC_NR(cmd) > IOC_MAXNR)
		return -EINVAL;
	if (copy_from_user(&cpu_id, (unsigned int __user *)arg, sizeof(unsigned int)))
		return -EFAULT;

	switch (cmd) {
		case IOC_SENDIPI:
			pr_info("received ioctl cmd to send ipi to cpu(%d)\n", cpu_id);
			send_clientos_ipi(cpumask_of(cpu_id));
			break;

		case IOC_CPUON:
			if (copy_from_user(&cpu_boot_addr, (unsigned long __user *)arg + 1, sizeof(unsigned long)))
				return -EFAULT;

			pr_info("start booting clientos on cpu(%d) addr(0x%lx)\n", cpu_id, cpu_boot_addr);

			ret = invoke_psci_fn(CPU_ON_FUNCID, cpu_id, cpu_boot_addr, 0);
			if (ret) {
				pr_err("boot clientos failed(%ld)\n", ret);
				return -EINVAL;
			}
			break;

		case IOC_AFFINITY_INFO:
			ret = invoke_psci_fn(AFFINITY_INFO_FUNCID, cpu_id, 0, 0);
			if (copy_to_user((unsigned long __user *)arg, &ret, sizeof(unsigned long)))
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
	size_t size = vma->vm_end - vma->vm_start;
	phys_addr_t offset = (phys_addr_t)vma->vm_pgoff << PAGE_SHIFT;

	/* Does it even fit in phys_addr_t? */
	if (offset >> PAGE_SHIFT != vma->vm_pgoff)
		return -EINVAL;

	/* It's illegal to wrap around the end of the physical address space. */
	if (offset + (phys_addr_t)size - 1 < offset)
		return -EINVAL;

	if (offset < valid_start || (offset + size) > valid_end)
		return -EINVAL;

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

static int get_mcs_reserved_mem(void)
{
	int len, naddr, nsize;
	struct device_node *nd;
	const __be32 *prop;

	nd = of_find_compatible_node(NULL, NULL, "mcs_mem");
	if (nd == NULL)
		return -ENODEV;

	naddr = of_n_addr_cells(nd);
	nsize = of_n_size_cells(nd);

	prop = of_get_property(nd, "reg", &len);
	of_node_put(nd);
	if (!prop)
		return -ENOENT;

	if (len && len != ((naddr + nsize) * sizeof(__be32)))
		return -EINVAL;

	valid_start = of_read_number(prop, naddr);
	valid_end = valid_start + of_read_number(prop + naddr, nsize);

	if (!request_mem_region(valid_start, valid_end - valid_start, "mcs_mem")) {
		pr_err("Can not request mcs_mem\n");
		return -EINVAL;
	}
	return 0;
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

		ret = get_mcs_reserved_mem();
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
		goto err_free_mcs_mem;
	}

	return ret;
err_free_mcs_mem:
	release_mem_region(valid_start, valid_end - valid_start);
	return ret;
}
module_init(mcs_dev_init);

static void __exit mcs_dev_exit(void)
{
	remove_mcs_ipi();
	unregister_mcs_dev();
	release_mem_region(valid_start, valid_end - valid_start);
	pr_info("remove mcs dev\n");
}
module_exit(mcs_dev_exit);

MODULE_AUTHOR("OpenEuler Embedded");
MODULE_DESCRIPTION("mcs device");
MODULE_LICENSE("Dual BSD/GPL");
