/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */

#define _GNU_SOURCE
#include <sys/socket.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/ioctl.h>
#include <net/if.h>
#include <sys/uio.h>
#include <fcntl.h>
#include <stdarg.h>
#include <unistd.h>
#include <string.h>
#include <errno.h>
#include <netdb.h>
#include <poll.h>
#include <sys/select.h>
#include <syscall.h>

#include <openamp/rpmsg_rpc_client_server.h>
#include <openamp/rpmsg.h>

#include "rpc_internal_model.h"
#include "rpc_server_internal.h"
#include "rpc_err.h"
#include "rpmsg_rpc.h"

#define DEFINE_VARS(name)                        \
	void *req_ptr = data;                        \
	rpc_##name##_req_t *req = req_ptr;           \
	rpc_##name##_resp_t resp;                    \
	size_t payload_size = sizeof(resp);          \
	int ret = 0;

#ifdef MULTI_WORKERS
#define CLEANUP(data) free(data)
#else
#define CLEANUP(data)
#endif

#define MIN(x, y) (((x) < (y)) ? (x) : (y))
#define RPC_BUFFER_SIZE 2048

int rpc_server_send(struct rpmsg_endpoint *ept, uint32_t rpc_id, int status, void *request_param, size_t param_size)
{
	struct rpmsg_proxy_answer msg;

	if (param_size > PROXY_MAX_BUF_LEN)
		return -EINVAL;

	msg.id = rpc_id;
	msg.status = status;
	memcpy(msg.params, request_param, param_size);
	return rpmsg_send(ept, &msg, MAX_FUNC_ID_LEN + param_size);
}

