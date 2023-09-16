#include <linux/kernel.h>
#include <linux/module.h>
#include <linux/err.h>
#include <linux/dma-mapping.h>
#include <linux/remoteproc.h>
#include <linux/of_reserved_mem.h>
#include <linux/pci.h>
#include <linux/dma-map-ops.h>
#include <linux/io.h>

#include "remoteproc_internal.h"
#include "jailhouse_rproc_helpers.h"

#define DRV_NAME "rpmsg-ivshmem"
#define PCI_DEVICE_ID_IVSHMEM		0x4106
#define VIRTIO_STATE_READY		cpu_to_le32(1)
#define IVSHM_CFG_PRIV_CNTL		0x03
#define IVSHM_CFG_STATE_TAB_SZ		0x04
#define IVSHM_CFG_RW_SECTION_SZ		0x08
#define IVSHM_CFG_ADDRESS		0x18
#define IVSHM_INT_ENABLE		BIT(0)
#define IVSHM_PROTO_RPMSG		0x4001

static struct rproc *rproc;
/* @workqueue: workqueue for processing virtio interrupts */
static struct work_struct workqueue;

struct ivshmem_v2_reg {
	u32 id;
	u32 max_peers;
	u32 int_control;
	u32 doorbell;
	u32 state;
};

/**
 * struct ivshmem_device - ivshmem device
 * @ivshm_regs: ivshmem register region
 * @peer_id: the peer
 * @shmem: point to the ivshmem RW section
 * @shmem_sz: total size of the ivshmem RW section
 */
struct ivshmem_device {
	struct ivshmem_v2_reg __iomem *ivshm_regs;
	u32 peer_id;
	void *shmem;
	resource_size_t shmem_sz;
};

/**
 * struct mcs_rproc_data - mcs rproc private data
 * @rproc: pointer to remoteproc instance
 * @rsc_table: point to the first page of the ivshmem RW section,
 *             used to load the resource table
 * @ivshmem_dev: the ivshmem device
 */
