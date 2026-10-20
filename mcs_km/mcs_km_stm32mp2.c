// SPDX-License-Identifier: GPL-2.0
/*
 * MCS STM32MP2(M33) pedestal backend.
 *
 * The M33 is controlled through SCMI reset lines and the NSVTOR boot
 * vector, while vring notifications use the IPCC mailbox channels.
 */

#define pr_fmt(fmt) "mcs: " fmt

#include <linux/interrupt.h>
#include <linux/kernel.h>
#include <linux/mailbox_client.h>
#include <linux/mfd/syscon.h>
#include <linux/of.h>
#include <linux/of_platform.h>
#include <linux/platform_device.h>
#include <linux/poll.h>
#include <linux/regmap.h>
#include <linux/reset.h>
#include <linux/wait.h>

#include "mcs_km_pedestal.h"

/* STM32MP2(M33) virtqueues over IPCC */
#define STM32MP2_MAX_VQ		2

static const char * const stm32mp2_mbox_names[STM32MP2_MAX_VQ] = { "vq0", "vq1" };
static struct platform_device *stm32mp2_pdev;
static bool stm32mp2_pdev_owned;
static struct mbox_client stm32mp2_mbox_cl[STM32MP2_MAX_VQ];
static struct mbox_chan *stm32mp2_mbox_ch[STM32MP2_MAX_VQ];
static struct reset_control *stm32mp2_mcu_rst;
static struct reset_control *stm32mp2_hold_boot_rst;
static struct regmap *stm32mp2_nsvtor_map;
static u32 stm32mp2_nsvtor_offset;
static u32 stm32mp2_nsvtor_mask;
static bool stm32mp2_running;
static DECLARE_WAIT_QUEUE_HEAD(mcs_stm32mp2_wait_queue);
static atomic_t stm32mp2_irq_ack[STM32MP2_MAX_VQ];

static struct mcs_rproc_mem stm32mp2_mem[RPROC_MEM_MAX];

/*
 * The IPCC rx callback is invoked either in the threaded irq context or
 * from the ipcc workqueue, depending on the high priority flag of the
 * mbox channel described in the device tree.
 */
static void handle_stm32mp2_irq(struct mbox_client *cl, void *msg)
{
	int vq = cl - stm32mp2_mbox_cl;

	if (WARN_ON(vq < 0 || vq >= STM32MP2_MAX_VQ))
		return;

	pr_info("received kick from M33 vq%d\n", vq);

	atomic_set(&stm32mp2_irq_ack[vq], 1);
	wake_up_interruptible(&mcs_stm32mp2_wait_queue);
}

static void free_stm32mp2_channels(void)
{
	int i;

	for (i = 0; i < STM32MP2_MAX_VQ; i++) {
		if (stm32mp2_mbox_ch[i]) {
			mbox_free_channel(stm32mp2_mbox_ch[i]);
			stm32mp2_mbox_ch[i] = NULL;
		}
	}
}

