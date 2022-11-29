#include <stdio.h>
#include <metal/alloc.h>
#include <metal/io.h>
#include <sys/ioctl.h>

#include "openamp_module.h"


struct rproc_priv {
    struct remoteproc *rproc;  /* pass a remoteproc instance pointer */
    unsigned int idx;          /* remoteproc instance idx */
    unsigned int cpu_id;       /* related arg: cpu id */
    unsigned long boot_address; /* related arg: boot address(in hex format) */
};

static struct remoteproc rproc_inst;

static struct remoteproc *rproc_init(struct remoteproc *rproc,
                                     const struct remoteproc_ops *ops, void *args)
{
    struct rproc_priv *priv;

    (void)rproc;

    priv = metal_allocate_memory(sizeof(struct rproc_priv ));
    if (!priv) {
        return NULL;
    }

    memcpy(priv, (struct rproc_priv *)args, sizeof(struct rproc_priv));
    priv->rproc->ops = ops;
    metal_list_init(&priv->rproc->mems);
    priv->rproc->priv = priv;
    rproc->state = RPROC_READY;

    return priv->rproc;
}

static void rproc_remove(struct remoteproc *rproc)
{
    struct rproc_priv *priv;

    priv = (struct rproc_priv *)rproc->priv;
    metal_free_memory(priv);
}

static int rproc_start(struct remoteproc *rproc)
{
    int ret;
    unsigned long boot_args[2];
    struct rproc_priv *args = (struct rproc_priv *)rproc->priv;

    int fd = open(MCS_DEVICE_NAME, O_RDWR);
    if (fd < 0) {
        printf("failed to open %s device.\n", MCS_DEVICE_NAME);
        return fd;
    }

    boot_args[0] = args->cpu_id;
    boot_args[1] = args->boot_address;
    ret = ioctl(fd, IOC_CPUON, boot_args);
    if (ret) {
        printf("boot clientos failed\n");
        return ret;
    }

    close(fd);
    return 0;
}

static int rproc_stop(struct remoteproc *rproc)
{
    /* TODO: send order to clientos by RPC service, clientos shut itself down by PSCI */
    printf("stop rproc\n");

    sys_service_power_off(0);

    return 0;
}

const struct remoteproc_ops rproc_ops = {
    .init = rproc_init,
    .remove = rproc_remove,
    .start = rproc_start,
    .stop = rproc_stop,
};

struct remoteproc *create_remoteproc(void)
{
    struct remoteproc *rproc;
    struct rproc_priv args;

    args.rproc = &rproc_inst;
    args.idx = 1;
    args.cpu_id = strtol(cpu_id, NULL, STR_TO_DEC);
    args.boot_address = strtol(target_binaddr, NULL, STR_TO_HEX);
    rproc = remoteproc_init(&rproc_inst, &rproc_ops, &args);
    if (!rproc) {
        return NULL;
    }

    return rproc;
}

void destory_remoteproc(void)
{
    remoteproc_stop(&rproc_inst);
    rproc_inst.state = RPROC_OFFLINE;

    if (rproc_inst.priv) {
        remoteproc_remove(&rproc_inst);
    }
}

int acquire_cpu_state(void)
{
    int ret;
    int fd;
    unsigned long state_arg;

    fd = open(MCS_DEVICE_NAME, O_RDWR);
    if (fd < 0) {
        printf("open %s device failed\n", MCS_DEVICE_NAME);
        return fd;
    }

    state_arg = strtol(cpu_id, NULL, STR_TO_DEC);
    ret = ioctl(fd, IOC_AFFINITY_INFO, &state_arg);
    if (ret) {
        printf("acquire cpu state failed\n");
        return ret;
    }

    close(fd);
    return state_arg;  /* secondary core power state */
}
