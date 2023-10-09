#include <linux/kernel.h>
#include <linux/module.h>
#include <linux/platform_device.h>
#include <linux/remoteproc.h>
#include <linux/interrupt.h>
#include <linux/irq.h>
#include <linux/of_address.h>
#include <linux/of_reserved_mem.h>
#include <linux/arm-smccc.h>

#include "remoteproc_internal.h"

#define CPU_ON_FUNCID	   	0xC4000003
#define IPI_MCS				8
#define RPROC_MEM_MAX		4

/* use resource tables's  reserved[0] to carry some extra information
 * the following IDs come from PSCI definition
 */
#define CPU_OFF_FUNCID		0x84000002
#define CPU_SUSPEND_FUNCID 	0xc4000001

static int __percpu *mcs_evt;
static struct rproc *rproc;
/* @workqueue: workqueue for processing virtio interrupts */
static struct work_struct workqueue;

/**
 * struct mcs_rproc_mem - internal memory structure
 * @cpu_addr: cpu virtual address of the memory region
 * @phy_addr: physical address of the memory region
 * @size: total size of the memory region
 */
struct mcs_rproc_mem {
	void __iomem *cpu_addr;
	phys_addr_t phy_addr;
	size_t size;
};

/**
 * struct mcs_rproc_data - mcs rproc private data
 * @smccc_conduit: smc or hvc psci conduit
 * @status:	virtual proc status based on rsc table reserved val
 * @mem: reserved memory regions
 * @rproc: pointer to remoteproc instance
 */
struct mcs_rproc_pdata {
	enum arm_smccc_conduit smccc_conduit;
	u32 __iomem *status;
	struct mcs_rproc_mem mem[RPROC_MEM_MAX];
	struct rproc *rproc;
};

/**
 * Main virtqueue message workqueue function
 */
static void handle_event(struct work_struct *work)
{
	rproc_vq_interrupt(rproc, 0);
}

/**
 * Interrupt handler for processing vring kicks from remote processor
 */
static irqreturn_t mcs_remoteproc_interrupt(int irq, void *dev_id)
{
	dev_dbg(rproc->dev.parent, "Kick Linux because of pending message\n");
	schedule_work(&workqueue);

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
	if (desc->action)
		return -EBUSY;

	mcs_evt = alloc_percpu(int);
	if (!mcs_evt)
		return -ENOMEM;

	err = request_percpu_irq(IPI_MCS, mcs_remoteproc_interrupt, "MCS IPI", mcs_evt);
	if (err) {
		free_percpu(mcs_evt);
		return err;
	}

	on_each_cpu(enable_mcs_ipi, NULL, 1);
	return 0;
}

/**
 * Make hvc/smc call to boot the processor
 */
static int rproc_cpu_boot(unsigned int cpu, unsigned long boot_addr,
			  enum arm_smccc_conduit conduit)
{
	struct arm_smccc_res res;

	switch (conduit) {
	case SMCCC_CONDUIT_HVC:
		dev_dbg(rproc->dev.parent, "cpu boot at 0x%lx, taget %d\n", boot_addr, cpu);
		arm_smccc_hvc(CPU_ON_FUNCID, cpu, boot_addr, 0, 0, 0, 0, 0, &res);
		break;
	case SMCCC_CONDUIT_SMC:
		dev_dbg(rproc->dev.parent, "cpu boot at 0x%lx, taget %d\n", boot_addr, cpu);
		arm_smccc_smc(CPU_ON_FUNCID, cpu, boot_addr, 0, 0, 0, 0, 0, &res);
		break;
	default:
		return -EPERM;
	}

	return (res.a0 == SMCCC_RET_SUCCESS) ? 0 : -EPERM;
}

static int mcs_rproc_start(struct rproc *rproc)
{
	int ret;
	struct device *dev = rproc->dev.parent;
	struct mcs_rproc_pdata *priv = rproc->priv;

	INIT_WORK(&workqueue, handle_event);

	ret = init_mcs_ipi();
	if (ret) {
		dev_err(dev, "Failed to init mcs ipi, ret = %d\n", ret);
		return ret;
	}

	/* use resource table's reserved[0] as extra status bit */
	priv->status = rproc->table_ptr->reserved;

	ret = rproc_cpu_boot(3, rproc->bootaddr, priv->smccc_conduit);
	if (ret) {
		remove_mcs_ipi();
		flush_work(&workqueue);
		dev_err(dev, "Failed to enable remote core, ret = %d\n", ret);
	}

	return ret;
}

/* kick the remote processor */
static void mcs_rproc_kick(struct rproc *rproc, int vqid)
{
	dev_dbg(rproc->dev.parent, "send ipi to cpu 3\n");
	ipi_send_mask(IPI_MCS, cpumask_of(3));
}

/* power off the remote processor */
static int mcs_rproc_stop(struct rproc *rproc)
{
	static int flg = 0;
	struct mcs_rproc_pdata *priv = rproc->priv;

	priv->status[0] = CPU_OFF_FUNCID;
	ipi_send_mask(IPI_MCS, cpumask_of(3));

	/* \todo: should check priv->status[0] be cleared? */

	remove_mcs_ipi();
	flush_work(&workqueue);
	return 0;
}

