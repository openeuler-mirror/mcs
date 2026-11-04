// SPDX-License-Identifier: GPL-2.0
/*
 * Common helpers shared by the MCS pedestal backends.
 */

#define pr_fmt(fmt) "mcs: " fmt

#include <linux/ioport.h>
#include <linux/kernel.h>
#include <linux/of.h>
#include <linux/of_address.h>
#include <linux/string.h>

#include "mcs_km_pedestal.h"

int mcs_init_ped_rsv_mem(const char *compatible_node,
			 struct mcs_rproc_mem *mem,
			 const char *mem_region_name,
			 bool support_cmdline,
			 unsigned long rmem_base,
			 unsigned long rmem_size)
{
	int n = 0;
	int i, count, ret = 0;
	struct device_node *np;

	if (support_cmdline && rmem_base != 0 && rmem_size != 0) {
		pr_info("assign memory region for %s mcs: 0x%lx - 0x%lx (%ld MB)\n",
			mem_region_name, rmem_base, rmem_base + rmem_size - 1,
			rmem_size >> 20);

		mem[0].phy_addr = rmem_base;
		mem[0].size = rmem_size;
		return 0;
	}

	np = of_find_compatible_node(NULL, NULL, compatible_node);
	if (np == NULL) {
		pr_info("%s node not found in DTS\n", compatible_node);
		return -ENODEV;
	}

	count = of_count_phandle_with_args(np, "memory-region", NULL);
	if (count <= 0) {
		pr_info("reserved mem is required for %s\n", mem_region_name);
		ret = -ENODEV;
		goto out;
	}

	for (i = 0; i < count; i++) {
		struct device_node *node;
		struct resource res;

		node = of_parse_phandle(np, "memory-region", i);
		if (!node) {
			pr_err("unable to parse %s memory region\n", mem_region_name);
			ret = -ENODEV;
			goto out;
		}

		ret = of_address_to_resource(node, 0, &res);
		of_node_put(node);
		if (ret) {
			pr_err("unable to resolve %s memory region\n", mem_region_name);
			goto out;
		}

		if (n >= RPROC_MEM_MAX)
			break;

		if (!request_mem_region(res.start, resource_size(&res),
					mem_region_name)) {
			pr_err("Can not request %s, 0x%llx-0x%llx\n",
			       mem_region_name, res.start, res.end);
			ret = -EINVAL;
			goto err_release;
		}

		mem[n].phy_addr = res.start;
		mem[n].size = resource_size(&res);
		n++;
	}

out:
	of_node_put(np);
	return ret;

err_release:
	while (n > 0) {
		n--;
		release_mem_region(mem[n].phy_addr, mem[n].size);
		mem[n].phy_addr = 0;
		mem[n].size = 0;
	}
	goto out;
}

void mcs_release_ped_rsv_mem(struct mcs_rproc_mem *mem, bool skip_release)
{
	int i;

	if (skip_release)
		return;

	for (i = 0; i < RPROC_MEM_MAX && mem[i].phy_addr != 0; i++) {
		release_mem_region(mem[i].phy_addr, mem[i].size);
		mem[i].phy_addr = 0;
		mem[i].size = 0;
	}
}

int mcs_copy_msg_mem(struct mcs_rproc_mem *mem,
		     struct core_msg_mem_info *info)
{
	unsigned long offset;

	if (info->instance_id > RPROC_MEM_MAX) {
		pr_err("GET_COPY_MSG_MEM failed: The required instance_id max to %d, your instance_id:%d\n",
		       RPROC_MEM_MAX, info->instance_id);
		return -EINVAL;
	}

	if (mem[0].phy_addr == 0) {
		pr_err("No mcs memory region available\n");
		return -ENODEV;
	}

	/* 使用2M。1M 用来发送，1M用来接收，尾部1M gap */
	offset = info->instance_id * INSTANCE_SIZE +
		 OPENAMP_SHM_SIZE - OPENAMP_SHM_COPY_SIZE * 3;
	info->phy_addr = mem[0].phy_addr + offset;
	if (info->phy_addr > mem[0].phy_addr + mem[0].size) {
		pr_err("GET_COPY_MSG_MEM failed: The required memory is out of mcs reserved memory, instance_id:%d\n",
		       info->instance_id);
		return -EINVAL;
	}

	info->size = OPENAMP_SHM_COPY_SIZE * 2;	/* 1M 发送，1M 接收 */
	return 0;
}
