#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <errno.h>
#include <signal.h>
#include <stdbool.h>
#include <sys/stat.h>
#include <stdint.h>
#include <fcntl.h>
#include <sys/types.h>
#include <sys/epoll.h>
#include <pthread.h>
#include <sys/time.h>
#include <time.h>
#include <sys/wait.h>
#include <pty.h>
#include <utmp.h>

/* Constants matching micad/socket_listener.c */
#define MICA_SOCKET_DIRECTORY "/tmp/mica"
#define MICA_GDB_SERVER_PORT 5678

#define MAX_EVENTS		64
#define MAX_PATH_LEN		64
#define MAX_CLIENTS		10

#define CTRL_MSG_SIZE		32
#define RESPONSE_MSG_SIZE	256

#define MICA_MSG_SUCCESS	"MICA-SUCCESS"
#define MICA_MSG_FAILED		"MICA-FAILED"

/* Max lengths from mica_client.h */
#define MAX_NAME_LEN		32
#define MAX_FIRMWARE_PATH_LEN	128
#define MAX_CPUSTR_LEN		128
#define MAX_IOMEM_LEN		512
#define MAX_NETWORK_LEN		512

/* Message format from socket_listener.c line 54-72 */
struct create_msg {
	/* required configs */
	char name[MAX_NAME_LEN];
	char path[MAX_FIRMWARE_PATH_LEN];
	/* optional configs for MICA*/
	char ped[MAX_NAME_LEN];
	char ped_cfg[MAX_FIRMWARE_PATH_LEN];
	bool debug;
	/* optional configs for pedestal */
	char cpu_str[MAX_CPUSTR_LEN];
	int vcpu_num;
	int max_vcpu_num;
	int cpu_weight;
	int cpu_capacity;
	int memory;
	int max_memory;
	char iomem[MAX_IOMEM_LEN];
	char network[MAX_NETWORK_LEN];
};

/* Client structure */
struct mock_client {
	char name[MAX_NAME_LEN];
	char status[32];          /* Created / Running / Stopped */
	pid_t shell_pid;          /* Shell process PID */
	int pty_master_fd;        /* PTY master fd */
	char socket_path[MAX_PATH_LEN];
	char pty_symlink[128];    /* /dev/ttyRPMSG_<name> symlink */
	char pts_slave_path[128]; /* /dev/pts/N real path */
	struct mock_client *next;
};

/* Listener unit structure */
struct listen_unit {
	char name[MAX_NAME_LEN];
	int socket_fd;
	char socket_path[MAX_PATH_LEN];
	bool is_create_socket;  /* true for mica-create.socket, false for client sockets */
	struct listen_unit *next;
};

/* Global variables */
static volatile bool is_running = true;
static struct listen_unit *listener_list = NULL;
static struct mock_client *client_list = NULL;
static pthread_mutex_t client_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t listener_mutex = PTHREAD_MUTEX_INITIALIZER;
static int global_epoll_fd = -1;

/* Function prototypes */
static void print_hex_dump(const void *data, size_t len);
static void print_as_string(const void *data, size_t len);
static void print_create_msg(const struct create_msg *msg);
static struct mock_client *find_client(const char *name);
static bool client_exists(const char *name);
static void register_client(const char *name);
static void set_client_status(const char *name, const char *status);
static void remove_client(const char *name);
static void print_all_client_statuses(void);

/* PTY functions */
static int create_pty_for_client(struct mock_client *client);
static void destroy_pty_for_client(struct mock_client *client);
static void terminate_shell(struct mock_client *client);

/* Socket functions */
static int setup_socket(const char *socket_path);
static int remove_socket(const char *client_name);
static int create_client_socket(const char *client_name);

/* Command handlers */
static void handle_client_create(int client_fd);
static void handle_client_ctrl(int client_fd, struct listen_unit *unit);

/* Cleanup and utils */
static void cleanup_all_resources(void);
static void signal_handler(int signum);

/* Debug logging macros */
#define INFO(fmt, ...) printf("[INFO] " fmt "\n", ##__VA_ARGS__)
#define ERROR(fmt, ...) printf("*ERROR* " fmt "\n", ##__VA_ARGS__)
#define WARN(fmt, ...) printf("*WARN* " fmt "\n", ##__VA_ARGS__)
#define DEBUG_PACKET(fmt, ...) printf("[PACKET] " fmt "\n", ##__VA_ARGS__)

