---
name: micrun-qemu-build
description: Build MicRun QEMU artifacts with OEBuild and run MicRun tests against them. Use when preparing a qemu-aarch64 image (Xen + mcs/micrun/k3s/containerd), staging outputs, or validating with tests/bin.
---

# MicRun QEMU Build

## Goal

Build and stage the QEMU artifacts MicRun tests need, then run `tests/bin/*`.

- Xen hypervisor, Linux kernel, and an openEuler rootfs with `k3s`, `mcs`,
  `micrun`, `containerd`, and virt features
- Never unpack or repack the rootfs
- Never prompt a human for sudo or guest passwords

## Workspace

Resolve `OEE_ROOT` / `PROJECT` in this order only. Do not search the host
for other oebuild / Yocto trees.

1. User-given `OEE_ROOT` / `PROJECT` wins. Keep that layout.
2. If the user did not name a workspace, use the repo-local default
   (gitignored under the mcs repo `tmp/`):

   ```bash
   MCS_REPO=<path-to-mcs-repo>
   OEE_ROOT=$MCS_REPO/tmp/micrun_build
   PROJECT=work_test
   ```

   `micrun/build/` in the repo is **not** a build directory — it holds
   local session records only. All build state lives under `OEE_ROOT`.

3. If that directory already has a venv, an oebuild clone, or `oebuild init`
   output, stop and ask the user whether to **wipe**, **reuse**, or pick
   another path. When the user confirms a wipe:

   ```bash
   rm -rf "$OEE_ROOT"            # full clean
   # or, to keep the oebuild clone + venv:
   rm -rf "$OEE_ROOT/$PROJECT"   # partial clean
   ```

The venv does not persist across shells. Re-activate it in every new
shell before running `oebuild`:

```bash
. "$OEE_ROOT/.venv/bin/activate"
```

```bash
MCS_REPO=<path-to-mcs-repo>
MICRUN_REPO=$MCS_REPO/micrun
OEE_ROOT=$MCS_REPO/tmp/micrun_build
PROJECT=work_test
PROJECT_DIR=$OEE_ROOT/$PROJECT
BUILD_DIR=$PROJECT_DIR/build/qemu-aarch64
OUTPUT_DIR=$BUILD_DIR/output/test
DEPLOY_DIR=$BUILD_DIR/tmp/deploy/images/qemu-aarch64
OEBUILD_SRC=$OEE_ROOT/oebuild
```

No machine-local paths, passwords, or tokens in committed docs.

## Non-interactive auth

```bash
unset SSH_ASKPASS SUDO_ASKPASS
export SSH_ASKPASS_REQUIRE=never
sudo -n true   # host sudo only if already NOPASSWD; otherwise fail, do not prompt
```

Guest SSH goes through `sshpass` (details in `tests/README.md`). The
standard oEE image accepts any non-empty secret, so the default `micrun`
password works out of the box. **Never** modify the build config to fix
a test auth failure — if a guest enforces `passwd-expire`,
`qemu_wait_for_ssh` automatically falls back to an expect-based password
change (`tests/common/qemu_setup_password.exp`).

## Build

### 1. Install oebuild

```bash
mkdir -p "$OEE_ROOT"
cd "$OEE_ROOT"
git clone https://atomgit.com/openeuler/oebuild.git "$OEBUILD_SRC"

uv venv "$OEE_ROOT/.venv"
. "$OEE_ROOT/.venv/bin/activate"
uv pip install -e "$OEBUILD_SRC"
oebuild --version
```

Docker is required for the container build. Do not pass `--toolchain_dir`.

### 2. Init project and pin mcs

```bash
cd "$OEE_ROOT"
oebuild init "$PROJECT"
cd "$PROJECT_DIR"
oebuild update
```

After `oebuild update`, edit **only** the `mcs` entry in
`$PROJECT_DIR/src/yocto-meta-openeuler/.oebuild/manifest.yaml`
(the `mcs-x86` entry is a different pin — leave it alone):