static int init_stm32mp2(void)
{
	struct device_node *np;
	struct reset_control *mcu_rst, *hold_boot_rst;
	struct platform_device *pdev = NULL;
	struct regmap *map;
	unsigned int args[2];
	int i, ret;

	np = of_find_compatible_node(NULL, NULL, "oe,mcs_stm32mp2_remoteproc");
	if (!np) {
		pr_info("oe,mcs_stm32mp2_remoteproc node not found in DTS\n");
		return -ENODEV;
	}

	mcu_rst = of_reset_control_get_exclusive(np, "mcu_rst");
	if (IS_ERR(mcu_rst)) {
		ret = PTR_ERR(mcu_rst);
		pr_err("unable to get mcu_rst: %d\n", ret);
		goto out_put_np;
	}

	hold_boot_rst = of_reset_control_get_exclusive(np, "hold_boot");
	if (IS_ERR(hold_boot_rst)) {
		ret = PTR_ERR(hold_boot_rst);
		pr_err("unable to get hold_boot: %d\n", ret);
		goto out_put_mcu_rst;
	}

	/*
	 * Get the NSVTOR register description (syscon phandle, offset and mask),
	 * the same way as the linux stm32_rproc driver does.
	 */
	map = syscon_regmap_lookup_by_phandle_args(np, "st,syscfg-nsvtor", 2, args);
	if (IS_ERR(map)) {
		ret = PTR_ERR(map);
		pr_err("unable to get nsvtor syscon: %d\n", ret);
		goto out_put_hold_boot;
	}

	/*
	 * The device is only used as mbox client for the ipcc channels.
	 * of_platform_default_populate() usually creates it at boot, in which
	 * case of_platform_device_create() fails with OF_POPULATED set, so
	 * reuse the existing device and only create our own if it is absent.
	 */
	pdev = of_find_device_by_node(np);
	if (pdev) {
		stm32mp2_pdev_owned = false;
	} else {
		pdev = of_platform_device_create(np, NULL, NULL);
		stm32mp2_pdev_owned = true;
	}
	if (!pdev) {
		ret = -ENODEV;
		pr_err("unable to create device for mcs stm32mp2\n");
		goto out_put_hold_boot;
	}

	for (i = 0; i < STM32MP2_MAX_VQ; i++) {
		struct mbox_client *cl = &stm32mp2_mbox_cl[i];

		cl->dev = &pdev->dev;
		cl->rx_callback = handle_stm32mp2_irq;
		cl->tx_block = false;
		cl->tx_tout = 0;
		cl->knows_txdone = false;

		stm32mp2_mbox_ch[i] = mbox_request_channel_byname(cl, stm32mp2_mbox_names[i]);
		if (IS_ERR(stm32mp2_mbox_ch[i])) {
			ret = PTR_ERR(stm32mp2_mbox_ch[i]);
			stm32mp2_mbox_ch[i] = NULL;
			pr_err("unable to request mbox %s: %d\n", stm32mp2_mbox_names[i], ret);
			goto out_free_chans;
		}
	}

	stm32mp2_mcu_rst = mcu_rst;
	stm32mp2_hold_boot_rst = hold_boot_rst;
	stm32mp2_nsvtor_map = map;
	stm32mp2_nsvtor_offset = args[0];
	stm32mp2_nsvtor_mask = args[1];
	stm32mp2_pdev = pdev;

	of_node_put(np);
	pr_info("STM32MP2(M33) resources initialized\n");
	return 0;

out_free_chans:
	free_stm32mp2_channels();
	if (stm32mp2_pdev_owned)
		platform_device_unregister(pdev);
	else
		platform_device_put(pdev);
out_put_hold_boot:
	reset_control_put(hold_boot_rst);
out_put_mcu_rst:
	reset_control_put(mcu_rst);
out_put_np:
	of_node_put(np);
	return ret;
}

static void remove_stm32mp2(void)
{
	free_stm32mp2_channels();

	if (stm32mp2_pdev) {
		if (stm32mp2_pdev_owned)
			platform_device_unregister(stm32mp2_pdev);
		else
			platform_device_put(stm32mp2_pdev);
		stm32mp2_pdev = NULL;
	}

	reset_control_put(stm32mp2_hold_boot_rst);
	reset_control_put(stm32mp2_mcu_rst);
	stm32mp2_hold_boot_rst = NULL;
	stm32mp2_mcu_rst = NULL;
	stm32mp2_nsvtor_map = NULL;
}

static int boot_stm32mp2(u64 boot_addr)
{
	int ret;

	if (!stm32mp2_nsvtor_map || !stm32mp2_hold_boot_rst)
		return -ENODEV;

	pr_info("Booting clientos on STM32MP2(M33) at 0x%llx ...\n", boot_addr);

	/* the boot address must be aligned on the nsvtor mask */
	if (boot_addr & ~(u64)stm32mp2_nsvtor_mask) {
		pr_err("boot address 0x%llx is not aligned on mask 0x%x\n",
		       boot_addr, stm32mp2_nsvtor_mask);
		return -EINVAL;
	}

	/* configure the boot vector register with the entry of the ELF */
	ret = regmap_update_bits(stm32mp2_nsvtor_map, stm32mp2_nsvtor_offset,
				 stm32mp2_nsvtor_mask, (u32)boot_addr);
	if (ret) {
		pr_err("failed to set nsvtor boot vector(%d)\n", ret);
		return ret;
	}

	/*
	 * Release then hold the boot again to start the M33,
	 * the same sequence as the linux stm32_rproc_start().
	 */
	ret = reset_control_deassert(stm32mp2_hold_boot_rst);
	if (ret) {
		pr_err("failed to deassert hold_boot(%d)\n", ret);
		return ret;
	}

	ret = reset_control_assert(stm32mp2_hold_boot_rst);
	if (ret) {
		pr_err("failed to assert hold_boot(%d)\n", ret);
		return ret;
	}

	stm32mp2_running = true;
	return 0;
}

static int stop_stm32mp2(void)
{
	int ret;

	if (!stm32mp2_hold_boot_rst || !stm32mp2_mcu_rst)
		return -ENODEV;

	pr_info("Stopping clientos on STM32MP2(M33) ...\n");

	/*
	 * Hold the boot then reset the M33 core,
	 * the same sequence as the linux stm32_rproc_stop().
	 */
	ret = reset_control_assert(stm32mp2_hold_boot_rst);
	if (ret) {
		pr_err("failed to assert hold_boot(%d)\n", ret);
		return ret;
	}

	ret = reset_control_assert(stm32mp2_mcu_rst);
	if (ret) {
		pr_err("failed to assert mcu_rst(%d)\n", ret);
		return ret;
	}

	stm32mp2_running = false;
	return 0;
}

