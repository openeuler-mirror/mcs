#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# SPDX-License-Identifier: MulanPSL-2.0
"""
Mock micad implementation in Python.
This program mimics the behavior of socket_listener.c, responding to
commands from mica.py and maintaining a list of mock clients.
"""

import errno
import logging
import os
import pty
import select
import signal
import socket
import struct
import sys
import threading
import time
import tty
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

MICA_SOCKET_DIRECTORY = "/tmp/mica"
MICA_GDB_SERVER_PORT = 5678

MAX_EVENTS = 64
MAX_PATH_LEN = 64
MAX_CLIENTS = 10

CTRL_MSG_SIZE = 32
RESPONSE_MSG_SIZE = 256

MICA_MSG_SUCCESS = "MICA-SUCCESS"
MICA_MSG_FAILED = "MICA-FAILED"

# PID file for micad compatibility
MICAD_PIDFILE = "/run/micad.pid"
MOCK_PIDFILE = "/tmp/mica/micad.pid"

# Max lengths from mica.py pack() method (line 85)
# Format: '66s256s16s256s?128siiiiii512s512s'
MAX_NAME_LEN = 66           # name
MAX_FIRMWARE_PATH_LEN = 256 # path and ped_cfg
MAX_PED_LEN = 16            # ped
MAX_CPUSTR_LEN = 128        # cpu_str
MAX_IOMEM_LEN = 512         # iomem
MAX_NETWORK_LEN = 512       # network

# Message format matching mica.py's pack() method
# struct create_msg {
#   char name[66];
#   char path[256];
#   char ped[16];
#   char ped_cfg[256];
#   bool debug;
#   char cpu_str[128];
#   int vcpu_num;
#   int max_vcpu_num;
#   int cpu_weight;
#   int cpu_capacity;
#   int memory;
#   int max_memory;
#   char iomem[512];
#   char network[512];
# }

CREATE_MSG_FORMAT = f"{MAX_NAME_LEN}s{MAX_FIRMWARE_PATH_LEN}s{MAX_PED_LEN}s{MAX_FIRMWARE_PATH_LEN}s?{MAX_CPUSTR_LEN}siiiiii{MAX_IOMEM_LEN}s{MAX_NETWORK_LEN}s"

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='[%(asctime)s] [%(levelname)s] %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S'
)
logger = logging.getLogger(__name__)


class MockClient:
    """Represents a mock client with state and PTY."""
    def __init__(self, name: str):
        self.name = name
        self.status = "Created"  # Created, Running, Stopped
        self.shell_pid: Optional[int] = None
        self.pty_master_fd: Optional[int] = None
        self.pty_slave_path: Optional[str] = None
        self.pty_symlink: Optional[str] = None
        self.socket_path = f"{MICA_SOCKET_DIRECTORY}/{name}.socket"
        self.quick_pty_proc = None  # subprocess.Popen instance for quick_pty
        self.quick_pty_pid = None   # PID of quick_pty process

        self.path = ""
        self.ped = ""
        self.ped_cfg = ""
        self.debug = False
        self.cpu_str = ""
        self.vcpu_num = 0
        self.max_vcpu_num = 0
        self.cpu_weight = 0
        self.cpu_capacity = 0
        self.memory = 0
        self.max_memory = 0
        self.iomem = ""
        self.network = ""

        # Thread for mock zephyr shell
        self.shell_thread: Optional[threading.Thread] = None
        self.running = False

    def __repr__(self):
        return f"MockClient(name={self.name}, status={self.status})"

    def update_config(self, config: dict):
        """Update configuration from create message."""
        for key, value in config.items():
            if hasattr(self, key):
                setattr(self, key, value)


