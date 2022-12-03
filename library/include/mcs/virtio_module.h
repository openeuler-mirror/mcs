#ifndef VIRTIO_MODULE_H
#define VIRTIO_MODULE_H

#include <metal/io.h>
#include <openamp/rpmsg_virtio.h>

#if defined __cplusplus
extern "C" {
#endif

void virtio_init(struct client_os_inst *client);
void virtio_deinit(struct client_os_inst *client);


#if defined __cplusplus
}
#endif

#endif  /* VIRTIO_MODULE_H */