/* Debug print functions */
static void print_hex_dump(const void *data, size_t len)
{
	const unsigned char *bytes = (const unsigned char *)data;
	size_t i;

	DEBUG_PACKET("Received data (%zu bytes):", len);
	for (i = 0; i < len; i++) {
		printf("%02x ", bytes[i]);
		if ((i + 1) % 16 == 0)
			printf("\n");
	}
	if (i % 16 != 0)
		printf("\n");
}

static void print_as_string(const void *data, size_t len)
{
	const char *str = (const char *)data;
	size_t i;

	printf("[PACKET] As string: '");
	for (i = 0; i < len && i < 200; i++) {
		char c = str[i];
		if (c >= 32 && c <= 126) {
			printf("%c", c);
		} else if (c == 0) {
			printf("\\0");
		} else {
			printf("\\x%02x", (unsigned char)c);
		}
	}
	if (i >= 200 && len > 200)
		printf("... (%zu more bytes)", len - 200);
	printf("'\n");
}

static void print_create_msg(const struct create_msg *msg)
{
	INFO("=== Create Message Details ===");
	INFO("Name: '%.*s'", (int)strnlen(msg->name, sizeof(msg->name)), msg->name);
	INFO("Path: '%.*s'", (int)strnlen(msg->path, sizeof(msg->path)), msg->path);
	INFO("Ped: '%.*s'", (int)strnlen(msg->ped, sizeof(msg->ped)), msg->ped);
	INFO("PedCfg: '%.*s'", (int)strnlen(msg->ped_cfg, sizeof(msg->ped_cfg)), msg->ped_cfg);
	INFO("Debug: %s", msg->debug ? "true" : "false");
	INFO("CPU String: '%.*s'", (int)strnlen(msg->cpu_str, sizeof(msg->cpu_str)), msg->cpu_str);
	INFO("VCPU Num: %d", msg->vcpu_num);
	INFO("Max VCPU Num: %d", msg->max_vcpu_num);
	INFO("CPU Weight: %d", msg->cpu_weight);
	INFO("CPU Capacity: %d", msg->cpu_capacity);
	INFO("Memory: %d", msg->memory);
	INFO("Max Memory: %d", msg->max_memory);
	INFO("IOMEM: '%.*s'", (int)strnlen(msg->iomem, sizeof(msg->iomem)), msg->iomem);
	INFO("Network: '%.*s'", (int)strnlen(msg->network, sizeof(msg->network)), msg->network);
	INFO("=== End Message ===");
}

/* Client management functions */
static struct mock_client *find_client(const char *name)
{
	struct mock_client *client;

	pthread_mutex_lock(&client_mutex);
	client = client_list;
	while (client) {
		if (strncmp(client->name, name, MAX_NAME_LEN) == 0) {
			pthread_mutex_unlock(&client_mutex);
			return client;
		}
		client = client->next;
	}
	pthread_mutex_unlock(&client_mutex);
	return NULL;
}

static bool client_exists(const char *name)
{
	return find_client(name) != NULL;
}

static void register_client(const char *name)
{
	struct mock_client *client;

	client = calloc(1, sizeof(*client));
	if (!client) {
		ERROR("Failed to allocate client");
		return;
	}

	snprintf(client->name, sizeof(client->name), "%s", name);
	snprintf(client->status, sizeof(client->status), "%s", "Created");
	client->shell_pid = -1;
	client->pty_master_fd = -1;

	pthread_mutex_lock(&client_mutex);
	client->next = client_list;
	client_list = client;
	pthread_mutex_unlock(&client_mutex);

	INFO("Registered client '%s' with status 'Created'", name);
}

static void set_client_status(const char *name, const char *status)
{
	struct mock_client *client;

	client = find_client(name);
	if (!client) {
		ERROR("Client '%s' not found", name);
		return;
	}

	snprintf(client->status, sizeof(client->status), "%s", status);
	INFO("Client '%s' status changed to '%s'", name, status);
}

static void remove_client(const char *name)
{
	struct mock_client *client, *prev = NULL;

	pthread_mutex_lock(&client_mutex);
	client = client_list;
	while (client) {
		if (strncmp(client->name, name, MAX_NAME_LEN) == 0) {
			if (prev)
				prev->next = client->next;
			else
				client_list = client->next;
			pthread_mutex_unlock(&client_mutex);

			destroy_pty_for_client(client);
			remove_socket(name);
			free(client);
			INFO("Removed client '%s'", name);
			return;
		}
		prev = client;
		client = client->next;
	}
	pthread_mutex_unlock(&client_mutex);
	ERROR("Client '%s' not found for removal", name);
}

