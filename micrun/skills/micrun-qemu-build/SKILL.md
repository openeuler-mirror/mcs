---
name: micrun-qemu-build
description: Build MicRun QEMU artifacts with openEuler Embedded/OEBuild, especially qemu-aarch64 images that include k3s agent, mcs, micrun, containerd and virtualization features. Use when preparing or repairing the full Yocto build, updating the mcs manifest revision, recording build failures, adding build patches/bbappends, or proving repeated stable builds.
---

# MicRun QEMU Build

## Goal

Build the QEMU artifacts MicRun tests need from openEuler Embedded:

- Xen hypervisor image
- Linux kernel
- openEuler rootfs with `k3s`, `mcs`, `micrun`, `containerd`, and virt features

Prefer a reproducible workspace and patch-based fixes. Do not edit generated
`tmp/work` sources as the only fix. If a build fails, record the failure and add
a patch, `.bbappend`, layer change, or manifest change that can be rerun.

## Workspace Variables

Set these variables from the user's workspace before running commands. Keep the
directory relationship the same unless the user gives a different layout:

```bash
MCS_REPO=<path-to-mcs-repo>
MICRUN_REPO=$MCS_REPO/micrun
OEE_ROOT=<path-to-openEuler-Embedded-build-root>
PROJECT=<oebuild-project-name>
PROJECT_DIR=$OEE_ROOT/$PROJECT
BUILD_BASE=$PROJECT_DIR/build/work_test
BUILD_DIR=$BUILD_BASE/$PROJECT
RECORD_DIR=$OEE_ROOT/micrun-build-records
OUTPUT_DIR=$BUILD_DIR/output/test
```

Do not record machine-local absolute paths, passwords, access tokens, or one-off
IP choices in committed docs or skills. Use the variables above, or placeholders
such as `<path-to-mcs-repo>`, `<guest-root-password-if-needed>`, and
`<edge-ip>`. Build notes under `RECORD_DIR` may contain local evidence while
debugging, but sanitize them before committing or sharing.

Current OEBuild validates that `BUILD_DIR` is under the project's managed
`build/` root. Older notes may place it under `$OEE_ROOT/work_test/build`; move
that path under `$PROJECT_DIR/build/` before running `generate` or
`neo-generate`.

```bash
BUILD_BASE=$PROJECT_DIR/build/work_test
BUILD_DIR=$BUILD_BASE/$PROJECT
OUTPUT_DIR=$BUILD_DIR/output/test
```

Keep build records under `RECORD_DIR`, for example:

```text
micrun-build-records/
|-- README.md
|-- attempts/
|   |-- 001-oebuild-update.md
|   `-- 002-bitbake-openeuler-image.md
`-- patches/
```

## Required Manifest Override

After every `oebuild update`, update
`yocto-meta-openeuler/.oebuild/manifest.yaml` before generating/building.

The `mcs` source must point to the current project commit:

- replace the upstream source URL or owner with the user's target `mcs` source
  remote, fork, or local mirror
- set the source revision field to `git -C "$MCS_REPO" rev-parse HEAD`
  (`version` in current `manifest.yaml`; `revision` in older variants)

Validate the exact YAML structure before editing. Keep the edit scoped to the
`mcs` entry. If the YAML format changes, record the observed structure in
`$RECORD_DIR/README.md` and apply the smallest patch.

## Initial Workspace

```bash
cd "$OEE_ROOT"

python3 -m venv .venv
. .venv/bin/activate
python -m pip install -U pip
python -m pip install -U oebuild

cd "$OEE_ROOT"
oebuild init "$PROJECT"
cd "$PROJECT_DIR"
oebuild update
```

Then apply the manifest override described above.

Generate qemu-aarch64 with k3s-agent and MicRun features.

Prefer `neo-generate` when it is available because MicRun is modeled under
`nightly-features` as `mcs/micrun`. Pass each feature with a separate `-f`
because current OEBuild uses an append-style option. For docker builds, do not
pass `--toolchain_dir`; the openEuler container provides the configured
`EXTERNAL_TOOLCHAIN_aarch64`. Only pass `-t/--toolchain_dir` for a host build
with a verified local toolchain.

```bash
cd "$PROJECT_DIR"
oebuild neo-generate \
  -p qemu-aarch64 \
  -d "$BUILD_DIR" \
  -f mcs/xen \
  -f mcs/micrun \
  -f containers/k3s/k3s-agent \
  -y
```

