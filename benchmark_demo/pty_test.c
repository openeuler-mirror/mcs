#include "benchmark.h"
#include <errno.h>
#include <fcntl.h>
#include <getopt.h>
#include <limits.h>
#include <math.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <termios.h>
#include <time.h>
#include <unistd.h>

#define PACKET_SIZE 1024   // Packet size (Bytes)
#define DURATION 10000     // Test duration (ms)
#define PRINT_INTERVAL 100 // Print Interval (ms)
#define PING_TIMES 5
static int64_t ping_delay;
static const char *pty_path;
static pthread_t thread_id;
static int flag_ping = 0;
static int flag_bandwidth = 0;
static int pty_fd;
static int64_t count_ping_msg, min_ping, total_ping, max_ping, count_bandwidth;
static double min_bandwidth, max_bandwidth;
struct timespec prev, now;

static void cleanup(int sig)
{
    /* Clear the buffer and close the descriptor. */
    tcflush(pty_fd, TCIOFLUSH);
    printf("\e[1BTest was interrupted and benchmark ended.\n");
    close(pty_fd);
    exit(0);
}

static void display_help()
{
    printf("Usage:\n"
           "pty_test <options>\n\n"
           "-P   --path=/dev/pts/x                 Parameter specification is required, and the path of\n"
           "                                       the virtual terminal device is required.\n\n"
           "-p   --ping                            Used to specify the ping test.\n\n"
           "-b   --bandwidth                       Used to specify bandwidth testing.\n\n"
           "-l   --loop=times_ping                 Used in conjunction with ping to specify the number\n"
           "                                       of ping tests.\n\n"
           "-t   --time=time                       Used in conjunction with bandwidth to specify the \n"
           "                                       duration of the test.\n\n"
           "-h   --help                            Display help information.\n\n");
    exit(0);
}

/* Create a child thread to read the data of the device/dev/pts/x */
static void *slave_read_thread(void *arg)
{
    ssize_t bytes_read;
    char buffer[PACKET_SIZE];
    while (1) {
        bytes_read = read(pty_fd, buffer, PACKET_SIZE);
        if (bytes_read < 0) {
            perror("slave thread read\n");
        }
    }
}

static int do_ping()
{
    char buffer[PACKET_SIZE];
    const char *message = "Hello,world\r";
    int64_t avg_ping;
    ssize_t bytes_read, bytes_written;

    get_current_time(&prev);
    bytes_written = write(pty_fd, message, strlen(message));
    if (bytes_written == -1) {
        perror("write");
        close(pty_fd);
        return 1;
    }

    bytes_read = read(pty_fd, buffer, sizeof(buffer));
    if (bytes_read == -1) {
        perror("read");
        close(pty_fd);
        return 1;
    }
    get_current_time(&now);
    ping_delay = calcdiff_us(now, prev);

    count_ping_msg++;
    min_ping = ping_delay < min_ping ? ping_delay : min_ping;
    max_ping = ping_delay > max_ping ? ping_delay : max_ping;
    total_ping += ping_delay;
    avg_ping = total_ping / count_ping_msg;
    printf("Count:%6ld | Realtime:%6ldμs | Min:%6ldμs | Avg:%6ldμs | Max:%6ldμs\n\e[1A", count_ping_msg, ping_delay, min_ping, avg_ping, max_ping);

    return 0;
}

static int ping_test(int loop)
{
    if (!loop) {
        while (1) {
            do_ping();
            usleep(200000);
        }
    } else {
        for (int i = 0; i < loop; i++) {
            do_ping();
            usleep(200000);
        }
    }

    printf("\e[1Bping test over!\n");
}

