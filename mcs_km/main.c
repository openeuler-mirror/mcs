/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: GPL-2.0
 */

#define pr_fmt(fmt) "mcs: " fmt

#include <linux/acpi.h>
#include <linux/device.h>
#include <linux/file.h>
#include <linux/namei.h>
#include <linux/wait.h>
#include <linux/poll.h>
#include <linux/module.h>
#include <linux/moduleparam.h>
#include <linux/delay.h>
#include <linux/interrupt.h>
#include <linux/cpumask.h>
#include <linux/uaccess.h>
#include <linux/mm.h>

#include <asm/apic.h>

#include "include/mmu_map.h"

#define MCS_DEVICE_NAME		"mcs"

#define MAGIC_NUMBER		'A'
#define IOC_SENDIPI		_IOW(MAGIC_NUMBER, 0, int)
#define IOC_CPUON		_IOW(MAGIC_NUMBER, 1, int)
#define IOC_AFFINITY_INFO	_IOW(MAGIC_NUMBER, 2, int)
#define IOC_LOAD_BOOT		_IOW(MAGIC_NUMBER, 3, int)
#define IOC_MAXNR		3

#define BOOT_BIN_ADDR		0x0

static struct class *mcs_class;
static int mcs_major;

static unsigned long rmem_base;
module_param(rmem_base, ulong, S_IRUSR);
MODULE_PARM_DESC(rmem_base, "The base address of the reserved mem");

static unsigned long rmem_size;
module_param(rmem_size, ulong, S_IRUSR);
MODULE_PARM_DESC(rmem_size, "The size of the reserved mem");

static DECLARE_WAIT_QUEUE_HEAD(mcs_wait_queue);
static atomic_t irq_ack;

static int wakeup_cpu_via_init(unsigned int cpu_id, unsigned long start_eip)
{
	int i, maxlvt;
	unsigned long send_status, accept_status;
	int apicid = apic->cpu_present_to_apicid(cpu_id);

	maxlvt = GET_APIC_VERSION(apic_read(APIC_LVR));
	maxlvt = APIC_INTEGRATED(maxlvt);

	if (APIC_INTEGRATED(boot_cpu_apic_version)) {
		if (maxlvt > 3)
			apic_write(APIC_ESR, 0);
		apic_read(APIC_ESR);
	}

	/* Turn INIT on target chip */
	apic_icr_write(APIC_INT_LEVELTRIG | APIC_INT_ASSERT | APIC_DM_INIT, apicid);

	pr_info("Waiting for send to finish...\n");
	send_status = safe_apic_wait_icr_idle();
	pr_info("Deasserting INIT\n");

	/* Target chip */
	/* Send IPI */
	apic_icr_write(APIC_INT_LEVELTRIG | APIC_DM_INIT, apicid);

	pr_info("Waiting for send to finish...\n");
	send_status = safe_apic_wait_icr_idle();

	mb();

	/* Send STARTUP IPIs */
	for (i = 1; i <= 2; i++) {
		pr_info("Sending STARTUP #%d\n", i);
		if (maxlvt > 3)		/* Due to the Pentium erratum 3AP.  */
			apic_write(APIC_ESR, 0);
		apic_read(APIC_ESR);
		pr_info("After apic_write\n");

		apic_icr_write(APIC_DM_STARTUP | (start_eip >> 12), apicid);

		udelay(10);
		pr_info("Startup point 1\n");

		pr_info("Waiting for send to finish...\n");
		send_status = safe_apic_wait_icr_idle();

		udelay(10);

		if (maxlvt > 3)
			apic_write(APIC_ESR, 0);
		accept_status = (apic_read(APIC_ESR) & 0xEF);
		if (send_status || accept_status)
			break;
	}

	pr_info("After Startup\n");
	if (send_status)
		pr_err("APIC never delivered???\n");
	if (accept_status)
		pr_err("APIC delivery error (%lx)\n", accept_status);

	return (send_status | accept_status);
}

static void handle_clientos_ipi(void)
{
	pr_info("received ipi from client os\n");
	atomic_set(&irq_ack, 1);
	wake_up_interruptible(&mcs_wait_queue);
}

static void remove_mcs_ipi(void)
{
	set_mcs_ipi_handler(NULL);
}

static int init_mcs_ipi(void)
{
	int err = 0;

	err = set_mcs_ipi_handler(handle_clientos_ipi);

	return err;
}

/*
 * send X86_MCS_IPI
 * Destination Mode: Physical
 */
static void send_clientos_ipi(const unsigned int cpu)
{
	int apicid = apic->cpu_present_to_apicid(cpu);

	weak_wrmsr_fence();
	wrmsrl(APIC_BASE_MSR + (APIC_ICR >> 4), ((__u64) apicid) << 32 | X86_MCS_IPI_VECTOR);
}

static unsigned int mcs_poll(struct file *file, poll_table *wait)
{
	unsigned int mask = 0;

	poll_wait(file, &mcs_wait_queue, wait);
	if (atomic_cmpxchg(&irq_ack, 1, 0) == 1)
		mask |= POLLIN | POLLRDNORM;

	return mask;
}