Fallback for older workspaces that do not support `neo-generate`:

```bash
cd "$PROJECT_DIR"
oebuild generate \
  -p qemu-aarch64 \
  -d "$BUILD_DIR" \
  -f mcs \
  -f xen \
  -f containerd \
  -f k3s \
  -y
```

Then append this line to `$BUILD_DIR/conf/local.conf` using a patch:

```bitbake
MCS_FEATURES:append = " micrun new_tty_suffix "
```

Ensure the QEMU rootfs contains the K3s agent and uses the system containerd
endpoint when that is the validated runtime model:

```bitbake
IMAGE_INSTALL:append = " packagegroup-k3s-agent "
K3S_EXTERNAL_ENDPOINT = "containerd"
```

`packagegroup-k3s-agent` is required when the generated feature only stages
the package group itself but not the agent binary. `K3S_EXTERNAL_ENDPOINT`
keeps the image on the rootfs system `containerd` and avoids installing a
second `ctr` path that can conflict with the system package.

Do not validate a QEMU rootfs by copying or installing K3s into the running
guest. If `/usr/bin/k3s` is missing, fix the image configuration and rebuild.

Current K3s agent/server startup validates the Go version metadata embedded at
link time. If the recipe leaves `pkg/version.UpstreamGolang` empty, K3s can
fail before joining the cluster. Add a recipe or layer-level `.bbappend` that
sets the value to the Go version actually used by the K3s build:

```bitbake
K3S_UPSTREAM_GOLANG ?= "<go-version-used-by-the-k3s-recipe>"
GO_BUILD_LDFLAGS += " -X github.com/k3s-io/k3s/pkg/version.UpstreamGolang=${K3S_UPSTREAM_GOLANG}"
```

Keep the value generic in docs and skills. Build records may note the observed
toolchain version, but should not hard-code machine-local paths.

Build:

```bash
cd "$BUILD_DIR"
. "$OEE_ROOT/.venv/bin/activate"
oebuild bitbake openeuler-image
```

## Failure Handling

For each failure:

1. Capture the command, exit code, relevant log paths, and the tail of the
   failing log in `RECORD_DIR/attempts/NNN-<topic>.md`.
2. Prefer a patchable build-system change:
   - `.bbappend` in a layer
   - recipe patch file
   - manifest source/revision change requested by the user
   - deterministic clean command for stale Yocto state
3. Do not rely on manually editing files under `tmp/work` or installing random
   host packages without recording them. If a host prerequisite is missing, log
   it and ask only when it cannot be solved inside the workspace.

Common known fixes:

- `openssl` or `perl` timestamp rebuild problems: prefer `cleansstate` first,
  then add a recipe append only if the issue repeats.
- CNI plugin source layout problems: verify which `v1.2.0.tar.gz` BitBake
  unpacks. In current OEBuild workspaces, the src-openeuler package path is
  `${OPENEULER_SP_DIR}/openeuler/cni`; if a layer uses
  `${OPENEULER_SP_DIR}/cni`, patch `FILESEXTRAPATHS` so the src-openeuler cni
  repo takes precedence over `oee_archive`.
- `acpica-native:do_fetch` failures for
  `https://acpica.org/sites/acpica/files/acpica-unix-<version>.tar.gz` can be
  caused by the old upstream URL redirecting to Intel pages. Verify an HTTPS
  Yocto/OpenEmbedded source mirror with the recipe checksum, then prefer a layer
  `.bbappend` over manually placing a tarball in the downloads cache:

```bitbake
SRC_URI = "https://downloads.yoctoproject.org/mirror/sources/acpica-unix-${PV}.tar.gz"
```

  Clean only the affected native recipe before retrying:

```bash
oebuild bitbake -c cleansstate acpica-native
```

- Large C++ recipes failing with `cc1plus` killed, truncated assembly, or
  incomplete CFI are usually per-recipe memory pressure from high
  `PARALLEL_MAKE`, especially `gdb` and `binutils/gold`. Prefer a recipe-local
  `.bbappend` over changing host memory or global bitbake concurrency:

```bitbake
PARALLEL_MAKE = "-j 4"
```

  If the fix must live in `local.conf` instead, scope it to the affected
  recipes:

```bitbake
PARALLEL_MAKE:pn-gdb = "-j 4"
PARALLEL_MAKE:pn-binutils = "-j 4"
```

  Clean only the affected recipes before retrying:

