/*
 * Copyright (c) KylinSoft 2026. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <elf.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <pthread.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <syslog.h>
#include <unistd.h>
#include <metal/alloc.h>
#include <metal/io.h>
#include <openamp/remoteproc.h>

#include <memory/shm_pool.h>
#include <remoteproc/remoteproc_module.h>
#include <remoteproc/mica_rsc.h>

struct cpu_info {
	uint32_t cpu;
	uint64_t boot_addr;
};

struct mem_info {
	uint64_t phy_addr;
	uint64_t size;
};

#define PIPE_READ_END  0
#define PIPE_WRITE_END 1

#define MCS_DEVICE_NAME	"/dev/mcs"

/* Number of virtqueues, which is also the number of IPCC channels. */
#define STM32MP2_VQ_NUM	2

#define MAGIC_NUMBER		'A'
#define IOC_SENDIPI        _IOW(MAGIC_NUMBER, 0, int)
#define IOC_MCUON          _IOW(MAGIC_NUMBER, 1, int) /* IOC_XPUON in mcs_km */
#define IOC_QUERY_MEM      _IOW(MAGIC_NUMBER, 3, int)
#define IOC_SET_PED_TYPE   _IOW(MAGIC_NUMBER, 5, int)
#define IOC_MCUOFF         _IOW(MAGIC_NUMBER, 6, int)

/*
 * struct rproc_pdata - rproc private data
 */
struct rproc_pdata {
	int mcs_fd;
	int pipe_fd[2];
	pthread_t notifier_thread;
	bool notifier_started;
};

static struct remoteproc *rproc_init(struct remoteproc *rproc,
				     const struct remoteproc_ops *ops, void *arg)
{
	int ret;
	struct mica_client *client = arg;
	struct rproc_pdata *pdata;
	int ped_type = MCS_KM_PED_STM32MP2;

	if (!client)
		return NULL;

	pdata = calloc(1, sizeof(*pdata));
	if (!pdata)
		return NULL;

	pdata->mcs_fd = -1;
	pdata->pipe_fd[PIPE_READ_END] = -1;
	pdata->pipe_fd[PIPE_WRITE_END] = -1;

	/* open mcs device for rproc->ops */
	pdata->mcs_fd = open(MCS_DEVICE_NAME, O_RDWR | O_SYNC);
	if (pdata->mcs_fd < 0) {
		syslog(LOG_ERR, "open %s device failed, err %d\n", MCS_DEVICE_NAME, pdata->mcs_fd);
		goto err_malloc;
	}

	ret = ioctl(pdata->mcs_fd, IOC_SET_PED_TYPE, &ped_type);
	if (ret) {
		syslog(LOG_ERR, "IOC_SET_PED_TYPE failed, err %d\n", ret);
		goto err_mcs_fd;
	}

	client->ped_ops = NULL;
	rproc->priv = pdata;
	rproc->ops = ops;

	return rproc;

err_mcs_fd:
	close(pdata->mcs_fd);
err_malloc:
	free(pdata);
	return NULL;
}

static void *rproc_mmap(struct remoteproc *rproc,
			metal_phys_addr_t *pa, metal_phys_addr_t *da,
			size_t size, unsigned int attribute,
			struct metal_io_region **io)
{
	void *va;
	struct rproc_pdata *pdata = rproc->priv;
	struct remoteproc_mem *mem;
	metal_phys_addr_t lpa, lda, aligned_addr, offset;
	struct metal_io_region *tmpio;
	size_t pagesize, aligned_size;

	lpa = *pa;
	lda = *da;
	if (lpa == METAL_BAD_PHYS && lda == METAL_BAD_PHYS)
		return NULL;
	if (lpa == METAL_BAD_PHYS)
		lpa = lda;
	if (lda == METAL_BAD_PHYS)
		lda = lpa;

	/* align to page boundary */
	pagesize = sysconf(_SC_PAGE_SIZE);
	aligned_addr = (lpa) & ~(pagesize - 1);
	offset = lpa - aligned_addr;
	aligned_size = (offset + size + pagesize - 1) & ~(pagesize - 1);

