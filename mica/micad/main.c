/*
 * Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved
 *
 * SPDX-License-Identifier: MulanPSL-2.0
 */
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <syslog.h>
#include <signal.h>
#include <semaphore.h>
#include <getopt.h>
#include <string.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/stat.h>
#include <errno.h>

#include "socket_listener.h"

#define PATH_PIDFILE "/var/run/micad.pid"

static char *pid_file;
static int pid_fd;

sem_t sem_micad_stop;

static int write_pid_file(void)
{
	int ret;
	char *tmp_pid = NULL;

	pid_file = strdup(PATH_PIDFILE);
	if(pid_file == NULL) {
		syslog(LOG_ERR, "could not alloc memory for pid file, aborting");
		return -1;
	}

	pid_fd = open(pid_file, O_RDWR|O_CREAT, 0640);
	if (pid_fd < 0) {
		syslog(LOG_ERR, "could not open pid file: %s, aborting", strerror(errno));
		goto err_free;
	}

	ret = lockf(pid_fd, F_TLOCK, 0);
	if (ret < 0) {
		/* another daemonize instance is already running, don't start up */
		syslog(LOG_ERR, "could not lock pid file, ensure micad is not running");
		goto err_close;
	}

	ret = asprintf(&tmp_pid, "%d\n", getpid());
	if (ret < 0)
		goto err_close;

	ret = write(pid_fd, tmp_pid, strlen(tmp_pid));
	if (ret < 0) {
		syslog(LOG_ERR, "could not write pid file, aborting");
		unlink(pid_file);
	}

	free((void*)tmp_pid);
	return 0;

err_close:
	close(pid_fd);
err_free:
	free(pid_file);
	return -1;
}

static void close_pid_file(void)
{
	if (pid_fd != -1) {
		(void)!lockf(pid_fd, F_ULOCK, 0);
		close(pid_fd);
	}

	if (pid_file != NULL) {
		unlink(pid_file);
		free(pid_file);
	}
}

static void daemonize(void)
{
	pid_t pid;

	/*
	 * First fork:
	 * Let the child process becomes session leader
	 */
	pid = fork();
	if (pid == -1)
		exit(EXIT_FAILURE);
	if (pid > 0)
		exit(EXIT_SUCCESS);
	if (setsid() < 0)
		exit(EXIT_FAILURE);

	/* Ignore signal sent from child to parent process */
	signal(SIGCHLD, SIG_IGN);

	/*
	 * second fork:
	 * ensure that the new process is not a session leader
	 */
	pid = fork();
	if (pid < 0)
		exit(EXIT_FAILURE);
	if (pid > 0)
		exit(EXIT_SUCCESS);

	/* Set new file permissions */
	umask(0);

	/* Change the working directory to the root directory */
	(void)!chdir("/");

	/* Reopen stdin (fd = 0), stdout (fd = 1), stderr (fd = 2) */
	close(STDIN_FILENO);
	close(STDOUT_FILENO);
	close(STDERR_FILENO);
	stdin = fopen("/dev/null", "r");
	stdout = fopen("/dev/null", "w+");
	stderr = fopen("/dev/null", "w+");

	openlog("micad", LOG_PID|LOG_CONS, LOG_DAEMON);
	/* Try to write PID of daemon to lockfile */
	if (write_pid_file() < 0) {
		syslog(LOG_ERR, "could not write pid file, aborting");
		closelog();
		exit(EXIT_FAILURE);
	}
}

void exit_handler(int sig)
{
	syslog(LOG_INFO, "received signal %d", sig);
	sem_post(&sem_micad_stop);
}

static int add_signal_handler(void)
{
	struct sigaction sa;

	memset(&sa, 0, sizeof(struct sigaction));
	sa.sa_handler = SIG_IGN;
	sigemptyset(&sa.sa_mask);
	sa.sa_flags = 0;
	if (sigaction(SIGHUP, &sa, NULL) < 0) {
		syslog(LOG_ERR, "Failed to ignore SIGHUP");
		return -1;
	}

	if (sigaction(SIGPIPE, &sa, NULL) < 0) {
		syslog(LOG_ERR, "Failed to ignore SIGPIPE");
		return -1;
	}

	if (sigaction(SIGUSR1, &sa, NULL) < 0) {
		syslog(LOG_ERR, "Failed to ignore SIGUSR1");
		return -1;
	}

	if (sem_init(&sem_micad_stop, 0, 0) == -1) {
		syslog(LOG_ERR, "Failed to init micad stop sem");
		return -1;
	}

	memset(&sa, 0, sizeof(struct sigaction));
	sa.sa_handler = exit_handler;
	sigemptyset(&sa.sa_mask);
	sa.sa_flags = 0;
	if (sigaction(SIGINT, &sa, NULL) < 0) {
		syslog(LOG_ERR, "Failed to add handler for SIGINT");
		return -1;
	}

	if (sigaction(SIGTERM, &sa, NULL) < 0) {
		syslog(LOG_ERR, "Failed to add handler for SIGTERM");
		return -1;
	}

	return 0;
}

int main(int argc, char **argv)
{
	int ret;

	daemonize();

	ret = add_signal_handler();
	if (ret)
		goto out;

	ret = register_socket_listener();
	if (ret) {
		syslog(LOG_ERR, "Failed to start micad");
		goto out;
	}
	syslog(LOG_INFO, "Started micad");
	sem_wait(&sem_micad_stop);
	unregister_socket_listener();
	syslog(LOG_INFO, "Stopped micad");
out:
	close_pid_file();
	return ret;
}
