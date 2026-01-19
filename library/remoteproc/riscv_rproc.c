/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2025. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <pthread.h>
#include <sys/ioctl.h>
#include <stdio.h>
#include <syslog.h>
#include <ctype.h>
#include <metal/alloc.h>
#include <metal/cache.h>
#include <metal/io.h>
#include <openamp/remoteproc.h>
#include <openamp/elf_loader.h>
#include <linux/limits.h>

#include <memory/shm_pool.h>
#include <remoteproc/remoteproc_module.h>
#include <remoteproc/mica_rsc.h>

#include <openamp/rsc_table_parser.h>

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

#define MAGIC_NUMBER		'A'
#define IOC_SENDIPI        _IOW(MAGIC_NUMBER, 0, int)
#define IOC_MCUON          _IOW(MAGIC_NUMBER, 1, int)
#define IOC_QUERY_MEM      _IOW(MAGIC_NUMBER, 3, int)
#define IOC_SET_PED_TYPE		_IOW(MAGIC_NUMBER, 5, int)

/*
 * struct rproc_pdata - rproc private data
 */
struct rproc_pdata {
	int mcs_fd;
	int pipe_fd[2];
	uint64_t shmem_addr;
	uint64_t shmem_size;
};

static struct remoteproc *rproc_init(struct remoteproc *rproc,
					 const struct remoteproc_ops *ops, void *arg)
{
	int ret;
	struct mica_client *client = arg;
	struct rproc_pdata *pdata;
	int ped_type = MCS_KM_PED_RISCV;

	if (!client)
		return NULL;

	pdata = malloc(sizeof(*pdata));
	if (!pdata)
		return NULL;

	
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

static int rproc_config(struct remoteproc *rproc, void *data)
{
	int ret;
	struct img_store *image = data;
	const struct loader_ops *loader = &elf_ops;
	size_t offset, noffset, pagesize;
	size_t len, nlen;
	int last_load_state;
	metal_phys_addr_t rsc_da, rw_addr;
	size_t rsc_size = 0;
	void *limg_info = NULL;
	void *rsc_table = NULL;
	struct rproc_pdata *pdata = rproc->priv;
	struct mica_client *client = metal_container_of(rproc, struct mica_client, rproc);
	struct mem_info info;

	/*
	 * Call rproc->ops->mmap to create shared memory io
	 */
	ret = ioctl(pdata->mcs_fd, IOC_QUERY_MEM, &info);
	if (ret < 0) {
		syslog(LOG_ERR, "unable to get shared memory information from mcs device, err: %d\n", ret);
		return ret;
	}

	pdata->shmem_addr = info.phy_addr;
	pdata->shmem_size = info.size;

	ret = init_shmem_pool(client, info.phy_addr, info.size);
	if (ret) {
		syslog(LOG_ERR, "init shared memory pool failed, err %d\n", ret);
		goto err_close_fd;
	}

	client->shmem_dynamic = false;

	/* parse executable headers */
	fseek(image->file, 0, SEEK_END);
	offset = 0;
	len = ftell(image->file);

	last_load_state = RPROC_LOADER_NOT_READY;
	ret = loader->load_header(image->buf, offset, len,
				  &limg_info, last_load_state,
				  &noffset, &nlen);
	if (ret < 0) {
		syslog(LOG_ERR, "load header failed 0x%lx,%ld", offset, len);
		goto err;
	}

	ret = loader->locate_rsc_table(limg_info, &rsc_da, &offset, &rsc_size);
	if (ret != 0 || rsc_size <= 0) {
		syslog(LOG_ERR, "unable to locate rsctable, ret: %d", ret);
		goto err;
	}

	DEBUG_PRINT("get rsctable from header, rsc_da: %lx, rsc_size: %ld\n", rsc_da, rsc_size);

	pagesize = sysconf(_SC_PAGE_SIZE);
	if (rsc_size > pagesize) {
		syslog(LOG_ERR, "rsc table size exceeds limit: (0x%lx vs. 0x%lx)", rsc_size, pagesize);
		ret = -EINVAL;
		goto err;
	}

	/*
	 * TODO: support for micad restart
	 */

	/*
	 * For riscv, we use the first shmem page to store the resource table.
	 */
	rw_addr = pdata->shmem_addr;
	rsc_table = alloc_shmem_region(client, rw_addr, pagesize);
	if (!rsc_table) {
		ret = -ENOMEM;
		goto err;
	}
	memcpy(rsc_table, image->buf + offset, rsc_size);

	ret = remoteproc_set_rsc_table(rproc, (struct resource_table *)rsc_table, rsc_size);
	if (ret) {
		syslog(LOG_ERR, "unable to set rsctable, ret: %d", ret);
		ret = -EINVAL;
		goto err;
	}

	rproc->bootaddr = loader->get_entry(limg_info);
	rproc->state = RPROC_READY;

	/*
	 * We don't need to release the rsc table here.
	 * It will be released when rproc is destroyed.
	 */
	loader->release(limg_info);
	return ret;

err:
	loader->release(limg_info);
err_close_fd:
	close(pdata->mcs_fd);
	return ret;
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

	ret = pipe(pdata->pipe_fd);
	if (ret == -1) {
		syslog(LOG_ERR, "unable to create pipe for notifier: %s\n", strerror(errno));
		return NULL;
	}

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
			remoteproc_get_notification(rproc, 0);
		}

		/* pipe fd used to exit the wait_event */
		if (fds[1].revents & POLLIN)
			break;
	}

	close(pdata->pipe_fd[PIPE_READ_END]);
	close(pdata->pipe_fd[PIPE_WRITE_END]);
	pthread_exit(NULL);
}

