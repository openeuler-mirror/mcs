#ifndef VIRTIO_MODULE_H
#define VIRTIO_MODULE_H

#include <metal/io.h>
#include <openamp/rpmsg_virtio.h>

#if defined __cplusplus
extern "C" {
#endif

#define VDEV_START_ADDR            0x70000000
#define VDEV_SIZE                  0x30000

#define VDEV_STATUS_ADDR           VDEV_START_ADDR
#define VDEV_STATUS_SIZE           0x4000

#define SHM_START_ADDR             (VDEV_START_ADDR + VDEV_STATUS_SIZE)
#define SHM_SIZE                   (VDEV_SIZE - VDEV_STATUS_SIZE)

#define VRING_COUNT                2
#define VRING_RX_ADDRESS           (VDEV_START_ADDR + SHM_SIZE - VDEV_STATUS_SIZE)
#define VRING_TX_ADDRESS           (VDEV_START_ADDR + SHM_SIZE)
#define VRING_ALIGNMENT            4
#define VRING_SIZE                 16

#define TXADDR(SHMADDR)     (SHMADDR + VRING_TX_ADDRESS - VDEV_START_ADDR)
#define RXADDR(SHMADDR)     (SHMADDR + VRING_RX_ADDRESS - VDEV_START_ADDR)
#define VDEVADDR(SHMADDR)   (SHMADDR + VDEV_STATUS_ADDR - VDEV_START_ADDR)
#define SHMEMADDR(SHMADDR)  (SHMADDR + SHM_START_ADDR - VDEV_START_ADDR)

void virtio_init(void);
void virtio_deinit(void);


extern struct virtqueue *vq[2];
extern void *shmaddr;
extern struct rpmsg_device *rdev;

#if defined __cplusplus
}
#endif

#endif  /* VIRTIO_MODULE_H */
