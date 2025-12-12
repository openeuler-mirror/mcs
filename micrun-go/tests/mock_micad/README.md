# Mock Micad

Mock implementation of micad for testing purposes.

## Overview

mock_micad simulates the mica daemon (micad) without requiring actual RTOS or hardware. It provides:

- Socket-based communication matching micad's protocol
- Client lifecycle management (create, start, stop, remove, status)
- PTY simulation with real shell processes
- Comprehensive debug logging

## Building

```bash
make
```

This creates the `mock_micad` binary.

## Running

Start mock_micad:

```bash
./mock_micad
```

The daemon will:
- Create socket directory `/tmp/mica` (if needed)
- Listen on `/tmp/mica/mica-create.socket` for client creation
- Create per-client sockets as `/tmp/mica/<client-name>.socket`
- Create PTY symlinks as `/tmp/mica/ttyRPMSG_<client-name>`
- Spawn shell processes for each client

Press Ctrl+C to stop.

## Testing

Run the automated test suite:

```bash
make test
```

Or manually test:

```bash
# Start mock_micad in background
./mock_micad &

# Create a client (using Python for binary protocol)
python3 -c "
import test_mock
test_mock.test_create_client('my-client')
"

# Check status
echo "status" | nc -U /tmp/mica/my-client.socket

# Start (if needed)
echo "start" | nc -U /tmp/mica/my-client.socket

# Stop
echo "stop" | nc -U /tmp/mica/my-client.socket

# Remove
echo "rm" | nc -U /tmp/mica/my-client.socket

# Stop mock_micad
pkill mock_micad
```

## Socket Protocol

### Create Client

Send a binary `struct create_msg` to `/tmp/mica/mica-create.socket`.

The message format:
- name (32 bytes) - client name
- path (128 bytes) - firmware path
- ped (32 bytes) - pedestal type
- ped_cfg (128 bytes) - pedestal config
- debug (1 byte) - bool
- cpu_str (128 bytes) - CPU string
- vcpu_num (4 bytes) - int
- max_vcpu_num (4 bytes) - int
- cpu_weight (4 bytes) - int
- cpu_capacity (4 bytes) - int
- memory (4 bytes) - int
- max_memory (4 bytes) - int
- iomem (512 bytes) - I/O memory mapping
- network (512 bytes) - network config

Total: 1084 bytes

All strings are null-padded to their fixed lengths.

### Control Commands

Send text commands to `/tmp/mica/<client-name>.socket`:

- `start` - Start the client (if stopped)
- `stop` - Stop the client
- `rm` - Remove the client completely
- `status` - Get client status
- `set <key> <value>` - Set parameters (simulated)

Response: `MICA-SUCCESS` or `MICA-FAILED`\n
## Debug Output

mock_micad prints detailed debug information:

- Received packet hex dumps
- Parsed message fields
- Client status changes
- PTY creation details
- Process lifecycle events

Example output:
```
[INFO] Mock micad starting...
[INFO] Socket created and listening: /tmp/mica/mica-create.socket
[INFO] Epoll thread started
[INFO] Mock micad started successfully
[INFO] Main socket: /tmp/mica/mica-create.socket
[INFO] Press Ctrl+C to stop
```

## Cleanup

If mock_micad crashes or leaves resources behind:

```bash
make clean-all
```

This removes:
- All socket files
- PTY symlinks
- Socket directory

## Architecture

### Components

1. **Epoll Event Loop** - Handles multiple socket connections
2. **Client Manager** - Tracks client lifecycle and state
3. **PTY Manager** - Creates/deletes PTY devices and shell processes
4. **Socket Manager** - Manages Unix domain sockets

### State Machine

```
Created --start--> Running --stop--> Stopped
   |                                  |
   |--------------rm------------------|
```

### Process Tree

```
mock_micad (parent)
  ├─ epoll thread
  ├─ shell process 1 (PID via forkpty)
  ├─ shell process 2 (PID via forkpty)
  └─ ...
```

## Implementation Details

### PTY Creation

For each client, mock_micad:
1. Calls `posix_openpt()` to create PTY master
2. Authorizes with `grantpt()` and `unlockpt()`
3. Gets slave name with `ptsname_r()`
4. Creates symlink `/tmp/mica/ttyRPMSG_<name>` → `/dev/pts/N`
5. Forks shell process via `forkpty()`
6. Execs `/bin/bash` (or `/bin/sh` as fallback)
7. Parent holds master fd for I/O

### Lifecycle Management

- **Create**: Socket + PTY + Shell process + Register client
- **Start**: Set status to Running (shell already started)
- **Stop**: Send SIGTERM to shell (wait 1s, then SIGKILL)
- **Remove**: Terminate shell + Close PTY + Remove socket + Unlink + Free client

### Cleanup on Exit

When mock_micad receives SIGINT/SIGTERM:
1. Sets `is_running = false`
2. Joins epoll thread
3. Iterates all clients:
   - Terminates shell
   - Closes PTY fd
   - Removes symlink
   - Removes socket
   - Frees memory
4. Closes all listener sockets
5. Removes main socket
6. Removes socket directory (if empty)

## Limitations

1. No actual RTOS communication - purely simulation
2. Fixed-size message struct - all strings padded
3. No resource limits (CPU, memory) - simulated only
4. No network/device passthrough
5. No GDB server simulation
6. Shell may not exit cleanly - may require SIGKILL

## Files

- `mock_micad.c` - Main implementation
- `Makefile` - Build configuration
- `test_mock.py` - Automated test suite
- `README.md` - This file

## License

Same as parent project.
