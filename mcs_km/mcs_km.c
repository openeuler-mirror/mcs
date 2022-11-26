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

#define MCS_DEVICE_NAME    "mcs"
#define CPU_ON_FUNCID       0xC4000003
#define AFFINITY_INFO_FUNCID   0xC4000004

#define MAGIC_NUMBER       'A'
#define IOC_SENDIPI        _IOW(MAGIC_NUMBER, 0, int)
#define IOC_CPUON          _IOW(MAGIC_NUMBER, 1, int)
#define IOC_AFFINITY_INFO  _IOW(MAGIC_NUMBER, 2, int)
#define IOC_MAXNR          2

#define OPENAMP_INIT_TRIGGER      0x0
#define OPENAMP_CLIENTOS_TRIGGER  0x1
#define OPENAMP_IRQ  8

#define START_INDEX     1
#define SIZE_INDEX      2

static struct class *mcs_class;
static int mcs_major;

static u64 valid_start;
static u64 valid_end;
static const char *smccc_method = "hvc";

static DECLARE_WAIT_QUEUE_HEAD(openamp_trigger_wait);
static int openamp_trigger;

static irqreturn_t handle_clientos_ipi(int irq, void *data)
{
    pr_info("mcs_km: received ipi from client os\n");
    openamp_trigger = OPENAMP_CLIENTOS_TRIGGER;
    wake_up_interruptible(&openamp_trigger_wait);
    return IRQ_HANDLED;
}

void set_openamp_ipi(void)
{
    int err;
    struct irq_desc *desc;

    /* use IRQ8 as IPI7, init irq resource once */
    desc = irq_to_desc(OPENAMP_IRQ);
    if (!desc->action) {
        err = request_percpu_irq(OPENAMP_IRQ, handle_clientos_ipi, "IPI", &cpu_number);
        if (err) {
            pr_err("mcs_km: request openamp irq failed(%d)\n", err);
            return;
        }
    }

    /* In SMP, all the cores run Linux should be enabled */
    if (!irq_percpu_is_enabled(OPENAMP_IRQ)) {
        preempt_disable(); /* fix kernel err message: using smp_processor_id() in preemptible */
        enable_percpu_irq(OPENAMP_IRQ, 0);
        preempt_enable();
    }

    return;
}

static void send_clientos_ipi(const struct cpumask *target)
{
    ipi_send_mask(OPENAMP_IRQ, target);
}

static unsigned int mcs_poll(struct file *file, poll_table *wait)
{
    unsigned int mask;

    poll_wait(file, &openamp_trigger_wait, wait);
    mask = 0;
    if (openamp_trigger == OPENAMP_CLIENTOS_TRIGGER)
        mask |= POLLIN | POLLRDNORM;

    openamp_trigger = OPENAMP_INIT_TRIGGER;
    return mask;
}

static long mcs_ioctl(struct file *f, unsigned int cmd, unsigned long arg)
{
    int err;
    int cpu_id, cpu_boot_addr;
    struct arm_smccc_res res;

    if (_IOC_TYPE(cmd) != MAGIC_NUMBER)
        return -EINVAL;
    if (_IOC_NR(cmd) > IOC_MAXNR)
        return -EINVAL;
    if (copy_from_user(&cpu_id, (int __user *)arg, sizeof(int)))
        return -EFAULT;

    switch (cmd) {
        case IOC_SENDIPI:
            pr_info("mcs_km: received ioctl cmd to send ipi to cpu(%d)\n", cpu_id);
            send_clientos_ipi(cpumask_of(cpu_id));
            break;

        case IOC_CPUON:
            if (copy_from_user(&cpu_boot_addr, (unsigned int __user *)arg + 1, sizeof(unsigned int)))
                return -EFAULT;

            pr_info("mcs_km: start booting clientos on cpu(%d) addr(0x%x) smccc(%s)\n", cpu_id, cpu_boot_addr, smccc_method);

            if (strcmp(smccc_method, "smc") == 0)
                arm_smccc_smc(CPU_ON_FUNCID, cpu_id, cpu_boot_addr, 0, 0, 0, 0, 0, &res);
            else
                arm_smccc_hvc(CPU_ON_FUNCID, cpu_id, cpu_boot_addr, 0, 0, 0, 0, 0, &res);

            if (res.a0) {
                pr_err("mcs_km: boot clientos failed(%ld)\n", res.a0);
                return -EINVAL;
            }
            break;

        case IOC_AFFINITY_INFO:
            if (strcmp(smccc_method, "smc") == 0)
                arm_smccc_smc(AFFINITY_INFO_FUNCID, cpu_id, 0, 0, 0, 0, 0, 0, &res);
            else
                arm_smccc_hvc(AFFINITY_INFO_FUNCID, cpu_id, 0, 0, 0, 0, 0, 0, &res);

            if (copy_to_user((unsigned int __user *)arg, &res.a0, sizeof(unsigned int)))
                return -EFAULT;
            break;

        default:
            pr_err("mcs_km: IOC param invalid(0x%x)\n", cmd);
            return -EINVAL;
    }

    return 0;
}

