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

/* Need to keep consistent with definition in jailhouse/cell-config.h */
#define JAILHOUSE_CELL_NAME_MAXLEN	31
#define JAILHOUSE_CONFIG_REVISION	13
typedef struct __attribute__((packed)) {
	char signature[5];
	__u8 architecture;
	__u16 revision;
	char name[JAILHOUSE_CELL_NAME_MAXLEN + 1];
	__u32 id;
} jailhouse_cell_desc_head;
/* jailhouse/cell-config.h */

struct ivshmem_v2_reg {
	uint32_t id;
	uint32_t max_peers;
	uint32_t int_control;
	uint32_t doorbell;
	uint32_t state;
};


#define PIPE_READ_END  0
#define PIPE_WRITE_END 1
/**
 * struct ivshmem_device - ivshmem device
 * @uio_fd: the uio device
 * @pipe_fd: used to exit the poll
 * @ivshm_regs: ivshmem register region
 * @peer_id: the peer
 * @shmem_addr: physical address of the ivshmem RW section
 * @shmem_sz: total size of the ivshmem RW section
 */
struct ivshmem_device {
	int uio_fd;
	int pipe_fd[2];
	struct ivshmem_v2_reg *ivshm_regs;
	uint32_t peer_id;
	uint64_t shmem_addr;
	uint64_t shmem_sz;
};

/**
 * struct rproc_pdata - rproc private data
 * @cell_name: the jailhouse cell name
 * @ivshmem_dev: the ivshmem device
 */
struct rproc_pdata {
	char cell_name[JAILHOUSE_CELL_NAME_MAXLEN + 1];
	struct ivshmem_device ivshmem_dev;
};

static inline void write32(void *address, uint32_t value)
{
	*(uint32_t *)address = value;
}

/*
 * Listen to events sent from the remote
 */