- `version` ← `git -C "$MCS_REPO" rev-parse HEAD`
- `remote_url` ← an HTTPS URL the build container can fetch (the host
  `origin` is often `git@...`; the container has no SSH key). If HTTPS
  needs credentials, bind-mount `$MCS_REPO` in `compile.yaml`
  `docker_param.volumns` and use a `file://` path.

Verify the pin is anonymously fetchable before building — a failure here
takes one command; the same failure inside `do_fetch` wastes an hour:

```bash
git ls-remote "$remote_url" "$version"
```

Do not change any other manifest entry.

### 3. Generate and build

```bash
cd "$PROJECT_DIR"
oebuild generate -p qemu-aarch64 -d qemu-aarch64 \
  -f mcs/xen -f mcs/micrun -f containers/k3s/k3s-agent -y
```

Verify the generated `compile.yaml` before building. Expect:

- `MCS_FEATURES:append` contains `micrun`, `new_tty_suffix`, `xen`
- `packagegroup-k3s-agent` in `IMAGE_INSTALL`
- `K3S_EXTERNAL_ENDPOINT` is `containerd`

**Kernel selection**: the layer defaults to kernel 5.10. `linux-openeuler.inc`
sets `LINUX_VERSION = 6.6` only when `DISTRO_FEATURES` contains `kernel6`.
The kp920 delivery targets kernel 6, so the test QEMU image must be built
with the same kernel. After `generate`, add

```
DISTRO_FEATURES:append = " kernel6 "
```

to **both** the `local_conf:` block of `compile.yaml` **and**
`conf/local.conf` (compile.yaml is what oebuild re-generates conf from;
local.conf is what bitbake actually reads). A kernel6 switch recompiles
the whole kernel chain from scratch even with a warm sstate.

**Never run `oebuild compile.yaml` or `oebuild <path>/compile.yaml` inside
the build dir** — it treats that as a workspace init request, prompts
interactively (Y/N/C), deletes `conf/`, and can wipe the build dir down to
a bare compile.yaml. The only build entry is `cd "$BUILD_DIR" && oebuild
bitbake openeuler-image`. Also note `oebuild generate -d qemu-aarch64`
resets the whole build dir (tmp/cache/output included); the shared sstate
under `src/sstate-cache` survives, so rebuilds still hit the cache.

```bash
cd "$BUILD_DIR"
oebuild bitbake openeuler-image > /tmp/bitbake.log 2>&1
echo "exit=$?"
```

Always run `oebuild bitbake openeuler-image` with the explicit target —
bare `oebuild bitbake` only opens an interactive shell. Always `cd` into
the directory containing `compile.yaml` first. Redirect the log to a file
and print the exit code explicitly — **never pipe through `tail`**,
which masks oebuild's exit code with its own.

The build target is **`openeuler-image`**, not `openeuler-image-mcs`.
The latter is a smaller MCS-only recipe without k3s/containerd.

### Build duration and known quirks

A from-scratch build runs ~4840 tasks and takes hours. Monitor with
`tail /tmp/bitbake.log` ("Running task N of 4840"); long silent
stretches are normal.

Known quirk: on a brand-new container the first `oebuild bitbake` run
may exit with `pip3 install <pkg> failed with exit code None` although
pip actually succeeded (oebuild misreads the exit code). Rerun once and
the build proceeds.

## Error handling

For **any** failure — including repeated fetch failures — stop and
report to the user with the command, exit code, and log tail, and let
the user decide. Do not set up retry loops or watcher scripts. Do not
modify build infrastructure (oebuild, Yocto layers, Docker), and never
"fix" a failure by editing build artifacts or the rootfs: tests must
exercise exactly what users get.

### 4. Stage artifacts