static void print_all_client_statuses(void)
{
	struct mock_client *client;
	int count = 0;

	pthread_mutex_lock(&client_mutex);
	client = client_list;
	if (!client) {
		INFO("No clients registered");
		pthread_mutex_unlock(&client_mutex);
		return;
	}

	INFO("=== Client Status List ===");
	while (client) {
		INFO("Client %d: name='%s', status='%s', pid=%d, pty=%s",
		     count++, client->name, client->status, client->shell_pid,
		     client->pty_symlink[0] ? client->pty_symlink : "N/A");
		client = client->next;
	}
	INFO("=== Total: %d clients ===", count);
	pthread_mutex_unlock(&client_mutex);
}

/* PTY functions */
static void sanitize_client_name(char *dst, size_t dst_sz, const char *src)
{
	size_t i, j = 0;
	if (!dst || !src || dst_sz == 0)
		return;

	for (i = 0; src[i] != '\0' && j + 1 < dst_sz; i++) {
		char c = src[i];
		if ((c >= 'a' && c <= 'z') ||
		    (c >= 'A' && c <= 'Z') ||
		    (c >= '0' && c <= '9') ||
		    c == '_' || c == '-') {
			dst[j++] = c;
		} else {
			dst[j++] = '_';
		}
	}
	dst[j] = '\0';
}

static int create_pty_for_client(struct mock_client *client)
{
	char suffix[MAX_NAME_LEN];
	char pts_name[128];
	int master_fd;
	pid_t pid;
	int ret;

	if (!client)
		return -1;

	/* Open PTY master */
	master_fd = posix_openpt(O_RDWR | O_NOCTTY);
	if (master_fd < 0) {
		ERROR("Failed to open PTY master: %s", strerror(errno));
		return -1;
	}

	ret = grantpt(master_fd);
	if (ret != 0) {
		ERROR("Failed to grantpt: %s", strerror(errno));
		close(master_fd);
		return -1;
	}

	ret = unlockpt(master_fd);
	if (ret != 0) {
		ERROR("Failed to unlockpt: %s", strerror(errno));
		close(master_fd);
		return -1;
	}

	ret = ptsname_r(master_fd, pts_name, sizeof(pts_name));
	if (ret != 0) {
		ERROR("Failed to get PTY name: %s", strerror(errno));
		close(master_fd);
		return -1;
	}

	/* Store PTS slave path */
	snprintf(client->pts_slave_path, sizeof(client->pts_slave_path), "%s", pts_name);

	/* Create symlink */
	sanitize_client_name(suffix, sizeof(suffix), client->name);
	snprintf(client->pty_symlink, sizeof(client->pty_symlink),
		 "/tmp/mica/ttyRPMSG_%s", suffix);

	unlink(client->pty_symlink);
	if (symlink(pts_name, client->pty_symlink) != 0) {
		ERROR("Failed to create PTY symlink %s: %s",
		      client->pty_symlink, strerror(errno));
		close(master_fd);
		return -1;
	}

	/* Also try to create in /dev/ if possible */
	char dev_link[128];
	snprintf(dev_link, sizeof(dev_link), "/dev/ttyRPMSG_%s", suffix);
	unlink(dev_link);
	if (symlink(pts_name, dev_link) != 0) {
		/* Not critical if this fails */
		DEBUG_PACKET("Failed to create /dev symlink (non-critical)");
	}

	/* Fork and execute shell */
	INFO("Starting shell for client '%s'...", client->name);

	pid = forkpty(&master_fd, NULL, NULL, NULL);
	if (pid < 0) {
		ERROR("Failed to forkpty: %s", strerror(errno));
		unlink(client->pty_symlink);
		unlink(dev_link);
		return -1;
	}

	if (pid == 0) {
		/* Child process - execute shell */
		const char *shell = getenv("SHELL");
		if (!shell)
			shell = "/bin/bash";

		execl(shell, shell, "-i", NULL);

		/* If execl fails, try /bin/sh */
		execl("/bin/sh", "sh", "-i", NULL);

		/* If both fail, exit */
		ERROR("Failed to execute shell");
		_exit(1);
	}

	/* Parent process */
	client->shell_pid = pid;
	client->pty_master_fd = master_fd;

	INFO("PTY created for client '%s':", client->name);
	INFO("  Slave: %s", pts_name);
	INFO("  Symlink: %s", client->pty_symlink);
	INFO("  Shell PID: %d", pid);

	return 0;
}

