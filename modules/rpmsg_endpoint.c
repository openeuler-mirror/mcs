#include <stdio.h>
#include <fcntl.h>
#include <poll.h>
#include <pthread.h>

#include "openamp_module.h"

#define RPMSG_SERVICE_NUM_ENDPOINTS 5

static struct {
	struct rpmsg_endpoint ep;
	const char *name;
	rpmsg_ept_cb cb;
	rpmsg_ns_unbind_cb unbind_cb;
	volatile bool bound;
} endpoints[RPMSG_SERVICE_NUM_ENDPOINTS];

static void rpmsg_service_unbind(struct rpmsg_endpoint *ept)
{
	rpmsg_destroy_ept(ept);
}

void ns_bind_cb(struct rpmsg_device *rdev,
					const char *name,
					uint32_t dest)
{
	int err;

	for (int i = 0; i < RPMSG_SERVICE_NUM_ENDPOINTS; ++i) {
		if (endpoints[i].name && (strcmp(name, endpoints[i].name) == 0)) {
			printf("found matched endpoint, creating ep in host os\n");
			/* create the endpoint from host side and allocate an address */
			err = rpmsg_create_ept(&endpoints[i].ep,
						   rdev,
						   name,
						   RPMSG_ADDR_ANY,
						   dest,
						   endpoints[i].cb,
						   endpoints[i].unbind_cb);

			if (err != 0) {
				printf("Creating remote endpoint %s failed wirh error %d", name, err);
			} else {
				endpoints[i].bound = true;
				/* send an empty msg to notify the bound endpoint's address */
				rpmsg_send(&endpoints[i].ep, (char *)"", 0);
			}

			return;
		}
	}

	printf("Remote endpoint %s not registered locally", name);
}

int rpmsg_service_register_endpoint(const char *name, rpmsg_ept_cb cb,
						rpmsg_ns_unbind_cb unbind_cb, void *priv)
{
	if (name == NULL || cb == NULL) {
		return -1;
	}

	for (int i = 0; i < RPMSG_SERVICE_NUM_ENDPOINTS; ++i) {
		if (!endpoints[i].name) {
			endpoints[i].name = name;
			endpoints[i].cb = cb;
			endpoints[i].unbind_cb = unbind_cb ? unbind_cb : rpmsg_service_unbind;
			endpoints[i].ep.priv = priv;
			return i;
		}
	}

	printf("No free slots to register endpoint %s", name);

	return -1;
}

int rpmsg_service_unregister_endpoint(unsigned int endpoint_id)
{
	if (endpoint_id < RPMSG_SERVICE_NUM_ENDPOINTS) {
		if (endpoints[endpoint_id].name) {
			endpoints[endpoint_id].name = NULL;

			rpmsg_destroy_ept(&endpoints[endpoint_id].ep);
		}
	}
}

bool rpmsg_service_endpoint_is_bound(unsigned int endpoint_id)
{
	if (endpoint_id < RPMSG_SERVICE_NUM_ENDPOINTS) {
		return endpoints[endpoint_id].bound;
	}

	return false;
}

void rpmsg_service_endpoint_bound(unsigned int endpoint_id)
{
	if (endpoint_id < RPMSG_SERVICE_NUM_ENDPOINTS) {
		endpoints[endpoint_id].bound = true;
	}
}

const char * rpmsg_service_endpoint_name(unsigned int endpoint_id)
{
	if (endpoint_id < RPMSG_SERVICE_NUM_ENDPOINTS) {
		return endpoints[endpoint_id].name;
	}

	return NULL;
}

int rpmsg_service_send(unsigned int endpoint_id, const void *data, size_t len)
{
	if (endpoint_id < RPMSG_SERVICE_NUM_ENDPOINTS) {
		return rpmsg_send(&endpoints[endpoint_id].ep, data, len);
	}

	return -1;
}

void *rpmsg_service_receive_loop(void *arg)
{
	int ret;
	int dev_fd;
	struct pollfd fds;

	dev_fd = open(MCS_DEVICE_NAME, O_RDWR);
	if (dev_fd < 0) {
		printf("rpmsg_receive_message: open %s device failed.\n", MCS_DEVICE_NAME);
		return (void*)-1;
	}

	fds.fd = dev_fd;
	fds.events = POLLIN;

	while (1) {
		ret = poll(&fds, 1, -1);
		if (ret < 0) {
			printf("rpmsg_receive_message: poll failed.\n");
			break;
		}

		if (fds.revents & POLLIN) {
			virtqueue_notification(vq[0]);  /* will call endpoint_cb or ns_bind_cb */
		}
	}

	close(dev_fd);
	return (void*)0;
}
