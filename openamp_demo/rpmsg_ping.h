/*
 * SPDX-License-Identifier: BSD-3-Clause
 */

#ifndef RPMSG_PING_H
#define RPMSG_PING_H

#define RPMSG_SERVICE_NAME         "rpmsg-openamp-demo-channel"
void ping(unsigned int ep_id);
int ping_cb(struct rpmsg_endpoint *ept, void *data, size_t len,
			     uint32_t src, void *priv);
int flood_ping(unsigned int ep_id);
int flood_ping_cb(struct rpmsg_endpoint *ept, void *data, size_t len,
			     uint32_t src, void *priv);             
#endif /* RPMSG_PING_H */