```bash
oebuild bitbake -c cleansstate gdb binutils
```

- `file://<repo>` unpack failures that mention a transient
  `<repo>/file.lock` are usually caused by the openEuler source-fetch lock being
  created inside the same repository BitBake recursively copies. Patch
  `meta-openeuler/classes/openeuler.bbclass` so `do_openeuler_fetch` places lock
  files outside source repositories, for example under
  `${OPENEULER_SP_DIR}/.locks/<repo>.lock`; also remove stale legacy
  `<repo>/file.lock` before initializing the source repository. If the next
  unpack failure mentions `.git/index.lock`, also give `do_unpack` the same
  per-repo lock with `do_unpack[lockfiles]` for `file://<repo>` sources, because
  sibling recipes can fetch the same repository while another recipe unpacks it.
  Clean the affected recipes before retrying.
- `mcsctl` or `mcs-linux` fetch/cache mismatch: clean both packages together:

```bash
oebuild bitbake -c cleansstate mcsctl mcs-linux
```

## Stability Rule

A build is stable only after three consecutive patch-build cycles succeed.

For each cycle:

1. Ensure the manifest still points to the target `mcs` commit.
2. Remove previous deploy artifacts, not the whole source cache:

```bash
rm -rf "$BUILD_DIR/tmp/deploy/images/qemu-aarch64"
```

3. Restore non-image deploy outputs that image tasks consume. A plain rebuild
   can be stamp-satisfied after the deploy directory is deleted, and forcing
   only `rootfs` can fail because `${DEPLOY_DIR_IMAGE}/Image` or Xen artifacts
   are missing.

```bash
cd "$BUILD_DIR"
. "$OEE_ROOT/.venv/bin/activate"
oebuild bitbake -f -c deploy virtual/kernel xen xen-tools mcs-km micrun mcsctl
```

4. Force the image stage so deleted deploy outputs are regenerated:

```bash
cd "$BUILD_DIR"
. "$OEE_ROOT/.venv/bin/activate"
oebuild bitbake -C rootfs openeuler-image
```

5. Record success in `RECORD_DIR/attempts/NNN-stability-cycle-<n>.md`.

After the third consecutive success, run one final build if artifacts were
removed for the last cycle, then leave the final QEMU artifacts in place.

## Expected Artifacts

Artifacts are normally under:

```text
$BUILD_DIR/tmp/deploy/images/qemu-aarch64/
|-- Image
|-- xen-qemu-aarch64
|-- xen-qemu-aarch64.efi
|-- openeuler-image-qemu-aarch64-<timestamp>.rootfs.cpio.gz
|-- openeuler-image-qemu-aarch64.qemuboot.dtb
`-- mcs-resources.dtbo
```

Copy QEMU test artifacts only after a successful build. Preserve the rootfs
artifact name from the build output; do not rename it to a generic local alias.

```bash
mkdir -p "$OUTPUT_DIR"
ROOTFS="$(ls -t "$BUILD_DIR/tmp/deploy/images/qemu-aarch64"/openeuler-image-qemu-aarch64-*.rootfs.cpio.gz | head -n1)"
cp "$BUILD_DIR/tmp/deploy/images/qemu-aarch64/Image" "$OUTPUT_DIR/"
cp "$BUILD_DIR/tmp/deploy/images/qemu-aarch64/xen-qemu-aarch64" "$OUTPUT_DIR/"
cp "$BUILD_DIR/tmp/deploy/images/qemu-aarch64/xen-qemu-aarch64.efi" "$OUTPUT_DIR/"
cp "$ROOTFS" "$OUTPUT_DIR/"
cp "$BUILD_DIR/tmp/deploy/images/qemu-aarch64/openeuler-image-qemu-aarch64.qemuboot.dtb" "$OUTPUT_DIR/"
cp "$BUILD_DIR/tmp/deploy/images/qemu-aarch64/mcs-resources.dtbo" "$OUTPUT_DIR/"
```

## QEMU Smoke Command

```bash
cd "$OUTPUT_DIR"
ROOTFS="$(ls -t openeuler-image-qemu-aarch64-*.rootfs.cpio.gz | head -n1)"
sudo qemu-system-aarch64 \
  -device virtio-net-pci,netdev=net0 \
  -netdev tap,id=net0,ifname=tap0,script=/etc/qemu-ifup \
  -device virtio-net-pci,netdev=net1 \
  -netdev user,id=net1,hostfwd=tcp::${SSH_FORWARD_PORT:-10023}-:22 \
  -initrd "$ROOTFS" \
  -device loader,file=Image,addr=0x45000000 \
  -machine virt,gic-version=3 \
  -machine virtualization=true \
  -cpu cortex-a53 -smp 4 -m 4096 \
  -serial mon:stdio -nographic \
  -kernel xen-qemu-aarch64 \
  -append 'root=/dev/ram0 rw debugshell mem=1536M console=ttyAMA0,115200' \
  -dtb openeuler-image-qemu-aarch64.qemuboot.dtb
