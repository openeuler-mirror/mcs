#!/usr/bin/env python3
"""
Test script for mock_micad
This script tests all major functionality of mock_micad
"""

import os
import signal
import socket
import struct
import subprocess
import sys
import time
from pathlib import Path

# Constants from mock_micad.c
MAX_NAME_LEN = 32
MAX_FIRMWARE_PATH_LEN = 128
MAX_CPUSTR_LEN = 128
MAX_IOMEM_LEN = 512
MAX_NETWORK_LEN = 512
MICA_SOCKET_DIR = "/tmp/mica"
MICA_CREATE_SOCKET = f"{MICA_SOCKET_DIR}/mica-create.socket"

# Create message format
class CreateMessage:
    def __init__(self, name, path, ped="", ped_cfg="", debug=False,
                 cpu_str="", vcpu_num=0, max_vcpu_num=0, cpu_weight=0,
                 cpu_capacity=0, memory=0, max_memory=0, iomem="", network=""):
        self.name = name
        self.path = path
        self.ped = ped
        self.ped_cfg = ped_cfg
        self.debug = debug
        self.cpu_str = cpu_str
        self.vcpu_num = vcpu_num
        self.max_vcpu_num = max_vcpu_num
        self.cpu_weight = cpu_weight
        self.cpu_capacity = cpu_capacity
        self.memory = memory
        self.max_memory = max_memory
        self.iomem = iomem
        self.network = network

    def pack(self):
        """Pack message into binary format matching struct create_msg"""
        # Create format string matching the C struct from mock_micad.c
        fmt = f"{MAX_NAME_LEN}s"  # name
        fmt += f"{MAX_FIRMWARE_PATH_LEN}s"  # path
        fmt += f"{MAX_NAME_LEN}s"  # ped
        fmt += f"{MAX_FIRMWARE_PATH_LEN}s"  # ped_cfg
        fmt += "?"  # debug (bool)
        fmt += f"{MAX_CPUSTR_LEN}s"  # cpu_str
        fmt += "i"  # vcpu_num
        fmt += "i"  # max_vcpu_num
        fmt += "i"  # cpu_weight
        fmt += "i"  # cpu_capacity
        fmt += "i"  # memory
        fmt += "i"  # max_memory
        fmt += f"{MAX_IOMEM_LEN}s"  # iomem
        fmt += f"{MAX_NETWORK_LEN}s"  # network

        return struct.pack(fmt,
                          self.name.ljust(MAX_NAME_LEN, '\0').encode(),
                          self.path.ljust(MAX_FIRMWARE_PATH_LEN, '\0').encode(),
                          self.ped.ljust(MAX_NAME_LEN, '\0').encode(),
                          self.ped_cfg.ljust(MAX_FIRMWARE_PATH_LEN, '\0').encode(),
                          self.debug,
                          self.cpu_str.ljust(MAX_CPUSTR_LEN, '\0').encode(),
                          self.vcpu_num,
                          self.max_vcpu_num,
                          self.cpu_weight,
                          self.cpu_capacity,
                          self.memory,
                          self.max_memory,
                          self.iomem.ljust(MAX_IOMEM_LEN, '\0').encode(),
                          self.network.ljust(MAX_NETWORK_LEN, '\0').encode())

def send_to_socket(socket_path, data):
    """Send data to unix socket"""
    try:
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.connect(socket_path)
        if isinstance(data, str):
            sock.send(data.encode())
        else:
            sock.send(data)
        sock.close()
        return True
    except Exception as e:
        print(f"Error sending to socket: {e}")
        return False

def test_create_client(client_name="test-client"):
    """Test creating a client"""
    print(f"\n{'='*60}")
    print(f"TEST: Create client '{client_name}'")
    print(f"{'='*60}")

    # Create message
    msg = CreateMessage(
        name=client_name,
        path="/tmp/test/firmware.elf",
        ped="xen",
        ped_cfg="/tmp/test/image.bin",
        debug=True,
        cpu_str="1,2,3",
        vcpu_num=1,
        cpu_weight=100,
        cpu_capacity=50,
        memory=128,
        network="eth0"
    )

    packed = msg.pack()
    print(f"Sending create message ({len(packed)} bytes)...")

    if send_to_socket(MICA_CREATE_SOCKET, packed):
        print("✓ Create message sent successfully")
        time.sleep(1)

        # Check if socket was created
        client_socket = f"{MICA_SOCKET_DIR}/{client_name}.socket"
        if os.path.exists(client_socket):
            print(f"✓ Client socket created: {client_socket}")
        else:
            print(f"✗ Client socket NOT found: {client_socket}")
            return False

        # Check if PTY symlink was created
        pty_symlink = f"/tmp/mica/ttyRPMSG_{client_name}"
        if os.path.exists(pty_symlink):
            print(f"✓ PTY symlink created: {pty_symlink}")
            # Check where it points
            target = os.readlink(pty_symlink)
            print(f"  -> Points to: {target}")
        else:
            print(f"✗ PTY symlink NOT found: {pty_symlink}")

        # Check if shell process exists
        time.sleep(0.5)
        result = subprocess.run(["pgrep", "-f", f"{client_name}.socket"],
                              capture_output=True, text=True)
        if result.returncode == 0:
            print(f"✓ Shell process running (PID(s): {result.stdout.strip()})")
        else:
            print("? Could not verify shell process")

        return True
    else:
        print("✗ Failed to send create message")
        return False

