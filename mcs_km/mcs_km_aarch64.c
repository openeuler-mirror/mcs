// SPDX-License-Identifier: GPL-2.0
/*
 * MCS aarch64 pedestal backend.
 *
 * This is the generic SMP AMP offload implementation: the remote core
 * is one of the AArch64 CPUs managed through PSCI, and kick/ack uses
 * the MCS per-CPU IPI.  It is the historical "baremetal" pedestal.
 */

#define pr_fmt(fmt) "mcs: " fmt

#include <linux/acpi.h>
#include <linux/arm-smccc.h>
#include <linux/cpumask.h>
#include <linux/interrupt.h>
#include <linux/irq.h>
#include <linux/kernel.h>
#include <linux/module.h>
#include <linux/of.h>
#include <linux/slab.h>
#include <linux/smp.h>
#include <linux/version.h>
#include <linux/wait.h>
#include <linux/workqueue.h>
#include <asm/cputype.h>
#include <asm/smp.h>

#ifdef CONFIG_KPROBES
#include <linux/kprobes.h>
#endif

#include "mcs_km_pedestal.h"

/**
 * PSCI Functions
 * For more detail see:
 * Arm Power State Coordination Interface Platform Design Document
 */
#define CPU_ON_FUNCID		0xC4000003
#define AFFINITY_INFO_FUNCID	0xC4000004
#define IPI_MCS			8

static struct mcs_rproc_mem aarch64_mem[RPROC_MEM_MAX];
static bool aarch64_irq_ready;

static int __percpu *mcs_evt;
static DECLARE_WAIT_QUEUE_HEAD(mcs_aarch64_wait_queue);
static atomic_t aarch64_irq_ack;
static struct work_struct aarch64_irq_wake_work;

static unsigned long rmem_base;
module_param(rmem_base, ulong, 0400);
MODULE_PARM_DESC(rmem_base, "The base address of aarch64 reserved mem");

static unsigned long rmem_size;
module_param(rmem_size, ulong, 0400);
MODULE_PARM_DESC(rmem_size, "The size of aarch64 reserved mem");

static int invoke_hvc = 1;

/**
 * make hvc/smc call
 * @return:
 *    CPU_ON_FUNCID:
 *	SUCCESS			0
 *	INVALID_PARAMETERS 	-2
 *	DENIED			-3
 *	ALREADY_ON		-4
 *	ON_PENDING		-5
 *	INTERNAL_FAILURE	-6
 *	INVALID_ADDRESS		-9
 *
 *    AFFINITY_INFO_FUNCID:
 *	ON			0
 *	OFF			1
 *	ON_PENDING		2
 *	INVALID_PARAMETERS 	-2
 *	DISABLED		-8
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

	if (!acpi_disabled) {
		/* For ACPI, only "smc" is supported */
		invoke_hvc = 0;
		return 0;
	}

	np = of_find_matching_node(NULL, psci_of_match);
	if (!np)
		return -ENODEV;

	if (!of_device_is_available(np)) {
		of_node_put(np);
		return -ENODEV;
	}

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

#ifdef CONFIG_KPROBES
static u64 (*cpu_logical_map_fn)(unsigned int cpu);

static int get_cpu_logical_map(void)
{
	int ret;
	struct kprobe probe = {
		.symbol_name = "cpu_logical_map",
	};

	ret = register_kprobe(&probe);
	if (ret < 0)
		return ret;

	cpu_logical_map_fn = (void *)probe.addr;
	unregister_kprobe(&probe);
	return 0;
}
#endif

/**
 * Enumerate the possible CPU set from __cpu_logical_map[]
 * and return the MPIDR values related to the @cpu.
 * If the @cpu is not found or the hwid is invalid, return INVALID_HWID.
 */
static u64 get_cpu_mpidr(u32 cpu)
{
	if (cpu >= NR_CPUS)
		return INVALID_HWID;

#ifdef CONFIG_KPROBES
	if (cpu_logical_map_fn != NULL)
		return cpu_logical_map_fn(cpu);

	return INVALID_HWID;
#else
	return cpu_logical_map(cpu);
#endif
}

static void aarch64_irq_wake_work_handler(struct work_struct *work)
{
	wake_up_interruptible(&mcs_aarch64_wait_queue);
}