static void bandwitdh_test(int time)
{
    char buffer[PACKET_SIZE];
    double elapsedSeconds, bandwidthMbps, avg_bandwidthMbps;
    ssize_t bytes_written;
    unsigned long startTime, tmp_time, currentTime;
    unsigned long long totalBytes_tmp = 0;
    unsigned long long totalBytes = 0;
    int duration = DURATION;

    printf("\nWaiting for senconds, Testing communication bandwidth...\n");

    /*Fill in the data and set the terminator '\r'*/
    memset(buffer, 'a', PACKET_SIZE);
    buffer[PACKET_SIZE - 1] = '\r';

    if (time)
        duration = time * 1000;

    startTime = get_system_time_milliseconds();
    tmp_time = startTime;
    while ((currentTime = get_system_time_milliseconds()) - startTime <=
           duration) {

        bytes_written = write(pty_fd, buffer, PACKET_SIZE);
        if (bytes_written < 0) {
            perror("bandwidth write\n");
            exit(1);
        } else {
            totalBytes_tmp += bytes_written;
            totalBytes += bytes_written;
        }

        /*Computational bandwidth*/
        if ((currentTime - tmp_time) >= PRINT_INTERVAL) {
            count_bandwidth++;
            elapsedSeconds = difftime(currentTime, tmp_time);
            bandwidthMbps = (totalBytes_tmp * 8.0) / (elapsedSeconds * 1000.0);
            min_bandwidth = bandwidthMbps < min_bandwidth ? bandwidthMbps : min_bandwidth;
            max_bandwidth = bandwidthMbps > max_bandwidth ? bandwidthMbps : max_bandwidth;
            avg_bandwidthMbps = (totalBytes * 8.0) / (difftime(currentTime, startTime) * 1000.0);
            printf("Count: %4ld | Elapsed: %6.2lf milliseconds | Min: %6.2lf Mbps | realtime: %6.2lf Mbps | Avg: %6.2lf Mbps | Max: %6.2lf Mbps\n\e[1A", count_bandwidth,
                   elapsedSeconds, min_bandwidth, bandwidthMbps, avg_bandwidthMbps, max_bandwidth);

            tmp_time = currentTime;
            totalBytes_tmp = 0;
        }
    }
    printf("\e[1B%lld Bytes was written!\n", totalBytes);
    printf("bandwidth test over!\n");
}

int main(int argc, char *argv[])
{
    struct termios tty;
    int ret, loop, opt, time;
    time = 0;
    loop = 0;
    struct option long_options[] = {{"path", required_argument, NULL, 'P'},
                                    {"ping", no_argument, NULL, 'p'},
                                    {"bandwidth", no_argument, NULL, 'b'},
                                    {"loop", required_argument, NULL, 'l'},
                                    {"time", required_argument, NULL, 't'},
                                    {"help", no_argument, NULL, 'h'},
                                    {NULL, 0, NULL, 0}};
    count_ping_msg = 0, min_ping = INT_MAX, total_ping = 0, max_ping = 0;
    count_bandwidth = 0, min_bandwidth = HUGE_VAL, max_bandwidth = 0;

    /* ctrl+c signal, do cleanup before program exit */
    signal(SIGINT, cleanup);

    while ((opt = getopt_long(argc, argv, "P:pbl:", long_options, NULL)) != -1) {
        switch (opt) {
        case 'P':
            pty_path = optarg;
            break;
        case 'p':
            flag_ping = 1;
            break;
        case 'b':
            flag_bandwidth = 1;
            break;
        case 'l':
            loop = atoi(optarg);
            break;
        case 't':
            time = atoi(optarg);
            break;
        case 'h':
            display_help();
            break;
        case '?':
            printf("Unknown option: %c ", (char)optopt);
            display_help();
        default:
            break;
        }
    }

    /*Turn on the pseudo terminal device and turn off the echo.*/
    /* Otherwise, the master device will scramble to read the data of the slave device */
    pty_fd = open(pty_path, O_RDWR);
    if (pty_fd == -1) {
        perror("open pty");
        return 1;
    }
    tcgetattr(pty_fd, &tty);
    tty.c_iflag &= ~ECHO;
    tcsetattr(pty_fd, TCSANOW, &tty);

    /*Test data ping delay*/
    if (flag_ping)
        ping_test(loop);
    else if (flag_bandwidth) {
        pthread_create(&thread_id, NULL, slave_read_thread, NULL);
        bandwitdh_test(time);

        ret = pthread_cancel(thread_id);
        if (ret != 0) {
            perror("pthread_cancel");
            exit(EXIT_FAILURE);
        }

        ret = pthread_join(thread_id, NULL);
        if (ret != 0) {
            perror("pthread_join");
            exit(EXIT_FAILURE);
        }
    }

    /*Empty descriptor cache and close it.*/
    tcflush(pty_fd, TCIOFLUSH);
    close(pty_fd);

    return 0;
}
