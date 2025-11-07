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
#include <linux/limits.h>

#include <memory/shm_pool.h>
#include <remoteproc/remoteproc_module.h>
#include <remoteproc/mica_rsc.h>

#include <openamp/rsc_table_parser.h>

#define PIPE_READ_END  0
#define PIPE_WRITE_END 1

#define MCS_DEVICE_NAME	"/dev/mcs_xen"

#define MAGIC_NUMBER		'M'
#define IOC_SET_DOMID		_IOW(MAGIC_NUMBER, 0, int)
#define IOC_QUERY_MEM		_IOW(MAGIC_NUMBER, 1, int)
#define IOC_INVOKE_EVTCHN	_IOW(MAGIC_NUMBER, 2, int)

/* shared memory pool size: 128 K */
#define SHM_POOL_SIZE	   0x20000
#define MAX_CFG_LINE_LEN   256

enum xenbus_state {
	XenbusStateUnknown       = 0,
	XenbusStateInitialising  = 1,
	XenbusStateInitWait      = 2,
	XenbusStateInitialised   = 3,
	XenbusStateConnected     = 4,
	XenbusStateClosing       = 5,
	XenbusStateClosed        = 6,
	XenbusStateReconfiguring = 7,
	XenbusStateReconfigured  = 8
};

/**
 * struct rproc_pdata - rproc private data
 * @domu_name: the xen domU name
 */
// TODO: Actually domU name max len needs to be shorter than PATH_MAX
// The log file path (/var/volatile/log/xen/xl-zephyr.log.1) needs to be shorter than PATH_MAX.
// Check libxl_create_logfile()
struct rproc_pdata {
	char domu_name[PATH_MAX + 1];
	int domu_id;
	int mcs_fd;
	int pipe_fd[2];
	uint64_t shmem_addr;
	uint64_t shmem_size;
};

struct ioctl_info {
	int domu_id;
	uint64_t phy_addr;
	uint64_t size;
};

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
		DEBUG_PRINT("Failed to execute %s: %s", argv[0], strerror(errno));
		ret = -1;
	} else {
		waitpid(pid, &status, 0);

		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
			ret = -1;
	}

	free(argv);
	return ret;
}

static int generate_xen_cfg(struct mica_client *client)
{
	char xen_cfg_path[PATH_MAX];
	FILE *fp;
	struct pedestal_setup *setup = &client->ped_setup;

	/* validate required fields */
	if (setup->vcpu_num < 0) {
		syslog(LOG_ERR, "VCPU number not set (%d). Must be >= 0.", setup->vcpu_num);
		return -EINVAL;
	}
	if (setup->memory < 0) {
		syslog(LOG_ERR, "Memory size not set (%d). Must be >= 0.", setup->memory);
		return -EINVAL;
	}
	if (strlen(client->ped_cfg) == 0) {
		syslog(LOG_ERR, "PedestalConfg (ped_cfg) is empty. Required for client path.");
		return -EINVAL;
	}

	/* generate xen cfg */
	snprintf(xen_cfg_path, sizeof(xen_cfg_path), "/etc/xen/%s-xen.cfg", setup->name);
	fp = fopen(xen_cfg_path, "w");
	if (!fp) {
		syslog(LOG_ERR, "Failed to create Xen config %s: %s", xen_cfg_path, strerror(errno));
		return -EIO;
	}

	fprintf(fp, "name = \"%s\"\n", setup->name);
	fprintf(fp, "vcpus = %d\n", setup->vcpu_num);
	fprintf(fp, "memory = %d\n", setup->memory);
	fprintf(fp, "kernel = \"%s\"\n", client->ped_cfg);
	fprintf(fp, "gic_version = \"v3\"\n");

	if (strlen(setup->cpu_str) > 0) {
		fprintf(fp, "cpus = \"%s\"\n", setup->cpu_str);
	}
	if (setup->cpu_weight >= 0) {
		fprintf(fp, "cpu_weight = %d\n", setup->cpu_weight);
	}
	if (setup->cpu_capacity >= 0) {
		fprintf(fp, "cap = %d\n", setup->cpu_capacity);
	}
	if (strlen(setup->network) > 0) {
		fprintf(fp, "network = \"%s\"\n", setup->network);
	}
	fclose(fp);

	/* Update client->ped_cfg to the new Xen config file */
	strlcpy(client->ped_cfg, xen_cfg_path, sizeof(client->ped_cfg));
	syslog(LOG_INFO, "Generated Xen config: %s", xen_cfg_path);

	return 0;
}