```

This Xen boot path is required for MicRun lifecycle validation. A direct Linux
boot with `-kernel Image -initrd "$ROOTFS"` is useful only for checking
that the rootfs, containerd and `containerd-shim-mica-v2` exist; MicRun will
detect the pedestal as `unsupported` without `/proc/xen/xenbus`.

The tap NIC is the standard local test network. The usernet NIC is only an
optional SSH convenience for automation and does not replace tap-based K3s
or fixed edge-address validation. The repository examples use
`192.168.7.0/24`; use `TEST_REMOTE_HOST`, `K3S_EDGE_NODE_IP`, and
`K3S_CLOUD_SERVER_IP` when a different lab network is used.

For K3s validation on the oEE/QEMU guest, keep the kubelet compatibility
arguments in the test scripts unless the target environment is known to use a
full cgroup v2 hierarchy:

```bash
--kubelet-arg=cgroups-per-qos=false \
  --kubelet-arg=enforce-node-allocatable= \
  --kubelet-arg=fail-cgroupv1=false
```

The last argument is only for kubelet versions that reject cgroup v1 by
default. If testing an older kubelet that rejects `fail-cgroupv1`, override
`K3S_KUBELET_ARGS` for that run instead of editing the rootfs artifact. The
verified oEE K3s v1.27 cloud-edge path uses only `cgroups-per-qos=false` and an
empty `enforce-node-allocatable`.

For cloud-edge validation with system containerd, use the rootfs K3s binary and
the rootfs system containerd. Do not copy a K3s binary into the guest:

```bash
export K3S_BIN=/usr/bin/k3s
export K3S_EDGE_CONTAINERD_MODE=external
export K3S_CONTAINERD_ADDRESS=/run/containerd/containerd.sock
export K3S_EDGE_CTR_BIN=ctr
export K3S_EDGE_CTR_SUBCOMMAND=
export K3S_CLOUD_SERVER_IMAGE=<k3s-server-image-matching-edge-version>
export K3S_CLOUD_KUBECTL_BIN=k3s
export K3S_CLOUD_KUBECTL_SUBCOMMAND=kubectl
export K3S_KUBELET_ARGS="--kubelet-arg=cgroups-per-qos=false --kubelet-arg=enforce-node-allocatable="
micrun/tests/bin/test-k3s-cloud-edge
micrun/tests/bin/test-k3s-interaction
```

`test-k3s-cloud-edge` deletes its test Pod by default and verifies that the
edge containerd task and Xen domain are gone. Set `K3S_E2E_KEEP_POD=true` only
for a focused debug run where preserving the live Pod is more useful than a
clean test environment.

Single-node K3s validation is useful when the edge rootfs can run `k3s server`,
but it is memory-sensitive. The single-node entry cleans old K3s processes,
containerd tasks, and Xen domains before checking available Dom0 memory. If it
skips because `MemAvailable` is below `K3S_SINGLE_NODE_MIN_AVAILABLE_MB`, keep
the cloud-edge external-containerd path as the primary validation instead of
installing or copying K3s into the guest.

SSH, when enabled by the image and root login is configured:

```bash
ssh -p "${SSH_FORWARD_PORT:-10023}" root@localhost
```

Before running MicRun lifecycle or IO validation, check that Dom0 Xen services
really finished initialization:

```bash
systemctl is-active containerd
systemctl is-active micad
systemctl is-active xenconsoled
systemctl is-active xen-init-dom0
xl list
xl cpupool-list
```

If `xen-init-dom0` failed because `/var/lib/xen` is missing, or
`xenconsoled` failed while creating a log directory under `/var/log`, repair the
runtime directories inside the running guest and restart only those services.
Do not unpack, patch, or repack the built QEMU rootfs artifact for a
standardized test:

```bash
mkdir -p /var/lib/xen /var/volatile/log /var/log/xen /run/xen
systemctl reset-failed xen-init-dom0.service xenconsoled.service
systemctl restart xenconsoled.service
systemctl restart xen-init-dom0.service
```

Without this check, `micad` can fail every `xl create` with `err -22`, and the
MicRun IO suite may report broad shell failures even though the image and shim
path are otherwise usable.

## UniProton Tarball and Runtime Test

Use the timestamped output directory that contains `micrun-files`, not the
staged `output/test` directory:

```bash
STAMPED_OUTPUT="$BUILD_DIR/output/<timestamp>"
cd "$STAMPED_OUTPUT"
python3 micrun-files/mica-image-builder.py \
  --pedestal xen \
  --os uniproton \
  --firmware micrun-files/uniproton.elf \
  --xen-image micrun-files/uniproton.bin \
  --image-name local/mica-uniproton-app:xen-arm64-0.1 \
  --platform linux/arm64 \
  --export ./exports
