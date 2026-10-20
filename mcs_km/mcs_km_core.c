// SPDX-License-Identifier: GPL-2.0
/*
 * MCS kernel module core.
 *
 * Owns the /dev/mcs character device and the ioctl ABI shared by all
 * pedestals.  Exactly one pedestal backend is linked into mcs_km.ko and
 * selected at build time; all platform specific behaviour is dispatched
 * through struct mcs_pedestal_ops.
 */

#define pr_fmt(fmt) "mcs: " fmt

#include <linux/capability.h>
#include <linux/device.h>
#include <linux/fs.h>
#include <linux/kernel.h>
#include <linux/mm.h>
#include <linux/module.h>
#include <linux/poll.h>
#include <linux/slab.h>
#include <linux/uaccess.h>
#include <linux/version.h>

#include "mcs_km_pedestal.h"

static struct class *mcs_class;
static int mcs_major;

/* struct mcs_file_private_data - private data for mcs device file */
struct mcs_file_private_data {
	enum mcs_km_pedestal_type ped_type;
};

static const struct mcs_pedestal_ops *mcs_ops;

static pgprot_t mcs_phys_mem_access_prot(struct file *file, unsigned long pfn,
					 unsigned long size, pgprot_t vma_prot)
{
#ifdef CONFIG_ARM64
	return __pgprot_modify(vma_prot, PTE_ATTRINDX_MASK,
			       PTE_ATTRINDX(MT_NORMAL_NC) | PTE_PXN | PTE_UXN);
#else
	return vma_prot;
#endif
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

	if (!mcs_ops->mmap_allowed(offset, size)) {
		pr_err("mmap failed: memory is not in mcs reserved memory for pedestal %s\n",
		       mcs_ops->name);
		return -EINVAL;
	}

	vma->vm_page_prot = mcs_phys_mem_access_prot(file, vma->vm_pgoff,
						      size, vma->vm_page_prot);

	vma->vm_ops = &mmap_mem_ops;

	/* Remap-pfn-range will mark the range VM_IO */
	if (remap_pfn_range(vma, vma->vm_start, vma->vm_pgoff, size,
			    vma->vm_page_prot) < 0)
		return -EAGAIN;

	return 0;
}

static __poll_t mcs_poll(struct file *file, poll_table *wait)
{
	if (!mcs_ops->poll)
		return 0;

	return mcs_ops->poll(file, wait);
}

static long mcs_ioctl(struct file *f, unsigned int cmd, unsigned long arg)
{
	struct mcs_file_private_data *priv = f->private_data;
	struct core_msg_mem_info copy_mem_info;
	struct mcs_rproc_mem mem;
	struct cpu_info info;
	int ped_type;
	int ret = 0;

	if (_IOC_TYPE(cmd) != MAGIC_NUMBER)
		return -EINVAL;
	if (_IOC_NR(cmd) > IOC_MAXNR)
		return -EINVAL;

	switch (cmd) {
	case IOC_SET_PED_TYPE:
		ret = copy_from_user(&ped_type, (int __user *)arg,
				     sizeof(ped_type));
		break;
	case IOC_GET_COPY_MSG_MEM:
		ret = copy_from_user(&copy_mem_info,
				     (struct core_msg_mem_info __user *)arg,
				     sizeof(copy_mem_info));
		break;
	case IOC_QUERY_MEM:
		break;
	default:
		ret = copy_from_user(&info, (struct cpu_info __user *)arg,
				     sizeof(info));
		break;
	}
	if (ret)
		return -EFAULT;

	switch (cmd) {
	case IOC_SET_PED_TYPE:
		if (ped_type >= MCS_KM_PED_INVALID ||
		    ped_type < MCS_KM_PED_AARCH64) {
			pr_err("invalid pedestal type %d\n", ped_type);
			return -EINVAL;
		}
		if (ped_type != mcs_ops->type) {
			pr_err("pedestal type %d does not match compiled backend %s(%d)\n",
			       ped_type, mcs_ops->name, mcs_ops->type);
			return -EOPNOTSUPP;
		}
		priv->ped_type = ped_type;
		break;

	case IOC_SENDIPI:
		if (!mcs_ops->send_ipi)
			return -EOPNOTSUPP;
		return mcs_ops->send_ipi(&info);

	case IOC_XPUON:
		if (!mcs_ops->xpu_on)
			return -EOPNOTSUPP;
		return mcs_ops->xpu_on(&info);

	case IOC_AFFINITY_INFO:
		if (!mcs_ops->affinity_info)
			return -EOPNOTSUPP;
		return mcs_ops->affinity_info(&info);

	case IOC_MCUOFF:
		if (!mcs_ops->xpu_off)
			return -EOPNOTSUPP;
		return mcs_ops->xpu_off();

	case IOC_QUERY_MEM:
		ret = mcs_ops->query_mem(&mem);
		if (ret)
			return ret;
		if (copy_to_user((void __user *)arg, &mem, sizeof(mem)))
			return -EFAULT;
		break;

	case IOC_GET_COPY_MSG_MEM:
		if (!mcs_ops->copy_msg_mem)
			return -EOPNOTSUPP;
		ret = mcs_ops->copy_msg_mem(&copy_mem_info);
		if (ret)
			return ret;
		if (copy_to_user((void __user *)arg, &copy_mem_info,
				 sizeof(copy_mem_info)))
			return -EFAULT;
		break;

	default:
		pr_err("IOC param invalid(0x%x)\n", cmd);
		return -EINVAL;
	}

	return 0;
}