static int rproc_register_notifier(struct remoteproc *rproc)
{
	int ret;
	pthread_t thread;

	ret = pthread_create(&thread, NULL, rproc_wait_event, rproc);
	if (ret)
		return ret;

	ret = pthread_detach(thread);
	if (ret) {
		pthread_cancel(thread);
		return ret;
	}

	return 0;
}

static int loadbin(int mcs_fd, unsigned long memaddr, const char *binfilepath)
{
	int fd;
	unsigned char *pu8buf = NULL;
	unsigned char *pu8filebuf = NULL;
	unsigned long slbinsize = 0;
	unsigned long long u64phyaddr = memaddr;
	int ret;

	if (binfilepath == NULL) {
		syslog(LOG_ERR, "binfilepath NULL!");
		return -1;
	}

	fd = open(binfilepath, O_RDONLY);
	if (fd < 0) {
		syslog(LOG_ERR, "open file failed!");
		goto fail_open;
	}

	slbinsize = lseek(fd, 0, SEEK_END);
	if (slbinsize < 0) {
		syslog(LOG_ERR, "file len invalid!");
		goto fail_size;
	}
	lseek(fd, 0, SEEK_SET);

	pu8buf = (unsigned char *)mmap(NULL, slbinsize, PROT_READ | PROT_WRITE, MAP_SHARED, mcs_fd, memaddr);
	if (pu8buf == MAP_FAILED) {
		syslog(LOG_ERR, "memmap failed!");
		goto fail_size;
	}

	pu8filebuf = mmap(NULL, slbinsize, PROT_READ, MAP_PRIVATE, fd, 0);
	if (pu8filebuf == MAP_FAILED) {
		syslog(LOG_ERR, "mmap failed!");
		goto fail_memmap;
	}

	syslog(LOG_INFO, "u64phyaddr 0x%llx slbinsize 0x%lx", u64phyaddr, slbinsize);
	memcpy(pu8buf, pu8filebuf, slbinsize);

	ret = munmap(pu8buf, slbinsize);
	if (ret != 0) {
		syslog(LOG_ERR, "munmap failed!");
		goto fail_end;
	}

	ret = munmap(pu8filebuf, slbinsize);
	if (ret != 0) {
		syslog(LOG_ERR, "munmap failed!");
		goto fail_end;
	}

	ret = close(fd);
	if (ret != 0) {
		syslog(LOG_ERR, "close file failed!");
		goto fail_end;
	}

	syslog(LOG_INFO, "load %s Size %ld to PhyAddr 0x%llx finish.", binfilepath, slbinsize, u64phyaddr);
	return 0;

fail_end:
	(void)munmap(pu8filebuf, slbinsize);
fail_memmap:
	(void)munmap(pu8buf, slbinsize);
fail_size:
	close(fd);
fail_open:
	return -1;
}

