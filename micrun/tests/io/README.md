# MicRun IO Tests

The maintained IO entrypoints are:

- `micrun/tests/io/run_all_io_tests.sh`
- `micrun/tests/bin/test-io-qemu`

## Environment

Use the shared test environment variables:

```bash
export EDGE_SSH_USER="${EDGE_SSH_USER:-root}"
export EDGE_IP="${EDGE_IP:-192.168.7.2}"
export TEST_REMOTE_HOST="${TEST_REMOTE_HOST:-${EDGE_SSH_USER}@${EDGE_IP}}"
export TEST_REMOTE_PASSWORD="<guest-root-password-if-needed>"
export TEST_IMAGE="localhost:5000/mica-uniproton-app:xen-0.1"
export NERDCTL_NETWORK_MODE="none"
export IMAGE_PROFILE="auto"
export QEMU_SOURCE_IMAGE_REF="localhost:5000/mica-uniproton-app:xen-0.1"
export QEMU_IMAGE_TAR="<path-to-stamped-output>/exports/local_mica-uniproton-app_xen-arm64-0.1.tar"
```

Use `TEST_REMOTE_HOST=qemu-k3s` only when you want the QEMU helper to start or
reuse the local QEMU guest through usernet SSH forwarding. Do not commit real
host paths, passwords, or lab-only IP addresses into this file.

## QEMU Regression

Run:

```bash
micrun/tests/bin/test-io-qemu
```

This flow now:

1. validates the QEMU guest is reachable
2. rebuilds the current worktree shim
3. deploys the shim to the guest
4. imports the current RTOS image tar
5. runs the adaptive IO suite

The QEMU regression copies files into the running guest and replaces the shim
inside that guest for validation. It must not unpack, patch, or repack the
QEMU rootfs artifact produced by the build.

## Covered User Workflows

The maintained adaptive suite now verifies user-facing `nerdctl` behavior in addition to `ctr`:

- `hello`-style images:
  - `nerdctl run -i --rm`
  - `nerdctl create --name` + `nerdctl start`
  - `nerdctl run -d --name`
  - `nerdctl ps`
  - `nerdctl rm -f`
- `shell`-style images:
  - `nerdctl run -it --rm`
  - `nerdctl run -dt --name`
  - `nerdctl create -i -t --name` + `nerdctl start`
  - `nerdctl stop` + `nerdctl rm`
  - `nerdctl attach`
  - detach back to the remote shell
  - foreground `nerdctl run -it` followed by repeated attach/detach
  - empty input, invalid command input, pasted command bursts, and partial-line
    detach recovery
  - TTY `Ctrl-C` interrupt and post-interrupt cleanup
  - `nerdctl ps`
  - `nerdctl inspect` for the real containerd/Xen ID behind `--name`
  - `nerdctl rm -f`
  - `ctr container create` + `ctr task start -d`
  - `ctr task ls`, `ctr containers info`, `xl list`, and runtime log diagnostics
  - cleanup after timeout, `exit`, `stop`, `kill`, and `rm`

When the current image only exposes a fixed startup banner such as `Hello, UniProton!`, the suite treats it as `hello` profile and only runs the commands that make sense for that runtime behavior.

The K3s interaction test extends this user-facing coverage through Kubernetes:

- `RuntimeClass micrun`
- RTOS Pod with `stdin: true` and `tty: false`
- `kubectl attach -i` command input
- output markers from UniProton shell or hello workloads
- K3s bundled containerd task lookup by Pod container ID
- Xen domain lookup by the same container ID
- Pod deletion cleanup for both task and Xen domain

Run it after a single-node or cloud-edge K3s environment is ready:

```bash
micrun/tests/bin/test-k3s-interaction
```

## Adaptive Profile Detection

The IO suite still distinguishes:

- `shell` images
- `hello` images

But an unknown profile is now treated as a **failing regression**, not a silent skip. This is important because fresh qemu validation has already exposed a real runtime failure in the current shim startup path.

## Notes

- `tests/io/test_helpers.sh` now uses the shared password-aware remote contract.
- Older `REMOTE_HOST`-only usage should be treated as legacy; prefer `TEST_REMOTE_HOST`.
- The cleanup helper avoids `pkill -f containerd-shim-mica-v2` because it can
  match its own remote shell command and abort cleanup before `xl destroy`.
- For `nerdctl --name`, use `nerdctl inspect <name>` before checking `ctr` or
  `xl`; containerd tasks and Xen domains use the inspected ID, not the display
  name shown by `nerdctl ps`.
