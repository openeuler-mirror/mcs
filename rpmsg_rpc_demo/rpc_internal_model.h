#ifndef _RPC_INTERNAL_MODEL_H
#define _RPC_INTERNAL_MODEL_H

#include <poll.h>
#include "ethercat_cmd/ioctl_rpc.h"

#define OPEN_ID           1UL
#define CLOSE_ID          2UL
#define WRITE_ID          3UL
#define READ_ID           4UL
#define LSEEK_ID          5UL
#define FCNTL_ID          6UL
#define IOCTL_ID          7UL
#define UNLINK_ID         8UL
#define GETDENTS64_ID     9UL
#define FOPEN_ID          10UL
#define FCLOSE_ID         11UL
#define FREAD_ID          12UL
#define FWRITE_ID         13UL
#define FREOPEN_ID        14UL
#define FPUTS_ID          15UL
#define FGETS_ID          16UL
#define FEOF_ID           17UL
#define FPRINTF_ID        18UL
#define GETC_ID           19UL
#define FERROR_ID         20UL
#define GETC_UNLOCK_ID    21UL
#define PCLOSE_ID         22UL
#define TMPFILE_ID        23UL
#define CLEARERR_ID       24UL
#define POPEN_ID          25UL
#define UNGETC_ID         26UL
#define FSEEKO_ID         27UL
#define FTELLO_ID         28UL
#define RENAME_ID         29UL
#define REMOVE_ID         30UL
#define MKSTMP_ID         31UL

#define NCPYWRITE_ID      43UL
#define NCPYREAD_ID       44UL

#define FREEADDRINFO_ID    100UL
#define GETADDRINFO_ID     101UL
#define GETHOSTBYADDR_ID   102UL
#define GETHOSTBYNAME_ID   103UL
#define POLL_ID            104UL
#define GETPEERNAME_ID     105UL
#define GETHOSTNAME_ID     106UL
#define GETSOCKNAME_ID     107UL
#define GETSOCKOPT_ID      108UL
#define SELECT_ID          109UL
#define ACCEPT_ID          110UL
#define BIND_ID            111UL
#define CONNECT_ID         112UL
#define LISTEN_ID          113UL
#define RECV_ID            114UL
#define RECVFROM_ID        115UL
#define SEND_ID            116UL
#define SENDTO_ID          117UL
#define SETSOCKOPT_ID      118UL
#define SHUTDOWN_ID        119UL
#define SOCKET_ID          120UL
#define PRINTF_ID          300UL

#define MIN_ID            OPEN_ID
#define MAX_ID            PRINTF_ID

#define MAX_SBUF_LEN      432  /* max socket buf len*/
#define MAX_CBUF_LEN      416
#define MAX_SADDR_SIZE    8
#define MAX_STRING_LEN    MAX_SBUF_LEN
#define MAX_FILE_NAME_LEN 128
#define MAX_POLL_FDS      32

#define MAX_FILE_MODE_LEN 6

#ifndef MAX_FUNC_ID_LEN
#define MAX_FUNC_ID_LEN sizeof(unsigned long)
#endif

typedef unsigned int uint32_t;

typedef unsigned long fileHandle;

typedef struct iaddrinfo {
  int ai_flags;
  int ai_family;
  int ai_socktype;
  int ai_protocol;
  socklen_t ai_addrlen;
  int namelen;
} iaddrinfo_t;

typedef struct ihostent {
  int h_name_idx;
  int h_aliases_idx;
  short aliaslen;
  short addrlen;
  int h_addrtype;
  int h_length;
  int h_addr_list_idx;
} ihostent_t;

typedef struct rpc_resp_base {
    uint32_t trace_id;
    int errnum;
} rpc_resp_base_t;

typedef struct rpc_outp_base {
    int *eptr;
} rpc_outp_base_t;

typedef struct rpc_common_resp {
    rpc_resp_base_t super;
    int ret;
} rpc_common_resp_t;

typedef struct rpc_common_outp {
    rpc_outp_base_t super;
    int ret;
} rpc_common_outp_t;

typedef struct rpc_fcommon_req {
    unsigned long func_id;
    uint32_t trace_id;
    fileHandle fhandle;
} rpc_fcommon_req_t;

typedef struct rpc_fcommon_resp {
    rpc_resp_base_t super;
    fileHandle fhandle;
} rpc_fcommon_resp_t;