class Mocker:
    """Main mock micad server."""
    def __init__(self, socket_dir: str = MICA_SOCKET_DIRECTORY, use_quick_pty: bool = False):
        self.socket_dir = socket_dir
        self.use_quick_pty = use_quick_pty
        self.clients: Dict[str, MockClient] = {}
        self.listeners: Dict[str, socket.socket] = {}  # name -> socket
        self.running = False
        self.epoll_fd: Optional[select.epoll] = None
        self.lock = threading.RLock()
        self.pid_file = None

        os.makedirs(self.socket_dir, mode=0o700, exist_ok=True)

    def _sanitize_name(self, name: str) -> str:
        """Sanitize client name for use in symlink."""
        sanitized = []
        for c in name:
            if c.isalnum() or c in ('_', '-'):
                sanitized.append(c)
            else:
                sanitized.append('_')
        return ''.join(sanitized)

    def _create_pid_file(self) -> bool:
        """Create PID file for micad compatibility."""
        try:
            pid_dir = os.path.dirname(MICAD_PIDFILE)
            if not os.path.exists(pid_dir):
                try:
                    os.makedirs(pid_dir, mode=0o755, exist_ok=True)
                except PermissionError:
                    logger.warning(f"Cannot create PID directory {pid_dir}, falling back to mock location")
                    self.pid_file = MOCK_PIDFILE
            else:
                if os.access(pid_dir, os.W_OK):
                    self.pid_file = MICAD_PIDFILE
                else:
                    logger.warning(f"Cannot write to PID directory {pid_dir}, falling back to mock location")
                    self.pid_file = MOCK_PIDFILE

            if self.pid_file == MOCK_PIDFILE:
                os.makedirs(os.path.dirname(self.pid_file), mode=0o755, exist_ok=True)

            with open(self.pid_file, 'w') as f:
                f.write(str(os.getpid()))

            logger.info(f"Created PID file: {self.pid_file}")
            return True

        except Exception as e:
            logger.error(f"Failed to create PID file: {e}")
            self.pid_file = None
            return False

    def _remove_pid_file(self):
        if self.pid_file and os.path.exists(self.pid_file):
            try:
                os.unlink(self.pid_file)
                logger.info(f"Removed PID file: {self.pid_file}")
            except Exception as e:
                logger.warning(f"Failed to remove PID file {self.pid_file}: {e}")
            self.pid_file = None

    def _create_pty(self, client: MockClient) -> bool:
        if self.use_quick_pty:
            return self._create_pty_with_quick_pty(client)
        try:
            master_fd, slave_fd = pty.openpty()
            slave_name = os.ttyname(slave_fd)

            client.pty_master_fd = master_fd
            client.pty_slave_path = slave_name

            sanitized_name = self._sanitize_name(client.name)
            symlink_path = f"{self.socket_dir}/ttyRPMSG_{sanitized_name}_0"
            if os.path.exists(symlink_path):
                os.unlink(symlink_path)
            os.symlink(slave_name, symlink_path)
            client.pty_symlink = symlink_path

            logger.info(f"Created PTY for client {client.name}: slave={slave_name}, symlink={symlink_path}")

            client.running = True
            client.shell_thread = threading.Thread(
                target=self._run_zephyr_mock,
                args=(client, master_fd, slave_fd),
                daemon=True
            )
            client.shell_thread.start()

            return True

        except Exception as e:
            logger.error(f"Failed to create PTY for client {client.name}: {e}")
            if client.pty_master_fd:
                os.close(client.pty_master_fd)
                client.pty_master_fd = None
            return False

    def _create_pty_with_quick_pty(self, client: MockClient) -> bool:
        try:
            import subprocess

            script_dir = os.path.dirname(os.path.abspath(__file__))
            script_path = os.path.join(script_dir, "quick_pty.py")

            if not os.path.exists(script_path):
                logger.error(f"quick_pty.py not found at {script_path}")
                return False

            proc = subprocess.Popen(
                [sys.executable, script_path, client.name],
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                bufsize=1,
                universal_newlines=True
            )

            client.quick_pty_proc = proc
            client.quick_pty_pid = proc.pid

            time.sleep(0.5)

            if proc.poll() is not None:
                stdout, _ = proc.communicate(timeout=1)
                logger.error(f"quick_pty.py exited early: {stdout}")
                client.quick_pty_proc = None
                client.quick_pty_pid = None
                return False

            sanitized_name = self._sanitize_name(client.name)
            symlink_path = f"{self.socket_dir}/ttyRPMSG_{sanitized_name}_0"
            if not os.path.exists(symlink_path):
                logger.warning(f"PTY symlink not created: {symlink_path}")
                # Process might still be running, we'll keep it
                # but return False? Actually quick_pty.py creates the symlink immediately
                # so if not created, something went wrong
                # Don't kill process yet, maybe it's slow
                time.sleep(1)
                if not os.path.exists(symlink_path):
                    logger.error(f"PTY symlink still not created after wait")
                    proc.terminate()
                    proc.wait(timeout=2)
                    client.quick_pty_proc = None
                    client.quick_pty_pid = None
                    return False

            logger.info(f"Started quick_pty.py for client {client.name} (PID: {proc.pid})")
            logger.info(f"PTY symlink: {symlink_path}")
            return True

        except Exception as e:
            logger.error(f"Failed to start quick_pty.py for {client.name}: {e}")
            if 'proc' in locals() and proc.poll() is None:
                proc.terminate()
                proc.wait(timeout=2)
            client.quick_pty_proc = None
            client.quick_pty_pid = None
            return False

    def _run_zephyr_mock(self, client: MockClient, master_fd: int, slave_fd: int):
        """Run mock zephyr shell (similar to quick_pty.py)."""
        try:
            slave_name = os.ttyname(slave_fd)
            logger.info(f"【RPMSG Proxy】Mock for {client.name}.")
            logger.info(f"【RPMSG Proxy】PTY: {slave_name}")

            import fcntl
            flags = fcntl.fcntl(master_fd, fcntl.F_GETFL)
            fcntl.fcntl(master_fd, fcntl.F_SETFL, flags | os.O_NONBLOCK)

            prompt = b"uart:~$ "
            os.write(master_fd, b"\r\n*** Booting Zephyr OS build v3.x.x ***\r\n")
            os.write(master_fd, prompt)

            input_buffer = b""

            while client.running:
                try:
                    data = os.read(master_fd, 1024)
                except (OSError, IOError) as e:
                    if e.errno == errno.EAGAIN or e.errno == errno.EWOULDBLOCK:
                        time.sleep(0.01)
                        continue
                    else:
                        break

                if not data:
                    time.sleep(0.01)
                    continue

                # Echo
                os.write(master_fd, data)

                input_buffer += data

                if b'\r' in data or b'\n' in data:
                    os.write(master_fd, b"\r\n")

                    cmd = input_buffer.decode(errors='ignore').strip()
                    input_buffer = b""

                    if cmd == "help":
                        resp = (
                            "Please press the <Tab> button to see all available commands.\r\n"
                            "hello     : say hello\r\n"
                            "kernel    : kernel commands\r\n"
                            "device    : device commands\r\n"
                            "history   : command history\r\n"
                        )
                        os.write(master_fd, resp.encode())
                    elif cmd == "hello":
                        os.write(master_fd, b"Hello from Zephyr!\r\n")
                    elif cmd == "":
                        pass  # empty enter
                    else:
                        msg = f"Error: {cmd} not found\r\n"
                        os.write(master_fd, msg.encode())

                    os.write(master_fd, prompt)

        except Exception as e:
            logger.error(f"Zephyr mock shell error for {client.name}: {e}")
        finally:
            os.close(master_fd)
            os.close(slave_fd)
            logger.info(f"Zephyr mock shell stopped for {client.name}")

    def _destroy_pty(self, client: MockClient):
        """Destroy PTY and stop shell thread."""
        if client.quick_pty_proc:
            try:
                client.quick_pty_proc.terminate()
                client.quick_pty_proc.wait(timeout=2.0)
                logger.info(f"Terminated quick_pty process for client {client.name}")
            except Exception as e:
                logger.warning(f"Error terminating quick_pty process for {client.name}: {e}")
                try:
                    client.quick_pty_proc.kill()
                except:
                    pass
            finally:
                client.quick_pty_proc = None
                client.quick_pty_pid = None

        client.running = False
        if client.shell_thread and client.shell_thread.is_alive():
            client.shell_thread.join(timeout=1.0)

        if client.pty_master_fd:
            try:
                os.close(client.pty_master_fd)
            except:
                pass
            client.pty_master_fd = None

        if client.pty_symlink and os.path.exists(client.pty_symlink):
            try:
                os.unlink(client.pty_symlink)
            except:
                pass
            client.pty_symlink = None

    def _setup_socket(self, socket_path: str) -> Optional[socket.socket]:
        """Create and bind a Unix domain socket."""
        try:
            if os.path.exists(socket_path):
                os.unlink(socket_path)

            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)

            sock.bind(socket_path)
            sock.listen(MAX_CLIENTS)
            sock.setblocking(False)

            logger.info(f"Socket created and listening: {socket_path}")
            return sock

        except Exception as e:
            logger.error(f"Failed to setup socket {socket_path}: {e}")
            return None

    def _add_listener(self, name: str, socket_path: str, is_create: bool = False) -> bool:
        """Add a listener socket."""
        sock = self._setup_socket(socket_path)
        if not sock:
            return False

        with self.lock:
            self.listeners[name] = sock

        if self.epoll_fd:
            self.epoll_fd.register(sock.fileno(), select.EPOLLIN | select.EPOLLET)

        return True

    def _send_response(self, conn: socket.socket, message: str, success: bool = True):
        """Send response to client."""
        try:
            if message:
                conn.send(message.encode())
            if success:
                conn.send(MICA_MSG_SUCCESS.encode())
            else:
                conn.send(MICA_MSG_FAILED.encode())
        except Exception as e:
            logger.error(f"Failed to send response: {e}")

    def _handle_create_message(self, conn: socket.socket, data: bytes):
        """Handle client creation message."""
        try:
            if len(data) < struct.calcsize(CREATE_MSG_FORMAT):
                logger.error(f"Create message too short: {len(data)} bytes")
                self._send_response(conn, "Invalid create message", False)
                return

            unpacked = struct.unpack(CREATE_MSG_FORMAT, data)

            name_bytes = unpacked[0]
            path_bytes = unpacked[1]
            ped_bytes = unpacked[2]
            ped_cfg_bytes = unpacked[3]
            debug = unpacked[4]
            cpu_str_bytes = unpacked[5]
            vcpu_num = unpacked[6]
            max_vcpu_num = unpacked[7]
            cpu_weight = unpacked[8]
            cpu_capacity = unpacked[9]
            memory = unpacked[10]
            max_memory = unpacked[11]
            iomem_bytes = unpacked[12]
            network_bytes = unpacked[13]

            name = name_bytes.decode('utf-8', errors='ignore').rstrip('\x00')
            path = path_bytes.decode('utf-8', errors='ignore').rstrip('\x00')
            ped = ped_bytes.decode('utf-8', errors='ignore').rstrip('\x00')
            ped_cfg = ped_cfg_bytes.decode('utf-8', errors='ignore').rstrip('\x00')
            cpu_str = cpu_str_bytes.decode('utf-8', errors='ignore').rstrip('\x00')
            iomem = iomem_bytes.decode('utf-8', errors='ignore').rstrip('\x00')
            network = network_bytes.decode('utf-8', errors='ignore').rstrip('\x00')

            logger.info(f"Creating client: name={name}, path={path}, ped={ped}, debug={debug}")

            with self.lock:
                if name in self.clients:
                    logger.error(f"Client {name} already exists")
                    self._send_response(conn, f"Client {name} already exists", False)
                    return

                client = MockClient(name)
                config = {
                    'path': path,
                    'ped': ped,
                    'ped_cfg': ped_cfg,
                    'debug': debug,
                    'cpu_str': cpu_str,
                    'vcpu_num': vcpu_num,
                    'max_vcpu_num': max_vcpu_num,
                    'cpu_weight': cpu_weight,
                    'cpu_capacity': cpu_capacity,
                    'memory': memory,
                    'max_memory': max_memory,
                    'iomem': iomem,
                    'network': network
                }
                client.update_config(config)

                client_socket_path = f"{self.socket_dir}/{name}.socket"
                if not self._add_listener(name, client_socket_path, is_create=False):
                    self._send_response(conn, f"Failed to create socket for {name}", False)
                    return

                if not self._create_pty(client):
                    with self.lock:
                        sock = self.listeners.pop(name, None)
                        if sock:
                            sock.close()
                            if os.path.exists(client_socket_path):
                                os.unlink(client_socket_path)
                    self._send_response(conn, f"Failed to create PTY for {name}", False)
                    return

                self.clients[name] = client
                client.status = "Running"

                logger.info(f"Successfully created client {name}")
                self._send_response(conn, f"Created client {name}", True)

        except Exception as e:
            logger.error(f"Error handling create message: {e}")
            self._send_response(conn, f"Internal error: {e}", False)

    def _handle_control_command(self, conn: socket.socket, client_name: str, command: str):
        """Handle control command for a client."""
        with self.lock:
            client = self.clients.get(client_name)
            if not client:
                self._send_response(conn, f"Client {client_name} not found", False)
                return

            command = command.strip()
            logger.info(f"Control command for {client_name}: {command}")

            if command == "start":
                if client.status == "Running":
                    self._send_response(conn, f"Client {client_name} is already running", True)
                    return

                if client.pty_master_fd is None:
                    if not self._create_pty(client):
                        self._send_response(conn, f"Failed to start client {client_name}", False)
                        return

                client.status = "Running"
                self._send_response(conn, f"Started client {client_name}", True)

            elif command == "stop":
                if client.status == "Created":
                    self._send_response(conn, f"Cannot stop client {client_name} in Created state", False)
                    return

                self._destroy_pty(client)
                client.status = "Stopped"
                self._send_response(conn, f"Stopped client {client_name}", True)

            elif command == "rm":
                self._destroy_pty(client)

                with self.lock:
                    sock = self.listeners.pop(client_name, None)
                    if os.path.exists(client.socket_path):
                        try:
                            os.unlink(client.socket_path)
                        except Exception as e:
                            logger.warning(f"Failed to unlink socket {client.socket_path}: {e}")
                    if sock:
                        sock.close()

                self.clients.pop(client_name, None)

                self._send_response(conn, f"Removed client {client_name}", True)

            elif command == "status":
                # Format status similar to socket_listener.c
                status_line = f"{client.name:<30}{client.cpu_str:<20}{client.status:<20}mock services"
                self._send_response(conn, status_line, True)

            elif command.startswith("set "):
                parts = command.split()
                if len(parts) != 3:
                    self._send_response(conn, "Invalid set command. Usage: set <key> <value>", False)
                    return

                key, value = parts[1], parts[2]
                logger.info(f"Set {key}={value} for client {client_name} (simulated)")
                self._send_response(conn, f"Set {key}={value} (simulated)", True)

            elif command == "gdb":
                if not client.debug:
                    self._send_response(conn, "The elf file does not support debugging", False)
                    return

                gdb_cmd = f"gdb {client.path} -ex 'set remotetimeout unlimited' -ex 'target extended-remote :{MICA_GDB_SERVER_PORT}' -ex 'set remote run-packet off'"
                self._send_response(conn, gdb_cmd, True)

            else:
                self._send_response(conn, f"Invalid command: {command}", False)

    def _handle_connection(self, sock: socket.socket, is_create_socket: bool = False):
        """Handle incoming connection on a socket."""
        try:
            conn, addr = sock.accept()
            conn.setblocking(True)

            if is_create_socket:
                data = conn.recv(struct.calcsize(CREATE_MSG_FORMAT))
                if data:
                    self._handle_create_message(conn, data)
            else:
                data = conn.recv(CTRL_MSG_SIZE)
                if data:
                    command = data.decode('utf-8', errors='ignore').rstrip('\x00')
                    client_name = None
                    with self.lock:
                        for name, listener_sock in self.listeners.items():
                            if listener_sock.fileno() == sock.fileno():
                                client_name = name
                                break

                    if client_name:
                        self._handle_control_command(conn, client_name, command)
                    else:
                        self._send_response(conn, "Client socket not found", False)

            conn.close()

        except Exception as e:
            logger.error(f"Error handling connection: {e}")

    def _event_loop(self):
        """Main event loop using epoll."""
        try:
            self.epoll_fd = select.epoll()
            with self.lock:
                for name, sock in self.listeners.items():
                    self.epoll_fd.register(sock.fileno(), select.EPOLLIN | select.EPOLLET)

            logger.info("Event loop started")

            while self.running:
                try:
                    events = self.epoll_fd.poll(1.0)  # timeout 1 second
                    for fileno, event in events:
                        if event & select.EPOLLIN:
                            sock = None
                            is_create = False
                            with self.lock:
                                for name, listener_sock in self.listeners.items():
                                    if listener_sock.fileno() == fileno:
                                        sock = listener_sock
                                        is_create = (name == "mica-create")
                                        break

                            if sock:
                                self._handle_connection(sock, is_create)

                except (IOError, OSError) as e:
                    if e.errno == errno.EINTR:
                        continue
                    logger.error(f"Epoll error: {e}")
                    break

        except Exception as e:
            logger.error(f"Event loop error: {e}")
        finally:
            if self.epoll_fd:
                self.epoll_fd.close()
                self.epoll_fd = None

    def start(self):
        """Start the mock server."""
        if self.running:
            return False

        logger.info(f"Starting mock micad on socket directory: {self.socket_dir}")

        for item in os.listdir(self.socket_dir):
            path = os.path.join(self.socket_dir, item)
            if os.path.isfile(path) or os.path.islink(path):
                try:
                    os.unlink(path)
                except:
                    pass

        main_socket_path = f"{self.socket_dir}/mica-create.socket"
        if not self._add_listener("mica-create", main_socket_path, is_create=True):
            return False

        self.running = True

        self.event_thread = threading.Thread(target=self._event_loop, daemon=True)
        self.event_thread.start()

        logger.info("Mock micad started successfully")

        if not self._create_pid_file():
            logger.warning("Failed to create PID file, but continuing...")

        return True

    def stop(self):
        """Stop the mock server."""
        if not self.running:
            return

        logger.info("Stopping mock micad...")
        self.running = False

        if hasattr(self, 'event_thread') and self.event_thread.is_alive():
            self.event_thread.join(timeout=2.0)

        with self.lock:
            for client in list(self.clients.values()):
                self._destroy_pty(client)
            self.clients.clear()

        with self.lock:
            for name, sock in self.listeners.items():
                socket_path = f"{self.socket_dir}/{name}.socket" if name != "mica-create" else f"{self.socket_dir}/mica-create.socket"
                if os.path.exists(socket_path):
                    try:
                        os.unlink(socket_path)
                    except:
                        pass
                try:
                    sock.close()
                except:
                    pass
            self.listeners.clear()

        self._remove_pid_file()

        logger.info("Mock micad stopped")

    def run(self):
        """Run the server until interrupted."""
        if not self.start():
            return

        try:
            while self.running:
                time.sleep(1)
        except KeyboardInterrupt:
            logger.info("Received interrupt, shutting down...")
        finally:
            self.stop()


def main():
    """Main entry point."""
    import argparse

    parser = argparse.ArgumentParser(description='Mock micad server')
    parser.add_argument('--socket-dir', default=MICA_SOCKET_DIRECTORY,
                        help=f'Socket directory (default: {MICA_SOCKET_DIRECTORY})')
    parser.add_argument('--quiet', action='store_true', help='Reduce output')
    parser.add_argument('-z', '--use-quick-pty', action='store_true',
                        help='Use quick_pty.py for Zephyr simulation')
    args = parser.parse_args()

    if args.quiet:
        logging.getLogger().setLevel(logging.WARNING)

    mocker = Mocker(socket_dir=args.socket_dir, use_quick_pty=args.use_quick_pty)
    mocker.run()


if __name__ == "__main__":
    main()