```bash
mkdir -p "$OUTPUT_DIR"
ROOTFS="$(find "$DEPLOY_DIR" -maxdepth 1 -type f -name 'openeuler-image-*.rootfs.cpio.gz' | sort | tail -n1)"
cp "$DEPLOY_DIR/Image" "$DEPLOY_DIR/xen-qemu-aarch64" "$ROOTFS" "$OUTPUT_DIR/"
cp "$DEPLOY_DIR/xen-qemu-aarch64.efi" "$DEPLOY_DIR/mcs-resources.dtbo" "$OUTPUT_DIR/" 2>/dev/null || true
DTB="$(find "$DEPLOY_DIR" -maxdepth 1 -type f -name 'openeuler-image-*.qemuboot.dtb' | sort | tail -n1)"
cp "$DTB" "$OUTPUT_DIR/"
[ "$(basename "$DTB")" = openeuler-image-qemu-aarch64.qemuboot.dtb ] || \
  ln -sfn "$(basename "$DTB")" "$OUTPUT_DIR/openeuler-image-qemu-aarch64.qemuboot.dtb"
```

Do not rename the rootfs.

## UniProton tar

```bash
STAMPED_OUTPUT="$(ls -td "$BUILD_DIR"/output/20* | head -n1)"
cd "$STAMPED_OUTPUT"
uv venv .venv && . .venv/bin/activate
uv pip install -r micrun-files/requirements.txt
python3 micrun-files/mica-image-builder.py \
  --pedestal xen --os uniproton \
  --firmware micrun-files/uniproton.elf \
  --xen-image micrun-files/uniproton.bin \
  --image-name local/mica-uniproton-app:xen-arm64-0.1 \
  --platform linux/arm64 --export ./exports
```

Expect `exports/local_mica-uniproton-app_xen-arm64-0.1.tar`.

## Tests

The QEMU suites exercise the shim binary **baked into the rootfs**, not the
working tree. After committing fixes to the mcs repo, update the `mcs` pin
in the manifest to the new HEAD, rebuild (`oebuild bitbake openeuler-image`,
incremental — only mcs tasks rerun), restage artifacts, and only then run
the suites. Running suites against an old rootfs silently validates stale
code (a real trap: a "green" regression run that never tested the fix).
If the first bitbake run after a pin bump fails in `do_unpack` with
`cannot stat .../mcs/file.lock`, it raced the source-sync lock release —
just rerun the same command.

Point `tests/bin/*` at the staged artifacts. Usernet-only is enough for
smoke, lifecycle, and IO; tap is for K3s cloud-edge.

```bash
unset SSH_ASKPASS SUDO_ASKPASS
export SSH_ASKPASS_REQUIRE=never
export QEMU_OUTPUT_DIR="$OUTPUT_DIR"
export QEMU_ROOTFS_IMAGE="$(find "$OUTPUT_DIR" -maxdepth 1 -type f -name 'openeuler-image-*.rootfs.cpio.gz' | sort | tail -n1)"
export QEMU_BIN="${QEMU_BIN:-$(command -v qemu-system-aarch64)}"
export QEMU_SMOKE_LOG_FILE=/tmp/micrun-tests/qemu-smoke.log
export QEMU_IMAGE_TAR="$STAMPED_OUTPUT/exports/local_mica-uniproton-app_xen-arm64-0.1.tar"
export TEST_IMAGE=docker.io/local/mica-uniproton-app:xen-arm64-0.1

"$MICRUN_REPO/tests/bin/test-qemu-smoke"
"$MICRUN_REPO/tests/bin/test-qemu-lifecycle"
"$MICRUN_REPO/tests/bin/test-qemu-features"
"$MICRUN_REPO/tests/bin/test-io-qemu"
```

Run the suites in that order: smoke validates the environment,
lifecycle and features cover the delivery spec (features alone maps all
8 acceptance criteria), io needs a guest-local registry setup (see
`tests/io/README.md`) and is skipped when that dependency is absent.
Set `QEMU_KEEP_RUNNING=true` to reuse one boot across suites.

If smoke/lifecycle pass but IO fails, that is a MicRun code problem — use
`micrun/skills/qemu-quickstart-debug/SKILL.md`, do not rebuild the rootfs.

## Related

- `micrun/AGENTS.md`, `micrun/tests/README.md`
- `micrun/skills/qemu-quickstart-debug/SKILL.md`
- `docs/quick-start.md`