static void rm_xen_cfg(struct mica_client *client)
{
	if (strlen(client->ped_cfg) > 0) {
		if (unlink(client->ped_cfg) < 0) {
			DEBUG_PRINT("Failed to delete xen cfg: %s (%s)",
				   client->ped_cfg, strerror(errno));
		} else {
			DEBUG_PRINT("Deleted xen cfg: %s", client->ped_cfg);
		}
	}
	return;
}

static int init_domu_name(struct mica_client *client, struct rproc_pdata *pdata)
{
	FILE *file;
	char line[MAX_CFG_LINE_LEN];
	const char *target_line = "name";
	char *left_quote, *right_quote;

	file = fopen(client->ped_cfg, "r");
	if (file == NULL) {
		syslog(LOG_ERR, "Error opening xen cfg(%s): %s", client->ped_cfg, strerror(errno));
		return -1;
	}

	// TODO: too deep
	while (fgets(line, sizeof(line), file) != NULL) {
		if (strncmp(line, target_line, strlen(target_line)) == 0) {
			char *equal_sign = strchr(line, '=');
			if (equal_sign != NULL) {
				left_quote = strchr(equal_sign, '"');
				if (left_quote != NULL) {
					right_quote = strchr(left_quote + 1, '"');
					if (right_quote != NULL) {
						size_t length = right_quote - left_quote - 1;
						// TODO: check length of domu_name
						strncpy(pdata->domu_name, left_quote + 1, length);
						pdata->domu_name[length] ='\0';
						fclose(file);
						return 0;
					}
				}
			}
		}
	}

	fclose(file);
	return -EINVAL;
}

static int get_domid(const char *domu_name, struct rproc_pdata *pdata)
{
	FILE *file;
	char line[PATH_MAX];
	int ret = -ESRCH;

	file = popen("xl list", "r");
	if (file == NULL) {
		syslog(LOG_ERR, "Failed to execute 'xl list': %s", strerror(errno));
		return ret;
	}

	/* skip the 1st line (header) */
	if (fgets(line, sizeof(line), file) == NULL) {
		syslog(LOG_ERR, "Failed to read 'xl list' output");
		goto out;
	}

	/* parse every line of cfg */
	while (fgets(line, sizeof(line), file) != NULL) {
		char *name, *domid_str;
		if (line[0] == '\n')
			continue;
		name = strtok(line, " \t");
		domid_str = strtok(NULL, " \t");
		DEBUG_PRINT("%s %s", name, domid_str);
		if (name !=NULL && domid_str != NULL) {
			if (strcmp(name, domu_name) == 0) {
				pdata->domu_id = atoi(domid_str);
				ret = 0;
				DEBUG_PRINT("domu %s id is %d", domu_name, pdata->domu_id);
				break;
			}
		}
	}

out:
	pclose(file);
	return ret;
}

static int wait_for_xenbus_probe(struct rproc_pdata *pdata)
{
	int ret = -1;
	char dom0_key_xenstore_evtchn[PATH_MAX];
	struct timespec start, now;
	const int timeout_sec = 5;	/* Total timeout: 5 seconds */
	const int retry_delay_ms = 1000; /* Retry interval: 100ms */

	snprintf(dom0_key_xenstore_evtchn, PATH_MAX, "/local/domain/0/backend/mica/%d/0/evtchn_port", pdata->domu_id);
	clock_gettime(CLOCK_MONOTONIC, &start);

	while (ret != 0) {
		/* Check timeout */
		clock_gettime(CLOCK_MONOTONIC, &now);
		if ((now.tv_sec - start.tv_sec) >= timeout_sec) {
			syslog(LOG_ERR, "Timeout waiting for evtchn_port in xenstore (5s)");
			return -ETIMEDOUT;
		}

		/* If evtchn xenstore is filled, it means xen-mcsback finished initializing probing */
		ret = run_command("xenstore-ls", dom0_key_xenstore_evtchn, NULL);
		if (ret != 0) {
			usleep(retry_delay_ms * 1000);
			syslog(LOG_INFO, "Timeout for xenstore probe. Try again.");
		} else {
			syslog(LOG_INFO, "Success for xenstore probe.");
			return 0;
		}
	}

	return -ENOENT;
}