static void destroy_pty_for_client(struct mock_client *client)
{
	if (!client)
		return;

	/* Terminate shell process */
	terminate_shell(client);

	/* Close PTY master fd */
	if (client->pty_master_fd >= 0) {
		close(client->pty_master_fd);
		client->pty_master_fd = -1;
	}

	/* Remove symlink */
	if (client->pty_symlink[0] != '\0') {
		unlink(client->pty_symlink);
		client->pty_symlink[0] = '\0';
	}

	INFO("Destroyed PTY for client '%s'", client->name);
}

static void terminate_shell(struct mock_client *client)
{
	if (!client || client->shell_pid <= 0)
		return;

	INFO("Terminating shell for client '%s' (PID %d)", client->name, client->shell_pid);

	/* Send SIGTERM */
	kill(client->shell_pid, SIGTERM);

	/* Wait for process to exit (with timeout) */
	int status;
	pid_t result;
	int timeout = 10; /* 10 * 100ms = 1 second */

	while (timeout-- > 0) {
		result = waitpid(client->shell_pid, &status, WNOHANG);
		if (result == client->shell_pid)
			break;
		usleep(100000); /* 100ms */
	}

	/* If still running, send SIGKILL */
	if (timeout <= 0) {
		INFO("Shell did not terminate gracefully, sending SIGKILL");
		kill(client->shell_pid, SIGKILL);
		waitpid(client->shell_pid, &status, 0);
	}

	client->shell_pid = -1;
	INFO("Shell terminated for client '%s'", client->name);
}

/* Socket functions */
static int setup_socket(const char *socket_path)
{
	int server_fd;
	struct sockaddr_un server_addr;
	struct stat st;
	char *dir;
	char *last_slash;

	/* Remove existing socket */
	if (stat(socket_path, &st) == 0)
		unlink(socket_path);

	/* Create directory if needed */
	dir = strdup(socket_path);
	if (!dir) {
		ERROR("strdup failed: %s", strerror(errno));
		return -1;
	}

	last_slash = strrchr(dir, '/');
	if (last_slash) {
		*last_slash = '\0';
		if (mkdir(dir, 0755) < 0 && errno != EEXIST) {
			ERROR("mkdir %s failed: %s", dir, strerror(errno));
			free(dir);
			return -1;
		}
	}
	free(dir);

	/* Create socket */
	server_fd = socket(AF_UNIX, SOCK_STREAM, 0);
	if (server_fd < 0) {
		ERROR("socket creation failed: %s", strerror(errno));
		return -1;
	}

	memset(&server_addr, 0, sizeof(server_addr));
	server_addr.sun_family = AF_UNIX;
	snprintf(server_addr.sun_path, sizeof(server_addr.sun_path), "%s", socket_path);

	if (bind(server_fd, (struct sockaddr *)&server_addr, sizeof(server_addr)) < 0) {
		ERROR("bind %s failed: %s", socket_path, strerror(errno));
		close(server_fd);
		return -1;
	}

	if (listen(server_fd, MAX_CLIENTS) < 0) {
		ERROR("listen failed: %s", strerror(errno));
		close(server_fd);
		return -1;
	}

	INFO("Socket created and listening: %s", socket_path);
	return server_fd;
}

static int remove_socket(const char *client_name)
{
	char socket_path[MAX_PATH_LEN];
	struct stat st;

	snprintf(socket_path, sizeof(socket_path), "%s/%s.socket",
		 MICA_SOCKET_DIRECTORY, client_name);

	if (stat(socket_path, &st) == 0 && S_ISSOCK(st.st_mode)) {
		unlink(socket_path);
		INFO("Removed socket: %s", socket_path);
	}

	return 0;
}