static int rpmsg_handle_open(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_read(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_write(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_close(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_lseek(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fcntl(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_ioctl(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_unlink(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_getdents64(void *data, struct rpc_instance *inst, void *priv);

static int rpmsg_handle_freeaddrinfo(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_getaddrinfo(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_gethostbyaddr(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_gethostbyname(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_poll(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_getpeername(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_gethostname(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_getsockname(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_getsockopt(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_select(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_accept(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_bind(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_connect(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_listen(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_recv(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_recvfrom(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_send(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_sendto(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_setsockopt(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_shutdown(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_socket(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_printf(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fopen(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fclose(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fread(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fwrite(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_freopen(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fputs(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fgets(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_feof(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fprintf(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_getc(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_ferror(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_getc_unlocked(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_pclose(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_tmpfile(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_clearerr(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_popen(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_ungetc(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fseeko(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_ftello(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fseek(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_ftell(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_rename(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_remove(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_mkstemp(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fflush(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_getwc(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_putwc(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_putc(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_ungetwc(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_stat(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_lstat(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_getcwd(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fstat(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fdopen(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fileno(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_setvbuf(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_system(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_readlink(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_access(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_dup2(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_mkfifo(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_chmod(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_chdir(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_mkdir(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_rmdir(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_pipe(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_fscanfx(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_ifnameindex(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_putchar(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_gaistrerror(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_accept4(void *data, struct rpc_instance *inst, void *priv);
static int rpmsg_handle_writev(void *data, struct rpc_instance *inst, void *priv);

/* Service table for rpc_instance */
static struct rpc_instance service_inst;
static struct rpc_service service_table[] = {
	{OPEN_ID, &rpmsg_handle_open},
	{READ_ID, &rpmsg_handle_read},
	{WRITE_ID, &rpmsg_handle_write},
	{CLOSE_ID, &rpmsg_handle_close},
	{LSEEK_ID, &rpmsg_handle_lseek},
	{FCNTL_ID, &rpmsg_handle_fcntl},
	{IOCTL_ID, &rpmsg_handle_ioctl},
	{UNLINK_ID, &rpmsg_handle_unlink},
	{GETDENTS64_ID, &rpmsg_handle_getdents64},
	{FREEADDRINFO_ID, &rpmsg_handle_freeaddrinfo},
	{GETADDRINFO_ID, &rpmsg_handle_getaddrinfo},
	{GETHOSTBYADDR_ID, &rpmsg_handle_gethostbyaddr},
	{GETHOSTBYNAME_ID, &rpmsg_handle_gethostbyname},
	{POLL_ID, &rpmsg_handle_poll},
	{GETPEERNAME_ID, &rpmsg_handle_getpeername},
	{GETHOSTNAME_ID, &rpmsg_handle_gethostname},
	{GETSOCKNAME_ID, &rpmsg_handle_getsockname},
	{GETSOCKOPT_ID, &rpmsg_handle_getsockopt},
	{SELECT_ID, &rpmsg_handle_select},
	{ACCEPT_ID, &rpmsg_handle_accept},
	{BIND_ID, &rpmsg_handle_bind},
	{CONNECT_ID, &rpmsg_handle_connect},
	{LISTEN_ID, &rpmsg_handle_listen},
	{RECV_ID, &rpmsg_handle_recv},
	{RECVFROM_ID, &rpmsg_handle_recvfrom},
	{SEND_ID, &rpmsg_handle_send},
	{SENDTO_ID, &rpmsg_handle_sendto},
	{SETSOCKOPT_ID, &rpmsg_handle_setsockopt},
	{SHUTDOWN_ID, &rpmsg_handle_shutdown},
	{SOCKET_ID, &rpmsg_handle_socket},
	{FOPEN_ID, &rpmsg_handle_fopen},
	{FCLOSE_ID, &rpmsg_handle_fclose},
	{FREAD_ID, &rpmsg_handle_fread},
	{FWRITE_ID, &rpmsg_handle_fwrite},
	{FREOPEN_ID, &rpmsg_handle_freopen},
	{FPUTS_ID, &rpmsg_handle_fputs},
	{FGETS_ID, &rpmsg_handle_fgets},
	{FEOF_ID, &rpmsg_handle_feof},
	{FPRINTF_ID, &rpmsg_handle_fprintf},
	{GETC_ID, &rpmsg_handle_getc},
	{FERROR_ID, &rpmsg_handle_ferror},
	{GETC_UNLOCK_ID, &rpmsg_handle_getc_unlocked},
	{PCLOSE_ID, &rpmsg_handle_pclose},
	{TMPFILE_ID, &rpmsg_handle_tmpfile},
	{CLEARERR_ID, &rpmsg_handle_clearerr},
	{POPEN_ID, &rpmsg_handle_popen},
	{UNGETC_ID, &rpmsg_handle_ungetc},
	{FSEEKO_ID, &rpmsg_handle_fseeko},
	{FTELLO_ID, &rpmsg_handle_ftello},
	{FSEEK_ID, &rpmsg_handle_fseek},
	{FTELL_ID, &rpmsg_handle_ftell},
	{RENAME_ID, &rpmsg_handle_rename},
	{REMOVE_ID, &rpmsg_handle_remove},
	{MKSTMP_ID, &rpmsg_handle_mkstemp},
	{STAT_ID, &rpmsg_handle_stat},
	{LSTAT_ID, &rpmsg_handle_lstat},
	{GETCWD_ID, &rpmsg_handle_getcwd},
	{PRINTF_ID, &rpmsg_handle_printf},
	{FFLUSH_ID, &rpmsg_handle_fflush},
	{GETWC_ID, &rpmsg_handle_getwc},
	{PUTWC_ID, &rpmsg_handle_putwc},
	{PUTC_ID, &rpmsg_handle_putc},
	{UNGETWC_ID, &rpmsg_handle_ungetwc},
	{FSTAT_ID, &rpmsg_handle_fstat},
	{FDOPEN_ID, &rpmsg_handle_fdopen},
	{FILENO_ID, &rpmsg_handle_fileno},
	{SETVBUF_ID, &rpmsg_handle_setvbuf},
	{SYSTEM_ID, &rpmsg_handle_system},
	{READLINK_ID, &rpmsg_handle_readlink},
	{ACCESS_ID, &rpmsg_handle_access},
	{DUP2_ID, &rpmsg_handle_dup2},
	{MKFIFO_ID, &rpmsg_handle_mkfifo},
	{CHMOD_ID, &rpmsg_handle_chmod},
	{CHDIR_ID, &rpmsg_handle_chdir},
	{MKDIR_ID, &rpmsg_handle_mkdir},
	{RMDIR_ID, &rpmsg_handle_rmdir},
	{PIPE_ID, &rpmsg_handle_pipe},
	{FSCANFX_ID, &rpmsg_handle_fscanfx},
	{IFNAMEINDEX_ID, &rpmsg_handle_ifnameindex},
	{PUTCHAR_ID, &rpmsg_handle_putchar},
	{GAISTRERROR_ID, &rpmsg_handle_gaistrerror},
	{ACCEPT4_ID, &rpmsg_handle_accept4},
	{WRITEV_ID, &rpmsg_handle_writev},
};

#define LOG_PATH "/tmp/rpc_accesslog"
#define LOG_RPC "/tmp/rpc_log.txt"
#define WBUF_LEN         0x200

static int lfd;
static FILE *fp;

static int __lprintf(const char *fmt, va_list list)
{
	int len;
	char buf[WBUF_LEN] = {0};

	len = vsnprintf(buf, WBUF_LEN, fmt, list);
	if (len < 0) {
		return len;
	}
	return write(lfd, buf, len);
}

int lprintf(const char *fmt, ...)
{
	va_list list;
	int count;

	va_start(list, fmt);
	count = __lprintf(fmt, list);
	va_end(list);
	return count;
}

static void lerror(int ret, int errnum)
{
	if (ret < 0) {
		lprintf("errstr:%s\n", strerror(errnum));
	}
}

static inline void set_rsp_base(rpc_resp_base_t *base, uint32_t trace_id)
{
	base->trace_id = trace_id;
	base->errnum = errno;
	errno = 0;
}

static int rpmsg_init_rpc_server(struct rpc_instance *inst,
			const struct rpc_service *services, int len)
{
	int ret = 0;

	/* parameter check */
	if (inst == NULL || services == NULL || len == 0) {
		return -1;
	}

	inst->services = services;
	inst->n_services = len;

	if (ret < 0) {
		lprintf("Creating endpoint %s failed with error %d", RPMSG_RPC_SERVICE_NAME, ret);
		return ret;
	}
	return ret;
}

static const struct rpc_service *find_service(struct rpc_instance *inst,
				unsigned int id)
{
	const struct rpc_service *service;

	for (unsigned int i = 0; i < inst->n_services; i++) {
		service = &inst->services[i];

		if (service->id == id) {
			return service;
		}
	}
	return NULL;
}

int rpmsg_rpc_server_cb(struct rpmsg_endpoint *ept, void *data,
					size_t len, uint32_t src, void *priv)
{
#ifdef MULTI_WORKERS
	unsigned char *buf;
#else
	unsigned char buf[RPC_BUFFER_SIZE];
#endif
	unsigned long id;
	struct rpc_instance *inst;
	(void)src;

	lprintf("ccb: src %x, len %lu\n", src, len);

	inst = &service_inst;
	id = *(unsigned long *)data;
	lprintf("fun_id:%d\n", id);
	if (len > RPC_BUFFER_SIZE) {
		lprintf("overlong data\n");
		rpc_server_send(ept, id, RPC_EOVERLONG, NULL, 0);
		return -EINVAL;
	}

#ifdef MULTI_WORKERS
	buf = malloc(len * sizeof(unsigned char));
	if (buf == NULL) {
		rpc_server_send(ept, id, RPC_ENOMEM, NULL, 0);
		return RPMSG_ERR_NO_MEM;
	}
#endif
	memcpy(buf, data, len);

	const struct rpc_service *service = find_service(inst, id);

	if (service) {
		priv = ept;
#ifdef MULTI_WORKERS
		enqueue_req(build_req(buf, service, inst, priv));
#else
		if (service->cb_function(buf, inst, priv)) {
			/**
			 * In this case, the client proactively detects a timeout
			 * failure and we do not send a response for the failure.
			 */
			lprintf("Service failed at rpc id: %u\r\n", id);
		}
#endif
	} else {
		lprintf("Handling remote procedure call errors: rpc id %u\r\n", id);
		rpc_server_send(ept, id, RPMSG_RPC_INVALID_ID, NULL, 0);
	}
	return RPMSG_SUCCESS;
}

int rpmsg_rpc_service_init(void)
{
	int ret;
	unsigned int n_services = sizeof(service_table) / sizeof(service_table[0]);

	lfd = open(LOG_PATH, O_CREAT | O_RDWR | O_APPEND, 0600);
	if (lfd < 0) {
		lfd = STDOUT_FILENO;
	}

	fp = fopen(LOG_RPC, "a+");
	if (fp == NULL) {
		if (lfd != STDOUT_FILENO) {
			lprintf("Failed to open or create file");
			close(lfd);
		}
		return -ENOMEM;
	}

	lprintf("number of services: %d, %p\n", n_services, service_table);
	ret = rpmsg_init_rpc_server(&service_inst, service_table, n_services);
#ifdef MULTI_WORKERS
	workers_init();
#endif
	return ret;
}

void rpmsg_rpc_service_terminate(void)
{
	if (fp != NULL) {
		fclose(fp);
	}

	if (lfd != STDOUT_FILENO) {
		lprintf("Destroying endpoint.\r\n");
		close(lfd);
	}
}

#define STDFILE_BASE 1

static inline FILE *handle2file(uintptr_t fhandle, void *priv)
{
	if (fhandle == STDOUT_FILENO + STDFILE_BASE ||
		fhandle == STDIN_FILENO + STDFILE_BASE ||
		fhandle == STDERR_FILENO + STDFILE_BASE) {
		return fp;
	}
	return (FILE *)fhandle;
}

static int is_pty_fd(uintptr_t fhandle)
{
	return (fhandle == STDOUT_FILENO + STDFILE_BASE ||
		fhandle == STDIN_FILENO + STDFILE_BASE ||
		fhandle == STDERR_FILENO + STDFILE_BASE);
}

static inline int file2fd(FILE *f)
{
	if (f == NULL) {
		return -1;
	}
	return f->_fileno;
}

static int rpmsg_handle_open(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(open)
	char *buf;
	int fd;

	if (!req || !inst)
		return -EINVAL;
	buf = req->buf;
	lprintf("==open(%s)\n", buf);
	/* Open remote fd */
	fd = open(buf, req->flags, req->mode);
	lprintf("==open ret:%d\n", fd);
	lerror(fd, errno);
	/* Construct rpc response */
	resp.ret = fd;
	set_rsp_base(&resp.super, req->trace_id);
	/* Transmit rpc response */
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, OPEN_ID, RPMSG_RPC_OK, &resp,
					payload_size);
	lprintf("==open send rsp:%d, %d\n", resp.ret, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_close(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(close)

	if (!req || !inst)
		return -EINVAL;
	lprintf("==close(%d)\n", req->fd);
	/* Close remote fd */
	ret = close(req->fd);
	lprintf("==close ret(%d)\n", ret);
	lerror(ret, errno);
	/* Construct rpc response */
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	/* Transmit rpc response */
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, CLOSE_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==close send rsp:%d, %d\n", req->fd, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_read(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(read)
	ssize_t sret;

	if (!req || !inst)
		return -EINVAL;
	lprintf("==read(%d, %d)\n", req->fd, req->count);
	if (req->fd == 0) {
		sret = MAX_STRING_LEN;
		/* Perform read from fd for large size since this is a
		 * STD/I request
		 */
		sret = read(req->fd, resp.buf, sret);
	} else {
		/* Perform read from fd */
		sret = read(req->fd, resp.buf, req->count);
	}
	lprintf("==read ret %ld\n", sret);
	lerror((int)sret, errno);
	/* Construct rpc response */
	resp.ret = sret;
	set_rsp_base(&resp.super, req->trace_id);

	payload_size -= sizeof(resp.buf);
	if (sret > 0) {
		payload_size += sret;
	}

	/* Transmit rpc response */
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, READ_ID, RPMSG_RPC_OK, &resp,
					payload_size);
	lprintf("==read send rsp:%d, %d\n", req->fd, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_write(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(write)
	ssize_t sret;

	if (!req || !inst)
		return -EINVAL;
	lprintf("==write(%d, %d)\n", req->fd, req->count);
	/* Write to remote fd */
	sret = write(req->fd, req->buf, req->count);
	lprintf("==write ret:%ld\n", sret);
	lerror((int)sret, errno);
	/* Construct rpc response */
	resp.ret = sret;
	set_rsp_base(&resp.super, req->trace_id);

	/* Transmit rpc response */
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, WRITE_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==write send rsp:%d, %d\n", req->fd, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_lseek(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(lseek)
	off_t off;

	if (!req || !inst)
		return -EINVAL;
	lprintf("==lseek(%d, %ld, %d)\n", req->fd, req->offset, req->whence);
	off = lseek(req->fd, req->offset, req->whence);
	lprintf("==lseek ret:%ld\n", off);
	lerror((int)off, errno);
	resp.ret = off;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, LSEEK_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==lseek send rsp:%d,%d\n", req->fd, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_fcntl(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(fcntl)

	if (!req || !inst)
		return -EINVAL;
	lprintf("==fcntl(%d, %d, %lu)\n", req->fd, req->cmd, req->arg);
	ret = fcntl(req->fd, req->cmd, req->arg);
	lprintf("==fcntl ret:%d\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FCNTL_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==fcntl send rsp:%d, %d\n", req->fd, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_ioctl(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(ioctl)

	if (!req || !inst)
		return -EINVAL;
	lprintf("==ioctl(%d, %ld)\n", req->fd, req->request);
	ret = ioctl(req->fd, req->request, req->buf);
	lprintf("==ioctl ret:%d\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	resp.len = req->len;
	set_rsp_base(&resp.super, req->trace_id);
	payload_size -= sizeof(req->buf);
	if (req->len > 0) {
		memcpy(resp.buf, req->buf, req->len);
		payload_size += req->len;
	}
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, IOCTL_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==ioctl send rsp:%d,%d\n", req->fd, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_unlink(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(unlink)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==unlink(%s)\n", req->buf);
	ret = unlink(req->buf);
	lprintf("==unlink ret:%d\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, IOCTL_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==unlink send rsp:%s,%d\n", req->buf, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_freeaddrinfo(void *data, struct rpc_instance *inst, void *priv)
{
	(void)data;
	(void)inst;
	lprintf("UNUSED\n");
	return 0;
}

static int rpmsg_handle_getaddrinfo(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(getaddrinfo)
	char *node = NULL, *service = NULL;
	struct addrinfo *hints = NULL, *res = NULL;

	if (!req || !inst || req->node >= req->buflen || req->service >= req->buflen)
		return -EINVAL;

	if (req->hints_cnt > 0) {
		ret = decode_addrlist(req->buf, req->hints_cnt, sizeof(req->buf), &hints);
		if (ret < 0) {
			lprintf("==getaddrinfo decode failed(%d)\n", ret);
			goto response;
		}
	}
	if (req->service > req->node) {
		node = &req->buf[req->node];
	}
	if (req->buflen > req->service) {
		service = &req->buf[req->service];
	}
	lprintf("==getaddrinfo(%s, %s)\n", node, service);
	ret = getaddrinfo(node, service, hints, &res);
	lprintf("==getaddrinfo ret:%d\n", ret);
	if (ret < 0) {
		goto response;
	}
	lerror(ret, errno);
	resp.cnt = 0;
	resp.buflen = sizeof(resp.buf);
	payload_size -= sizeof(resp.buf);

	if (res != NULL) {
		resp.cnt = encode_addrlist(res, resp.buf, &resp.buflen);
		payload_size += resp.buflen;
	} else {
		resp.cnt = 0;
		resp.buflen = 0;
	}

response:
	if (res != NULL) {
		freeaddrinfo(res);
	}
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETADDRINFO_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==getaddrinfo send rsp:%d,%d\n", payload_size, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

/* paddr: print the IP address in a standard decimal dotted format */
static void paddr(unsigned char *a)
{
	lprintf("%d.%d.%d.%d\n", a[0], a[1], a[2], a[3]);
}

static void print_host(struct hostent *hp)
{
	int i;

	if (hp == NULL) {
		return;
	}
	lprintf("name:%s, %d\n", hp->h_name, hp->h_length);
	for (i = 0; hp->h_addr_list[i] != 0; i++) {
		paddr((unsigned char *) hp->h_addr_list[i]);
	}
	for (i = 0; hp->h_aliases[i] != 0; i++) {
		lprintf("alias:%s\n", hp->h_aliases[i]);
	}
}

static int rpmsg_handle_gethostbyaddr(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(gethostbyaddr)
	struct hostent *ht;

	if (!req || !inst)
		return -EINVAL;

	lprintf("==gethostbyaddr(%d, %d)\n", req->len, req->type);
	ht = gethostbyaddr(req->buf, req->len, req->type);
	lprintf("==gethostbyaddr ret:%p\n", ht);
	if (ht == NULL) {
		lprintf("errstr:%s\n", strerror(errno));
	}
	payload_size -= sizeof(resp.buf);
	set_rsp_base(&resp.super, req->trace_id);
	print_host(ht);
	if (ht == NULL) {
		resp.len = 0;
		goto response;
	}
	resp.len = encode_hostent(ht, resp.buf, sizeof(resp.buf));
	if (resp.len >= 0) {
		payload_size += resp.len;
	}
response:
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETHOSTBYADDR_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==gethostbyaddr send rsp, %d, %d\n", ret, resp.len);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_gethostbyname(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(gethostbyname)
	struct hostent *ht;

	if (!req || !inst)
		return -EINVAL;

	lprintf("==gethostbyname(%s)\n", req->buf);
	ht = gethostbyname(req->buf);
	lprintf("==gethostbyname ret:%p\n", ht);
	if (ht == NULL) {
		lprintf("errstr:%s\n", strerror(errno));
	}
	payload_size -= sizeof(resp.buf);
	set_rsp_base(&resp.super, req->trace_id);
	print_host(ht);

	if (ht == NULL) {
		resp.len = 0;
		goto response;
	}
	resp.len = encode_hostent(ht, resp.buf, sizeof(resp.buf));
	if (resp.len >= 0) {
		payload_size += resp.len;
	}
response:
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETHOSTBYNAME_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==gethostbyname send rsp, %d, %d\n", ret, resp.len);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_getpeername(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(getpeername)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==getpeername(%d, %d)\n", req->sockfd, req->addrlen);
	ret = getpeername(req->sockfd, (struct sockaddr *)req->addr_buf,
					  (socklen_t *)&req->addrlen);
	lprintf("==getpeername ret:%d\n", ret);
	lerror(ret, errno);

	payload_size -= sizeof(resp.addr_buf);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	if (req->addrlen > 0) {
		payload_size += req->addrlen;
		memcpy(resp.addr_buf, req->addr_buf, req->addrlen);
	}
	resp.addrlen = req->addrlen;

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETPEERNAME_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==getpeername send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_getsockname(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(getsockname)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==getsockname(%d, %d)\n", req->sockfd, req->addrlen);
	ret = getsockname(req->sockfd, (struct sockaddr *)req->addr_buf,
					  (socklen_t *)&req->addrlen);
	lprintf("==getsockname ret:%d\n", ret);
	lerror(ret, errno);
	payload_size -= sizeof(resp.addr_buf);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	if (req->addrlen > 0) {
		payload_size += req->addrlen;
		memcpy(resp.addr_buf, req->addr_buf, req->addrlen);
	}
	resp.addrlen = req->addrlen;

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETSOCKNAME_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==getsockname send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_accept(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(accept)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==accept(%d, %d)\n", req->sockfd, req->addrlen);
	if (req->addrlen > 0) {
		ret = accept(req->sockfd, (struct sockaddr *)req->addr_buf, &req->addrlen);
	} else {
		ret = accept(req->sockfd, NULL, NULL);
	}
	lprintf("==accept ret:%d, addrlen:%d\n", ret, req->addrlen);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	resp.addrlen = req->addrlen;
	payload_size -= sizeof(resp.addrlen);

	if (req->addrlen > 0) {
		memcpy(resp.buf, req->addr_buf, req->addrlen);
		payload_size += req->addrlen;
	}

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, ACCEPT_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==accept send rsp,%d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_bind(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(bind)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==bind(%d, %d)\n", req->sockfd, req->addrlen);
	ret = bind(req->sockfd, (struct sockaddr *)req->addr_buf, req->addrlen);
	lprintf("==bind ret:%d, addrlen:%d\n", ret, req->addrlen);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, BIND_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==bind send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_connect(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(connect)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==connect(%d, %d)\n", req->sockfd, req->addrlen);
	ret = connect(req->sockfd, (struct sockaddr *)req->addr_buf, req->addrlen);
	lprintf("==connect ret:%d, addrlen:%d\n", ret, req->addrlen);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, CONNECT_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==connect send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}
static int rpmsg_handle_listen(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(listen)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==listen(%d, %d)\n", req->sockfd, req->backlog);
	ret = listen(req->sockfd, req->backlog);
	lprintf("==listen ret:%d\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, LISTEN_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==listen send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_recv(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(recv)
	ssize_t sret;

	if (!req || !inst)
		return -EINVAL;

	lprintf("==recv(%d, %lu, %d)\n", req->fd, req->len, req->flags);
	sret = recv(req->fd, resp.buf, req->len, req->flags);
	lprintf("==recv ret:%ld\n", sret);
	lerror(ret, errno);
	payload_size -= sizeof(resp.buf);
	resp.ret = sret;
	set_rsp_base(&resp.super, req->trace_id);
	if (sret > 0) {
		payload_size += sret;
	}

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, RECV_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==recv send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_recvfrom(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(recvfrom)
	ssize_t sret;
	int len;

	if (!req || !inst)
		return -EINVAL;

	lprintf("==recvfrom(%d, %lu, %d)\n", req->fd, req->len, req->flags);
	len = MIN(sizeof(resp.buf), req->len);
	sret = recvfrom(req->fd, resp.buf, len, req->flags, (struct sockaddr *)req->buf,
					(socklen_t *)&req->addrlen);
	lprintf("==recvfrom ret:%ld\n", sret);
	lerror(ret, errno);
	resp.ret = sret;
	set_rsp_base(&resp.super, req->trace_id);
	resp.addrlen = req->addrlen;
	payload_size -= sizeof(resp.buf);
	if (req->addrlen > sizeof(resp.addr)) {
		resp.ret = -RPC_EOVERLONG;
		lprintf("==recvfrom addr overflow:%d, %d\n", req->addrlen, sizeof(resp.addr));
		goto response;
	}
	if (req->addrlen > 0) {
		memcpy(resp.addr, req->buf, req->addrlen);
	}

	if (sret > 0) {
		payload_size += sret;
	}
response:
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, RECVFROM_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==recv send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_send(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(send)
	ssize_t sret;

	if (!req || !inst)
		return -EINVAL;

	lprintf("==send(%d, %lu, %d)\n", req->fd, req->len, req->flags);
	sret = send(req->fd, req->buf, req->len, req->flags);
	lprintf("==send ret:%ld\n", sret);
	lerror(ret, errno);
	resp.ret = sret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, SEND_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==send send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_sendto(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(sendto)
	ssize_t sret;

	if (!req || !inst)
		return -EINVAL;

	lprintf("==sendto(%d, %lu, %d, %d)\n", req->fd, req->len, req->flags, req->addrlen);
	sret = sendto(req->fd, &req->buf[req->addrlen], req->len, req->flags,
				  (struct sockaddr *)req->buf, req->addrlen);
	lprintf("==sendto ret:%ld\n", sret);
	lerror(ret, errno);
	resp.ret = sret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, SENDTO_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==sendto send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_setsockopt(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(setsockopt)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==setsockopt(%d, %d, %d, %d)\n", req->fd, req->level, req->optname,
			req->optlen);
	ret = setsockopt(req->fd, req->level, req->optname, req->optval, req->optlen);
	lprintf("==setsockopt ret:%d\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, SETSOCKOPT_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==setsockopt send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_shutdown(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(shutdown)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==shutdown(%d, %d)\n", req->fd, req->how);
	ret = shutdown(req->fd, req->how);
	lprintf("==shutdown ret:%d\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, SHUTDOWN_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==shutdown send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_socket(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(socket)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==socket(%d, %d, %d)\n", req->domain, req->type, req->protocol);
	ret = socket(req->domain, req->type, req->protocol);
	lprintf("==socket ret:%d\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, SOCKET_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==socket send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}


static int rpmsg_handle_poll(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(poll)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==poll(%d,%d,%d,%ld,%d)\n", req->fds[0].fd, req->fds[0].events,
			 req->fds[0].revents, req->nfds, req->timeout);
	ret = poll(req->fds, req->nfds, req->timeout);
	lprintf("==poll ret:%d\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	memcpy(resp.fds, req->fds, sizeof(struct pollfd) * req->nfds);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, POLL_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==poll send rsp:(%d,%d,%d)\n", resp.fds[0].fd, resp.fds[0].revents, ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;

}

static int rpmsg_handle_select(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(select)

	if (!req || !inst)
		return -EINVAL;

	lprintf("\n==select(%d,%d,%d,%d,%d,%ld,%ld)\n", req->nfds, (int)req->is_readfds_not_null,
		(int)req->is_writefds_not_null, (int)req->is_exceptfds_not_null, (int)req->is_timeout_not_null,
		req->timeout.tv_sec, req->timeout.tv_usec);

	fd_set *readfds = NULL;
	fd_set *writefds = NULL;
	fd_set *exceptfds = NULL;
	struct timeval *timeout = NULL;

	if (req->is_readfds_not_null) {
		readfds = &(req->readfds);
	}
	if (req->is_writefds_not_null) {
		writefds = &(req->writefds);
	}
	if (req->is_exceptfds_not_null) {
		exceptfds = &(req->exceptfds);
	}
	if (req->is_timeout_not_null) {
		timeout = &(req->timeout);
	}
	ret = select(req->nfds, readfds, writefds, exceptfds, timeout);
	lprintf("==select ret:%d\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	memcpy(&(resp.readfds), &(req->readfds), sizeof(fd_set));
	memcpy(&(resp.writefds), &(req->writefds), sizeof(fd_set));
	memcpy(&(resp.exceptfds), &(req->exceptfds), sizeof(fd_set));
	memcpy(&(resp.timeout), &(req->timeout), sizeof(struct timeval));
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, SELECT_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==select send rsp:%d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_gethostname(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(gethostname)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==gethostname(%lu)\n", (unsigned long)req->len);
	if (req->len > MAX_STRING_LEN) {
		req->len = MAX_STRING_LEN;
	}

	ret = gethostname(resp.name, req->len);
	lprintf("==gethostname ret:%d\n", ret);
	lerror(ret, errno);
	payload_size -= sizeof(resp.name);
	resp.len = 0;
	if (!ret) {
		resp.len = strlen(resp.name) + 1;
		payload_size += resp.len;
	}
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETHOSTNAME_ID, RPMSG_RPC_OK,
		&resp, payload_size);

	lprintf("==gethostname send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_getsockopt(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(getsockopt)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==getsockopt(%d, %d, %d)\n", req->sockfd, req->level,
		req->optname);

	ret = getsockopt(req->sockfd, req->level, req->optname, &resp.optval, &req->optlen);
	payload_size -= sizeof(resp.optval);
	if (req->optlen > sizeof(resp.optval)) {
		ret = -RPC_EOVERLONG;
	} else {
		payload_size += req->optlen;
	}
	lprintf("==getsockopt(%d)\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	resp.optlen = req->optlen;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETSOCKOPT_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==getsockopt send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

int rpc_write(void *data, size_t len)
{
	int ret, i, j;
	char msg[len * 2 + 1];
	char *msg_data = (char *)data;

	/* when using fwrite, translate '\n' to "\r\n" */
	for (i = 0, j = 0; i < len; ++i, ++msg_data) {
		if (*msg_data == '\n') {
			msg[i + j] = '\r';
			++j;
		}
		msg[i + j] = *msg_data;
	}

	len = i + j;
	msg[len] = '\0';
	ret = fwrite(msg, sizeof(char), len, fp);
	fflush(fp);
	if (ret < 0) {
		printf("write pty master error:%d\n", ret);
		return ret;
	}

	return 0;
}

static int rpmsg_handle_printf(void *data, struct rpc_instance *inst, void *priv)
{
	rpc_printf_req_t *req = (rpc_printf_req_t *)data;
	int ret = 0;

	if (!req || !inst)
		return -EINVAL;

	ret = rpc_write(req->buf, MIN(sizeof(req->buf), req->len));
	lprintf("==printf(%d), ret(%d)\n", req->len, ret);

	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_getdents64(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(getdents64)
	off_t off = 0;

	if (!req || !inst)
		return -EINVAL;
	int buflen = MIN(req->count, sizeof(resp.buf));

	payload_size -= sizeof(resp.buf);
	lprintf("==getdents64 fd:%d, pos:%ld\n", req->fd, req->pos);
	if (req->pos != -1) {
		off = lseek(req->fd, req->pos, SEEK_SET);
	}
	if (off < 0) {
		resp.ret = -1;
		lprintf("==getdents64 seek fail fd:%d, ret:%ld\n", req->fd, off);
	} else {
		ret = syscall(SYS_getdents64, req->fd, resp.buf, buflen);
		lprintf("==getdents64 fd:%d, ret:%d\n", req->fd, ret);
		if (ret > 0) {
			payload_size += ret;
		}
		resp.ret = ret;
	}
	lerror(resp.ret, errno);
	set_rsp_base(&resp.super, req->trace_id);
	resp.pos = lseek(req->fd, 0, SEEK_CUR);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETDENTS64_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==getdents64 send rsp(%d), new pos: %ld\n", ret, resp.pos);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_fopen(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *fd;
	rpc_fopen_req_t *req = data;
	rpc_fcommon_resp_t resp = {0};

	lprintf("==fopen(%s, %s)\n", req->filename, req->mode);
	fd = fopen(req->filename, req->mode);
	lprintf("==fopen %s.\n", (fd == 0 ? "fail" : "success"));

	resp.fhandle = (fileHandle)fd;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FOPEN_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==fopen send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_fclose(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_fclose_req_t *req = data;
	rpc_common_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==fclose(0x%x)\n", req->fhandle);
	ret = fclose(f);
	lprintf("==fclose ret(%d)\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FCLOSE_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==fclose send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_fread(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	size_t sz, payload_size;
	rpc_fread_req_t *req = data;
	rpc_fread_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==fread(0x%x, %u, %u)\n", req->fhandle, req->size, req->count);
	sz = fread(resp.buf, req->size, req->count, f);
	lprintf("==fread ret(%u)\n", sz);
	payload_size = sizeof(resp) - sizeof(resp.buf) + sz;
	resp.ret = sz;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FREAD_ID, RPMSG_RPC_OK, &resp, payload_size);
	lprintf("==fread send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_fwrite(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	size_t sz;
	rpc_fwrite_req_t *req = data;
	rpc_fwrite_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==fwrite(0x%x, %u, %u)\n", req->fhandle, req->size, req->count);
	sz = fwrite(req->buf, req->size, req->count, f);
	lprintf("==fwrite ret(%u)\n", sz);

	resp.ret = sz;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FWRITE_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==fwrite send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_freopen(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f, *fd;
	rpc_freopen_req_t *req = data;
	rpc_fcommon_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==freopen(%s, %s)\n", req->filename, req->mode);
	fd = freopen(req->filename, req->mode, f);
	lprintf("==freopen %s.\n", (fd == 0 ? "fail" : "success"));

	resp.fhandle = (fileHandle)fd;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FREOPEN_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==freopen send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_fputs(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_fputs_req_t *req = data;
	rpc_common_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==fputs(0x%x)\n", req->fhandle);
	ret = fputs(req->str, f);
	lprintf("==fputs ret(%d)\n", ret);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FPUTS_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==fputs send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_fgets(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	char *retStr;
	rpc_fgets_req_t *req = data;
	rpc_fgets_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==fgets(0x%x)\n", req->fhandle);
	retStr = fgets(resp.str, req->size, f);
	lprintf("==fgets ret(%s)\n", (retStr == NULL ? "end" : "continue"));

	resp.isEof = (retStr == NULL ? 1 : 0);
	lprintf("==fgets isEof %d\n", resp.isEof);
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FGETS_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==fgets send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_feof(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_feof_req_t *req = data;
	rpc_common_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==feof(0x%x)\n", req->fhandle);
	ret = feof(f);
	lprintf("==feof ret(%d)\n", ret);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FEOF_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==feof send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_fprintf(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	rpc_fprintf_req_t *req = (rpc_fprintf_req_t *)data;
	int ret = 0;

	if (is_pty_fd(req->fhandle)) {
		ret = rpc_write(req->buf, MIN(sizeof(req->buf), req->len));
		lprintf("==fprintf printf(%d), ret(%d)\n", req->len, ret);
	} else {
		ret = fwrite(req->buf, sizeof(char), req->len, (FILE *)req->fhandle);
		lprintf("==fprintf(%d), ret(%d)\n", req->len, ret);
	}

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_getc(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_getc_req_t *req = data;
	rpc_common_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==getc(0x%x)\n", req->fhandle);
	ret = getc(f);
	lprintf("==getc ret(%d)\n", ret);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETC_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==getc send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_ferror(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_ferror_req_t *req = data;
	rpc_common_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==ferror(0x%x)\n", req->fhandle);
	ret = ferror(f);
	lprintf("==ferror ret(%d)\n", ret);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FERROR_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==ferror send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_getc_unlocked(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_getc_unlocked_req_t *req = data;
	rpc_common_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==getc_unlocked(0x%x)\n", req->fhandle);
	ret = getc_unlocked(f);
	lprintf("==getc_unlocked ret(%d)\n", ret);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETC_UNLOCK_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==getc_unlocked send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_pclose(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_pclose_req_t *req = data;
	rpc_common_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==pclose(0x%x)\n", req->fhandle);
	ret = pclose(f);
	lprintf("==pclose ret(%d)\n", ret);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, PCLOSE_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==pclose send rsp:0x%x, %d\n", req->fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_tmpfile(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_tmpfile_req_t *req = data;
	rpc_fcommon_resp_t resp = {0};

	lprintf("==tmpfile\n");
	f = tmpfile();
	resp.fhandle = (fileHandle)f;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, TMPFILE_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==tmpfile send rsp:0x%x, %d\n", resp.fhandle, ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_clearerr(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	FILE *f;
	rpc_clearerr_req_t *req = data;

	f = handle2file(req->fhandle, priv);
	lprintf("==clearerr(0x%x)\n", req->fhandle);
	clearerr(f);

	CLEANUP(data);
	return 0;
}

static int rpmsg_handle_popen(void *data, struct rpc_instance *inst, void *priv)
{
	int ret;
	FILE *fd;

	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	rpc_popen_req_t *req = data;
	rpc_fcommon_resp_t resp = {0};

	lprintf("==popen(%s, %s)\n", req->cmd, req->mode);
	fd = popen(req->cmd, req->mode);
	lprintf("==popen %s, fd: 0x%x.\n", (fd == 0 ? "fail" : "success"), fd);

	resp.fhandle = (fileHandle)fd;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, POPEN_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==popen send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_ungetc(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_ungetc_req_t *req = data;
	rpc_common_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==ungetc(%d)\n", req->c);
	resp.ret = ungetc(req->c, f);
	lprintf("==ungetc %d.\n", resp.ret);

	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, UNGETC_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==ungetc send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;

}

static int rpmsg_handle_fseeko(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_fseeko_req_t *req = data;
	rpc_common_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==fseeko(%ld, %d)\n", req->offset, req->whence);
	resp.ret = fseeko(f, req->offset, req->whence);
	lprintf("==fseeko %d.\n", resp.ret);

	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FSEEKO_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==fseeko send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_ftello(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_ftello_req_t *req = data;
	rpc_ftello_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==ftello\n");
	resp.ret = ftello(f);
	lprintf("==ftello %d.\n", resp.ret);

	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FTELLO_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==ftello send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_fseek(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	rpc_fseeko_req_t *req = data;
	rpc_common_resp_t resp = {0};
	FILE *f = handle2file(req->fhandle, priv);

	lprintf("==fseek(%ld, %d)\n", req->offset, req->whence);
	resp.ret = fseek(f, req->offset, req->whence);
	lprintf("==fseek %d.\n", resp.ret);

	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FSEEK_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==fseek send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_ftell(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	FILE *f;
	rpc_ftello_req_t *req = data;
	rpc_ftello_resp_t resp = {0};

	f = handle2file(req->fhandle, priv);
	lprintf("==ftello\n");
	resp.ret = ftello(f);
	lprintf("==ftello %d.\n", resp.ret);

	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FTELL_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==ftello send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_rename(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	rpc_rename_req_t *req = data;
	rpc_common_resp_t resp = {0};

	lprintf("==rename(%s, %s)\n", req->old, req->new);
	resp.ret = rename(req->old, req->new);
	lprintf("==rename %d.\n", resp.ret);

	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, RENAME_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==rename send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_remove(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	rpc_remove_req_t *req = data;
	rpc_common_resp_t resp = {0};

	lprintf("==remove(%s, %s)\n", req->path);
	resp.ret = remove(req->path);
	lprintf("==remove %d.\n", resp.ret);

	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, REMOVE_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==remove send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_mkstemp(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	rpc_mkstemp_req_t *req = data;
	rpc_common_resp_t resp = {0};

	lprintf("==mkstemp(%s)\n", req->tmp);
	resp.ret = mkstemp(req->tmp);
	lprintf("==mkstemp %d.\n", resp.ret);

	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, MKSTMP_ID, RPMSG_RPC_OK, &resp, sizeof(resp));
	lprintf("==mkstemp send rsp:%d\n", ret);

	CLEANUP(data);
	return ret > 0 ? 0 : ret;
}

static int rpmsg_handle_fflush(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(fflush)
	FILE *f;

	if (!req || !inst)
		return -EINVAL;

	f = handle2file(req->fhandle, priv);
	lprintf("==fflush(fileno: %d)\n", file2fd(f));

	ret = fflush(f);

	lprintf("==fflush(%d)\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FFLUSH_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==fflush send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_getwc(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(getwc)
	FILE *f;
	wint_t wret;

	if (!req || !inst)
		return -EINVAL;

	f = handle2file(req->fhandle, priv);
	lprintf("==getwc(fileno: %d)\n", file2fd(f));
	wret = getwc(f);
	lprintf("==getwc(%d)\n", wret);

	resp.ret = wret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETWC_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==getwc send rsp(%d)\n", wret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_putwc(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(putwc)
	FILE *f;
	wint_t wret;

	if (!req || !inst)
		return -EINVAL;

	f = handle2file(req->fhandle, priv);
	lprintf("==putwc(%d, fileno: %d)\n", req->wc, file2fd(f));
	wret = putwc(req->wc, f);
	lprintf("==putwc(%d)\n", wret);

	resp.ret = wret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, PUTWC_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==putwc send rsp(%d)\n", wret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_putc(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(putc)
	FILE *f;

	if (!req || !inst)
		return -EINVAL;

	f = handle2file(req->fhandle, priv);
	lprintf("==putc(%d, fileno: %d)\n", req->c, file2fd(f));
	ret = putc(req->c, f);
	lprintf("==putc(%d)\n", ret);

	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, PUTC_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==putc send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_ungetwc(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(ungetwc)
	FILE *f;
	wint_t wret;

	if (!req || !inst)
		return -EINVAL;

	f = handle2file(req->fhandle, priv);
	lprintf("==ungetwc(%d, fileno: %d)\n", req->wc, file2fd(f));
	wret = ungetwc(req->wc, f);
	lprintf("==ungetwc(%d)\n", wret);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, UNGETWC_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==ungetwc send rsp(%d)\n", wret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static inline void set_stat_buff(rpc_stat_resp_t *resp, struct stat *statbuff)
{
	resp->st_dev = statbuff->st_dev;
	resp->st_ino = statbuff->st_ino;
	resp->st_nlink = statbuff->st_nlink;
	resp->st_mode = statbuff->st_mode;
	resp->st_uid = statbuff->st_uid;
	resp->st_gid = statbuff->st_gid;
	resp->st_rdev = statbuff->st_rdev;
	resp->st_size = statbuff->st_size;
	resp->st_blksize = statbuff->st_blksize;
	resp->st_blocks = statbuff->st_blocks;

	resp->st_atime_sec = statbuff->st_atim.tv_sec;
	resp->st_atime_nsec = statbuff->st_atim.tv_nsec;
	resp->st_mtime_sec = statbuff->st_mtim.tv_sec;
	resp->st_mtime_nsec = statbuff->st_mtim.tv_nsec;
	resp->st_ctime_sec = statbuff->st_ctim.tv_sec;
	resp->st_ctime_nsec = statbuff->st_ctim.tv_nsec;
}

static int rpmsg_handle_stat(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(stat)
	struct stat statbuff = {0};

	if (!req || !inst)
		return -EINVAL;

	lprintf("==stat(%s)\n", req->path);
	ret = stat(req->path, &statbuff);
	lprintf("==stat(%d)\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_stat_buff(&resp, &statbuff);
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, STAT_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==stat send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_lstat(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(stat)
	struct stat statbuff = {0};

	if (!req || !inst)
		return -EINVAL;

	lprintf("==lstat(%s)\n", req->path);
	ret = lstat(req->path, &statbuff);
	lprintf("==lstat(%d)\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_stat_buff(&resp, &statbuff);
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, LSTAT_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==lstat send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_getcwd(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(getcwd)
	char *buf;

	if (!req || !inst || req->size > sizeof(resp.buf))
		return -EINVAL;

	lprintf("==getcwd(%ld)\n", req->size);
	buf = getcwd(resp.buf, req->size);
	if (buf != NULL) {
		lprintf("==getcwd(%s)\n", resp.buf);
		resp.isNull = 0;
	} else {
		lprintf("==getcwd(NULL) %s\n", strerror(errno));
		resp.isNull = 1;
	}
	set_rsp_base(&resp.super, req->trace_id);
	payload_size = payload_size - sizeof(resp.buf) + req->size;

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GETCWD_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==getcwd send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_fstat(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(fstat)
	struct stat statbuff = {0};

	if (!req || !inst)
		return -EINVAL;

	lprintf("==fstat(%d)\n", req->fd);
	ret = fstat(req->fd, &statbuff);
	lprintf("==fstat(%d)\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_stat_buff(&resp, &statbuff);
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FSTAT_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==fstat send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_fdopen(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(fdopen)
	FILE *f;

	if (!req || !inst)
		return -EINVAL;

	lprintf("==fdopen(%d)\n", req->fd);
	f = fdopen(req->fd, req->mode);
	lprintf("==fstat(%p)\n", f);
	if (f == NULL) {
		lprintf("errstr:%s\n", strerror(errno));
	}

	resp.fhandle = (fileHandle)f;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FDOPEN_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==fdopen send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_fileno(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(fileno)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==fileno(0x%lx)\n", req->fhandle);
	ret = fileno((FILE *)req->fhandle);
	lprintf("==fileno(%d)\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FILENO_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==fileno send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_setvbuf(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(setvbuf)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==setvbuf(0x%lx) [_IONBF]\n", req->fhandle);

	// only support unbuffered mode
	ret = setvbuf((FILE *)req->fhandle, NULL, _IONBF, 0);
	lprintf("==setvbuf(%d)\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, SETVBUF_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==setvbuf send rsp(%d)\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_readlink(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(readlink)
	ssize_t sret;

	if (!req || !inst)
		return -EINVAL;

	lprintf("==readlink(%s, %lu)\n", req->pathname, req->bufsiz);
	sret = readlink(req->pathname, resp.buf, MIN(sizeof(resp.buf), req->bufsiz));
	lprintf("==readlink ret:%ld\n", sret);
	lerror((int)sret, errno);

	resp.ret = sret;
	set_rsp_base(&resp.super, req->trace_id);
	payload_size -= sizeof(resp.buf);
	if (sret > 0) {
		payload_size += sret;
	}
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, READLINK_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==readlink send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_system(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(system)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==system(%s)\n", req->buf);
	ret = system(req->buf);
	lprintf("==system ret:%d\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, SYSTEM_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==system send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_access(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(access)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==access(%s,%d)\n", req->pathname, req->mode);
	ret = access(req->pathname, req->mode);
	lprintf("==access ret:%d\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, ACCESS_ID, RPMSG_RPC_OK,
				&resp, payload_size);
	lprintf("==access send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_dup2(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(dup2)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==dup2(%d,%d)\n", req->oldfd, req->newfd);
	ret = dup2(req->oldfd, req->newfd);
	lprintf("==dup2 ret:%d\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, DUP2_ID, RPMSG_RPC_OK,
				&resp, payload_size);
	lprintf("==dup2 send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_mkfifo(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(mkfifo)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==mkfifo(%s,%u)\n", req->pathname, req->mode);
	ret = mkfifo(req->pathname, req->mode);
	lprintf("==mkfifo ret:%d\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, MKFIFO_ID, RPMSG_RPC_OK,
				&resp, payload_size);
	lprintf("==mkfifo send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_chmod(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(chmod)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==chmod(%s,%d)\n", req->pathname, req->mode);
	ret = chmod(req->pathname, req->mode);
	lprintf("==chmod ret:%d\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, CHMOD_ID, RPMSG_RPC_OK,
				&resp, payload_size);
	lprintf("==chmod send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_chdir(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(chdir)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==chdir(%s)\n", req->buf);
	ret = chdir(req->buf);
	lprintf("==chdir ret:%d\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, CHDIR_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==chdir send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_mkdir(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(mkdir)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==mkdir(%s,%d)\n", req->pathname, req->mode);
	ret = mkdir(req->pathname, req->mode);
	lprintf("==mkdir ret:%d\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	ret = rpc_server_send((struct rpmsg_endpoint *)priv, MKDIR_ID, RPMSG_RPC_OK,
				&resp, payload_size);
	lprintf("==mkdir send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_rmdir(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(rmdir)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==rmdir(%s)\n", req->buf);
	ret = rmdir(req->buf);
	lprintf("==rmdir ret:%d\n", ret);
	lerror(ret, errno);

	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, RMDIR_ID, RPMSG_RPC_OK,
					&resp, payload_size);

	lprintf("==rmdir send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_pipe(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(pipe)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==pipe\n");
	ret = pipe(resp.fd);
	lprintf("==pipe ret:%d fd[2]:%d %d\n", ret, resp.fd[0], resp.fd[1]);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, PIPE_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==pipe send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_fscanfx(void *data, struct rpc_instance *inst, void *priv)
{
	if (data == NULL || inst == NULL || priv == NULL) {
		return -EINVAL;
	}

	int ret;
	rpc_fscanfx_req_t *req = data;
	rpc_fscanfx_resp_t resp = {0};
	size_t payload_size = sizeof(resp);
	FILE *f = handle2file(req->fhandle, priv);

	lprintf("==fscanfx(0x%x, %s)\n", req->fhandle, req->fmt);
	ret = fscanf(f, req->fmt, &(resp.data));
	lprintf("==fscanfx ret:%d data:0x%llx\n", ret, resp.data);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, FSCANFX_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==fscanfx send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_ifnameindex(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(if_nameindex)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==ifnameindex\n");
	struct if_nameindex *ifNameIndex = if_nameindex();
	struct if_nameindex *p;
	int cnt = 0;

	if (ifNameIndex != NULL) {
		for (p = ifNameIndex; !(p->if_index == 0 && p->if_name == NULL); p++) {
			resp.if_index[cnt].if_index = p->if_index;
			memcpy(resp.if_index[cnt].if_name, p->if_name, strlen(p->if_name)+1);
			cnt++;
		}
	}

	resp.cnt = cnt;

	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, IFNAMEINDEX_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==ifnameindex send rsp, %d\n", ret);
	if_freenameindex(ifNameIndex);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_putchar(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(putchar)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==putchar\n");
	ret = putchar(req->ch);
	lprintf("==putchar ret:%d\n", ret);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, PUTCHAR_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==putchar send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_gaistrerror(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(gai_strerror)
	const char *retStr = NULL;

	if (!req || !inst)
		return -EINVAL;

	lprintf("==gaistrerror\n");
	retStr = gai_strerror(req->error);
	if (retStr != NULL) {
		memcpy(resp.buf, retStr, strlen(retStr)+1);
		lprintf("==gaistrerror(%s)\n", resp.buf);
		resp.isNull = 0;
	} else {
		lprintf("==gaistrerror(NULL) %s\n", strerror(errno));
		resp.isNull = 1;
	}
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, GAISTRERROR_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==gaistrerror send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_accept4(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(accept4)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==accept4(%d, %d)\n", req->sockfd, req->addrlen);
	if (req->addrlen > 0) {
		ret = accept4(req->sockfd, (struct sockaddr *)req->addr_buf, &req->addrlen, req->flags);
	} else {
		ret = accept4(req->sockfd, NULL, NULL, req->flags);
	}
	lprintf("==accept4 ret:%d, addrlen:%d\n", ret, req->addrlen);
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);
	resp.addrlen = req->addrlen;
	payload_size -= sizeof(resp.addrlen);

	if (req->addrlen > 0) {
		memcpy(resp.buf, req->addr_buf, req->addrlen);
		payload_size += req->addrlen;
	}

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, ACCEPT4_ID, RPMSG_RPC_OK,
					&resp, payload_size);
	lprintf("==accept4 send rsp,%d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}

static int rpmsg_handle_writev(void *data, struct rpc_instance *inst, void *priv)
{
	DEFINE_VARS(writev)

	if (!req || !inst)
		return -EINVAL;

	lprintf("==writev\n");
	struct iovec iov[req->iovcnt];

	for (int i = 0; i < req->iovcnt; i++) {
		iov[i].iov_base = (void *)malloc(sizeof(char) * MAX_IOV_SIZE);
		memcpy(iov[i].iov_base, req->buf[i].iov, req->buf[i].len);
		iov[i].iov_len = req->buf[i].len;
	}

	ret = writev(req->fd, (const struct iovec *)&iov[0], req->iovcnt);
	lprintf("==writev ret:%d\n", ret);
	for (int i = 0; i < req->iovcnt; i++) {
		free(iov[i].iov_base);
	}
	lerror(ret, errno);
	resp.ret = ret;
	set_rsp_base(&resp.super, req->trace_id);

	ret = rpc_server_send((struct rpmsg_endpoint *)priv, WRITEV_ID, RPMSG_RPC_OK,
		&resp, payload_size);
	lprintf("==writev send rsp, %d\n", ret);
	CLEANUP(data);
	return ret > 0 ?  0 : ret;
}
