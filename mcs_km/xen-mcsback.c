/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2023. All rights reserved
 *
 * SPDX-License-Identifier: GPL-2.0
 */

#define pr_fmt(fmt) "mcs-dom0: " fmt

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
#include <linux/delay.h>
#include <linux/of.h>
#include <linux/of_address.h>
#include <linux/of_irq.h>
#include <linux/version.h>
#include <linux/kprobes.h>
#include <asm-generic/memory_model.h>
#include <xen/grant_table.h>
#include <xen/events.h>
#include <xen/arm/page.h>
#include <xen/xenbus.h>

#define MCS_DEVICE_NAME		"mcs_xen"
#define SHMEM_ORDER			5
#define SHMEM_NPAGES		(1 << SHMEM_ORDER)
#define INVALID_GRANT_REF	0
#define INVALID_EVTCHN		-1

#define XENSTORE_KEY_GREF_NUM	"gref_num"
#define XENSTORE_KEY_GREF_PREFIX	"gref_"
#define XENSTORE_KEY_EVTCHN	"evtchn_port"

#define MAGIC_NUMBER		'M'
#define IOC_SET_DOMID		_IOW(MAGIC_NUMBER, 0, int)
#define IOC_QUERY_MEM		_IOW(MAGIC_NUMBER, 1, int)
#define IOC_INVOKE_EVTCHN	_IOW(MAGIC_NUMBER, 2, int)
#define IOC_MAXNR			2

static struct class *mcs_xen_class;
static int mcs_xen_major;

#define XENBUS_REGION_SIZE	0x1000

/* struct mcs_backend_info - private info for xenbus driver */
struct mcs_backend_info {
	struct list_head list;				/* For multiple instances */
	struct xenbus_device *xdev;			/* Associated Xenbus device */
	uint32_t grant_refs[SHMEM_NPAGES];	/* Grant reference for shared memory */
	evtchn_port_t evtchn;				/* Event channel port number */
	int evtchn_irq;						/* Event channel IRQ */
	void *shmem_virt;					/* Kernel virtual address */
	phys_addr_t shmem_phys;				/* Physical address */
	size_t shmem_size;					/* Memory region size */
	unsigned int domuid;				/* Placeholder for domU ID (from external interface) */
	wait_queue_head_t mcs_evtchn_wait;	/* Wait queue for event channel */
	atomic_t irq_triggered;				/* Atomic flag to indicate IRQ triggered */
};

/* struct mcs_file_private_data - private data for mcs device file */
struct mcs_file_private_data {
	uint32_t domid;
	struct mcs_backend_info *backend_info;
};

/* struct ioctl_info - memory info returned to userspace through ioctl */
struct ioctl_info {
	uint32_t domuid;
	u64 phy_addr;
	u64 size;
};

static LIST_HEAD(mcs_backend_infos); /* Track all active backend devices */
static DEFINE_MUTEX(mcs_backend_lock);

/*
 * ------------------------------------------------------------------
 * MCS device file operations
 * ------------------------------------------------------------------
 */

/*
 * find_mcs_info - Find the mcs_backend_info for a given domain ID.
 * For the first time, we match mcs_backend_info by domid. Then we cache resulting
 * mcs_backend_info in file_priv.
 * 
 * @file_priv: Pointer to the mcs_file_private_data structure.
 * @domid: The domain ID to search for.
 *
 * Returns a pointer to the mcs_backend_info structure if found, NULL otherwise.
 */
static struct mcs_backend_info *find_mcs_info(struct mcs_file_private_data *file_priv, uint32_t domid)
{
	struct mcs_backend_info *mcs_info;

	if (file_priv->backend_info)
		return file_priv->backend_info;

	mutex_lock(&mcs_backend_lock);
	list_for_each_entry(mcs_info, &mcs_backend_infos, list) {
		if (mcs_info->domuid == domid) {
			mutex_unlock(&mcs_backend_lock);
			file_priv->backend_info = mcs_info;
			return mcs_info;
		}
	}
	mutex_unlock(&mcs_backend_lock);
	return NULL;
}

