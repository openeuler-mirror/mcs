#include <user_msg/user_msg.h>

#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/mman.h>
#include <errno.h>
#include <sys/ioctl.h>
#include <linux/ioctl.h>
#include <syslog.h>

#define MCS_DEVICE_NAME    "/dev/mcs"

#define IOC_GET_COPY_MSG_MEM  _IOWR('A', 4, struct core_msg_mem_info)


int init_core_shared_memory(struct core_msg_mem_info *info)
{
	int ret;
    int mcs_fd;

    unsigned long lpa, aligned_addr;
    size_t pagesize, aligned_size, offset;

	mcs_fd = open(MCS_DEVICE_NAME, O_RDWR);
	if (mcs_fd < 0) {
		syslog(LOG_ERR, "open %s device failed, err %d\n", MCS_DEVICE_NAME, mcs_fd);
		return -1;
	}
    ret = ioctl(mcs_fd, IOC_GET_COPY_MSG_MEM, info);
	if (ret < 0) {
		syslog(LOG_ERR, "unable to get shared memory information from mcs device, err: %d\n", ret);
		goto err;
	}

    /* align to page boundary */
    lpa = info->phy_addr;
	pagesize = sysconf(_SC_PAGE_SIZE);
	aligned_addr = (lpa) & ~(pagesize - 1);
	offset = lpa - aligned_addr;
	aligned_size = (offset + info->size + pagesize - 1) & ~(pagesize - 1);
	info->vir_addr = mmap(NULL, aligned_size, PROT_READ | PROT_WRITE, MAP_SHARED, mcs_fd, aligned_addr);
	if (info->vir_addr == MAP_FAILED) {
		syslog(LOG_ERR, "mmap failed \n");
		goto err;
	}
	info->align_size = aligned_size;
    info->phy_addr = aligned_addr;
    return 0;
err:
	close(mcs_fd);
	return -1;
}

/* 当前不支持多实例，使用时赋值为0；支持多实例以后修改成具体实例号 */
int create_sem(int instance_id, sem_t **sem_user_to_micad, sem_t **sem_micad_to_user)
{
    char tmp_name[64] = {0};
    sprintf(tmp_name, SEM_USER_TO_MICAD, instance_id);
    *sem_user_to_micad = sem_open(tmp_name, O_CREAT | O_RDWR, 0666, 0);
    if (*sem_user_to_micad == SEM_FAILED) {
        syslog(LOG_ERR,"sem_user_to_micad open failed");
        return -1;
    }

    sprintf(tmp_name, SEM_MICAD_TO_USER, instance_id);
    *sem_micad_to_user = sem_open(tmp_name, O_CREAT | O_RDWR, 0666, 0);
    if (*sem_micad_to_user == SEM_FAILED) {
	syslog(LOG_ERR,"sem_micad_to_user failed");
        sem_close(*sem_user_to_micad);
        return -1;
    }
    return 0;

}

/* 当前不支持多实例，使用时赋值为0；支持多实例以后修改成具体实例号 */
process_shared_data_t *init_process_shared_memory(int instance_id)
{
    int shm_fd;
    process_shared_data_t *shared_data;
    char tmp_name[64] = {0};

    sprintf(tmp_name, SHM_NAME, instance_id);
    shm_fd = shm_open(tmp_name, O_CREAT | O_RDWR, 0666);
    if (shm_fd == -1) {
	syslog(LOG_ERR,"shm_open open failed");
        return NULL;
    }

    if (ftruncate(shm_fd, sizeof(process_shared_data_t)) == -1) {
        syslog(LOG_ERR,"ftruncate failed");
        close(shm_fd);
        return NULL;
    }

    shared_data = mmap(NULL, sizeof(process_shared_data_t), PROT_READ | PROT_WRITE, MAP_SHARED, shm_fd, 0);
    if (shared_data == MAP_FAILED) {
        syslog(LOG_ERR,"shared_data mmap failed");
        close(shm_fd);
        return NULL;
    }
    close(shm_fd);

    return shared_data;
}