static int create_client_socket(const char *client_name)
{
	char socket_path[MAX_PATH_LEN];
	struct listen_unit *unit;
	int server_fd;

	snprintf(socket_path, sizeof(socket_path), "%s/%s.socket",
		 MICA_SOCKET_DIRECTORY, client_name);

	server_fd = setup_socket(socket_path);
	if (server_fd < 0)
		return -1;

	unit = calloc(1, sizeof(*unit));
	if (!unit) {
		close(server_fd);
		return -1;
	}

	snprintf(unit->name, sizeof(unit->name), "%s", client_name);
	snprintf(unit->socket_path, sizeof(unit->socket_path), "%s", socket_path);
	unit->socket_fd = server_fd;
	unit->is_create_socket = false;

	pthread_mutex_lock(&listener_mutex);
	unit->next = listener_list;
	listener_list = unit;
	pthread_mutex_unlock(&listener_mutex);

	/* Add to epoll if epoll is ready */
	if (global_epoll_fd >= 0) {
		struct epoll_event ev;
		ev.events = EPOLLIN;
		ev.data.ptr = unit;
		if (epoll_ctl(global_epoll_fd, EPOLL_CTL_ADD, server_fd, &ev) < 0) {
			ERROR("Failed to add client socket to epoll: %s", strerror(errno));
		}
	}

	INFO("Created client socket: %s", socket_path);
	return 0;
}

static void handle_client_create(int client_fd)
{
	char buffer[sizeof(struct create_msg)];
	ssize_t bytes_received;
	struct create_msg *msg;
	char client_name[MAX_NAME_LEN];
	size_t name_len;
	int ret;

	bytes_received = recv(client_fd, buffer, sizeof(buffer), 0);
	if (bytes_received < 0) {
		ERROR("recv failed: %s", strerror(errno));
		return;
	}

	DEBUG_PACKET("Received %zd bytes on create socket", bytes_received);
	print_hex_dump(buffer, bytes_received);
	print_as_string(buffer, bytes_received);

	if (bytes_received >= offsetof(struct create_msg, debug) + sizeof(bool)) {
		msg = (struct create_msg *)buffer;
		print_create_msg(msg);

		/* Extract client name */
		name_len = strnlen(msg->name, sizeof(msg->name));
		memcpy(client_name, msg->name, name_len);
		client_name[name_len] = '\0';

		INFO("Creating client: '%s'", client_name);

		/* Check if client exists */
		if (client_exists(client_name)) {
			ERROR("Client '%s' already exists", client_name);
			return;
		}

		/* Create client socket first */
		ret = create_client_socket(client_name);
		if (ret < 0) {
			ERROR("Failed to create client socket for '%s'", client_name);
			return;
		}

		/* Create mock client structure */
		register_client(client_name);

		/* Create PTY for the client */
		struct mock_client *client = find_client(client_name);
		if (client) {
			ret = create_pty_for_client(client);
			if (ret < 0) {
				ERROR("Failed to create PTY for client '%s'", client_name);
				remove_client(client_name);
				return;
			}
		}

		INFO("Successfully created client '%s' with PTY and shell", client_name);
	} else {
		DEBUG_PACKET("Received incomplete message (%zd bytes) - may be string command", bytes_received);

		/* Handle text commands like "create <name>" or "status" */
		buffer[bytes_received] = '\0';

		/* Remove trailing newline if present */
		if (bytes_received > 0 && buffer[bytes_received-1] == '\n')
			buffer[bytes_received-1] = '\0';

		/* Parse command */
		char *cmd = buffer;
		while (*cmd == ' ') cmd++; /* Skip leading spaces */

		if (strncasecmp(cmd, "create ", 7) == 0 || strncasecmp(cmd, "create\t", 7) == 0) {
			/* Extract name after "create" */
			char *name_start = cmd + 6;
			while (*name_start == ' ' || *name_start == '\t') name_start++;

			char name[MAX_NAME_LEN] = {0};
			size_t i = 0;
			while (name_start[i] && name_start[i] != ' ' && name_start[i] != '\n' &&
			       name_start[i] != '\t' && i < MAX_NAME_LEN-1) {
				name[i] = name_start[i];
				i++;
			}

			if (name[0] == '\0') {
				ERROR("Create command missing client name");
				return;
			}

			INFO("Creating client via text command: '%s'", name);

			/* Check if client exists */
			if (client_exists(name)) {
				ERROR("Client '%s' already exists - cannot create duplicate", name);
				return;
			}

			/* Create client socket */
			if (create_client_socket(name) < 0) {
				ERROR("Failed to create client socket for '%s'", name);
				return;
			}

			/* Register client */
			register_client(name);

			/* Create PTY */
			struct mock_client *client = find_client(name);
			if (client) {
				if (create_pty_for_client(client) < 0) {
					ERROR("Failed to create PTY for client '%s'", name);
					remove_client(name);
					return;
				}
			}

			INFO("Successfully created client '%s' via text command", name);
		} else if (strncasecmp(cmd, "status", 6) == 0) {
			/* Show status of all clients */
			INFO("Status command received on create socket");
			print_all_client_statuses();
		} else if (bytes_received > 0) {
			WARN("Unknown command on create socket: '%s'", buffer);
			INFO("Valid commands: 'create <name>' or 'status'");
		}
	}
}