static void *rproc_wait_event(void *arg)
{
	int ret;
	struct remoteproc *rproc = arg;
	struct rproc_pdata *pdata = rproc->priv;
	struct ivshmem_device *ivshmem_dev = &pdata->ivshmem_dev;
	struct pollfd fds[2];

	ret = pipe(ivshmem_dev->pipe_fd);
	if (ret == -1) {
		syslog(LOG_ERR, "unable to create pipe for notifier: %s\n", strerror(errno));
		return NULL;
	}

	fds[0].fd = ivshmem_dev->uio_fd;
	fds[0].events = POLLIN;
	fds[1].fd = ivshmem_dev->pipe_fd[PIPE_READ_END];
	fds[1].events = POLLIN;

	while (1) {
		ret = poll(fds, 2, -1);
		if (ret == -1) {
			syslog(LOG_ERR, "%s failed: %s\n", __func__, strerror(errno));
			break;
		}

		if (fds[0].revents & POLLIN) {
			uint32_t dummy;

			if (read(ivshmem_dev->uio_fd, &dummy, 4) < 0)
				syslog(LOG_ERR, "UIO read failed");

			write32(&ivshmem_dev->ivshm_regs->int_control, 1);
			remoteproc_get_notification(rproc, 0);
		}

		/* pipe fd used to exit the wait_event */
		if (fds[1].revents & POLLIN)
			break;
	}

	close(ivshmem_dev->pipe_fd[PIPE_READ_END]);
	close(ivshmem_dev->pipe_fd[PIPE_WRITE_END]);
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

static int run_command(const char *arg, ...)
{
	int ret = 0;
	int n;
	va_list ap;
	char **argv;
	pid_t pid;
	int status;

	va_start(ap, arg);
	n = 1;
	while (va_arg(ap, char *) != NULL)
		n++;
	va_end(ap);

	/* an additional one for NULL */
	argv = (char **)malloc(sizeof(char *) * (n + 1));
	if (argv == NULL) {
		syslog(LOG_ERR, "Failed to allocate memory: %s", strerror(errno));
		return -1;
	}

	va_start(ap, arg);
	argv[0] = (char *)arg;
	n = 1;
	while ((argv[n] = va_arg(ap, char *)) != NULL)
		n++;
	va_end(ap);

	pid = fork();

	if (pid < 0) {
		syslog(LOG_ERR, "Failed to fork(%s): %s", __func__, strerror(errno));
		ret = -1;
	} else if (pid == 0) {
		execvp(argv[0], argv);
		ret = -1;
	} else {
		waitpid(pid, &status, 0);

		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
			ret = -1;
	}

	free(argv);
	return ret;
}

static int init_ivshmem_dev(const char *uio_dev, struct ivshmem_device *ivshmem_dev)
{
	int ret, uio_fd, info_fd;
	char *uio_devname;
	char sysfs_path[64];
	char info_str[64];

	uio_fd = open(uio_dev, O_RDWR);
	if (uio_fd < 0) {
		syslog(LOG_ERR, "open %s failed, err: %d\n", uio_dev, uio_fd);
		return -1;
	}

	ivshmem_dev->ivshm_regs = mmap(NULL, 4096, PROT_READ | PROT_WRITE, MAP_SHARED, uio_fd, 0);
	if (ivshmem_dev->ivshm_regs == NULL) {
		syslog(LOG_ERR, "mmap of registers failed");
		goto err;
	}

	uio_devname = strstr(uio_dev, "/uio");
	snprintf(sysfs_path, sizeof(sysfs_path), "/sys/class/uio%s/maps/map2/size", uio_devname);
	info_fd = open(sysfs_path, O_RDONLY);
	if (info_fd < 0) {
		syslog(LOG_ERR, "open %s failed", sysfs_path);
		goto err;
	}
	ret = read(info_fd, info_str, sizeof(info_str));
	close(info_fd);
	if (ret < 0) {
		syslog(LOG_ERR, "read from %s failed", sysfs_path);
		goto err;
	}
	ivshmem_dev->shmem_sz = strtoll(info_str, NULL, 16);

	snprintf(sysfs_path, sizeof(sysfs_path), "/sys/class/uio%s/maps/map2/addr", uio_devname);
	info_fd = open(sysfs_path, O_RDONLY);
	if (info_fd < 0) {
		syslog(LOG_ERR, "open %s failed", sysfs_path);
		goto err;
	}
	ret = read(info_fd, info_str, sizeof(info_str));
	close(info_fd);
	if (ret < 0) {
		syslog(LOG_ERR, "read from %s failed", sysfs_path);
		goto err;
	}
	ivshmem_dev->shmem_addr = strtoll(info_str, NULL, 16);
	ivshmem_dev->uio_fd = uio_fd;
	ivshmem_dev->peer_id = !(ivshmem_dev->ivshm_regs->id);

	write32(&ivshmem_dev->ivshm_regs->int_control, 1);
	write32(&ivshmem_dev->ivshm_regs->state, 1);

	DEBUG_PRINT("init ivshmem device, uio_dev: %s, R/W section: 0x%lx-0x%lx",
		    uio_dev, ivshmem_dev->shmem_addr, ivshmem_dev->shmem_addr + ivshmem_dev->shmem_sz);

	return 0;
err:
	close(uio_fd);
	return -1;
}

static struct remoteproc *rproc_init(struct remoteproc *rproc,
					 const struct remoteproc_ops *ops, void *arg)
{
	int ret;
	FILE *file;
	struct mica_client *client = arg;
	struct rproc_pdata *pdata;
	jailhouse_cell_desc_head cell_desc_hdr;

	if (!client)
		return NULL;

	pdata = malloc(sizeof(*pdata));
	if (!pdata)
		return NULL;

	/* TODO: use dynamic UIO device id */
	ret = init_ivshmem_dev("/dev/uio0", &pdata->ivshmem_dev);
	if (ret) {
		syslog(LOG_ERR, "Failed to init ivshmem device, ret: %d", ret);
		free(pdata);
		return NULL;
	}

	file = fopen(client->ped_cfg, "rb");
	if (file == NULL) {
		syslog(LOG_ERR, "Error opening non-root cell(%s): %s", client->ped_cfg, strerror(errno));
		goto err;
	}

	ret = fread(&cell_desc_hdr, sizeof(jailhouse_cell_desc_head), 1, file);
	fclose(file);
	if (ret != 1) {
		syslog(LOG_ERR, "Error reading %s: %s", client->ped_cfg, strerror(errno));
		goto err;
	}

	if (cell_desc_hdr.revision != JAILHOUSE_CONFIG_REVISION) {
		syslog(LOG_ERR, "jailhouse configuration revision mismatch");
		goto err;
	}

	/* TODO : get the cell name from cell_desc_hdr and check if the cell has already been created. */
	ret = run_command("jailhouse", "cell", "create", client->ped_cfg, NULL);
	if (ret) {
		syslog(LOG_ERR, "Failed to run 'jailhouse cell create %s'", client->ped_cfg);
		goto err;
	}

	/* After creating cell, we need the name of cell to manage the corresponding cell. */
	strlcpy(pdata->cell_name, cell_desc_hdr.name, sizeof(pdata->cell_name));

	rproc->ops = ops;
	rproc->priv = pdata;
	return rproc;
err:
	close(pdata->ivshmem_dev.uio_fd);
	free(pdata);
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
	metal_phys_addr_t lpa, lda, aligned_addr, offset, uio_mem_addr;
	struct metal_io_region *tmpio;
	struct rproc_pdata *pdata = rproc->priv;

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
	/*
	 * The first page is the Register Region. The second page is the State Table.
	 * We need to map the Read/Write Section starting from the third page.
	 */
	uio_mem_addr = aligned_addr - pdata->ivshmem_dev.shmem_addr + 2 * pagesize;

	va = mmap(NULL, aligned_size, PROT_READ | PROT_WRITE, MAP_SHARED,
		  pdata->ivshmem_dev.uio_fd, uio_mem_addr);
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

	DEBUG_PRINT("mmap succeeded, paddr: 0x%lx, uio addr: 0x%lx, vaddr: %p, size 0x%lx, uio addr:\n",
		    (unsigned long)mem->pa, uio_mem_addr, va + offset, size);
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

	ret = init_shmem_pool(client, pdata->ivshmem_dev.shmem_addr, pdata->ivshmem_dev.shmem_sz);
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

	pagesize = sysconf(_SC_PAGE_SIZE);
	if (rsc_size > pagesize) {
		syslog(LOG_ERR, "rsc table size exceeds limit: (0x%lx vs. 0x%lx)", rsc_size, pagesize);
		ret = -EINVAL;
		goto err;
	}

	/*
	 * load the image via jailhouse and store the
	 * rsc table into the first page of the RW section.
	 * TODO: support for micad restart
	 */
	ret = run_command("jailhouse", "cell", "load", pdata->cell_name, client->path, NULL);
	if (ret) {
		syslog(LOG_ERR, "Failed to run 'jailhouse cell load %s %s'", pdata->cell_name, client->path);
		goto err;
	}

	/*
	 * For jailhouse, we use the first page of the RW section to store the resource table,
	 */
	rw_addr = pdata->ivshmem_dev.shmem_addr;
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

	rproc->state = RPROC_READY;

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
	struct rproc_pdata *pdata = rproc->priv;

	ret = run_command("jailhouse", "cell", "start", pdata->cell_name, NULL);
	if (ret) {
		syslog(LOG_ERR, "Failed to run 'jailhouse cell start %s'", pdata->cell_name);
		return ret;
	}

	/* set up the notification waiter */
	ret = rproc_register_notifier(rproc);
	if (ret)
		syslog(LOG_ERR, "unable to register notifier, err: %d\n", ret);

	return ret;
}

static int rproc_shutdown(struct remoteproc *rproc)
{
	int ret;
	struct remoteproc_mem *mem;
	struct metal_list *node;
	struct rproc_pdata *pdata = rproc->priv;
	struct ivshmem_device ivshmem_dev = pdata->ivshmem_dev;
	uint32_t dummy;


	ret = run_command("jailhouse", "cell", "shutdown", pdata->cell_name, NULL);
	if (ret) {
		syslog(LOG_ERR, "Failed to run 'jailhouse cell shutdown %s'", pdata->cell_name);
		return ret;
	}

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
	(void)!write(ivshmem_dev.pipe_fd[PIPE_WRITE_END], &dummy, sizeof(dummy));

	rproc->rsc_table = NULL;
	rproc->rsc_len = 0;
	rproc->bitmap = 0;
	return 0;
}

static void rproc_remove(struct remoteproc *rproc)
{
	int ret;
	struct rproc_pdata *pdata = rproc->priv;

	ret = run_command("jailhouse", "cell", "destroy", pdata->cell_name, NULL);
	if (ret) {
		syslog(LOG_ERR, "Failed to run 'jailhouse cell destroy %s'", pdata->cell_name);
		return;
	}

	close(pdata->ivshmem_dev.uio_fd);
	free(pdata);
	rproc->priv = NULL;
}

static int rproc_notify(struct remoteproc *rproc, uint32_t id)
{
	struct rproc_pdata *pdata = rproc->priv;
	struct ivshmem_device ivshmem_dev = pdata->ivshmem_dev;

	(void)id;
	write32(&ivshmem_dev.ivshm_regs->doorbell, ivshmem_dev.peer_id << 16);

	return 0;
}

const struct remoteproc_ops rproc_jailhouse_ops = {
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
