#ifndef RPMSG_MATRIX_MULTIPLY_H
#define RPMSG_MATRIX_MULTIPLY_H

#if defined __cplusplus
extern "C" {
#endif


int matrix_endpoint_cb(struct rpmsg_endpoint *ept, void *data,
		size_t len, uint32_t src, void *priv);
void cal_matrix(unsigned int ep_id);

#if defined __cplusplus
}
#endif

#endif  /* RPMSG_MATRIX_MULTIPLY_H */