static int kick_stm32mp2(u32 vq_id)
{
	int ret;

	if (vq_id >= STM32MP2_MAX_VQ || !stm32mp2_mbox_ch[vq_id]) {
		pr_err("invalid vq id %d\n", vq_id);
		return -EINVAL;
	}

	ret = mbox_send_message(stm32mp2_mbox_ch[vq_id], NULL);
	if (ret < 0) {
		pr_err("failed to kick M33 vq%d(%d)\n", vq_id, ret);
		return ret;
	}

	return 0;
}

static int mcs_stm32mp2_init(void)
{
	int ret;

	ret = mcs_init_ped_rsv_mem("oe,mcs_stm32mp2_remoteproc", stm32mp2_mem,
				   "mcs_stm32mp2_mem", false, 0, 0);
	if (ret) {
		pr_err("Failed to get stm32mp2 mcs mem, ret = %d\n", ret);
		return ret;
	}

	ret = init_stm32mp2();
	if (ret) {
		pr_err("Failed to init stm32mp2 resources, ret = %d\n", ret);
		mcs_release_ped_rsv_mem(stm32mp2_mem, false);
		return ret;
	}

	return 0;
}

static void mcs_stm32mp2_exit(void)
{
	remove_stm32mp2();
	mcs_release_ped_rsv_mem(stm32mp2_mem, false);
}

static int mcs_stm32mp2_query_mem(struct mcs_rproc_mem *mem)
{
	if (stm32mp2_mem[0].phy_addr == 0) {
		pr_err("No stm32mp2 memory region available\n");
		return -ENODEV;
	}

	*mem = stm32mp2_mem[0];
	return 0;
}

static bool mcs_stm32mp2_mmap_allowed(phys_addr_t offset, size_t size)
{
	int i;

	for (i = 0; (i < RPROC_MEM_MAX) && (stm32mp2_mem[i].phy_addr != 0); i++) {
		if (offset >= stm32mp2_mem[i].phy_addr &&
		    offset + size <= stm32mp2_mem[i].phy_addr + stm32mp2_mem[i].size)
			return true;
	}

	return false;
}

static __poll_t mcs_stm32mp2_poll(struct file *file, poll_table *wait)
{
	__poll_t mask = 0;
	int i;

	poll_wait(file, &mcs_stm32mp2_wait_queue, wait);
	for (i = 0; i < STM32MP2_MAX_VQ; i++) {
		if (atomic_cmpxchg(&stm32mp2_irq_ack[i], 1, 0) == 1)
			mask |= POLLIN | POLLRDNORM;
	}

	return mask;
}

static int mcs_stm32mp2_send_ipi(const struct cpu_info *info)
{
	pr_info("received ioctl cmd to kick M33 vq%d\n", info->cpu);
	return kick_stm32mp2(info->cpu);
}

static int mcs_stm32mp2_xpu_on(const struct cpu_info *info)
{
	int ret;

	pr_info("received ioctl cmd to boot stm32mp2 clientos\n");
	ret = boot_stm32mp2(info->boot_addr);
	if (ret) {
		pr_err("boot stm32mp2 failed(%d)\n", ret);
		return ret;
	}

	return 0;
}

static int mcs_stm32mp2_xpu_off(void)
{
	int ret;

	pr_info("received ioctl cmd to stop stm32mp2 clientos\n");
	ret = stop_stm32mp2();
	if (ret) {
		pr_err("stop stm32mp2 failed(%d)\n", ret);
		return ret;
	}

	return 0;
}

static int mcs_stm32mp2_affinity_info(const struct cpu_info *info)
{
	if (stm32mp2_running) {
		pr_info("M33 is not in the OFF state\n");
		return -EFAULT;
	}

	return 0;
}

const struct mcs_pedestal_ops mcs_stm32mp2_ops = {
	.name = "stm32mp2",
	.type = MCS_KM_PED_STM32MP2,
	.init = mcs_stm32mp2_init,
	.exit = mcs_stm32mp2_exit,
	.query_mem = mcs_stm32mp2_query_mem,
	.mmap_allowed = mcs_stm32mp2_mmap_allowed,
	.poll = mcs_stm32mp2_poll,
	.send_ipi = mcs_stm32mp2_send_ipi,
	.xpu_on = mcs_stm32mp2_xpu_on,
	.xpu_off = mcs_stm32mp2_xpu_off,
	.affinity_info = mcs_stm32mp2_affinity_info,
	.copy_msg_mem = NULL,
};