static int rproc_start(struct remoteproc *rproc)
{
	int ret;
	struct rproc_pdata *pdata = rproc->priv;
	struct mica_client *client = metal_container_of(rproc, struct mica_client, rproc);
	struct cpu_info info = {
		.cpu = 0,
		.boot_addr = rproc->bootaddr
	};

	/* set up the notification waiter */
	ret = rproc_register_notifier(rproc);
	if (ret) {
		syslog(LOG_ERR, "unable to register notifier, err: %d\n", ret);
		return ret;
	}

	if (client && client->ped_cfg[0] != '\0') {
		syslog(LOG_INFO, "Loading binary from %s to boot_addr 0x%lx", client->ped_cfg, rproc->bootaddr);
		ret = loadbin(pdata->mcs_fd, rproc->bootaddr, client->ped_cfg);
		if (ret) {
			syslog(LOG_ERR, "Failed to load binary: %d", ret);
			return ret;
		}
	} else {
		syslog(LOG_WARNING, "PedestalConf not found or empty");
	}

	ret = ioctl(pdata->mcs_fd, IOC_MCUON, &info);
	if (ret) {
		syslog(LOG_ERR, "ioctl(IOC_MCUON) failed: %s\n", strerror(errno));
	}

	return ret;
}

static int rproc_shutdown(struct remoteproc *rproc)
{
	struct remoteproc_mem *mem;
	struct metal_list *node;
	struct rproc_pdata *pdata = rproc->priv;
	uint32_t dummy;

	/* Delete all the registered remoteproc memories */
	metal_list_for_each(&rproc->mems, node) {
		struct metal_list *tmpnode;

		mem = metal_container_of(node, struct remoteproc_mem, node);
		munmap(mem->io->virt, mem->io->size);
		tmpnode = node;
		node = tmpnode->prev;
		metal_list_del(tmpnode);
		metal_free_memory(mem->io);
		metal_free_memory(mem);
	}

	/* exit notifier */
	(void)!write(pdata->pipe_fd[PIPE_WRITE_END], &dummy, sizeof(dummy));

	rproc->rsc_table = NULL;
	rproc->rsc_len = 0;
	rproc->bitmap = 0;
	return 0;
}

static void rproc_remove(struct remoteproc *rproc)
{
	struct rproc_pdata *pdata = rproc->priv;

	/* clean up pdata etc. */
	close(pdata->mcs_fd);
	free(pdata);
	rproc->priv = NULL;
}

static int rproc_notify(struct remoteproc *rproc, uint32_t id)
{
	struct rproc_pdata *pdata = rproc->priv;
	int ret;
	struct cpu_info dummy_info;

	ret = ioctl(pdata->mcs_fd, IOC_SENDIPI, &dummy_info);
	if (ret) {
		syslog(LOG_ERR, "ioctl(IOC_SENDIPI) failed: %s\n", strerror(errno));
		return -EINVAL;
	}
	return 0;

}

const struct remoteproc_ops rproc_riscv_ops = {
	.init = rproc_init,
	.remove = rproc_remove,
	.config = rproc_config,
	.handle_rsc = handle_mica_rsc,
	.start = rproc_start,
	.stop = NULL,
	.shutdown = rproc_shutdown,
	.mmap = rproc_mmap,
	.notify = rproc_notify,
};