static int mcs_xen_open(struct inode *inode, struct file *filp)
{
	struct mcs_file_private_data *file_priv;

	if (!capable(CAP_SYS_RAWIO))
		return -EPERM;

	file_priv = kmalloc(sizeof(struct mcs_file_private_data), GFP_KERNEL);
	if (!file_priv) {
		pr_err("Failed to allocate private data\n");
		return -ENOMEM;
	}

	file_priv->domid = 0;
	file_priv->backend_info = NULL;
	filp->private_data = file_priv;

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
static int mcs_xen_mmap(struct file *file, struct vm_area_struct *vma)
{
	int found = 0;
	size_t size = vma->vm_end - vma->vm_start;
	phys_addr_t offset = (phys_addr_t)vma->vm_pgoff << PAGE_SHIFT;
	struct mcs_file_private_data *file_priv = file->private_data;
	uint32_t domid = file_priv->domid;
	struct mcs_backend_info *mcs_info;

	/* Does it even fit in phys_addr_t? */
	if (offset >> PAGE_SHIFT != vma->vm_pgoff)
		return -EINVAL;

	/* It's illegal to wrap around the end of the physical address space. */
	if (offset + (phys_addr_t)size - 1 < offset)
		return -EINVAL;

	mcs_info = find_mcs_info(file_priv, domid);
	if (mcs_info && offset >= mcs_info->shmem_phys && size <= mcs_info->shmem_size) {
		found = 1;
	}

	if (found == 0) {
		pr_err("mmap failed: mmap memory is not in mcs reserved memory.\n");
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

static unsigned int mcs_xen_poll(struct file *file, poll_table *wait)
{
	unsigned int mask = 0;
	struct mcs_file_private_data *file_priv = file->private_data;
	struct mcs_backend_info *mcs_info = find_mcs_info(file_priv, file_priv->domid);

	if (mcs_info) {
		poll_wait(file, &mcs_info->mcs_evtchn_wait, wait);
		if (atomic_cmpxchg(&mcs_info->irq_triggered, 1, 0) == 1)
			mask |= POLLIN | POLLRDNORM;
	} else {
		pr_err("Poll failed. Domid %u not found\n", file_priv->domid);
	}

	return mask;
}

static void ioctl_set_domid(struct file *f, uint32_t domid)
{
	struct mcs_file_private_data *file_priv = f->private_data;

	file_priv->domid = domid;

	return;
}

static int ioctl_query_mem(struct mcs_file_private_data *file_priv, struct ioctl_info *query_info)
{
	struct mcs_backend_info *mcs_info;

	mcs_info = find_mcs_info(file_priv, query_info->domuid);
	if (!mcs_info) {
		pr_err("Domain ID %u not found\n", query_info->domuid);
		return -EINVAL;
	}

	query_info->phy_addr = mcs_info->shmem_phys;
	query_info->size = mcs_info->shmem_size;

	return 0;
}

static int ioctl_invoke_evtchn(struct mcs_file_private_data *file_priv, int domid)
{
	struct mcs_backend_info *mcs_info;

	mcs_info = find_mcs_info(file_priv, domid);
	if (!mcs_info) {
		pr_err("Domain ID %u not found\n", domid);
		return -EINVAL;
	}

	(void) notify_remote_via_evtchn(mcs_info->evtchn);

    return 0;
}

static long mcs_xen_ioctl(struct file *f, unsigned int cmd, unsigned long arg)
{
	int ret = 0;
	struct mcs_file_private_data *file_priv = f->private_data;
	struct ioctl_info query_info;

	if (_IOC_TYPE(cmd) != MAGIC_NUMBER)
		return -EINVAL;
	if (_IOC_NR(cmd) > IOC_MAXNR)
		return -EINVAL;

	/* Copy the query information from user space */
	ret = copy_from_user(&query_info, (struct ioctl_info __user *)arg, sizeof(query_info));
	if (ret) {
		pr_err("Failed to copy query info from user space\n");
		return -EFAULT;
	}

	switch (cmd) {
		case IOC_SET_DOMID:
			ioctl_set_domid(f, query_info.domuid);
			break;
		case IOC_QUERY_MEM:
			ret = ioctl_query_mem(file_priv, &query_info);
			if (ret) {
				pr_err("ioctl_query_mem failed\n");
				return ret;
			}

			/* Copy the result back to user space */
			ret = copy_to_user((struct ioctl_info __user *)arg, &query_info, sizeof(query_info));
			if (ret) {
				pr_err("Failed to copy result to user space\n");
				return -EFAULT;
			}
			break;
		case IOC_INVOKE_EVTCHN:
			ret = ioctl_invoke_evtchn(file_priv, query_info.domuid);
			if (ret) {
				pr_err("ioctl_invoke_evtchn failed\n");
				return -EFAULT;
			}
			break;
		default:
			pr_err("IOC param invalid(0x%x)\n", cmd);
			return -EINVAL;
	}
	return 0;
}

static int mcs_xen_release(struct inode *inode, struct file *filp)
{
	if (filp->private_data) {
		kfree(filp->private_data);
		filp->private_data = NULL;
	}
	return 0;
}

static const struct file_operations mcs_xen_fops = {
	.open = mcs_xen_open,
	.mmap = mcs_xen_mmap,
	.poll = mcs_xen_poll,
	.unlocked_ioctl = mcs_xen_ioctl,
	.compat_ioctl = compat_ptr_ioctl,
	.llseek = generic_file_llseek,
	.release = mcs_xen_release,
};

static int register_mcs_xen_dev(void)
{
	int ret;
	struct device *mcs_xen_dev;

	mcs_xen_major = register_chrdev(0, MCS_DEVICE_NAME, &mcs_xen_fops);
	if (mcs_xen_major < 0) {
		ret = mcs_xen_major;
		pr_err("register_chrdev failed (%d)\n", ret);
		goto err;
	}

#if LINUX_VERSION_CODE >= KERNEL_VERSION(6, 6, 0)
	mcs_xen_class = class_create(MCS_DEVICE_NAME);
#else
	mcs_xen_class = class_create(THIS_MODULE, MCS_DEVICE_NAME);
#endif
	if (IS_ERR(mcs_xen_class)) {
		ret = PTR_ERR(mcs_xen_class);
		pr_err("class_create failed (%d)\n", ret);
		goto err_class;
	}

	mcs_xen_dev = device_create(mcs_xen_class, NULL, MKDEV(mcs_xen_major, 0),
				NULL, MCS_DEVICE_NAME);
	if (IS_ERR(mcs_xen_dev)) {
		ret = PTR_ERR(mcs_xen_dev);
		pr_err("device_create failed (%d)\n", ret);
		goto err_device;
	}
	return 0;

err_device:
	class_destroy(mcs_xen_class);
err_class:
	unregister_chrdev(mcs_xen_major, MCS_DEVICE_NAME);
err:
	return ret;
}

static void unregister_mcs_xen_dev(void)
{
	device_destroy(mcs_xen_class, MKDEV(mcs_xen_major, 0));
	class_destroy(mcs_xen_class);
	unregister_chrdev(mcs_xen_major, MCS_DEVICE_NAME);
}

/*
 * ------------------------------------------------------------------
 * Xenbus driver operations
 * ------------------------------------------------------------------
 */

/*
 * mcs_cleanup_gnttab - Clean up grant table references
 * @mcs_info: Pointer to backend info structure
 * @num_pages: Number of grant pages to clean up
 */
static void mcs_cleanup_gnttab(struct mcs_backend_info *mcs_info, int num_pages)
{
    int i;
	char gref_key[64];

	if (!mcs_info->shmem_virt) {
		return;
	}

    for (i = 0; i < num_pages; i++) {
        if (mcs_info->grant_refs[i] != INVALID_GRANT_REF) {
            /* This already frees pages. No need to free them ourselves. */
			gnttab_end_foreign_access(mcs_info->grant_refs[i], 0,
				(unsigned long)mcs_info->shmem_virt + i * PAGE_SIZE);
            mcs_info->grant_refs[i] = INVALID_GRANT_REF;

			/* Remove gref key from xenstore */
			snprintf(gref_key, sizeof(gref_key), "%s%u", XENSTORE_KEY_GREF_PREFIX, i);
			(void) xenbus_rm(XBT_NIL, mcs_info->xdev->nodename, gref_key);
        }
    }

	(void) xenbus_rm(XBT_NIL, mcs_info->xdev->nodename, XENSTORE_KEY_GREF_NUM);

	mcs_info->shmem_virt = NULL;
	pr_info("Ending access for grant ref\n");
}

/*
 * mcs_cleanup_evtchn - Clean up event channel resources
 * @mcs_info: Pointer to backend info structure
 */
static void mcs_cleanup_evtchn(struct mcs_backend_info *mcs_info)
{
    if (mcs_info->evtchn_irq >= 0) {
        unbind_from_irqhandler(mcs_info->evtchn_irq, mcs_info);
        mcs_info->evtchn_irq = 0;
    }
    
    if (mcs_info->evtchn != INVALID_EVTCHN) {
		// This could fail due to unreleased evtchn from remote.
		// Make sure remote RTOS release it before 
        xenbus_free_evtchn(mcs_info->xdev, mcs_info->evtchn);
        mcs_info->evtchn = INVALID_EVTCHN;
		mcs_info->evtchn_irq = 0;
    }
	(void) xenbus_rm(XBT_NIL, mcs_info->xdev->nodename, XENSTORE_KEY_EVTCHN);

	pr_info("Ending access for evtchn\n");
}

static int mcs_init_gnttab(struct xenbus_device *dev, struct mcs_backend_info *mcs_info, int num_pages)
{
	int ret, i;
	struct page *page = NULL;
	char gref_key[64];

	page = alloc_pages(GFP_KERNEL | __GFP_ZERO, SHMEM_ORDER);
	if (!page) {
		pr_err("Failed to allocate coherent memory\n");
		return -ENOMEM;
	}

	mcs_info->shmem_virt = page_address(page);
	mcs_info->shmem_phys = page_to_phys(page);

	ret = xenbus_grant_ring(dev, mcs_info->shmem_virt, num_pages, mcs_info->grant_refs);
	if (ret) {
		pr_err("Failed to grant foreign access for %pK\n", mcs_info->shmem_virt);
		goto err_free_pages;
	}

	for ( i = 0; i < num_pages; i++) {
		pr_info("gref %u: 0x%llx\n", mcs_info->grant_refs[i], (unsigned long long)mcs_info->shmem_virt + i * PAGE_SIZE);
		snprintf(gref_key, sizeof(gref_key), "%s%u", XENSTORE_KEY_GREF_PREFIX, i);
		ret = xenbus_printf(XBT_NIL, dev->nodename, gref_key, "%u", mcs_info->grant_refs[i]);
		if (ret) {
			pr_err("Failed to write %s to xenstore: %d\n", gref_key, ret);
			goto err_printf_grefs;
		}
	}

	ret = xenbus_printf(XBT_NIL, dev->nodename, XENSTORE_KEY_GREF_NUM, "%u", num_pages);
	if (ret) {
		pr_err("Failed to write gref_num to xenstore: %d\n", ret);
		goto err_printf_grefs;
	}

	pr_info("gref %d updated to %s, shmem_virt = 0x%llx, shmem_phys = 0x%llx\n",
		mcs_info->grant_refs[0], dev->nodename, (unsigned long long)mcs_info->shmem_virt, (unsigned long long)mcs_info->shmem_phys);

	return 0;

err_printf_grefs:
	for (i = 0; i < num_pages; i++) {
		char gref_key[64];
		snprintf(gref_key, sizeof(gref_key), "%s%u", XENSTORE_KEY_GREF_PREFIX, i);
		(void)xenbus_rm(XBT_NIL, dev->nodename, gref_key);
	}

err_free_pages:
	__free_pages(page, SHMEM_ORDER);
	return ret;
}

static irqreturn_t evtchn_handler(int irq, void *dev_id)
{
	struct mcs_backend_info *mcs_info = dev_id;
	atomic_set(&mcs_info->irq_triggered, 1);
	wake_up_interruptible(&mcs_info->mcs_evtchn_wait);
	return IRQ_HANDLED;
}

static int mcs_init_evtchn(struct xenbus_device *dev, struct mcs_backend_info *mcs_info)
{
	int ret;

	ret = xenbus_alloc_evtchn(dev, &mcs_info->evtchn);
	if (ret) {
		pr_err("xenbus_alloc_evtchn failed (%d)\n", ret);
		return ret;
	}

	ret = bind_evtchn_to_irqhandler(mcs_info->evtchn, evtchn_handler, 0, "mica-evtchn", mcs_info);
	if (ret < 0) {
		pr_err("bind_evtchn_to_irqhandler failed (%d)\n", ret);
		goto err_free_evtchn;
	}
	mcs_info->evtchn_irq = ret;

	ret = xenbus_printf(XBT_NIL, dev->nodename, XENSTORE_KEY_EVTCHN, "%u", mcs_info->evtchn);
	if (ret) {
		pr_err("Failed to write evtchn to xenstore: %d\n", ret);
		goto err_free_irq;
	}

	pr_info("evtchn initialized: %d, irq: %d\n", mcs_info->evtchn, mcs_info->evtchn_irq);

	return 0;

err_free_irq:
	unbind_from_irqhandler(mcs_info->evtchn_irq, mcs_info);
	mcs_info->evtchn_irq = 0;
err_free_evtchn:
	xenbus_free_evtchn(dev, mcs_info->evtchn);
	mcs_info->evtchn = INVALID_EVTCHN;

	return ret;
}

/* Placeholder: Called when a frontend device is detected */
static int mcs_backend_probe(struct xenbus_device *dev, const struct xenbus_device_id *id)
{
	struct mcs_backend_info *mcs_info;
	int ret;

	pr_info("MCS backend probed for device: %s %s, otherend is %d: %s\n",
			dev->devicetype, dev->nodename, dev->otherend_id, dev->otherend);

	mcs_info = kzalloc(sizeof(*mcs_info), GFP_KERNEL);
	if (!mcs_info) {
		pr_err("Failed to allocate backend device\n");
		return -ENOMEM;
	}
	INIT_LIST_HEAD(&mcs_info->list);
	mcs_info->xdev = dev;
	mcs_info->shmem_size = 4096 * SHMEM_NPAGES; // 16KB example
	mcs_info->evtchn = INVALID_EVTCHN;
	mcs_info->shmem_virt = NULL;
	mcs_info->domuid = dev->otherend_id;
	atomic_set(&mcs_info->irq_triggered, 0);
	init_waitqueue_head(&mcs_info->mcs_evtchn_wait);

	/* Initialize memory sharing */
	ret = mcs_init_gnttab(dev, mcs_info, SHMEM_NPAGES);
	if (ret) {
		pr_err("Failed to init grant table to share pages to domU\n");
		goto err_alloc_mcs_info;
	}

	/* Initialize notification channel */
	ret = mcs_init_evtchn(dev, mcs_info);
	if (ret) {
		pr_err("Failed to initialize event channel to notify domU\n");
		goto err_gnttab;
	}

	/* Add to active list */
	dev_set_drvdata(&dev->dev, mcs_info);
	mutex_lock(&mcs_backend_lock);
	list_add(&mcs_info->list, &mcs_backend_infos);
	mutex_unlock(&mcs_backend_lock);

	xenbus_switch_state(dev, XenbusStateInitialised);

	pr_info("Backend initialized for domU %u: grant=%u, evtchn=%u\n",
			mcs_info->domuid, mcs_info->grant_refs[0], mcs_info->evtchn);

	return 0;

err_gnttab:
	mcs_cleanup_gnttab(mcs_info, SHMEM_NPAGES);

err_alloc_mcs_info:
	kfree(mcs_info);
	return ret;
}

/* Helper: Cleanup single backend device */
static void mcs_backend_cleanup(struct xenbus_device *dev)
{
	struct mcs_backend_info *mcs_info;

	pr_info("MCS backend removing device: %s\n", dev->nodename);

	mcs_info = dev_get_drvdata(&dev->dev);
	if (!mcs_info) {
		pr_warn("No backend info found for device: %s\n", dev->nodename);
		return;
	}

	mcs_cleanup_gnttab(mcs_info, SHMEM_NPAGES);
	mcs_cleanup_evtchn(mcs_info);

	list_del(&mcs_info->list);

	kfree(mcs_info);
	dev_set_drvdata(&dev->dev, NULL);
}

static int mcs_backend_remove(struct xenbus_device *dev)
{
	mutex_lock(&mcs_backend_lock);

	mcs_backend_cleanup(dev);

	mutex_unlock(&mcs_backend_lock);

	return 0;
}

/* Placeholder: Handle frontend state changes */
static void mcs_frontend_changed(struct xenbus_device *dev, enum xenbus_state frontend_state)
{
	pr_info("MCS frontend state changed: %s/state -> %d\n", dev->otherend, frontend_state);

	mutex_lock(&mcs_backend_lock);
	switch (frontend_state) {
		case XenbusStateInitialising:
			pr_info("%s: Frontend initializing - preparing resources", dev->nodename);
			break;

		case XenbusStateInitialised:
			pr_info("%s: Frontend initialised - enabling communication", dev->nodename);
			break;

		case XenbusStateConnected:
			pr_info("%s: Frontend initialized/connected - enabling communication", dev->nodename);
			break;

		case XenbusStateClosing:
			pr_info("%s: Frontend closing - stopping communication", dev->nodename);
			break;

		case XenbusStateClosed:
			pr_info("%s: Frontend closed - releasing resources", dev->nodename);
			mcs_backend_cleanup(dev);
			if (dev->state == XenbusStateClosed) {
				xenbus_switch_state(dev, XenbusStateClosed);
			}
			break;

		case XenbusStateUnknown:
		default:
			pr_warn("%s: Other frontend state %d", dev->nodename, frontend_state);
			break;
	}
	mutex_unlock(&mcs_backend_lock);
}

/* Xenbus driver definition */
static const struct xenbus_device_id mcs_backend_ids[] = {
	{ "mica" },
	{ "" }
};

static struct xenbus_driver mcs_backend_driver = {
	.name		= "mcs-backend",
	.ids		= mcs_backend_ids,
	.probe		= mcs_backend_probe,
	.remove		= mcs_backend_remove,
	.otherend_changed	= mcs_frontend_changed,
};

static int __init xen_mcs_xenbus_init(void)
{
	int ret;

	ret = register_mcs_xen_dev();
	if (ret) {
		pr_err("Failed to register mcs_xen dev, ret = %d\n", ret);
		return ret;
		
	}

	ret = xenbus_register_backend(&mcs_backend_driver);
	if (ret) {
		pr_err("Failed to register MCS Xenbus backend: %d\n", ret);
		goto err_mcs_xen_dev;
	}

	pr_info("MCS Xenbus backend initialized\n");

	return 0;

err_mcs_xen_dev:
	unregister_mcs_xen_dev();
	return ret;
}

static void __exit xen_mcs_xenbus_exit(void)
{
	xenbus_unregister_driver(&mcs_backend_driver);
	unregister_mcs_xen_dev();
	pr_info("MCS Xenbus backend exited\n");
}

module_init(xen_mcs_xenbus_init);
module_exit(xen_mcs_xenbus_exit);

MODULE_AUTHOR("openEuler Embedded");
MODULE_DESCRIPTION("Xen MCS Backend Driver");
MODULE_LICENSE("Dual BSD/GPL");