	va = mmap(NULL, aligned_size, PROT_READ | PROT_WRITE, MAP_SHARED, pdata->mcs_fd, aligned_addr);
	if (va == MAP_FAILED) {
		syslog(LOG_ERR, "mmap(0x%lx-0x%lx) failed: %s\n",
			aligned_addr, aligned_addr + aligned_size, strerror(errno));
		return NULL;
	}

	mem = metal_allocate_memory(sizeof(*mem));
	if (!mem)
		goto err_unmap;
	tmpio = metal_allocate_memory(sizeof(*tmpio));
	if (!tmpio) {
		metal_free_memory(mem);
		goto err_unmap;
	}
	remoteproc_init_mem(mem, NULL, lpa, lda, size, tmpio);
	metal_io_init(tmpio, va + offset, &mem->pa, size, -1, attribute, NULL);
	remoteproc_add_mem(rproc, mem);
	*pa = lpa;
	*da = lda;
	if (io)
		*io = tmpio;

	DEBUG_PRINT("mmap succeeded, paddr: 0x%lx, vaddr: %p, size 0x%lx\n",
		     (unsigned long)mem->pa, va + offset, size);
	return metal_io_phys_to_virt(tmpio, mem->pa);

err_unmap:
	munmap(va, aligned_size);
	return NULL;
}

static void get_mmap_addr_info(struct metal_io_region *io, void **virt, size_t *aligned_size)
{
	size_t pagesize;
	metal_phys_addr_t aligned_addr, offset;
	metal_phys_addr_t phy;

	phy = *(io->physmap);
	pagesize = sysconf(_SC_PAGE_SIZE);
	aligned_addr = (phy) & ~(pagesize - 1);
	offset = phy - aligned_addr;
	*aligned_size = (offset + io->size + pagesize - 1) & ~(pagesize - 1);
	*virt = io->virt - offset;
}

static int rproc_config(struct remoteproc *rproc, void *data)
{
	int ret;
	struct rproc_pdata *pdata = rproc->priv;
	struct mica_client *client = metal_container_of(rproc, struct mica_client, rproc);
	struct mem_info info;

	/*
	 * The image restore is not used here: client runtime like Zephyr (openamp_rsc_table
	 * sample) uses a static resource table inside its firmware ELF image,
	 * which is mapped and updated in place by the following
	 * remoteproc_load() step.
	 */
	(void)data;

	/*
	 * Call rproc->ops->mmap to create shared memory io
	 */
	ret = ioctl(pdata->mcs_fd, IOC_QUERY_MEM, &info);
	if (ret < 0) {
		syslog(LOG_ERR, "unable to get shared memory information from mcs device, err: %d\n", ret);
		return ret;
	}

	ret = init_shmem_pool(client, info.phy_addr, info.size);
	if (ret) {
		syslog(LOG_ERR, "init shared memory pool failed, err %d\n", ret);
		return ret;
	}

	client->shmem_dynamic = false;

	return 0;
}

/*
 * Listen to events sent from the remote
 */
static void *rproc_wait_event(void *arg)
{
	int ret;
	struct remoteproc *rproc = arg;
	struct rproc_pdata *pdata = rproc->priv;
	struct pollfd fds[2];

	fds[0].fd = pdata->mcs_fd;
	fds[0].events = POLLIN;
	fds[1].fd = pdata->pipe_fd[PIPE_READ_END];
	fds[1].events = POLLIN;

	while (1) {
		ret = poll(fds, 2, -1);
		if (ret == -1) {
			syslog(LOG_ERR, "%s failed: %s\n", __func__, strerror(errno));
			break;
		}

		if (fds[0].revents & POLLIN) {
			/*
			 * The M33 notifies the master through the IPCC channels
			 * without indicating which virtqueue has changed,
			 * process all the virtqueues as the in-kernel
			 * rproc_virtio does.
			 */
			remoteproc_get_notification(rproc, RSC_NOTIFY_ID_ANY);
		}

		/* pipe fd used to exit the wait_event */
		if (fds[1].revents & POLLIN)
			break;
	}

	pthread_exit(NULL);
}