```

Expected tarball:

```text
$STAMPED_OUTPUT/exports/local_mica-uniproton-app_xen-arm64-0.1.tar
```

If Docker buildx is unavailable, the builder should still produce a usable
single-platform archive by exporting the legacy Docker image and normalizing the
OCI archive platform metadata to `linux/arm64`. Local build/export must not
pull or start a local registry unless `--push` is requested.

For QEMU validation, add a 9p share for the `exports` directory to the Xen QEMU
command:

```bash
-virtfs local,path="$STAMPED_OUTPUT/exports",mount_tag=hostshare,security_model=none
```

Then, in Dom0:

```bash
mkdir -p /mnt/hostshare
mount -t 9p -o trans=virtio,version=9p2000.L hostshare /mnt/hostshare
ctr image import /mnt/hostshare/local_mica-uniproton-app_xen-arm64-0.1.tar
ctr run --runtime io.containerd.mica.v2 --detach \
  docker.io/local/mica-uniproton-app:xen-arm64-0.1 micrun-xen-test
ctr tasks ls
xl list
```

Successful validation should show:

- `ctr run` exits `0`
- `ctr tasks ls` shows `micrun-xen-test` as `RUNNING`
- `xl list` shows a non-Dom0 domain for `micrun-xen-test`
- journal logs include MicRun shim startup, micad Xen config generation and
  `startClient: Start OK`

### UniProton Shell Interaction

For an interactive UniProton shell, prefer foreground TTY mode:

```bash
ctr run --runtime io.containerd.mica.v2 -t --rm \
  docker.io/local/mica-uniproton-app:xen-arm64-0.1 ctr-shell
```

or:

```bash
nerdctl run -it --rm --network=none \
  --runtime io.containerd.mica.v2 \
  --name nerd-shell \
  docker.io/local/mica-uniproton-app:xen-arm64-0.1
```

Expected shell markers:

```text
openEuler UniProton #
support shell commond:
UniProton 24.03-LTS
```

Useful shell commands are `help`, `uname`, `systeminfo`, `taskInfo`, and
`memInfo`. In a TTY session, `Ctrl-C` should interrupt and stop the current
container task. `exit` should also close the UniProton shell and the container
task as a shell-compatible fallback.

For a long-lived `nerdctl` shell that can be detached and reattached:

```bash
nerdctl run -dt --runtime io.containerd.mica.v2 --network=none \
  -l org.openeuler.micrun.container.auto_close=false \
  --name nerd-attach \
  docker.io/local/mica-uniproton-app:xen-arm64-0.1