static void *mcs_rproc_da_to_va(struct rproc *rproc, u64 da, size_t len)
{
	int i;
	void *va = NULL;
	unsigned long offset;
	struct mcs_rproc_pdata *priv = rproc->priv;

	if (len == 0)
		return NULL;

	for (i = 0; i < RPROC_MEM_MAX; i++) {
		if (da >= priv->mem[i].phy_addr &&
		    da + len < (priv->mem[i].phy_addr + priv->mem[i].size)) {
			offset = da - priv->mem[i].phy_addr;
			va = (void *)(priv->mem[i].cpu_addr + offset);
			break;
		}
	}

	dev_dbg(&rproc->dev, "da = 0x%llx len = 0x%zx va = 0x%p\n",
		da, len, va);

	return va;
}

static struct rproc_ops mcs_rproc_ops = {
	.start		= mcs_rproc_start,
	.stop		= mcs_rproc_stop,
	.da_to_va   = mcs_rproc_da_to_va,
	.kick		= mcs_rproc_kick,
};

static int get_psci_method(struct mcs_rproc_pdata *priv)
{
	const char *method;
	struct device_node *np;
	struct of_device_id psci_of_match[] = {
		{ .compatible = "arm,psci" },
		{ .compatible = "arm,psci-0.2" },
		{ .compatible = "arm,psci-1.0" },
		{},
	};

	priv->smccc_conduit = SMCCC_CONDUIT_NONE;
	np = of_find_matching_node(NULL, psci_of_match);

	if (!np || !of_device_is_available(np))
		return -ENODEV;

	if (of_property_read_string(np, "method", &method)) {
		of_node_put(np);
		return -ENXIO;
	}

	of_node_put(np);

	if (!strcmp("hvc", method))
		priv->smccc_conduit = SMCCC_CONDUIT_HVC;
	else if (!strcmp("smc", method))
		priv->smccc_conduit = SMCCC_CONDUIT_SMC;
	else
	 	return -EOPNOTSUPP;

	return 0;
}

static int init_reserved_mem(struct device *dev,
			     struct mcs_rproc_pdata *priv)
{
	int n = 0;
	int i, count, ret;
	void __iomem *addr;
	struct device_node *np = dev->of_node;

	count = of_count_phandle_with_args(np, "memory-region", NULL);
	/* We at least require two memory-region, one for virtio and one for elf firmware. */
	if (count < 2) {
		dev_err(dev, "reserved mem is required\n");
		return -ENODEV;
	}

	ret = of_reserved_mem_device_init_by_idx(dev, np, 0);
	if (ret) {
		dev_err(dev, "device cannot initialize DMA pool, ret = %d\n",
			ret);
		return ret;
	}

	count--;
	for (i = 0; i < count; i++) {
		struct device_node *node;
		struct resource res;

		node = of_parse_phandle(np, "memory-region", i + 1);
		ret = of_address_to_resource(node, 0, &res);
		if (ret) {
			dev_err(dev, "unable to resolve memory region\n");
			return ret;
		}

		if (n >= RPROC_MEM_MAX)
			break;

		addr = devm_ioremap_resource(dev, &res);
		if (IS_ERR(addr)) {
			dev_err(dev, "devm_ioremap_resource failed\n");
			ret = PTR_ERR(addr);
			return ret;
		}
		priv->mem[n].cpu_addr = addr;
		priv->mem[n].phy_addr = res.start;
		priv->mem[n].size = resource_size(&res);
		n++;
	}

	return 0;
}

static int mcs_remoteproc_probe(struct platform_device *pdev)
{
	int ret = 0;
	struct device *dev = &pdev->dev;
	struct device_node *np = dev->of_node;
	struct mcs_rproc_pdata *priv;

	rproc = devm_rproc_alloc(dev, np->name, &mcs_rproc_ops,
				NULL, sizeof(struct mcs_rproc_pdata));

	if (!rproc) {
		dev_err(&pdev->dev, "rproc allocation failed\n");
		ret = -ENOMEM;
		return ret;
	}
	priv = rproc->priv;
	priv->rproc = rproc;

	ret = get_psci_method(priv);
	if (ret) {
		dev_err(&pdev->dev, "Failed to get psci \"method\" property, ret = %d\n", ret);
		return ret;
	}

	ret = init_reserved_mem(dev, priv);
	if (ret) {
		dev_err(&pdev->dev, "Failed to init memory region, ret = %d\n", ret);
		return ret;
	}

	dev_set_drvdata(dev, rproc);

	/* Manually start the rproc */
	rproc->auto_boot = false;

	ret = devm_rproc_add(dev, rproc);
	if (ret)
		dev_err(&pdev->dev, "rproc add failed\n");

	return ret;
}

static int mcs_remoteproc_remove(struct platform_device *pdev)
{
	struct rproc *rproc = platform_get_drvdata(pdev);

	dev_info(&pdev->dev, "removing rproc %s\n", rproc->name);

	return 0;
}

static const struct of_device_id mcs_remoteproc_match[] = {
	{ .compatible = "oe,mcs_remoteproc", },
	{ /* end of list */ },
};
MODULE_DEVICE_TABLE(of, mcs_remoteproc_match);

static struct platform_driver mcs_remoteproc_driver = {
	.probe = mcs_remoteproc_probe,
	.remove = mcs_remoteproc_remove,
	.driver = {
		.name = "mcs_remoteproc",
		.of_match_table = mcs_remoteproc_match,
	},
};
module_platform_driver(mcs_remoteproc_driver);

MODULE_LICENSE("GPL v2");
MODULE_DESCRIPTION("mcs remote processor control driver");