static int trigger_mcs_backend_probe(struct rproc_pdata *pdata)
{
	int ret;

	char domu_key_backend_id[PATH_MAX];
	char domu_key_backend[PATH_MAX];
	char dom0_key_frontend_id[PATH_MAX];
	char dom0_key_frontend[PATH_MAX];
	char domu_key_state[PATH_MAX];
	char dom0_key_state[PATH_MAX];

	char dom0_key_xenstore[PATH_MAX];
	char frontend_id_str[16];

	snprintf(domu_key_backend_id, PATH_MAX, "/local/domain/%d/device/mica/0/backend-id", pdata->domu_id);
	snprintf(domu_key_backend, PATH_MAX, "/local/domain/%d/device/mica/0/backend", pdata->domu_id);
	snprintf(dom0_key_frontend_id, PATH_MAX, "/local/domain/0/backend/mica/%d/0/frontend-id", pdata->domu_id);
	snprintf(dom0_key_frontend, PATH_MAX, "/local/domain/0/backend/mica/%d/0/frontend", pdata->domu_id);
	snprintf(domu_key_state, PATH_MAX, "/local/domain/%d/device/mica/0/state", pdata->domu_id);
	snprintf(dom0_key_state, PATH_MAX, "/local/domain/0/backend/mica/%d/0/state", pdata->domu_id);
	snprintf(dom0_key_xenstore, PATH_MAX, "/local/domain/0/backend/mica/%d/0", pdata->domu_id);

	ret = run_command("xenstore-write", domu_key_backend_id, "0", NULL);
	if (ret) {
		return ret;
	}

	ret = run_command("xenstore-write", domu_key_backend, dom0_key_frontend, NULL);
	if (ret) {
		return ret;
	}

	snprintf(frontend_id_str, sizeof(frontend_id_str), "%d", pdata->domu_id);
	ret = run_command("xenstore-write", dom0_key_frontend_id, frontend_id_str, NULL);
	if (ret) {
		return ret;
	}

	ret = run_command("xenstore-write", dom0_key_frontend, domu_key_backend, NULL);
	if (ret) {
		return ret;
	}

	ret = run_command("xenstore-chmod", dom0_key_frontend_id, "r", NULL);
	if (ret) {
		return ret;
	}

	ret = run_command("xenstore-write", domu_key_state, "1", NULL);
	if (ret) {
		return ret;
	}

	ret = run_command("xenstore-write", dom0_key_state, "1", NULL);
	if (ret) {
		return ret;
	}

	ret = wait_for_xenbus_probe(pdata);
	if (ret) {
		syslog(LOG_ERR, "Timeout for Xenbus probe. Try again.");
		return ret;
	}

	ret = run_command("xenstore-chmod", dom0_key_xenstore, "-r", "r", NULL);
	if (ret) {
		syslog(LOG_ERR, "Failed to run 'xenstore-chmod %s -r r'", dom0_key_xenstore);
		return ret;
	}

	return 0;
}