nerdctl attach nerd-attach
```

Use Docker-style detach keys `Ctrl-P Ctrl-Q`. After detach, confirm the task is
still up with `nerdctl ps`, then reattach if needed. `Ctrl-C` in the attached
TTY should stop the container; non-TTY `0x03` bytes should remain ordinary input.

For shell workloads, prefer `run -it`, `run -dt`, or
`create -i -t`/`start`. Plain `nerdctl run -d` does not allocate an interactive
TTY and is not the long-lived shell path.

`nerdctl --name` is a display/user handle. The containerd task ID and Xen
domain name are the ID reported by `nerdctl inspect`:

```bash
cid=nerd-attach
nid=$(nerdctl inspect "$cid" | sed -n 's/.*"Id": "\([^"]*\)".*/\1/p' | head -1)
nerdctl ps | grep "$cid"
ctr task ls | awk -v id="$nid" '$1 == id { print }'
xl list | awk -v id="$nid" '$1 == id { print }'
```

For lifecycle validation, `nerdctl stop <name>` should remove the Xen domain and
transition the containerd task out of `RUNNING`. A stopped task can remain listed
until `nerdctl rm <name>` removes the task/container records.

For user diagnostics, do not rely on `nerdctl logs` for a TTY UniProton shell;
it can legitimately be empty. Prefer:

```bash
nerdctl inspect "$cid"
ctr containers info "$nid"
ctr task ls
xl list
tail -200 /var/log/mica/mica-runtime.log
```

Non-interactive `nerdctl attach` may require a real TTY. For regression tests,
use Expect or `ssh -tt`, send an initial Enter to nudge the prompt, and detach
with `Ctrl-P Ctrl-Q`.

For UX regression coverage, run the maintained matrix test after importing the
image and installing the current shim:

```bash
TEST_REMOTE_HOST=root@127.0.0.1 \
TEST_REMOTE_PORT=${SSH_FORWARD_PORT:-10023} \
TEST_REMOTE_PASSWORD='<guest-root-password-if-needed>' \
TEST_IMAGE=docker.io/local/mica-uniproton-app:xen-arm64-0.1 \
NERDCTL_NETWORK_MODE=none \
IMAGE_PROFILE=shell \
micrun/tests/io/run_all_io_tests.sh
```

The shell-family suite includes `test_nerdctl_tty_ux_matrix.exp`, which verifies
foreground `nerdctl run -it`, repeated `nerdctl attach`/`Ctrl-P Ctrl-Q` detach,
empty input, invalid input, pasted command bursts, partial-line detach recovery,
TTY `Ctrl-C` interrupt, and post-interrupt task/Xen cleanup. The full shell
profile also checks `exit`, `nerdctl create -i -t` + start/stop/rm, `ctr`
lifecycle status, inspected ID mapping, runtime log diagnostics, and timeout
cleanup; expect 13 tests for the current shell image profile.

Current MicRun shim paces stdin writes to the RPMSG TTY, including a short
delay after line endings, so small command bursts such as `help\nuname\n`
should work through QEMU serial, `ctr -t`, `nerdctl -it`, `nerdctl attach`, and
`kubectl attach -i`. Keep long-lived shell tests from sending `exit` unless the
test is explicitly checking shutdown or deletion cleanup. If a downstream image
carries an older shim, the symptom is missing/reordered letters or double echo
during rapid paste.

Detached non-TTY `ctr` tasks can validate lifecycle and stdin delivery:

```bash
ctr container create --runtime io.containerd.mica.v2 \
  docker.io/local/mica-uniproton-app:xen-arm64-0.1 ctr-bg
ctr task start -d ctr-bg
printf '\nhelp\nuname\n' | timeout 20 ctr task attach ctr-bg
```

The non-TTY attach path should now keep stdout open after piped stdin closes, so
the command above should replay the UniProton prompt, `help` output, and
`UniProton 24.03-LTS`. The default non-TTY auto-close timeout can still stop the
task later unless disabled with annotations.

Clean up after the test:

```bash
ctr task kill -s 9 micrun-xen-test || true
ctr task delete micrun-xen-test || true
ctr container delete micrun-xen-test || true
xl list
```

For scripted cleanup, avoid `pkill -f containerd-shim-mica-v2` inside a remote
shell command because it can match and kill the cleanup shell itself before
`xl destroy` runs. Use a bracketed pgrep pattern instead:

```bash
for pid in $(pgrep -f '[c]ontainerd-shim-mica-v2' 2>/dev/null || true); do
  kill -9 "$pid" 2>/dev/null || true
done
```

For repeated QEMU or K3s runs, also clear stale micad clients. Stop old K3s
agent/server processes first, then remove MicRun runtime state by full
container IDs from `/run/micrun/containers` and `/run/micrun/runtime/container`.
If `mica status` still shows non-`qemu-*` clients after that, restart `micad`
before starting the next validation. The repository test scripts perform this
cleanup automatically; use the same ordering for manual recovery so kubelet
does not recreate an old Pod while runtime state is being removed.

## Build Record Template

```markdown
# Attempt NNN: <topic>

- Time:
- Workspace:
- Command:
- Result:
- mcs revision:
- Manifest:
- Failure summary:
- Log paths:
- Patch/fix applied:
- Follow-up:
```
