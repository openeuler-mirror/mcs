/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
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
#include <metal/alloc.h>
#include <metal/cache.h>
#include <metal/io.h>
#include <openamp/remoteproc.h>
#include <openamp/elf_loader.h>

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

static int mcs_fd;
static int pipe_fd[2];
#define PIPE_READ_END  0
#define PIPE_WRITE_END 1
#define MCS_DEVICE_NAME    "/dev/mcs"
#define IOC_SENDIPI        _IOW('A', 0, int)
#define IOC_CPUON          _IOW('A', 1, int)
#define IOC_AFFINITY_INFO  _IOW('A', 2, int)
#define IOC_QUERY_MEM      _IOW('A', 3, int)

/* PSCI FUNCTIONS */
#define CPU_ON_FUNCID      0xC4000003
#define CPU_OFF_FUNCID     0x84000002
#define SYSTEM_RESET       0x84000009

/* shared memory pool size: 128 K */
#define SHM_POOL_SIZE      0x20000

static atomic_bool notifier;
static pthread_cond_t cond = PTHREAD_COND_INITIALIZER;
static pthread_mutex_t mutex = PTHREAD_MUTEX_INITIALIZER;

static void rproc_notify_all(void)
{
	struct metal_list *node;
	struct mica_client *client;

	metal_list_for_each(&g_client_list, node) {
		client = metal_container_of(node, struct mica_client, node);
		if (client->ped == BARE_METAL)
			remoteproc_get_notification(&client->rproc, 0);
	}
}

/*
 * Listen to events sent from the remote
 */
static void *rproc_wait_event(void *arg)
{
	int ret;
	struct pollfd fds[2] = {
		{ .fd = mcs_fd, .events = POLLIN, },
		{ .fd = pipe_fd[PIPE_READ_END], .events = POLLIN, }
	};

	notifier = true;
	pthread_mutex_lock(&mutex);
	pthread_cond_broadcast(&cond);
	pthread_mutex_unlock(&mutex);

	while (notifier) {
		ret = poll(fds, 2, -1);
		if (ret == -1) {
			syslog(LOG_ERR, "%s failed: %s\n", __func__, strerror(errno));
			break;
		}

		if (fds[0].revents & POLLIN)
			rproc_notify_all();
	}

	pthread_exit(NULL);
}

static int rproc_register_notifier(void)
{
	int ret = 0;
	pthread_t thread;

	/*
	 * For bare-metal, we only need to register one notifier.
	 */
	if (notifier)
		return ret;

	ret = pipe(pipe_fd);
	if (ret == -1) {
		syslog(LOG_ERR, "unable to create pipe for notifier: %s\n", strerror(errno));
		return ret;
	}

	ret = pthread_create(&thread, NULL, rproc_wait_event, NULL);
	if (ret)
		goto err;

	ret = pthread_detach(thread);
	if (ret) {
		pthread_cancel(thread);
		goto err;
	}

	pthread_mutex_lock(&mutex);
	pthread_cond_wait(&cond, &mutex);
	pthread_mutex_unlock(&mutex);
	return 0;
err:
	close(pipe_fd[PIPE_READ_END]);
	close(pipe_fd[PIPE_WRITE_END]);
	return ret;
}

static struct remoteproc *rproc_init(struct remoteproc *rproc,
				     const struct remoteproc_ops *ops, void *arg)
{
	int ret;

	rproc->ops = ops;
	/* open mcs device for rproc->ops */
	mcs_fd = open(MCS_DEVICE_NAME, O_RDWR | O_SYNC);
	if (mcs_fd < 0) {
		syslog(LOG_ERR, "open %s device failed, err %d\n", MCS_DEVICE_NAME, mcs_fd);
		return NULL;
	}

	/* set up the notification waiter */
	ret = rproc_register_notifier();
	if (ret) {
		syslog(LOG_ERR, "unable to register notifier, err: %d\n", ret);
		goto err;
	}

	return rproc;
err:
	close(mcs_fd);
	return NULL;
}

static void *rproc_mmap(struct remoteproc *rproc,
			metal_phys_addr_t *pa, metal_phys_addr_t *da,
			size_t size, unsigned int attribute,
			struct metal_io_region **io)
{
	void *va;
	size_t pagesize, aligned_size;
	struct remoteproc_mem *mem;
	metal_phys_addr_t lpa, lda, aligned_addr, offset;
	struct metal_io_region *tmpio;

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