static struct remoteproc *rproc_init(struct remoteproc *rproc,
					 const struct remoteproc_ops *ops, void *arg)
{
	int ret;
	struct mica_client *client = arg;
	struct rproc_pdata *pdata;

	if (!client)
		return NULL;

	pdata = malloc(sizeof(*pdata));
	if (!pdata)
		return NULL;

	ret = generate_xen_cfg(client);
	if (ret) {
		syslog(LOG_ERR, "Failed to generate xen cfg");
		goto err_malloc;
	}

	/* record domU name and domid for further xl usage */
	ret = init_domu_name(client, pdata);
	if (ret) {
		syslog(LOG_ERR, "Failed to find 'name' configuration in %s", client->ped_cfg);
		goto err_cfg;
	}

	rproc->priv = pdata;
	rproc->ops = ops;

	return rproc;

err_cfg:
	rm_xen_cfg(client);
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

static int create_and_pause_domu(struct remoteproc *rproc, void *data)
{
	int ret;
	struct rproc_pdata *pdata = rproc->priv;
	struct mica_client *client = metal_container_of(rproc, struct mica_client, rproc);
	struct ioctl_info info;

	/* 
	 * Event channel and grant table initialization depends on domid, which can be
	 * obtained only after domU is created. We still need to prepare other configuration
	 * before client actually starts, therefore we pause domu until everything is ready.
	 */

	ret = run_command("xl", "create", "-p", client->ped_cfg, NULL);
	if (ret) {
		syslog(LOG_ERR, "Failed to run 'xl create %s'", client->ped_cfg);
		return -EINVAL;
	}

	ret = get_domid(pdata->domu_name, pdata);
	if (ret) {
		syslog(LOG_ERR, "Failed to get domid of domain '%s' from xl", pdata->domu_name);
		goto err_domu;
	}

	info.domu_id = pdata->domu_id;
	ret = ioctl(pdata->mcs_fd, IOC_SET_DOMID, &info);
	if (ret) {
		syslog(LOG_ERR, "ioctl(IOC_SET_DOMID) failed: %s\n", strerror(errno));
		goto err_domu;
	}

	DEBUG_PRINT("trigger_mcs_backend_probe");
	ret = trigger_mcs_backend_probe(pdata);
	if (ret) {
		syslog(LOG_ERR, "Failed to trigger mcs backend probe with err %d", ret);
		goto err_domu;
	}

	return 0;

err_domu:
	if (run_command("xl", "destroy", pdata->domu_name, NULL)) {
		syslog(LOG_ERR, "Failed to run 'xl destroy %s'", pdata->domu_name);
	}
	return ret;
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
	struct ioctl_info info;

	pdata->mcs_fd = open(MCS_DEVICE_NAME, O_RDWR | O_SYNC);
	if (pdata->mcs_fd < 0) {
		syslog(LOG_ERR, "open %s device failed, err %d\n", MCS_DEVICE_NAME, pdata->mcs_fd);
		return -ENODEV;
	}

	ret = create_and_pause_domu(rproc, data);
	if (ret) {
		syslog(LOG_ERR, "Create new domU failed, err %d", ret);
		goto err_close_fd;
	}

	info.domu_id = pdata->domu_id;
	ret = ioctl(pdata->mcs_fd, IOC_QUERY_MEM, &info);
	if (ret) {
		syslog(LOG_ERR, "ioctl(IOC_QUERY_MEM) failed: %s\n", strerror(errno));
		goto err_close_fd;
	}

	pdata->shmem_addr = info.phy_addr;
	pdata->shmem_size = info.size;

	ret = init_shmem_pool(client, info.phy_addr, info.size);
	if (ret) {
		syslog(LOG_ERR, "init shared memory pool failed, err %d\n", ret);
		goto err_close_fd;
	}

	client->shmem_dynamic = true;

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
	 * For xen, we use the first grant page to store the resource table.
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

static int rproc_start(struct remoteproc *rproc)
{
	int ret;
	struct rproc_pdata *pdata = rproc->priv;

	/* set up the notification waiter */
	ret = rproc_register_notifier(rproc);
	if (ret) {
		syslog(LOG_ERR, "unable to register notifier, err: %d\n", ret);
		return ret;
	}

	ret = run_command("xl", "unpause", pdata->domu_name, NULL);
	if (ret) {
		syslog(LOG_ERR, "Failed to run 'xl unpause %s'", pdata->domu_name);
		return ret;
	}

	return ret;
}

static int rproc_shutdown(struct remoteproc *rproc)
{
	int ret;
	struct remoteproc_mem *mem;
	struct metal_list *node;
	struct rproc_pdata *pdata = rproc->priv;
	uint32_t dummy;
	struct ioctl_info info;
	char state_path[PATH_MAX];
	char state_str[16];

	ret = run_command("xl", "destroy", pdata->domu_name, NULL);
	if (ret) {
		syslog(LOG_ERR, "Failed to run 'xl destroy %s'", pdata->domu_name);
		return ret;
	}

	info.domu_id = 0;
	ret = ioctl(pdata->mcs_fd, IOC_SET_DOMID, &info);
	if (ret) {
		syslog(LOG_ERR, "ioctl(IOC_SET_DOMID) failed: %s\n", strerror(errno));
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

	/* tell mcs_dev to unmap gnttab and release evtchn */
	snprintf(state_path, sizeof(state_path), "/local/domain/%d/device/mica/0/backend/state", pdata->domu_id);
	snprintf(state_str, sizeof(state_str), "%d", XenbusStateClosed);
	ret = run_command("xenstore-write", state_path, state_str, NULL);
	if (ret) {
		// Try to proceed anyway?
		syslog(LOG_ERR, "Failed to run 'xenstore-write %s %s'. Unable to release resource.", state_path, state_str);
	}

	close(pdata->mcs_fd);

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
	struct mica_client *client = metal_container_of(rproc, struct mica_client, rproc);

	/* delete auto-generated xen cfg */
	rm_xen_cfg(client);

	/* clean up pdata etc. */
	free(pdata);
	rproc->priv = NULL;
}

static int rproc_notify(struct remoteproc *rproc, uint32_t id)
{
	struct rproc_pdata *pdata = rproc->priv;
	struct ioctl_info info;
	int ret;

	info.domu_id = pdata->domu_id;
	ret = ioctl(pdata->mcs_fd, IOC_INVOKE_EVTCHN, &info);
	if (ret) {
		syslog(LOG_ERR, "ioctl(IOC_INVOKE_EVTCHN) failed: %s\n", strerror(errno));
		return -EINVAL;
	}
	return 0;

}

const struct remoteproc_ops rproc_xen_ops = {
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
