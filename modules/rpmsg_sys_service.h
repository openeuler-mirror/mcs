#ifndef RPMSG_SYS_SERVICE_H
#define RPMSG_SYS_SERVICE_H

#if defined __cplusplus
extern "C" {
#endif


#define RPMSG_SYS_SERVICE_POWER_OFF 1


int rpmsg_sys_service_init(void);
int sys_service_power_off(int client);

#if defined __cplusplus
}
#endif

#endif /* RPMSG_SYS_SERVICE_H */