	va = mmap(NULL, aligned_size, PROT_READ | PROT_WRITE, MAP_SHARED, mcs_fd, aligned_addr);
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

static void set_cpu_status(struct resource_table *rsc_table, uint32_t status)
{
	rsc_table->reserved[0] = status;
	metal_cache_flush(rsc_table->reserved, sizeof(rsc_table->reserved));
}

static uint32_t get_cpu_status(struct resource_table *rsc_table)
{
	metal_cache_invalidate(rsc_table->reserved, sizeof(rsc_table->reserved));
	return rsc_table->reserved[0];
}

static int wait_cpu_status_reset(struct resource_table *rsc_table, unsigned int timeout)
{
	unsigned int diff;
	struct timespec start, now;

	clock_gettime(CLOCK_MONOTONIC, &start);

	while (get_cpu_status(rsc_table) != 0) {
		clock_gettime(CLOCK_MONOTONIC, &now);
		diff = 1000 * (long long)((int)now.tv_sec - (int)start.tv_sec);
		diff += ((int)now.tv_nsec - (int)start.tv_nsec) / 1000000;
		if (diff >= timeout)
			return -1;
	}

	return 0;
}

static int rproc_config(struct remoteproc *rproc, void *data)
{
	int ret;
	uint32_t status;
	struct img_store *image = data;
	const struct loader_ops *loader = &elf_ops;
	size_t offset, noffset;
	size_t len, nlen;
	int last_load_state;
	metal_phys_addr_t rsc_da;
	size_t rsc_size = 0;
	void *limg_info = NULL;
	void *rsc_table = NULL;
	struct mem_info info;
	struct metal_io_region *io = NULL;
	struct mica_client *client;

	/*
	 * Call rproc->ops->mmap to create shared memory io
	 */
	ret = ioctl(mcs_fd, IOC_QUERY_MEM, &info);
	if (ret < 0) {
		syslog(LOG_ERR, "unable to get shared memory information from mcs device, err: %d\n", ret);
		return ret;
	}

	client = metal_container_of(rproc, struct mica_client, rproc);
	ret = init_shmem_pool(client, info.phy_addr + (client->cpu_id * SHM_POOL_SIZE), SHM_POOL_SIZE);
	if (ret) {
		syslog(LOG_ERR, "init shared memory pool failed, err %d\n", ret);
		return ret;
	}

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
	rsc_table = remoteproc_mmap(rproc, NULL, &rsc_da, rsc_size, 0, &io);
	if (!rsc_table) {
		ret = -ENOMEM;
		goto err;
	}

	status = get_cpu_status((struct resource_table *)rsc_table);
	DEBUG_PRINT("remote status: %x\n", status);

	/* Update resource table */
	if (status == CPU_ON_FUNCID) {
		/*
		 * Set the CPU state to SYSTEM_RESET to notify the remote to reinitialise vdev.
		 * We then wait for the remote to clear SYSTEM_RESET. If this takes longer than 200 ms,
		 * we assume that the remote is not alive and return.
		 */
		set_cpu_status((struct resource_table *)rsc_table, SYSTEM_RESET);
		rproc->ops->notify(rproc, 0);
		ret = wait_cpu_status_reset((struct resource_table *)rsc_table, 200);
		if (ret) {
			syslog(LOG_INFO, "The CPU status is CPU_ON, but remote didn't respond, try to reload it.");
			ret = 0;
			goto err;
		}

		syslog(LOG_INFO, "the remote is alive, restore rsc table.");
		/*
		 * The reserved fields was reset by remote, and we can restore the rsc table.
		 * Note:  handle_rsc_table() requires that the reserved fields must be zero.
		 */
		ret = remoteproc_set_rsc_table(rproc, (struct resource_table *)rsc_table, rsc_size);
		if (ret) {
			syslog(LOG_ERR, "unable to set rsctable, ret: %d", ret);
			goto err;
		}

		rproc->bootaddr = loader->get_entry(limg_info);
		rproc->state = RPROC_READY;

		/* Set the CPU state to CPU_ON_FUNCID to skip rproc_start. */
		set_cpu_status((struct resource_table *)rsc_table, CPU_ON_FUNCID);
	}

	/*
	 * We don't need to release the rsc table here.
	 * It will be released when rproc is destroyed.
	 */
err:
	loader->release(limg_info);
	return ret;
}

static int rproc_start(struct remoteproc *rproc)
{
	int ret;
	uint32_t status;
	struct mica_client *client = metal_container_of(rproc, struct mica_client, rproc);
	struct resource_table *rsc_table = rproc->rsc_table;
	struct cpu_info info = {
		.cpu = client->cpu_id,
		.boot_addr = rproc->bootaddr
	};

	status = get_cpu_status(rsc_table);
	if (status == CPU_ON_FUNCID)
		return 0;

	ret = ioctl(mcs_fd, IOC_CPUON, &info);
	if (ret < 0) {
		syslog(LOG_ERR, "boot client os on CPU%d failed, err: %d\n", info.cpu, ret);
		return ret;
	}

	set_cpu_status(rproc->rsc_table, CPU_ON_FUNCID);
	return 0;
}

static int rproc_shutdown(struct remoteproc *rproc)
{
	struct remoteproc_mem *mem;
	struct metal_list *node;
	struct resource_table *rsc_table = rproc->rsc_table;

	/* Tell clientos shut itself down by PSCI */
	set_cpu_status((struct resource_table *)rsc_table, CPU_OFF_FUNCID);
	rproc->ops->notify(rproc, 0);

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

	rproc->rsc_table = NULL;
	rproc->rsc_len = 0;
	rproc->bitmap = 0;
	return 0;
}

static void rproc_remove(struct remoteproc *rproc)
{
	int find = 0;
	struct metal_list *node;
	struct mica_client *client;

	metal_list_for_each(&g_client_list, node) {
		client = metal_container_of(node, struct mica_client, node);
		if (client->ped == BARE_METAL) {
			find = 1;
			break;
		}
	}

	notifier = find ? true : false;
	if (!notifier) {
		(void)!write(pipe_fd[PIPE_WRITE_END], &find, sizeof(find));
		close(mcs_fd);
		close(pipe_fd[PIPE_READ_END]);
		close(pipe_fd[PIPE_WRITE_END]);
	}
}

static int rproc_notify(struct remoteproc *rproc, uint32_t id)
{
	int ret;
	struct mica_client *client = metal_container_of(rproc, struct mica_client, rproc);
	struct cpu_info info = {
		.cpu = client->cpu_id,
	};

	(void)id;
	ret = ioctl(mcs_fd, IOC_SENDIPI, &info);
	if (ret < 0) {
		syslog(LOG_ERR, "send ipi to CPU%d failed, err: %d\n", info.cpu, ret);
		return ret;
	}

	return 0;
}

const struct remoteproc_ops rproc_bare_metal_ops = {
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