typedef struct rpc_fcommon_outp {
    rpc_outp_base_t super;
    fileHandle fhandle;
} rpc_fcommon_outp_t;

/* common ssize_t ret*/
typedef struct rpc_csret_resp {
    rpc_resp_base_t super;
    ssize_t ret;
} rpc_csret_resp_t;

typedef struct rpc_csret_outp {
    rpc_outp_base_t super;
    ssize_t ret;
} rpc_csret_outp_t;

/* open */
typedef struct rpc_open_req {
    unsigned long func_id;
    uint32_t trace_id;
    int flags;
    uint32_t mode;
    char buf[MAX_FILE_NAME_LEN];
} rpc_open_req_t;

typedef rpc_common_resp_t rpc_open_resp_t;

typedef rpc_common_outp_t rpc_open_outp_t;


/* read */
typedef struct rpc_read_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    uint32_t count;
} rpc_read_req_t;

typedef struct rpc_read_resp {
    rpc_resp_base_t super;
    ssize_t ret;
    char buf[MAX_STRING_LEN];
} rpc_read_resp_t;

typedef struct rpc_read_outp {
    rpc_outp_base_t super;
    ssize_t ret;
    void *buf;
} rpc_read_outp_t;

/* no copy read */
typedef struct rpc_ncpyread_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    unsigned long baddr_offset;
    uint32_t count;
} rpc_ncpyread_req_t;

typedef rpc_csret_resp_t rpc_ncpyread_resp_t;

typedef rpc_csret_outp_t rpc_ncpyread_outp_t;

/* write */
typedef struct rpc_write_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    uint32_t count;
    char buf[MAX_STRING_LEN];
} rpc_write_req_t;

typedef rpc_csret_resp_t rpc_write_resp_t;

typedef rpc_csret_outp_t rpc_write_outp_t;

/* no copy write */
typedef struct rpc_ncpywrite_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    uint32_t count;
    unsigned long baddr_offset;
} rpc_ncpywrite_req_t;

typedef rpc_write_resp_t rpc_ncpywrite_resp_t;

typedef rpc_write_outp_t rpc_ncpywrite_outp_t;

/* close */
typedef struct rpc_close_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
} rpc_close_req_t;

typedef rpc_common_resp_t rpc_close_resp_t;

typedef rpc_common_outp_t rpc_close_outp_t;


/* ioctl */
typedef struct rpc_ioctl_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    unsigned long request;
    size_t len;
    char buf[MAX_STRING_LEN];
} rpc_ioctl_req_t;

typedef struct rpc_ioctl_resp {
    rpc_resp_base_t super;
    int ret;
    size_t len;
    char buf[MAX_STRING_LEN];
} rpc_ioctl_resp_t;

typedef struct rpc_ioctl_outp {
    rpc_outp_base_t super;
    int ret;
    void *buf;
} rpc_ioctl_outp_t;


/* lseek */
typedef struct rpc_lseek_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    off_t offset;
    int whence;
} rpc_lseek_req_t;

typedef struct rpc_lseek_resp {
    rpc_resp_base_t super;
    off_t ret;
} rpc_lseek_resp_t;

typedef struct rpc_lseek_outp {
    rpc_outp_base_t super;
    off_t ret;
} rpc_lseek_outp_t;


/* fcntl */
typedef struct rpc_fcntl_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    int cmd;
    unsigned long arg;
} rpc_fcntl_req_t;

typedef rpc_common_resp_t rpc_fcntl_resp_t;

typedef rpc_common_outp_t rpc_fcntl_outp_t;


/* unlink */
typedef struct rpc_unlink_req {
    unsigned long func_id;
    uint32_t trace_id;
    char buf[MAX_FILE_NAME_LEN];
} rpc_unlink_req_t;

typedef rpc_common_resp_t rpc_unlink_resp_t;

typedef rpc_common_outp_t rpc_unlink_outp_t;

/* gethostbyaddr */
typedef struct rpc_gethostbyaddr_req {
    unsigned long func_id;
    uint32_t trace_id;
    socklen_t len;
    int type;
    char buf[MAX_SBUF_LEN];
} rpc_gethostbyaddr_req_t;

typedef struct rpc_gethostbyaddr_resp {
    rpc_resp_base_t super;
    int h_errnum;
    int len;
    char buf[MAX_SBUF_LEN];
} rpc_gethostbyaddr_resp_t;