static void handle_client_ctrl(int client_fd, struct listen_unit *unit)
{
	char buffer[CTRL_MSG_SIZE];
	ssize_t bytes_received;
	struct mock_client *client;

	bytes_received = recv(client_fd, buffer, sizeof(buffer) - 1, 0);
	if (bytes_received < 0) {
		ERROR("recv failed: %s", strerror(errno));
		return;
	}

	buffer[bytes_received] = '\0';
	DEBUG_PACKET("Control command for '%s': %s", unit->name, buffer);

	client = find_client(unit->name);
	if (!client) {
		ERROR("Client '%s' not found", unit->name);
		return;
	}

	if (strncmp(buffer, "start", 5) == 0) {
		if (strcmp(client->status, "Running") == 0) {
			ERROR("Client '%s' is already Running", unit->name);
			return;
		}

		if (client->shell_pid <= 0) {
			/* Re-create PTY and shell */
			int ret = create_pty_for_client(client);
			if (ret < 0) {
				ERROR("Failed to start client '%s'", unit->name);
				return;
			}
		}

		set_client_status(unit->name, "Running");
	} else if (strncmp(buffer, "stop", 4) == 0) {
		if (strcmp(client->status, "Created") == 0) {
			ERROR("Cannot stop client '%s' in 'Created' state", unit->name);
			return;
		}

		terminate_shell(client);
		set_client_status(unit->name, "Stopped");
	} else if (strncmp(buffer, "rm", 2) == 0) {
		INFO("Removing client '%s'", unit->name);
		remove_client(unit->name);
	} else if (strncmp(buffer, "status", 6) == 0) {
		INFO("Status for client '%s': %s, PID=%d, PTY=%s",
		     unit->name, client->status, client->shell_pid,
		     client->pty_symlink[0] ? client->pty_symlink : "N/A");
		print_all_client_statuses();
	} else if (strncmp(buffer, "set", 3) == 0) {
		/* Simulate set command - just log it */
		DEBUG_PACKET("Set command received: %s (simulated - no actual effect)", buffer);
		INFO("Set command for client '%s': %s", unit->name, buffer);
	} else {
		ERROR("Unknown command for client '%s': %s", unit->name, buffer);
	}
}

static void *epoll_thread(void *arg)
{
	struct epoll_event events[MAX_EVENTS];
	struct listen_unit *unit;
	int nfds;
	int i;

	(void)arg; /* Unused */

	INFO("Epoll thread started");

	while (is_running) {
		nfds = epoll_wait(global_epoll_fd, events, MAX_EVENTS, 1000);
		if (nfds < 0) {
			if (errno == EINTR)
				continue;
			ERROR("epoll_wait failed: %s", strerror(errno));
			break;
		}

		for (i = 0; i < nfds; i++) {
			unit = (struct listen_unit *)events[i].data.ptr;
			int client_fd = accept(unit->socket_fd, NULL, NULL);
			if (client_fd < 0) {
				if (errno != EINTR)
					ERROR("accept failed: %s", strerror(errno));
				continue;
			}

			if (unit->is_create_socket) {
				handle_client_create(client_fd);
			} else {
				handle_client_ctrl(client_fd, unit);
			}

			close(client_fd);
		}
	}

	INFO("Epoll thread exiting");
	return NULL;
}

