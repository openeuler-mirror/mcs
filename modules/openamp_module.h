#ifndef OPENAMP_MODULE_H
#define OPENAMP_MODULE_H

#include <openamp/open_amp.h>

#include "mcs_common.h"
#include "remoteproc_module.h"
#include "virtio_module.h"
#include "rpmsg_endpoint.h"
#include "rpmsg_rpc_service.h"
#include "rpmsg_sys_service.h"

#if defined __cplusplus
extern "C" {
#endif

extern char *target_binfile;
extern char *target_binaddr;
extern char *cpu_id;
extern int mcs_dev_fd;

/* initialize openamp module, including remoteproc, virtio, rpmsg */
int openamp_init(void);

/* release openamp resource */
void openamp_deinit(void);

#if defined __cplusplus
}
#endif

#endif  /* OPENAMP_MODULE_H */
