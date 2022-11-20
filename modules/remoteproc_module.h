#ifndef REMOTEPROC_MODULE_H
#define REMOTEPROC_MODULE_H

#include <openamp/remoteproc.h>

#if defined __cplusplus
extern "C" {
#endif

#define CPU_STATE_ON          0
#define CPU_STATE_OFF         1
#define CPU_STATE_ON_PENDING  2

/* create remoteproc */
struct remoteproc *create_remoteproc(void);

/*
 start remoteproc: refet to <openamp/remoteproc.h>
	int remoteproc_start(struct remoteproc *rproc);
*/

/*
 stop remoteproc: refet to <openamp/remoteproc.h>
	int remoteproc_stop(struct remoteproc *rproc);
*/

/*
 remove remoteproc: refet to <openamp/remoteproc.h>
	int remoteproc_remove(struct remoteproc *rproc);
*/

/* destory remoteproc */
void destory_remoteproc(void);

/* acquire cpu power state */
int acquire_cpu_state(void);

extern char *cpu_id;
extern char *target_binaddr;

#if defined __cplusplus
}
#endif

#endif	/* REMOTEPROC_MODULE_H */