typedef struct rpc_gethostbyaddr_outp {
    rpc_outp_base_t super;
    int len;
    void *buf;
} rpc_gethostbyaddr_outp_t;

/* gethostbyname */
typedef struct rpc_gethostbyname_req {
    unsigned long func_id;
    uint32_t trace_id;
    char buf[MAX_SBUF_LEN];
} rpc_gethostbyname_req_t;

typedef rpc_gethostbyaddr_resp_t rpc_gethostbyname_resp_t;

typedef rpc_gethostbyaddr_outp_t rpc_gethostbyname_outp_t;

/* getaddrinfo */
typedef struct rpc_getaddrinfo_req {
    unsigned long func_id;
    uint32_t trace_id;
    int hints_cnt;
    int hints;   /* hints index*/
    int node;    /* node index*/
    int service; /* service index*/
    int buflen;
    char buf[MAX_SBUF_LEN];
} rpc_getaddrinfo_req_t;

typedef struct rpc_getaddrinfo_resp {
    rpc_resp_base_t super;
    int h_errnum;
    int ret;
    int cnt;
    int buflen;
    char buf[MAX_SBUF_LEN];
} rpc_getaddrinfo_resp_t;

typedef struct rpc_getaddrinfo_outp {
    rpc_outp_base_t super;
    int ret;
    int cnt;
    void *buf;
} rpc_getaddrinfo_outp_t;

/* getpeername */
typedef struct rpc_getpeername_req {
    unsigned long func_id;
    uint32_t trace_id;
    int sockfd;
    socklen_t addrlen;
    char addr_buf[MAX_SBUF_LEN];
} rpc_getpeername_req_t;

typedef struct rpc_getpeername_resp {
    rpc_resp_base_t super;
    int ret;
    socklen_t addrlen;
    char addr_buf[MAX_SBUF_LEN];
} rpc_getpeername_resp_t;

typedef struct rpc_getpeername_outp {
    rpc_outp_base_t super;
    ssize_t ret;
    socklen_t *addrlen;
    struct sockaddr *addr;
} rpc_getpeername_outp_t;

/* getsockname */
typedef rpc_getpeername_req_t rpc_getsockname_req_t;

typedef rpc_getpeername_resp_t rpc_getsockname_resp_t;

typedef rpc_getpeername_outp_t rpc_getsockname_outp_t;

/* accept */
typedef struct rpc_accept_req {
    unsigned long func_id;
    uint32_t trace_id;
    int sockfd;
    socklen_t addrlen;
    char addr_buf[MAX_SBUF_LEN];
} rpc_accept_req_t;

typedef struct rpc_accept_resp {
    rpc_resp_base_t super;
    int ret;
    socklen_t addrlen;
    char buf[MAX_SBUF_LEN];
} rpc_accept_resp_t;

typedef struct rpc_accept_outp {
    rpc_outp_base_t super;
    ssize_t ret;
    socklen_t *addrlen;
    struct sockaddr *addr;
} rpc_accept_outp_t;

/* bind */
typedef struct rpc_bind_req {
    unsigned long func_id;
    uint32_t trace_id;
    int sockfd;
    socklen_t addrlen;
    int errnum;
    char addr_buf[MAX_SBUF_LEN];
} rpc_bind_req_t;

typedef rpc_common_resp_t rpc_bind_resp_t;

typedef rpc_common_outp_t rpc_bind_outp_t;

/* connect */
typedef rpc_bind_req_t rpc_connect_req_t;

typedef rpc_common_resp_t rpc_connect_resp_t;

typedef rpc_common_outp_t rpc_connect_outp_t;

/* listen */
typedef struct rpc_listen_req {
    unsigned long func_id;
    uint32_t trace_id;
    int sockfd;
    int backlog;
} rpc_listen_req_t;

typedef rpc_common_resp_t rpc_listen_resp_t;

typedef rpc_common_outp_t rpc_listen_outp_t;

/* recv */
typedef struct rpc_recv_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    int flags;
    size_t len;
} rpc_recv_req_t;

typedef struct rpc_recv_resp {
    rpc_resp_base_t super;
    ssize_t ret;
    char buf[MAX_SBUF_LEN];
} rpc_recv_resp_t;

typedef struct rpc_recv_outp {
    rpc_outp_base_t super;
    ssize_t ret;
    void *buf;
} rpc_recv_outp_t;