static int load_boot_bin(const char *file_path, const phys_addr_t load_addr)
{
	int ret;
	void __iomem *base;
	void *buf;
	struct path path;
	struct file *fp;
	struct kstat stat;
	size_t size;
	loff_t pos = 0;

	ret = kern_path(file_path, LOOKUP_FOLLOW, &path);
	if (ret)
		goto err;

	ret = vfs_getattr(&path, &stat, STATX_BASIC_STATS, AT_STATX_SYNC_AS_STAT);
	path_put(&path);
	if (ret)
		goto err;

	fp = filp_open(file_path, O_RDONLY, 0);
	if (IS_ERR(fp)) {
		ret = PTR_ERR(fp);
		pr_err("open %s failed\n", file_path);
		goto err;
	}

	size = stat.size;
	buf = kmalloc(size, GFP_KERNEL);
	if (buf == NULL) {
		ret = -ENOMEM;
		pr_err("failed to allocate buffer\n");
		goto err_fclose;
	}

	base = ioremap(load_addr, size);
	if (!base) {
		ret = -ENXIO;
		pr_err("ioremap boot addr(0x%llx) size(0x%lx) failed\n", load_addr, size);
		goto err_free;
	}

	ret = kernel_read(fp, buf, size, &pos);
	if (ret != size) {
		pr_err("failed to write %d bytes to boot area, ret %d\n", (int)size, ret);
	} else {
		pr_info("load boot bin done\n");
		ret = 0;
		memcpy(base, buf, size);
	}

	iounmap(base);
err_free:
	kfree(buf);
err_fclose:
	filp_close(fp, NULL);
err:
	return ret;
}

static long mcs_ioctl(struct file *f, unsigned int cmd, unsigned long arg)
{
	unsigned int cpu_id;
	unsigned long cpu_boot_addr, ret;
	char boot_bin_path[256];

	if (_IOC_TYPE(cmd) != MAGIC_NUMBER)
		return -EINVAL;
	if (_IOC_NR(cmd) > IOC_MAXNR)
		return -EINVAL;
	if (copy_from_user(&cpu_id, (unsigned int __user *)arg, sizeof(unsigned int)))
		return -EFAULT;

	switch (cmd) {
		case IOC_SENDIPI:
			pr_info("received ioctl cmd to send ipi to cpu(%d)\n", cpu_id);
			send_clientos_ipi(cpu_id);
			break;

		case IOC_CPUON:
			if (copy_from_user(&cpu_boot_addr, (unsigned long __user *)arg + 1, sizeof(unsigned long)))
				return -EFAULT;

			mem_map_info_set(cpu_boot_addr);
			pr_info("start booting clientos on cpu(%d) addr(0x%lx)\n", cpu_id, cpu_boot_addr);

			ret = wakeup_cpu_via_init(cpu_id, BOOT_BIN_ADDR);
			if (ret) {
				pr_err("boot clientos failed(%ld)\n", ret);
				return -EINVAL;
			}
			break;

		case IOC_AFFINITY_INFO:
			/* for x86, just return CPU_STATE_OFF(1) */
			ret = 1;
			if (copy_to_user((unsigned long __user *)arg, &ret, sizeof(unsigned long)))
				return -EFAULT;
			break;

		case IOC_LOAD_BOOT:
			if (copy_from_user(boot_bin_path, (char __user *)arg, 256))
				return -EFAULT;

			pr_info("start loading boot bin: %s\n", boot_bin_path);

			ret = load_boot_bin(boot_bin_path, BOOT_BIN_ADDR);
			if (ret) {
				pr_err("load boot bin failed(%ld)\n", ret);
				return -EFAULT;
			}
			break;

		default:
			pr_err("IOC param invalid(0x%x)\n", cmd);
			return -EINVAL;
	}
	return 0;
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

	pr_info("mcs_mmap:%llx %lx %lx\n", offset, size, vma->vm_pgoff);
	/* Does it even fit in phys_addr_t? */
	if (offset >> PAGE_SHIFT != vma->vm_pgoff)
		return -EINVAL;

	/* It's illegal to wrap around the end of the physical address space. */
	if (offset + (phys_addr_t)size - 1 < offset)
		return -EINVAL;

	if (offset < rmem_base || size > rmem_size)
		return -EINVAL;

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

	if (rmem_base == 0 || rmem_size == 0) {
		pr_err("you must supply rmem_base and rmem_size parameters!\n");
		return -ENXIO;
	}

	if (!request_mem_region(rmem_base, rmem_size, "mcs_mem")) {
		pr_err("Can not request mcs_mem. Did you reserve the memory with "
		       "\"memmap=\" or \"mem=\"?\n");
		return -EINVAL;
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
	release_mem_region(rmem_base, rmem_size);
	return ret;
}
module_init(mcs_dev_init);

static void __exit mcs_dev_exit(void)
{
	remove_mcs_ipi();
	unregister_mcs_dev();
	release_mem_region(rmem_base, rmem_size);
	pr_info("remove mcs dev\n");
}
module_exit(mcs_dev_exit);

MODULE_AUTHOR("OpenEuler Embedded");
MODULE_DESCRIPTION("mcs device");
MODULE_LICENSE("Dual BSD/GPL");