def test_control_command(client_name, command):
    """Test a control command"""
    print(f"\n{'='*60}")
    print(f"TEST: Command '{command}' for client '{client_name}'")
    print(f"{'='*60}")

    client_socket = f"{MICA_SOCKET_DIR}/{client_name}.socket"

    if not os.path.exists(client_socket):
        print(f"✗ Client socket not found: {client_socket}")
        return False

    if send_to_socket(client_socket, command):
        print(f"✓ Command '{command}' sent successfully")
        time.sleep(0.5)
        return True
    else:
        print(f"✗ Failed to send command '{command}'")
        return False

def test_status(client_name):
    """Test status command"""
    return test_control_command(client_name, "status")

def test_start(client_name):
    """Test start command (start shell process)"""
    return test_control_command(client_name, "start")

def test_stop(client_name):
    """Test stop command (stop shell process)"""
    return test_control_command(client_name, "stop")

def test_remove(client_name):
    """Test rm command (remove client)"""
    return test_control_command(client_name, "rm")

def cleanup_existing():
    """Clean up any existing mock_micad instances and sockets"""
    print("Cleaning up existing resources...")

    # Kill existing mock_micad processes
    subprocess.run(["pkill", "-f", "mock_micad"], capture_output=True)
    time.sleep(0.5)

    # Remove socket directory
    if os.path.exists(MICA_SOCKET_DIR):
        for item in os.listdir(MICA_SOCKET_DIR):
            path = os.path.join(MICA_SOCKET_DIR, item)
            if os.path.isfile(path) or os.path.islink(path):
                os.unlink(path)
        os.rmdir(MICA_SOCKET_DIR)
        print(f"Removed socket directory: {MICA_SOCKET_DIR}")

def start_mock_micad():
    """Start mock_micad process"""
    print("\nStarting mock_micad...")

    # Check if already running
    result = subprocess.run(["pgrep", "-f", "mock_micad"], capture_output=True)
    if result.returncode == 0:
        print("mock_micad already running")
        return None

    # Start mock_micad
    proc = subprocess.Popen(["./mock_micad"],
                          stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT,
                          text=True,
                          bufsize=1,
                          universal_newlines=True)

    # Wait for startup
    time.sleep(2)

    # Check if it's running
    result = subprocess.run(["pgrep", "-f", "mock_micad"], capture_output=True)
    if result.returncode == 0:
        print("✓ mock_micad started successfully")
        return proc
    else:
        print("✗ Failed to start mock_micad")
        return None

def stop_mock_micad(proc):
    """Stop mock_micad process"""
    if not proc:
        subprocess.run(["pkill", "-f", "mock_micad"], capture_output=True)
        return

    print("\nStopping mock_micad...")
    proc.terminate()

    try:
        proc.wait(timeout=5)
        print("✓ mock_micad stopped gracefully")
    except subprocess.TimeoutExpired:
        proc.kill()
        print("✓ mock_micad killed")
    except Exception as e:
        print(f"Error stopping mock_micad: {e}")

def main():
    print("Mock micad Test Suite")
    print("=" * 60)

    # Check if mock_micad exists
    if not os.path.exists("./mock_micad"):
        print("✗ mock_micad binary not found")
        print("Please run 'make' first")
        return 1

    # Clean up existing resources
    cleanup_existing()

    # Start mock_micad
    proc = start_mock_micad()
    if not proc:
        return 1

    client_name = "test-client-1"

    try:
        # Test 1: Create client
        if not test_create_client(client_name):
            print("\n✗ CREATE test failed")
            return 1

        time.sleep(2)

        # Test 2: Status command
        test_status(client_name)
        time.sleep(1)

        # Test 3: Start command (shell already started during create)
        test_start(client_name)
        time.sleep(1)

        # Test 4: Stop command
        test_stop(client_name)
        time.sleep(1)

        # Test 5: Start again
        test_start(client_name)
        time.sleep(1)

        # Test 6: Remove client
        test_remove(client_name)
        time.sleep(1)

        # Test 7: Create again (should work after removal)
        if test_create_client(client_name):
            time.sleep(1)
            test_remove(client_name)

        print("\n" + "=" * 60)
        print("All basic tests completed!")
        print("=" * 60)

        return 0

    except Exception as e:
        print(f"\n✗ Test failed with exception: {e}")
        import traceback
        traceback.print_exc()
        return 1

    finally:
        # Cleanup
        time.sleep(1)
        stop_mock_micad(proc)

if __name__ == "__main__":
    sys.exit(main())