static int add_listener(const char *name, const char *socket_path, bool is_create_socket)
{
	struct listen_unit *unit;
	int server_fd;
	struct epoll_event ev;

	server_fd = setup_socket(socket_path);
	if (server_fd < 0)
		return -1;

	unit = calloc(1, sizeof(*unit));
	if (!unit) {
		close(server_fd);
		return -1;
	}

	snprintf(unit->name, sizeof(unit->name), "%s", name);
	snprintf(unit->socket_path, sizeof(unit->socket_path), "%s", socket_path);
	unit->socket_fd = server_fd;
	unit->is_create_socket = is_create_socket;

	pthread_mutex_lock(&listener_mutex);
	unit->next = listener_list;
	listener_list = unit;
	pthread_mutex_unlock(&listener_mutex);

	/* Add to epoll */
	if (global_epoll_fd >= 0) {
		ev.events = EPOLLIN;
		ev.data.ptr = unit;
		if (epoll_ctl(global_epoll_fd, EPOLL_CTL_ADD, server_fd, &ev) < 0) {
			ERROR("Failed to add socket to epoll: %s", strerror(errno));
		}
	}

	return 0;
}

static void cleanup_all_resources(void)
{
	struct listen_unit *unit, *unit_next;
	struct mock_client *client, *client_next;

	INFO("=== Starting cleanup ===");

	/* Cleanup all clients */
	pthread_mutex_lock(&client_mutex);
	client = client_list;
	client_list = NULL;
	pthread_mutex_unlock(&client_mutex);

	while (client) {
		client_next = client->next;
		INFO("Cleaning up client '%s'", client->name);
		destroy_pty_for_client(client);
		remove_socket(client->name);
		free(client);
		client = client_next;
	}

	/* Cleanup all listeners */
	pthread_mutex_lock(&listener_mutex);
	unit = listener_list;
	listener_list = NULL;
	pthread_mutex_unlock(&listener_mutex);

	while (unit) {
		unit_next = unit->next;
		INFO("Closing listener socket: %s", unit->socket_path);
		close(unit->socket_fd);
		unlink(unit->socket_path);
		free(unit);
		unit = unit_next;
	}

	/* Close epoll fd */
	if (global_epoll_fd >= 0) {
		close(global_epoll_fd);
		global_epoll_fd = -1;
	}

	/* Remove main socket */
	char main_socket[128];
	snprintf(main_socket, sizeof(main_socket), "%s/mica-create.socket",
		 MICA_SOCKET_DIRECTORY);
	unlink(main_socket);

	/* Try to remove directory (if empty) */
	rmdir(MICA_SOCKET_DIRECTORY);

	INFO("=== Cleanup completed ===");
}

static void signal_handler(int signum)
{
	INFO("Received signal %d, shutting down...", signum);
	is_running = false;
}

int main(int argc, char *argv[])
{
	pthread_t thread;
	char main_socket[128];
	int opt;
	bool quiet_mode = false;

	/* Parse options */
	while ((opt = getopt(argc, argv, "q")) != -1) {
		switch (opt) {
		case 'q':
			quiet_mode = true;
			/* For now, quiet_mode not implemented - all messages print */
			break;
		default:
			fprintf(stderr, "Usage: %s [-q]\n", argv[0]);
			fprintf(stderr, "  -q: quiet mode (not implemented)\n");
			return 1;
		}
	}

	(void)quiet_mode; /* Suppress unused warning */

	INFO("Mock micad starting...");

	/* Setup signal handlers */
	signal(SIGINT, signal_handler);
	signal(SIGTERM, signal_handler);
	signal(SIGPIPE, SIG_IGN);

	/* Create epoll fd */
	global_epoll_fd = epoll_create1(0);
	if (global_epoll_fd < 0) {
		ERROR("Failed to create epoll: %s", strerror(errno));
		return 1;
	}

	/* Setup main socket */
	snprintf(main_socket, sizeof(main_socket), "%s/mica-create.socket",
		 MICA_SOCKET_DIRECTORY);

	if (add_listener("mica-create", main_socket, true) < 0) {
		ERROR("Failed to add main listener");
		close(global_epoll_fd);
		return 1;
	}

	/* Start epoll thread */
	if (pthread_create(&thread, NULL, epoll_thread, NULL) != 0) {
		ERROR("Failed to create epoll thread: %s", strerror(errno));
		cleanup_all_resources();
		return 1;
	}

	INFO("Mock micad started successfully");
	INFO("Main socket: %s", main_socket);
	INFO("Press Ctrl+C to stop");
	print_all_client_statuses();

	/* Main loop */
	while (is_running) {
		sleep(1);
	}

	INFO("Shutting down...");
	pthread_join(thread, NULL);
	cleanup_all_resources();

	INFO("Mock micad stopped");
	return 0;
}