struct mcs_rproc_pdata {
	struct rproc *rproc;
	void *rsc_table;
	struct ivshmem_device ivshmem_dev;
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

static void remove_mcs_doorbell(struct rproc *rproc)
{
	struct pci_dev *pci_dev = to_pci_dev(rproc->dev.parent);

	free_irq(pci_irq_vector(pci_dev, 0), NULL);
	pci_free_irq_vectors(pci_dev);
}

static int init_mcs_doorbell(struct rproc *rproc)
{
	int err, vectors;
	unsigned int desired_vectors = 1;
	struct pci_dev *pci_dev = to_pci_dev(rproc->dev.parent);
	struct mcs_rproc_pdata *priv = rproc->priv;

	vectors = pci_alloc_irq_vectors(pci_dev, desired_vectors,
					desired_vectors, PCI_IRQ_MSIX);
	if (vectors != desired_vectors) {
		vectors = pci_alloc_irq_vectors(pci_dev, 1, 2,
						PCI_IRQ_LEGACY | PCI_IRQ_MSIX);
		if (vectors < 0)
			return vectors;
	}

	err = request_irq(pci_irq_vector(pci_dev, 0), mcs_remoteproc_interrupt, 0,
			  "MCS DOORBELL", NULL);
	if (err)
		pci_free_irq_vectors(pci_dev);
	else
		writel(IVSHM_INT_ENABLE, &priv->ivshmem_dev.ivshm_regs->int_control);

	return err;
}

/**
 * kick the remote processor
 */
static void mcs_rproc_kick(struct rproc *rproc, int vqid)
{
	struct mcs_rproc_pdata *priv = rproc->priv;

	dev_dbg(rproc->dev.parent, "trigger doorbell to client os(peer_id: %d)\n", priv->ivshmem_dev.peer_id);
	writel((priv->ivshmem_dev.peer_id << 16), &priv->ivshmem_dev.ivshm_regs->doorbell);
}

/**
 * prepare jailhouse cell via sysfs
 */
static int rproc_prepare_jailhouse(struct rproc *rproc)
{
	int err = 0;
	static int cell_created = 0;
	const struct firmware *firmware_p;
	struct device *dev;
	struct mcs_rproc_pdata *priv = rproc->priv;

	/* create jailhouse cell */
	if (rproc->jh_cell && !cell_created) {
		err = jh_cell_create_by_rproc(rproc);
		if (!err)
			cell_created = 1;
	}

	/* load inmate firmware to jailhouse cell */
	if (rproc->jh_inmate && cell_created) {
		dev = &rproc->dev;
		err = request_firmware(&firmware_p, rproc->jh_inmate, dev);
		if (err < 0) {
			dev_err(dev, "request_firmware failed: %d\n", err);
			return err;
		}

		/* load rsc table from jh_inmate */
		err = rproc_elf_load_rsc_table(rproc, firmware_p);
		if (err < 0) {
			dev_err(dev, "load rsc table failed\n");
			release_firmware(firmware_p);
			return err;
		}

		memcpy(priv->rsc_table, rproc->table_ptr, rproc->table_sz);
		rproc->table_ptr = priv->rsc_table;
		kfree(rproc->cached_table);
		rproc->cached_table = NULL;

		err = jh_cell_load_by_rproc(rproc, firmware_p);

		release_firmware(firmware_p);
	}

	return err;
}

/**
 * trigger the jailhouse to start the remote processor
 */
static int rproc_attach_jailhouse(struct rproc *rproc)
{
	int err;

	if (rproc->jh_cell == NULL || rproc->jh_inmate == NULL) {
		dev_err(&rproc->dev, "Please prepare the jailhouse cell before before starting\n");
		return -EINVAL;
	}

	INIT_WORK(&workqueue, handle_event);

	err = init_mcs_doorbell(rproc);
	if (err) {
		dev_err(&rproc->dev, "Failed to init mcs doorbell, err = %d\n", err);
		return err;
	}

	return jh_cell_start_by_rproc(rproc);
}

/**
 * power off the remote processor
 */
static int rproc_stop_jailhouse(struct rproc *rproc)
{
	int err;

	err = jh_cell_stop_by_rproc(rproc);
	if (err) {
		dev_err(&rproc->dev, "Failed to stop remote, err = %d\n", err);
		return err;
	}

	remove_mcs_doorbell(rproc);
	flush_work(&workqueue);
	return 0;
}

static struct rproc_ops mcs_rproc_ops = {
	.kick		= mcs_rproc_kick,
	.attach		= rproc_attach_jailhouse,
	.stop		= rproc_stop_jailhouse,
	.prepare	= rproc_prepare_jailhouse,
};

static inline unsigned int get_custom_order(unsigned long size,
					    unsigned int shift)
{
	size--;
	size >>= shift;
#if BITS_PER_LONG == 32
	return fls(size);
#else
	return fls64(size);
#endif
}

static u64 get_config_qword(struct pci_dev *pci_dev, unsigned int pos)
{
	u32 lo, hi;

	pci_read_config_dword(pci_dev, pos, &lo);
	pci_read_config_dword(pci_dev, pos + 4, &hi);
	return lo | ((u64)hi << 32);
}

static int init_reserved_mem(struct device *dev)
{
	int count, ret;
	struct device_node *np;

	np = of_find_compatible_node(NULL, NULL, "oe,mcs_remoteproc");
	if (np == NULL)
		return -ENODEV;

	count = of_count_phandle_with_args(np, "memory-region", NULL);
	if (count <= 0) {
		dev_err(dev, "reserved mem is required\n");
		return -ENODEV;
	}

	/* Use reserved memory region 0 for vring DMA allocations */
	ret = of_reserved_mem_device_init_by_idx(dev, np, 0);
	if (ret) {
		dev_err(dev, "device cannot initialize DMA pool, ret = %d\n",
			ret);
		return ret;
	}

	/* Set of_node for virtio device. See rproc_add_virtio_dev() for more details. */
	rproc->dev.parent->of_node = np;

	return 0;
}

static void free_reserved_mem(struct device *dev)
{
	of_reserved_mem_device_release(dev);
}

static int init_ivshmem_dev(struct pci_dev *pci_dev, struct ivshmem_device *ivshmem_dev)
{
	int ret, vendor_cap;
	u32 id, dword;
	resource_size_t section_sz;
	phys_addr_t section_addr;
	unsigned int cap_pos;

	ret = pcim_iomap_regions(pci_dev, BIT(0), DRV_NAME);
	if (ret)
		return ret;

	ivshmem_dev->ivshm_regs = pcim_iomap_table(pci_dev)[0];

	id = readl(&ivshmem_dev->ivshm_regs->id);
	if (id > 1) {
		dev_err(&pci_dev->dev, "invalid ID %d\n", id);
		return -EINVAL;
	}
	if (readl(&ivshmem_dev->ivshm_regs->max_peers) != 2) {
		dev_err(&pci_dev->dev, "number of peers must be 2\n");
		return -EINVAL;
	}

	ivshmem_dev->peer_id = !id;

	vendor_cap = pci_find_capability(pci_dev, PCI_CAP_ID_VNDR);
	if (vendor_cap < 0) {
		dev_err(&pci_dev->dev, "missing vendor capability\n");
		return -EINVAL;
	}

	/* Get the base address of ivshmem from BAR2 or Vendor Specific Capability (offset 18h) */
	if (pci_resource_len(pci_dev, 2) > 0) {
		section_addr = pci_resource_start(pci_dev, 2);
	} else {
		cap_pos = vendor_cap + IVSHM_CFG_ADDRESS;
		section_addr = get_config_qword(pci_dev, cap_pos);
	}

	/* We use rsc table to check the state of the remote, so ignore the state table */
	cap_pos = vendor_cap + IVSHM_CFG_STATE_TAB_SZ;
	pci_read_config_dword(pci_dev, cap_pos, &dword);
	section_sz = dword;
	section_addr += section_sz;

	cap_pos = vendor_cap + IVSHM_CFG_RW_SECTION_SZ;
	section_sz = get_config_qword(pci_dev, cap_pos);
	if (section_sz < 2 * PAGE_SIZE) {
		dev_err(&pci_dev->dev, "R/W section too small\n");
		return -EINVAL;
	}

	dev_dbg(&pci_dev->dev, "ivshmem R/W section: addr %llx, size %llx\n", section_addr, section_sz);
	ivshmem_dev->shmem_sz = section_sz;
	ivshmem_dev->shmem = devm_memremap(&pci_dev->dev, section_addr, section_sz, MEMREMAP_WB);
	if (!ivshmem_dev->shmem) {
		dev_err(&pci_dev->dev, "devm_memremap failed\n");
		return -ENOMEM;
	}

	pci_write_config_byte(pci_dev, vendor_cap + IVSHM_CFG_PRIV_CNTL, 0);

	writel(VIRTIO_STATE_READY, &ivshmem_dev->ivshm_regs->state);

	return 0;
}

static int mcs_remoteproc_probe(struct pci_dev *pci_dev,
				const struct pci_device_id *pci_id)

{
	int ret;
	struct mcs_rproc_pdata *priv;

	rproc = devm_rproc_alloc(&pci_dev->dev, pci_name(pci_dev), &mcs_rproc_ops,
				NULL, sizeof(struct mcs_rproc_pdata));
	if (!rproc) {
		dev_err(&pci_dev->dev, "rproc allocation failed\n");
		ret = -ENOMEM;
		return ret;
	}
	priv = rproc->priv;
	priv->rproc = rproc;

	ret = pcim_enable_device(pci_dev);
	if (ret)
		return ret;

	ret = init_ivshmem_dev(pci_dev, &priv->ivshmem_dev);
	if (ret)
		return ret;

	ret = init_reserved_mem(&pci_dev->dev);
	if (ret) {
		dev_err(&pci_dev->dev, "Failed to init memory region, ret = %d\n", ret);
		return ret;
	}

	/* load rsc table to the first page of the RW section */
	priv->rsc_table = priv->ivshmem_dev.shmem;

	/* Manually start the rproc */
	rproc->auto_boot = false;

	/*
	 * The elf will be loaded by jailhouse, so set the status to RPROC_DETACHED.
	 * And the resource table will be loaded into ivshmem RW section, no need
	 * to work with a cached table.
	 */
	rproc->state = RPROC_DETACHED;
	rproc->cached_table = NULL;

	ret = devm_rproc_add(&pci_dev->dev, rproc);
	if (ret) {
		free_reserved_mem(&pci_dev->dev);
		dev_err(&pci_dev->dev, "rproc add failed\n");
		return ret;
	}

	pci_set_master(pci_dev);
	pci_set_drvdata(pci_dev, rproc);

	return ret;
}

static void mcs_remoteproc_remove(struct pci_dev *pci_dev)
{
	free_reserved_mem(&pci_dev->dev);
	dev_info(&pci_dev->dev, "removing %s\n", DRV_NAME);
}

static const struct pci_device_id virtio_ivshmem_id_table[] = {
	{ PCI_DEVICE(PCI_VENDOR_ID_SIEMENS, PCI_DEVICE_ID_IVSHMEM),
	  (PCI_CLASS_OTHERS << 16) | IVSHM_PROTO_RPMSG, 0xffff00 },
	{ 0 }
};

MODULE_DEVICE_TABLE(pci, virtio_ivshmem_id_table);

static struct pci_driver virtio_ivshmem_driver = {
	.name		= DRV_NAME,
	.id_table	= virtio_ivshmem_id_table,
	.probe		= mcs_remoteproc_probe,
	.remove		= mcs_remoteproc_remove,
};

module_pci_driver(virtio_ivshmem_driver);

MODULE_LICENSE("GPL v2");
MODULE_DESCRIPTION("mcs remote processor control driver, support for ivshmem");