/* recvfrom */
typedef struct rpc_recvfrom_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    int flags;
    socklen_t addrlen;
    size_t len;
    char buf[MAX_SBUF_LEN];
} rpc_recvfrom_req_t;

typedef struct rpc_recvfrom_resp {
    rpc_resp_base_t super;
    socklen_t addrlen;
    ssize_t ret;
    struct sockaddr addr[MAX_SADDR_SIZE];
    char buf[MAX_SBUF_LEN - MAX_SADDR_SIZE * sizeof(struct sockaddr)];
} rpc_recvfrom_resp_t;

typedef struct rpc_recvfrom_outp {
    rpc_outp_base_t super;
    ssize_t ret;
    void *buf;
    socklen_t *addrlen;
    struct sockaddr *src_addr;
} rpc_recvfrom_outp_t;

/* send */
typedef struct rpc_send_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    size_t len;
    int flags;
    char buf[MAX_SBUF_LEN];
} rpc_send_req_t;

typedef rpc_csret_resp_t rpc_send_resp_t;

typedef rpc_csret_outp_t rpc_send_outp_t;

/* sendto */
typedef struct rpc_sendto_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    size_t len;
    int flags;
    socklen_t addrlen;
    char buf[MAX_SBUF_LEN];
} rpc_sendto_req_t;

typedef rpc_csret_resp_t rpc_sendto_resp_t;

typedef rpc_csret_outp_t rpc_sendto_outp_t;

/* setsockopt */
typedef struct rpc_setsockopt_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    int level;
    int optname;
    socklen_t optlen;
    char optval[MAX_STRING_LEN];
} rpc_setsockopt_req_t;

typedef rpc_common_resp_t rpc_setsockopt_resp_t;

typedef rpc_common_outp_t rpc_setsockopt_outp_t;

/* shutdown */
typedef struct rpc_shutdown_req {
    unsigned long func_id;
    uint32_t trace_id;
    int fd;
    int how;
} rpc_shutdown_req_t;

typedef rpc_common_resp_t rpc_shutdown_resp_t;

typedef rpc_common_outp_t rpc_shutdown_outp_t;

/* socket */
typedef struct rpc_socket_req {
    unsigned long func_id;
    uint32_t trace_id;
    int domain;
    int type;
    int protocol;
} rpc_socket_req_t;

typedef rpc_common_resp_t rpc_socket_resp_t;

typedef rpc_common_outp_t rpc_socket_outp_t;

/* poll */
typedef struct rpc_poll_req {
    unsigned long func_id;  /* 8 */
    uint32_t trace_id;      /* 4 */
    int timeout;            /* 4 */
    struct pollfd fds[MAX_POLL_FDS];  /* 8 * MAX_POLL_FDS*/
    nfds_t nfds;            /* 8 */
} rpc_poll_req_t;

typedef struct rpc_poll_resp {
    rpc_resp_base_t super;
    int ret;
    struct pollfd fds[MAX_POLL_FDS];
} rpc_poll_resp_t;

typedef struct rpc_poll_outp {
    rpc_outp_base_t super;
    int ret;
    int fdsNum;
    struct pollfd* fds;
} rpc_poll_outp_t;

/* select */
typedef struct rpc_select_req {
    unsigned long func_id;  /* 8 */
    uint32_t trace_id;      /* 4 */
    int nfds;            /* 4 */
    fd_set readfds;     /* 128 */
    fd_set writefds;    /* 128 */
    fd_set exceptfds;   /* 128 */
    struct timeval timeout; /* 16 */
    char is_readfds_not_null;
    char is_writefds_not_null;
    char is_exceptfds_not_null;
    char is_timeout_not_null;
} rpc_select_req_t;

typedef struct rpc_select_resp {
    rpc_resp_base_t super;
    fd_set readfds;     /* 128 */
    fd_set writefds;    /* 128 */
    fd_set exceptfds;   /* 128 */
    struct timeval timeout; /* 16 */
    int ret;
} rpc_select_resp_t;

typedef struct rpc_select_outp {
    rpc_outp_base_t super;
    int ret;
    fd_set *readfds;
    fd_set *writefds;
    fd_set *exceptfds;
    struct timeval *timeout;
} rpc_select_outp_t;

/* gethostname */
typedef struct rpc_gethostname_req {
    unsigned long func_id;
    uint32_t trace_id;
    size_t len;
} rpc_gethostname_req_t;