static int mcs_open(struct inode *inode, struct file *filp)
{
	struct mcs_file_private_data *file_priv;

	if (!capable(CAP_SYS_RAWIO))
		return -EPERM;

	if (!mcs_ops)
		return -ENODEV;

	file_priv = kmalloc(sizeof(struct mcs_file_private_data), GFP_KERNEL);
	if (!file_priv) {
		pr_err("Failed to allocate private data\n");
		return -ENOMEM;
	}

	file_priv->ped_type = mcs_ops->type;
	filp->private_data = file_priv;

	return 0;
}

static int mcs_release(struct inode *inode, struct file *filp)
{
	kfree(filp->private_data);
	filp->private_data = NULL;
	return 0;
}

static const struct file_operations mcs_fops = {
	.open = mcs_open,
	.release = mcs_release,
	.mmap = mcs_mmap,
	.poll = mcs_poll,
	.unlocked_ioctl = mcs_ioctl,
	.compat_ioctl = compat_ptr_ioctl,
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

#if defined(MCS_PEDESTAL_AARCH64)
#define MCS_PEDESTAL_OPS (&mcs_aarch64_ops)
#elif defined(MCS_PEDESTAL_HISI_RISCV)
#define MCS_PEDESTAL_OPS (&mcs_hisi_riscv_ops)
#elif defined(MCS_PEDESTAL_STM32MP2)
#define MCS_PEDESTAL_OPS (&mcs_stm32mp2_ops)
#else
#error "No MCS pedestal backend selected"
#endif

static int __init mcs_dev_init(void)
{
	int ret;

	mcs_ops = MCS_PEDESTAL_OPS;

	ret = mcs_ops->init();
	if (ret) {
		pr_err("Failed to init %s pedestal, ret = %d\n",
		       mcs_ops->name, ret);
		return ret;
	}

	ret = register_mcs_dev();
	if (ret) {
		pr_err("Failed to register mcs dev, ret = %d\n", ret);
		mcs_ops->exit();
		return ret;
	}

	pr_info("MCS %s pedestal initialized\n", mcs_ops->name);
	return 0;
}
module_init(mcs_dev_init);

static void __exit mcs_dev_exit(void)
{
	unregister_mcs_dev();
	mcs_ops->exit();
	pr_info("remove mcs dev\n");
}
module_exit(mcs_dev_exit);

MODULE_AUTHOR("openEuler Embedded");
MODULE_DESCRIPTION("mcs device");
MODULE_LICENSE("Dual BSD/GPL");
