// SPDX-License-Identifier: GPL-2.0
/*
 * MCS HiSilicon heterogeneous RISC-V pedestal backend.
 *
 * This backend drives the RISC-V remote core integrated in a HiSilicon
 * SoC.  The remote image is loaded by mica through the reserved memory
 * region; this driver only performs the boot register sequence and the
 * IPC interrupt handshake.
 */

#define pr_fmt(fmt) "mcs: " fmt

#include <linux/interrupt.h>
#include <linux/io.h>
#include <linux/irq.h>
#include <linux/kernel.h>
#include <linux/limits.h>
#include <linux/of.h>
#include <linux/of_address.h>
#include <linux/of_irq.h>
#include <linux/poll.h>
#include <linux/wait.h>

#include "mcs_km_pedestal.h"

/* RISCV interrupt */
#define IPC_INT_SET		(0x00)
#define IPC_INT_CLEAR		(0x04)
#define IPC_INT_MSTS		(0x08)
#define IPC_INT_MASK		(0x0C)
#define IPC_INT_RSTS		(0x10)

#define IPC_INT_A55MP_NUM	0x0
#define IPC_INT_RISCV_NUM	0x6

#define RISCV_REG_SIZE		0x100

static void __iomem *riscv_int_base;
static int riscv_int_irq;

static struct mcs_rproc_mem hisi_riscv_mem[RPROC_MEM_MAX];
static DECLARE_WAIT_QUEUE_HEAD(mcs_hisi_riscv_wait_queue);
static atomic_t hisi_riscv_irq_ack;

static irqreturn_t handle_riscv_irq(int irq, void *data)
{
	pr_info("received ipi from riscv\n");

	/* mask irq */
	writel(IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_MASK);

	/* clear irq */
	writel(IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_CLEAR);

	atomic_set(&hisi_riscv_irq_ack, 1);
	wake_up_interruptible(&mcs_hisi_riscv_wait_queue);

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
		ret = -ENXIO;
		goto err_unmap;
	}

	/* clear irq */
	writel(IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_CLEAR);
	/* unmask irq */
	writel(0x80000000 + IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_MASK);
	ret = request_irq(riscv_int_irq, handle_riscv_irq, 0,
			  "MCS RISCV IRQ", NULL);
	if (ret) {
		pr_err("Failed to request irq %d, error: %d\n", riscv_int_irq, ret);
		goto err_unmap;
	}

	pr_info("MCS RISCV interrupt registered successfully. irq: %d\n",
		riscv_int_irq);

	return 0;

err_unmap:
	iounmap(riscv_int_base);
	riscv_int_base = NULL;
	riscv_int_irq = 0;
	return ret;
}

static void remove_riscv_irq(void)
{
	if (!riscv_int_irq)
		return;

	free_irq(riscv_int_irq, NULL);
	/* clear irq */
	writel(IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_CLEAR);
	/* unmask irq */
	writel(0x80000000 + IPC_INT_A55MP_NUM, riscv_int_base + IPC_INT_MASK);
	iounmap(riscv_int_base);
	riscv_int_base = NULL;
	riscv_int_irq = 0;
}

static void send_riscv_interrupt(void)
{
	writel(IPC_INT_RISCV_NUM, riscv_int_base + IPC_INT_SET);
}

static int boot_riscv(u64 boot_addr)
{
	int ret = -1;
	int index;
	unsigned int phy_addr[4] = {0x110D2004, 0x11024000, 0x11016400, 0x110D2000};
	void __iomem *v_addr[4] = {NULL, NULL, NULL, NULL};

	pr_info("Booting clientos on RISCV at 0x%llx ...\n", boot_addr);

	if (boot_addr > U32_MAX) {
		pr_err("boot address 0x%llx does not fit the 32-bit boot register\n",
		       boot_addr);
		return -EINVAL;
	}

	for (index = 0; index < 4; index++) {
		v_addr[index] = ioremap(phy_addr[index], RISCV_REG_SIZE);
		if (!v_addr[index]) {
			pr_err("ioremap failed for address 0x%x\n", phy_addr[index]);
			goto iounmap_out;
		}
	}

	writel((u32)boot_addr, v_addr[0]); /* set start addr */
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

static int mcs_hisi_riscv_init(void)
{
	int ret;

	ret = mcs_init_ped_rsv_mem("oe,mcs_riscv_remoteproc", hisi_riscv_mem,
				   "mcs_hisi_riscv_mem", false, 0, 0);
	if (ret) {
		pr_err("Failed to get hisi riscv mcs mem, ret = %d\n", ret);
		return ret;
	}

	ret = init_riscv_irq();
	if (ret) {
		pr_err("Failed to init hisi riscv mcs ipi, ret = %d\n", ret);
		mcs_release_ped_rsv_mem(hisi_riscv_mem, false);
		return ret;
	}

	return 0;
}

static void mcs_hisi_riscv_exit(void)
{
	remove_riscv_irq();
	mcs_release_ped_rsv_mem(hisi_riscv_mem, false);
}

static int mcs_hisi_riscv_query_mem(struct mcs_rproc_mem *mem)
{
	if (hisi_riscv_mem[0].phy_addr == 0) {
		pr_err("No hisi riscv memory region available\n");
		return -ENODEV;
	}

	*mem = hisi_riscv_mem[0];
	return 0;
}

static bool mcs_hisi_riscv_mmap_allowed(phys_addr_t offset, size_t size)
{
	int i;

	for (i = 0; (i < RPROC_MEM_MAX) && (hisi_riscv_mem[i].phy_addr != 0); i++) {
		if (offset >= hisi_riscv_mem[i].phy_addr &&
		    offset + size <= hisi_riscv_mem[i].phy_addr + hisi_riscv_mem[i].size)
			return true;
	}

	return false;
}

static __poll_t mcs_hisi_riscv_poll(struct file *file, poll_table *wait)
{
	__poll_t mask = 0;

	poll_wait(file, &mcs_hisi_riscv_wait_queue, wait);
	if (atomic_cmpxchg(&hisi_riscv_irq_ack, 1, 0) == 1)
		mask |= POLLIN | POLLRDNORM;

	return mask;
}

static int mcs_hisi_riscv_send_ipi(const struct cpu_info *info)
{
	pr_info("received ioctl cmd to send riscv interrupt to mcu\n");
	send_riscv_interrupt();
	return 0;
}

static int mcs_hisi_riscv_xpu_on(const struct cpu_info *info)
{
	int ret;

	pr_info("received ioctl cmd to boot riscv clientos\n");
	ret = boot_riscv(info->boot_addr);
	if (ret) {
		pr_err("boot riscv failed(%d)\n", ret);
		return -EINVAL;
	}

	return 0;
}

static int mcs_hisi_riscv_copy_msg_mem(struct core_msg_mem_info *info)
{
	return mcs_copy_msg_mem(hisi_riscv_mem, info);
}

const struct mcs_pedestal_ops mcs_hisi_riscv_ops = {
	.name = "hisi-riscv",
	.type = MCS_KM_PED_HISI_RISCV,
	.init = mcs_hisi_riscv_init,
	.exit = mcs_hisi_riscv_exit,
	.query_mem = mcs_hisi_riscv_query_mem,
	.mmap_allowed = mcs_hisi_riscv_mmap_allowed,
	.poll = mcs_hisi_riscv_poll,
	.send_ipi = mcs_hisi_riscv_send_ipi,
	.xpu_on = mcs_hisi_riscv_xpu_on,
	.xpu_off = NULL,
	.affinity_info = NULL,
	.copy_msg_mem = mcs_hisi_riscv_copy_msg_mem,
};