typedef struct rpc_gethostname_resp {
    rpc_resp_base_t super;
    int ret;
    int len;
    char name[MAX_STRING_LEN];
} rpc_gethostname_resp_t;

typedef struct rpc_gethostname_outp {
    rpc_outp_base_t super;
    int ret;
    char* name;
    size_t len;
} rpc_gethostname_outp_t;

/* getsockopt*/
typedef struct rpc_getsockopt_req {
    unsigned long func_id;
    uint32_t trace_id;
    int sockfd;
    socklen_t optlen;
    int level;
    int optname;
} rpc_getsockopt_req_t;

typedef struct rpc_getsockopt_resp {
    rpc_resp_base_t super;
    int ret;
    socklen_t optlen;
    char optval[MAX_STRING_LEN];
} rpc_getsockopt_resp_t;

typedef struct rpc_getsockopt_outp {
    rpc_outp_base_t super;
    int ret;
    void* optval;
    socklen_t* optlen;
} rpc_getsockopt_outp_t;

/* printf */
typedef struct rpc_printf_req {
    unsigned long func_id;
    int len;
    char buf[MAX_STRING_LEN];
} rpc_printf_req_t;

/* getdents64*/
typedef struct rpc_getdents64_req {
    unsigned long func_id;
    uint32_t trace_id;
    int count;
    long pos;
    int fd;
} rpc_getdents64_req_t;

typedef struct rpc_getdents64_resp {
    rpc_resp_base_t super;
    long pos;
    int ret;
    char buf[MAX_STRING_LEN];
} rpc_getdents64_resp_t;

typedef struct rpc_getdents64_outp {
    rpc_outp_base_t super;
    long pos;
    char *buf;
    int bufsize;
    int ret;
} rpc_getdents64_outp_t;

/* ethercat ioctl */
typedef struct rpc_cmd_ioctl_req {
    unsigned long func_id;
    pid_t cmd_pid;
    char resv[4]; // 8字节对齐
    cmd_ioctl_resp_t ioctl_respond;
} rpc_cmd_ioctl_req_t; // actually for respond

/* ethercat open or close */
typedef struct rpc_cmd_base_req {
    unsigned long func_id;
    pid_t cmd_pid;
    char resv[4]; // 8字节对齐
    cmd_base_resp_t base_respond;
} rpc_cmd_base_req_t; // actually for respond
/* fopen */
typedef struct rpc_fopen_req {
    unsigned long func_id;
    uint32_t trace_id;
    char filename[MAX_FILE_NAME_LEN];
    char mode[MAX_FILE_MODE_LEN];
} rpc_fopen_req_t;

/* fclose */
typedef rpc_fcommon_req_t rpc_fclose_req_t;

/* fread */
typedef struct rpc_fread_req {
    unsigned long func_id;
    uint32_t trace_id;
    size_t size;
    size_t count;
    fileHandle fhandle;
} rpc_fread_req_t;

typedef struct rpc_fread_resp {
    rpc_resp_base_t super;
    size_t ret;
    char buf[MAX_STRING_LEN];
} rpc_fread_resp_t;

typedef struct rpc_fread_outp {
    rpc_outp_base_t super;
    size_t ret;
    size_t totalSize;
    void *buf;
} rpc_fread_outp_t;

/* fwrite */
typedef struct rpc_fwrite_req {
    unsigned long func_id;
    uint32_t trace_id;
    size_t size;
    size_t count;
    fileHandle fhandle;
    char buf[MAX_STRING_LEN];
} rpc_fwrite_req_t;

typedef struct rpc_fwrite_resp {
    rpc_resp_base_t super;
    size_t ret;
} rpc_fwrite_resp_t;

typedef struct rpc_fwrite_outp {
    rpc_outp_base_t super;
    size_t ret;
} rpc_fwrite_outp_t;

/* freopen */
typedef struct rpc_freopen_req {
    unsigned long func_id;
    uint32_t trace_id;
    char filename[MAX_FILE_NAME_LEN];
    char mode[MAX_FILE_MODE_LEN];
    fileHandle fhandle;
} rpc_freopen_req_t;

/* fputs */
typedef struct rpc_fputs_req {
    unsigned long func_id;
    uint32_t trace_id;
    fileHandle fhandle;
    char str[MAX_STRING_LEN];
} rpc_fputs_req_t;

