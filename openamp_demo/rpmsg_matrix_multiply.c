#include <stdlib.h>
#include <stdio.h>
#include <fcntl.h>
#include <errno.h>
#include <unistd.h>
#include <string.h>
#include <pthread.h>

#include "openamp_module.h"

/* define the keys according to your terminfo */
#define KEY_CTRL_D      4

#define	MAX_SIZE      6
#define NUM_MATRIX    2

#define raw_printf(format, ...) printf(format, ##__VA_ARGS__)
#define LPRINTF(format, ...) raw_printf("CLIENT> " format, ##__VA_ARGS__)
#define LPERROR(format, ...) LPRINTF("ERROR: " format, ##__VA_ARGS__)

typedef struct _matrix {
	unsigned int size;
	unsigned int elements[MAX_SIZE][MAX_SIZE];
} matrix;

/* Globals */
static struct rpmsg_endpoint lept;
static struct _matrix i_matrix[2];
static struct _matrix e_matrix;
static unsigned int result_returned = 0;
static int err_cnt = 0;
static int ept_deleted = 0;

static void matrix_print(struct _matrix *m)
{
	unsigned int i, j;

	/* Generate two random matrices */
	LPRINTF("Printing matrix... \r\n");

	for (i = 0; i < m->size; ++i) {
		for (j = 0; j < m->size; ++j)
			raw_printf(" %u ", m->elements[i][j]);
		raw_printf("\r\n");
	}
}

static void generate_matrices(int num_matrices,
			      unsigned int matrix_size, void *p_data)
{
	unsigned int i, j, k;
	struct _matrix *p_matrix = p_data;
	unsigned long value;


	for (i = 0; i < (unsigned int)num_matrices; i++) {
		/* Initialize workload */
		p_matrix[i].size = matrix_size;

		LPRINTF("Generate matrix %d \r\n", i);
		for (j = 0; j < matrix_size; j++) {
			raw_printf("\r\n");
			for (k = 0; k < matrix_size; k++) {

				value = (rand() & 0x7F);
				value = value % 10;
				p_matrix[i].elements[j][k] = value;
				raw_printf(" %u ", p_matrix[i].elements[j][k]);
			}
		}
		raw_printf("\r\n");
	}

}

static void matrix_multiply(const matrix * m, const matrix * n, matrix * r)
{
	unsigned int i, j, k;

	memset(r, 0x0, sizeof(matrix));
	r->size = m->size;

	for (i = 0; i < m->size; ++i) {
		for (j = 0; j < n->size; ++j) {
			for (k = 0; k < r->size; ++k) {
				r->elements[i][j] +=
				    m->elements[i][k] * n->elements[k][j];
			}
		}
	}
}

int matrix_endpoint_cb(struct rpmsg_endpoint *ept, void *data,
		size_t len, uint32_t src, void *priv)
{
    int ret;    
    struct _matrix *r_matrix = (struct _matrix *)data;
	int i, j;

	(void)ept;
	(void)priv;
	(void)src;
	if (len != sizeof(struct _matrix)) {
		LPERROR("Received matrix is of invalid len: %d:%lu\r\n",
			(int)sizeof(struct _matrix), (unsigned long)len);
		err_cnt++;
		return 0;
	}
	for (i = 0; i < MAX_SIZE; i++) {
		for (j = 0; j < MAX_SIZE; j++) {
			if (r_matrix->elements[i][j] !=
				e_matrix.elements[i][j]) {
				err_cnt++;
				break;
			}
		}
	}
	if (err_cnt) {
		LPERROR("Result mismatched...\r\n");
		LPERROR("Expected matrix:\r\n");
		matrix_print(&e_matrix);
		LPERROR("Actual matrix:\r\n");
		matrix_print(r_matrix);
	} else {
		result_returned = 1;
        LPRINTF("Matrix multiply Result: \n");
        matrix_print(r_matrix);
        LPRINTF("Result is correct, pass!!!!!\n");
	}
    return 0;
}

void cal_matrix(unsigned int ep_id)
{
    int ret;
	srand(time(NULL));
	for (int c = 0; c < 1; c++) {
		generate_matrices(2, MAX_SIZE, i_matrix);
		matrix_multiply(&i_matrix[0], &i_matrix[1],
				&e_matrix);
		result_returned = 0;
        ret = rpmsg_service_send(ep_id, i_matrix, sizeof(i_matrix));
    }
}