static int rproc_register_notifier(struct remoteproc *rproc)
{
	struct rproc_pdata *pdata = rproc->priv;
	int ret;

	ret = pthread_create(&pdata->notifier_thread, NULL, rproc_wait_event, rproc);
	if (ret)
		return ret;

	pdata->notifier_started = true;
	return 0;
}

static void rproc_stop_notifier(struct rproc_pdata *pdata)
{
	char token = 0;

	if (!pdata->notifier_started)
		return;

	if (write(pdata->pipe_fd[PIPE_WRITE_END], &token, sizeof(token)) < 0)
		syslog(LOG_WARNING, "unable to wake up the notifier: %s\n", strerror(errno));

	pthread_join(pdata->notifier_thread, NULL);
	pdata->notifier_started = false;

	close(pdata->pipe_fd[PIPE_READ_END]);
	close(pdata->pipe_fd[PIPE_WRITE_END]);
	pdata->pipe_fd[PIPE_READ_END] = -1;
	pdata->pipe_fd[PIPE_WRITE_END] = -1;
}

/*
 * The M33 boots from its vector table: NSVTOR must hold the address of the
 * .isr_vectors section, while the ELF entry point is the (Thumb) reset
 * handler and is not necessarily 128-byte aligned. Read the section address
 * the same way as the in-kernel stm32_rproc_get_vect_table().
 */
static uint64_t stm32mp2_get_boot_addr(const char *path, uint64_t fallback)
{
	Elf32_Ehdr ehdr;
	Elf32_Shdr shdr;
	Elf32_Shdr shstr;
	char *names = NULL;
	uint64_t addr = fallback;
	FILE *fp;
	int i;

	fp = fopen(path, "rb");
	if (!fp)
		return fallback;

	if (fread(&ehdr, sizeof(ehdr), 1, fp) != 1 ||
	    memcmp(ehdr.e_ident, ELFMAG, SELFMAG) != 0 ||
	    ehdr.e_ident[EI_CLASS] != ELFCLASS32 ||
	    !ehdr.e_shoff || !ehdr.e_shnum || !ehdr.e_shstrndx)
		goto out;

	if (fseek(fp, ehdr.e_shoff + ehdr.e_shstrndx * sizeof(shdr), SEEK_SET) ||
	    fread(&shstr, sizeof(shstr), 1, fp) != 1 ||
	    !shstr.sh_size || shstr.sh_size > 64 * 1024)
		goto out;

	names = malloc(shstr.sh_size);
	if (!names)
		goto out;
	if (fseek(fp, shstr.sh_offset, SEEK_SET) ||
	    fread(names, 1, shstr.sh_size, fp) != shstr.sh_size)
		goto out;

	for (i = 0; i < ehdr.e_shnum; i++) {
		if (fseek(fp, ehdr.e_shoff + i * sizeof(shdr), SEEK_SET) ||
		    fread(&shdr, sizeof(shdr), 1, fp) != 1)
			break;
		if (shdr.sh_name >= shstr.sh_size)
			continue;
		if (!strcmp(names + shdr.sh_name, ".isr_vectors")) {
			addr = shdr.sh_addr;
			break;
		}
	}

out:
	free(names);
	fclose(fp);
	return addr;
}

static int rproc_start(struct remoteproc *rproc)
{
	int ret;
	struct rproc_pdata *pdata = rproc->priv;
	struct mica_client *client = metal_container_of(rproc, struct mica_client, rproc);
	struct cpu_info info = {
		.cpu = 0,
		.boot_addr = stm32mp2_get_boot_addr(client->path, rproc->bootaddr),
	};

	/* a retried start must not leak the waiter of the failed attempt */
	rproc_stop_notifier(pdata);

	/* create the pipe before the waiter, which only owns its read side */
	ret = pipe(pdata->pipe_fd);
	if (ret) {
		ret = -errno;
		syslog(LOG_ERR, "unable to create pipe for notifier: %s\n", strerror(errno));
		return ret;
	}

	/* set up the notification waiter */
	ret = rproc_register_notifier(rproc);
	if (ret) {
		syslog(LOG_ERR, "unable to register notifier, err: %d\n", ret);
		close(pdata->pipe_fd[PIPE_READ_END]);
		close(pdata->pipe_fd[PIPE_WRITE_END]);
		pdata->pipe_fd[PIPE_READ_END] = -1;
		pdata->pipe_fd[PIPE_WRITE_END] = -1;
		return ret;
	}

	/*
	 * Boot the M33: mcs_km sets the nsvtor boot vector with boot_addr
	 * and runs the SCMI reset sequence of the core.
	 */
	ret = ioctl(pdata->mcs_fd, IOC_MCUON, &info);
	if (ret) {
		syslog(LOG_ERR, "ioctl(IOC_MCUON) failed: %s\n", strerror(errno));
	}

	return ret;
}