#ifdef CONFIG_STRICT_DEVMEM
static inline int range_is_allowed(unsigned long pfn, unsigned long size)
{
    u64 from = ((u64)pfn) << PAGE_SHIFT;
    u64 to = from + size;
    u64 cursor = from;

    while (cursor < to) {
        if (page_is_ram(pfn))
            return 0;
        cursor += PAGE_SIZE;
        pfn++;
    }
    return 1;
}
#else
static inline int range_is_allowed(unsigned long pfn, unsigned long size)
{
    return 1;
}
#endif

int mcs_phys_mem_access_prot_allowed(struct file *file,
	unsigned long pfn, unsigned long size, pgprot_t *vma_prot)
{
    u64 start, end;
    start = ((u64)pfn) << PAGE_SHIFT;
    end = start + size;

    if (valid_start == 0 && valid_end == 0) {
        if (!range_is_allowed(pfn, size))
            return 0;
        return 1;
    }

    if (start < valid_start || end > valid_end)
        return 0;

    return 1;
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

	if (!mcs_phys_mem_access_prot_allowed(file, vma->vm_pgoff, size,
						&vma->vm_page_prot))
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
			    vma->vm_page_prot))
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

static int get_mcs_node_info(void)
{
    int ret = 0;
    struct device_node *nd = NULL;
    u8 datasize;
    u32 *val = NULL;

    nd = of_find_compatible_node(NULL, NULL, "mcs_mem");
    if (nd == NULL) {
        pr_info("no reserved-memory mcs node.\n");
        return -EINVAL;
    }

    datasize = of_property_count_elems_of_size(nd, "reg", sizeof(u32));
    if (datasize != 3) {
        pr_err("invalid reserved-memory mcs reg size.\n");
        return -EINVAL;
    }

    val = kmalloc(datasize * sizeof(u32), GFP_KERNEL);
    if (val == NULL)
        return -ENOMEM;

    ret = of_property_read_u32_array(nd, "reg", val, datasize);
    if (ret < 0)
        goto out;

    valid_start = (u64)(*(val + START_INDEX));
    valid_end = valid_start + (u64)(*(val + SIZE_INDEX));

    ret = of_property_read_string(nd, "smccc", &smccc_method);
    if (ret < 0)
        goto out;

out:
    kfree(val);
    return ret;
}

static int __init mcs_dev_init(void)
{
	struct device *class_dev = NULL;
	int ret = 0;

	mcs_major = register_chrdev(0, MCS_DEVICE_NAME, &mcs_fops);
	if (mcs_major < 0) {
		pr_err("mcs_km: unable to get major %d for memory devs.\n", mcs_major);
		return -1;
	}

	mcs_class = class_create(THIS_MODULE, MCS_DEVICE_NAME);
	if (IS_ERR(mcs_class)) {
		ret = PTR_ERR(mcs_class);
		goto error_class_create;
	}

	class_dev = device_create(mcs_class, NULL, MKDEV((unsigned int)mcs_major, 1), 
			NULL, MCS_DEVICE_NAME);
	if (unlikely(IS_ERR(class_dev))) {
		ret = PTR_ERR(class_dev);
		goto error_device_create;
	}

    set_openamp_ipi();

    if (get_mcs_node_info() < 0)
		pr_info("there's no mcsmem dts node info. Allow page isn't ram mmap.\n");
    else
        pr_info("valid mcsmem node detected.\n");

	pr_info("mcs_km: create major %d for mcs dev.\n", mcs_major);
	return 0;

error_device_create:
	class_destroy(mcs_class);
error_class_create:
	unregister_chrdev(mcs_major, MCS_DEVICE_NAME);
	return ret;
}
module_init(mcs_dev_init);

static void __exit mcs_dev_exit(void)
{
	device_destroy(mcs_class, MKDEV((unsigned int)mcs_major, 1));
	class_destroy(mcs_class);
	unregister_chrdev(mcs_major, MCS_DEVICE_NAME);

    pr_info("mcs_km: remove mcs dev.\n");
}
module_exit(mcs_dev_exit);

MODULE_AUTHOR("OpenEuler Embedded");
MODULE_DESCRIPTION("mcs device");
MODULE_LICENSE("Dual BSD/GPL");