/* fgets */
typedef struct rpc_fgets_req {
    unsigned long func_id;
    uint32_t trace_id;
    fileHandle fhandle;
    int size;
} rpc_fgets_req_t;

typedef struct rpc_fgets_resp {
    rpc_resp_base_t super;
    int isEof;
    char str[MAX_STRING_LEN];
} rpc_fgets_resp_t;

typedef struct rpc_fgets_outp {
    rpc_outp_base_t super;
    char *str;
    int size;
    int isEof;
} rpc_fgets_outp_t;

/* fflush */
typedef rpc_fcommon_req_t rpc_fflush_req_t;

/* feof */
typedef rpc_fcommon_req_t rpc_feof_req_t;

/* fprintf */
typedef struct rpc_fprintf_req {
    unsigned long func_id;
    fileHandle fhandle;
    int len;
    char buf[MAX_STRING_LEN];
} rpc_fprintf_req_t;

/* getc */
typedef rpc_fcommon_req_t rpc_getc_req_t;

/* ferror */
typedef rpc_fcommon_req_t rpc_ferror_req_t;

/* flockfile */
typedef struct rpc_flockfile_req {
    unsigned long func_id;
    fileHandle fhandle;
} rpc_flockfile_req_t;

/* funlockfile */
typedef struct rpc_funlockfile_req {
    unsigned long func_id;
    fileHandle fhandle;
} rpc_funlockfile_req_t;

/* getc_unlocked */
typedef rpc_fcommon_req_t rpc_getc_unlocked_req_t;

/* pclose */
typedef rpc_fcommon_req_t rpc_pclose_req_t;

/* tmpfile */
typedef struct rpc_tmpfile_req {
    unsigned long func_id;
    uint32_t trace_id;
} rpc_tmpfile_req_t;

/* clearerr */
typedef struct rpc_clearerr_req {
    unsigned long func_id;
    fileHandle fhandle;
} rpc_clearerr_req_t;

/* popen */
typedef struct rpc_popen_req {
    unsigned long func_id;
    uint32_t trace_id;
    char cmd[MAX_FILE_NAME_LEN];
    char mode[MAX_FILE_MODE_LEN];
} rpc_popen_req_t;

/* ungetc */
typedef struct rpc_ungetc_req {
    unsigned long func_id;
    uint32_t trace_id;
    int c;
    fileHandle fhandle;
} rpc_ungetc_req_t;

/* setvbuf */
typedef struct rpc_setvbuf_req {
    unsigned long func_id;
    uint32_t trace_id;
    fileHandle fhandle;
    int type;
    size_t size;
    int isNullbuf;
} rpc_setvbuf_req_t;

typedef struct rpc_setvbuf_resp {
    rpc_resp_base_t super;
    int ret;
    char buf[MAX_STRING_LEN];
} rpc_setvbuf_resp_t;

typedef struct rpc_setvbuf_outp {
    rpc_outp_base_t super;
    int ret;
    char *buf;
    size_t size;
    int isNullbuf;
} rpc_setvbuf_outp_t;

/* fseeko */
typedef struct rpc_fseeko_req {
    unsigned long func_id;
    uint32_t trace_id;
    fileHandle fhandle;
    long a;
    int b;
} rpc_fseeko_req_t;

/* ftello */
typedef rpc_fcommon_req_t rpc_ftello_req_t;

typedef struct rpc_ftello_resp {
    rpc_resp_base_t super;
    long ret;
} rpc_ftello_resp_t;

typedef struct rpc_ftello_outp {
    rpc_outp_base_t super;
    long ret;
} rpc_ftello_outp_t;

/* rename */
typedef struct rpc_rename_req {
    unsigned long func_id;
    uint32_t trace_id;
    char old[MAX_FILE_NAME_LEN];
    char new[MAX_FILE_NAME_LEN];
} rpc_rename_req_t;

/* remove */
typedef struct rpc_remove_req {
    unsigned long func_id;
    uint32_t trace_id;
    char path[MAX_FILE_NAME_LEN];
} rpc_remove_req_t;

/* mkstemp */
typedef struct rpc_mkstemp_req {
    unsigned long func_id;
    uint32_t trace_id;
    char tmp[MAX_FILE_NAME_LEN];
} rpc_mkstemp_req_t;

#endif  /* _RPC_INTERNAL_MODEL_H */