static int rproc_stop(struct remoteproc *rproc)
{
	struct rproc_pdata *pdata = rproc->priv;
	struct cpu_info info = {0};
	int ret;

	/*
	 * stop the M33 through the SCMI resets
	 * owned by mcs_km instead of asking the remote to power off.
	 */
	ret = ioctl(pdata->mcs_fd, IOC_MCUOFF, &info);
	if (ret)
		syslog(LOG_ERR, "ioctl(IOC_MCUOFF) failed: %s\n", strerror(errno));

	/*
	 * Join the waiter here, before mica_unregister_all_services() and
	 * release_rpmsg_device() destroy the virtqueues it may still touch.
	 * Keeping it alive until remoteproc_shutdown() leaves a window where
	 * a late kick makes it run rpmsg_virtio_rx_callback() on freed queues.
	 */
	rproc_stop_notifier(pdata);

	return ret;
}

static int rproc_shutdown(struct remoteproc *rproc)
{
	struct remoteproc_mem *mem;
	struct metal_list *node;
	struct rproc_pdata *pdata = rproc->priv;
	void *virt = NULL;
	size_t size = 0;

	/* Stop the waiter before releasing the resources it may still touch. */
	rproc_stop_notifier(pdata);

	/* Delete all the registered remoteproc memories */
	metal_list_for_each(&rproc->mems, node) {
		struct metal_list *tmpnode;

		mem = metal_container_of(node, struct remoteproc_mem, node);
		get_mmap_addr_info(mem->io, &virt, &size);
		munmap(virt, size);
		tmpnode = node;
		node = tmpnode->prev;
		metal_list_del(tmpnode);
		metal_free_memory(mem->io);
		metal_free_memory(mem);
	}

	rproc->rsc_table = NULL;
	rproc->rsc_len = 0;
	rproc->bitmap = 0;
	return 0;
}

static void rproc_remove(struct remoteproc *rproc)
{
	struct rproc_pdata *pdata = rproc->priv;

	/* clean up pdata etc. */
	rproc_stop_notifier(pdata);
	close(pdata->mcs_fd);
	free(pdata);
	rproc->priv = NULL;
}

static int rproc_notify(struct remoteproc *rproc, uint32_t id)
{
	int ret;
	struct rproc_pdata *pdata = rproc->priv;
	struct cpu_info info = {
		/*
		 * The id is the notifyid of the notified entity. Open-amp uses
		 * the vring notifyids (0/1) to select the IPCC channel which
		 * kicks the M33. Notifications at vdev level (e.g. created or
		 * ready) use a notifyid allocated on top of the vring ones and
		 * have no dedicated channel; they are only a wake-up hint, so
		 * route them to the first channel instead of failing.
		 */
		.cpu = id < STM32MP2_VQ_NUM ? id : 0,
	};

	ret = ioctl(pdata->mcs_fd, IOC_SENDIPI, &info);
	if (ret) {
		syslog(LOG_ERR, "ioctl(IOC_SENDIPI) failed: %s\n", strerror(errno));
		return -EINVAL;
	}

	return 0;
}

const struct remoteproc_ops rproc_stm32mp2_ops = {
	.init = rproc_init,
	.remove = rproc_remove,
	.config = rproc_config,
	.handle_rsc = handle_mica_rsc,
	.start = rproc_start,
	.stop = rproc_stop,
	.shutdown = rproc_shutdown,
	.mmap = rproc_mmap,
	.notify = rproc_notify,
};