static irqreturn_t handle_clientos_ipi(int irq, void *data)
{
	pr_info("received ipi from client os\n");
	atomic_set(&aarch64_irq_ack, 1);
	/*
	 * On PREEMPT_RT the wait queue lock is a sleeping lock.  The
	 * interrupted task may hold it inside poll_freewait(), so waking
	 * up directly from the hard IRQ would deadlock; defer it.
	 */
	schedule_work(&aarch64_irq_wake_work);
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

static int init_aarch64_irq(void)
{
	int err;
	struct irq_desc *desc;

#if LINUX_VERSION_CODE >= KERNEL_VERSION(6, 6, 0)
	desc = irq_data_to_desc(irq_get_irq_data(IPI_MCS));
#else
	desc = irq_to_desc(IPI_MCS);
#endif
	if (!desc) {
		pr_err("IRQ %d has no descriptor\n", IPI_MCS);
		return -ENODEV;
	}

	if (desc->action) {
		pr_err("IRQ %d is not free\n", IPI_MCS);
		return -EBUSY;
	}

	mcs_evt = alloc_percpu(int);
	if (!mcs_evt)
		return -ENOMEM;

	INIT_WORK(&aarch64_irq_wake_work, aarch64_irq_wake_work_handler);

	err = request_percpu_irq(IPI_MCS, handle_clientos_ipi, "MCS IPI", mcs_evt);
	if (err) {
		free_percpu(mcs_evt);
		mcs_evt = NULL;
		return err;
	}

	on_each_cpu(enable_mcs_ipi, NULL, 1);
	aarch64_irq_ready = true;

	return 0;
}

static void remove_aarch64_irq(void)
{
	if (!aarch64_irq_ready)
		return;

	on_each_cpu(disable_mcs_ipi, NULL, 1);
	free_percpu_irq(IPI_MCS, mcs_evt);
	cancel_work_sync(&aarch64_irq_wake_work);
	free_percpu(mcs_evt);
	mcs_evt = NULL;
	aarch64_irq_ready = false;
}

static void send_clientos_ipi(const struct cpumask *target)
{
	ipi_send_mask(IPI_MCS, target);
}

static int mcs_aarch64_init(void)
{
	int ret;

	ret = get_psci_method();
	if (ret) {
		pr_err("Failed to get psci \"method\" property, ret = %d\n", ret);
		return ret;
	}

#ifdef CONFIG_KPROBES
	ret = get_cpu_logical_map();
	if (ret) {
		pr_err("Failed to get cpu_logical_map symbol, ret = %d\n", ret);
		return ret;
	}
#endif

	ret = mcs_init_ped_rsv_mem("oe,mcs_remoteproc", aarch64_mem,
				   "mcs_aarch64_mem", true,
				   rmem_base, rmem_size);
	if (ret) {
		pr_err("Failed to get aarch64 mcs mem, ret = %d\n", ret);
		return ret;
	}

	ret = init_aarch64_irq();
	if (ret) {
		pr_err("Failed to init aarch64 mcs ipi, ret = %d\n", ret);
		mcs_release_ped_rsv_mem(aarch64_mem,
					rmem_base != 0 && rmem_size != 0);
		return ret;
	}

	return 0;
}

static void mcs_aarch64_exit(void)
{
	remove_aarch64_irq();
	mcs_release_ped_rsv_mem(aarch64_mem,
				rmem_base != 0 && rmem_size != 0);
}

static int mcs_aarch64_query_mem(struct mcs_rproc_mem *mem)
{
	if (aarch64_mem[0].phy_addr == 0) {
		pr_err("No aarch64 memory region available\n");
		return -ENODEV;
	}

	*mem = aarch64_mem[0];
	return 0;
}

static bool mcs_aarch64_mmap_allowed(phys_addr_t offset, size_t size)
{
	int i;

	for (i = 0; (i < RPROC_MEM_MAX) && (aarch64_mem[i].phy_addr != 0); i++) {
		if (offset >= aarch64_mem[i].phy_addr &&
		    offset + size <= aarch64_mem[i].phy_addr + aarch64_mem[i].size)
			return true;
	}

	return false;
}

static __poll_t mcs_aarch64_poll(struct file *file, poll_table *wait)
{
	__poll_t mask = 0;

	poll_wait(file, &mcs_aarch64_wait_queue, wait);
	if (atomic_cmpxchg(&aarch64_irq_ack, 1, 0) == 1)
		mask |= POLLIN | POLLRDNORM;

	return mask;
}

static int mcs_aarch64_send_ipi(const struct cpu_info *info)
{
	if (info->cpu >= nr_cpu_ids) {
		pr_err("invalid target cpu %d\n", info->cpu);
		return -EINVAL;
	}

	pr_info("received ioctl cmd to send ipi to cpu(%d)\n", info->cpu);
	send_clientos_ipi(cpumask_of(info->cpu));
	return 0;
}

static int mcs_aarch64_xpu_on(const struct cpu_info *info)
{
	u64 mpidr;
	int ret;

	mpidr = get_cpu_mpidr(info->cpu);
	if (mpidr == INVALID_HWID) {
		pr_err("boot clientos failed, invalid MPIDR\n");
		return -EINVAL;
	}

	pr_info("start booting clientos on cpu%d(%llx) addr(0x%llx)\n",
		info->cpu, mpidr, info->boot_addr);

	ret = invoke_psci_fn(CPU_ON_FUNCID, mpidr, info->boot_addr, 0);
	if (ret) {
		pr_err("boot clientos failed(%d)\n", ret);
		return -EINVAL;
	}

	return 0;
}

static int mcs_aarch64_affinity_info(const struct cpu_info *info)
{
	u64 mpidr;
	int ret;

	mpidr = get_cpu_mpidr(info->cpu);
	if (mpidr == INVALID_HWID) {
		pr_err("cpu state check failed! Invalid MPIDR\n");
		return -EINVAL;
	}

	ret = invoke_psci_fn(AFFINITY_INFO_FUNCID, mpidr, 0, 0);
	if (ret != 1) {
		pr_err("cpu state check failed! cpu(%d) is not in the OFF state, current state: %d\n",
		       info->cpu, ret);
		return -EFAULT;
	}

	return 0;
}

static int mcs_aarch64_copy_msg_mem(struct core_msg_mem_info *info)
{
	return mcs_copy_msg_mem(aarch64_mem, info);
}

const struct mcs_pedestal_ops mcs_aarch64_ops = {
	.name = "aarch64",
	.type = MCS_KM_PED_AARCH64,
	.init = mcs_aarch64_init,
	.exit = mcs_aarch64_exit,
	.query_mem = mcs_aarch64_query_mem,
	.mmap_allowed = mcs_aarch64_mmap_allowed,
	.poll = mcs_aarch64_poll,
	.send_ipi = mcs_aarch64_send_ipi,
	.xpu_on = mcs_aarch64_xpu_on,
	.xpu_off = NULL,
	.affinity_info = mcs_aarch64_affinity_info,
	.copy_msg_mem = mcs_aarch64_copy_msg_mem,
};
